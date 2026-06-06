(function () {
  if (window.__webgenCanvasBridgeLoaded) return;
  window.__webgenCanvasBridgeLoaded = true;

  const EXT_SOURCE = 'webgen-bridge-extension';
  const PAGE_SOURCE = 'infinite-canvas-webgen-bridge';
  let requestSeq = 0;

  window.addEventListener('message', event => {
    const data = event.data || {};
    if (data.source !== 'infinite-canvas-page' || data.type !== 'send-prompt-to-extension') return;
    const prompt = data.prompt || {};
    const text = String(prompt.text || '').trim();
    if (!text) return;
    const replyTarget = event.source && typeof event.source.postMessage === 'function' ? event.source : window;
    chrome.runtime.sendMessage({
      type: 'add-prompt',
      prompt: {
        id: prompt.id ? `canvas_${prompt.id}` : undefined,
        text,
        title: prompt.name || firstLine(text),
        source: 'canvas',
        siteId: 'infinite-canvas',
        conversationUrl: location.href,
        createdAt: new Date().toISOString()
      }
    }).then(response => {
      replyTarget.postMessage({
        source: 'webgen-bridge-content',
        type: 'prompt-sent-result',
        ok: !!response?.ok,
        error: response?.error || ''
      }, '*');
    }).catch(error => {
      replyTarget.postMessage({
        source: 'webgen-bridge-content',
        type: 'prompt-sent-result',
        ok: false,
        error: error?.message || String(error)
      }, '*');
    });
  });

  chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
    if (message?.type === 'webgen-probe' && !isCanvasPage()) return false;
    if (!String(message?.type || '').startsWith('canvas-') && message?.type !== 'webgen-probe') return false;
    handleMessage(message || {}).then(sendResponse).catch(error => {
      sendResponse({ ok: false, error: error?.message || String(error) });
    });
    return true;
  });

  async function handleMessage(message) {
    if (message.type === 'webgen-probe') {
      return {
        kind: isCanvasPage() ? 'canvas' : 'generator',
        siteId: isCanvasPage() ? 'infinite-canvas' : '',
        title: document.title,
        url: location.href
      };
    }
    if (message.type === 'canvas-get-selected-prompt') {
      if (!isCanvasPage()) return { ok: false, error: 'Not an Infinite Canvas page' };
      const prompt = await canvasPageRequest('get-selected-prompt', {});
      return { ok: true, prompt: prompt.prompt || null };
    }
    if (message.type === 'canvas-create-image-node') {
      if (!isCanvasPage()) return { ok: false, error: 'Not an Infinite Canvas page' };
      const file = await uploadCapturedImage(message.image || {});
      const nodeResult = await canvasPageRequest('create-image-node', {
        url: file.url,
        filename: file.filename,
        name: file.name || file.filename,
        prompt: message.image?.prompt || '',
        promptId: message.image?.promptId || '',
        sourceSite: message.image?.siteId || '',
        sourceUrl: message.image?.conversationUrl || '',
        sourceTabTitle: message.image?.sourceTabTitle || '',
        capturedAt: message.image?.createdAt || ''
      });
      return { ok: true, file, node: nodeResult.node || null };
    }
    return { ok: false, error: `Unknown canvas message: ${message.type || ''}` };
  }

  function isCanvasPage() {
    if (!isLocalInfiniteCanvasOrigin()) return false;
    return !!document.querySelector('#board, #world, #nodes, #frame-canvas')
      || document.title === 'AI Studio'
      || /\/static\/canvas\.html/i.test(location.pathname);
  }

  function isLocalInfiniteCanvasOrigin() {
    return /^https?:\/\/(127\.0\.0\.1|localhost)(:\d+)?\//i.test(location.href);
  }

  function isCanvasFramePage() {
    return isLocalInfiniteCanvasOrigin()
      && (!!document.querySelector('#board, #world, #nodes') || /\/static\/canvas\.html/i.test(location.pathname));
  }

  function canvasPageRequest(type, payload) {
    if (isCanvasFramePage()) return pageRequest(type, payload, window);
    const frame = document.querySelector('#frame-canvas');
    if (!frame?.contentWindow) throw new Error('Canvas iframe is not available');
    return pageRequest(type, payload, frame.contentWindow);
  }

  function pageRequest(type, payload, targetWindow = window) {
    const requestId = `webgen_${Date.now()}_${++requestSeq}`;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        window.removeEventListener('message', onMessage);
        reject(new Error('Infinite Canvas bridge timed out'));
      }, 10000);
      function onMessage(event) {
        if (event.source !== window) return;
        const data = event.data || {};
        if (data.source !== PAGE_SOURCE || data.requestId !== requestId) return;
        clearTimeout(timer);
        window.removeEventListener('message', onMessage);
        if (data.type === 'error') reject(new Error(data.error || 'Infinite Canvas bridge failed'));
        else resolve(data);
      }
      window.addEventListener('message', onMessage);
      targetWindow.postMessage({ source: EXT_SOURCE, type, requestId, payload }, '*');
    });
  }

  async function uploadCapturedImage(image) {
    const blob = dataUrlToBlob(image.dataUrl || '');
    if (!blob) throw new Error('Captured image data is missing');
    const form = new FormData();
    const filename = image.fileName || `webgen-image-${Date.now()}.png`;
    form.append('file', blob, filename);
    form.append('prompt', image.prompt || image.promptText || '');
    form.append('prompt_id', image.promptId || image.prompt_id || '');
    form.append('source_url', image.conversationUrl || '');
    form.append('source_site', image.siteId || '');
    form.append('source_tab_title', image.sourceTabTitle || '');
    form.append('captured_at', image.createdAt || '');
    form.append('width', String(image.width || ''));
    form.append('height', String(image.height || ''));
    form.append('byte_size', String(image.byteSize || blob.size || ''));
    const response = await fetch('/api/extension/webgen-import', { method: 'POST', body: form });
    if (!response.ok) {
      let detail = '';
      try {
        const data = await response.json();
        detail = data.detail || JSON.stringify(data);
      } catch (_) {
        detail = await response.text();
      }
      throw new Error(detail || `Import failed: ${response.status}`);
    }
    return response.json();
  }

  function dataUrlToBlob(dataUrl) {
    if (!/^data:image\//i.test(dataUrl) || !dataUrl.includes(',')) return null;
    const [header, encoded] = dataUrl.split(',', 2);
    const mime = (header.match(/^data:([^;]+)/i) || [])[1] || 'image/png';
    const raw = atob(encoded);
    const bytes = new Uint8Array(raw.length);
    for (let i = 0; i < raw.length; i += 1) bytes[i] = raw.charCodeAt(i);
    return new Blob([bytes], { type: mime });
  }

  function firstLine(text) {
    const line = String(text || '').trim().split(/\r?\n/).find(Boolean) || 'Prompt';
    return line.length > 60 ? `${line.slice(0, 57)}...` : line;
  }
})();
