(function () {
  if (window.__webgenGeneratorBridgeLoaded) return;
  window.__webgenGeneratorBridgeLoaded = true;

  const MIN_CAPTURE_BYTES = 1.5 * 1024 * 1024;
  const MIN_DIMENSION = 512;
  let lastFocusedElement = null;

  document.addEventListener('focus', event => {
    lastFocusedElement = event.target;
  }, true);

  chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
    if (isCanvasPage()) return false;
    if (!String(message?.type || '').startsWith('webgen-')) return false;
    handleMessage(message || {}).then(sendResponse).catch(error => {
      sendResponse({ ok: false, error: error?.message || String(error), images: [] });
    });
    return true;
  });

  async function handleMessage(message) {
    const adapter = activeAdapter();
    if (message.type === 'webgen-probe') {
      if (isCanvasPage()) return { kind: 'canvas', siteId: 'infinite-canvas', title: document.title, url: location.href };
      return { kind: adapter ? 'generator' : 'unsupported', siteId: adapter?.id || '', title: document.title, url: location.href };
    }
    if (message.type === 'webgen-fill-input') {
      if (!adapter) return { ok: false, error: 'No generator adapter matched this page' };
      insertText(adapter, message.promptText || '');
      return { ok: true };
    }
    if (message.type === 'webgen-capture') {
      if (!adapter) return { ok: false, siteId: '', images: [], error: 'No generator adapter matched this page' };
      const images = await captureImages(adapter, message.promptText || '', message.knownFingerprints || []);
      return { ok: true, siteId: adapter.id, images };
    }
    return { ok: false, error: `Unknown generator message: ${message.type || ''}`, images: [] };
  }

  function isCanvasPage() {
    return /^https?:\/\/(127\.0\.0\.1|localhost)(:\d+)?\//i.test(location.href);
  }

  function activeAdapter() {
    const adapters = window.WebgenAdapters || [];
    const registered = adapters.find(adapter => {
      try { return adapter.matches(); } catch (_) { return false; }
    });
    return registered || builtinAdapter() || null;
  }

  function builtinAdapter() {
    if (!/^https:\/\/(chatgpt\.com|chat\.openai\.com)\//i.test(location.href)) return null;
    return {
      id: 'chatgpt',
      label: 'ChatGPT',
      findInput() {
        return document.querySelector('#prompt-textarea')
          || document.querySelector('[data-testid="prompt-textarea"]')
          || document.querySelector('textarea[data-id="root"]')
          || document.querySelector('div[contenteditable="true"]');
      },
      promptContainers() {
        return [
          ...document.querySelectorAll('[data-message-author-role="user"], article, [role="article"]')
        ];
      },
      searchRoot() {
        return document.querySelector('main') || document.body;
      }
    };
  }

  function insertText(adapter, text) {
    if (!text) return;
    const target = adapter.findInput?.() || lastFocusedElement || document.activeElement;
    if (!target || target === document.body) {
      navigator.clipboard?.writeText(text).catch(() => {});
      return;
    }
    target.focus();
    target.click?.();
    try {
      const ok = document.execCommand('insertText', false, text);
      if (ok) return;
    } catch (_) {}
    if (target.isContentEditable || target.getAttribute?.('contenteditable') === 'true') {
      target.textContent = text;
      target.dispatchEvent(new InputEvent('input', { bubbles: true, cancelable: true, inputType: 'insertText', data: text }));
    } else if ('value' in target) {
      const proto = target.tagName === 'TEXTAREA' ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
      const setter = Object.getOwnPropertyDescriptor(proto, 'value')?.set;
      if (setter) setter.call(target, text);
      else target.value = text;
      target.dispatchEvent(new InputEvent('input', { bubbles: true, cancelable: true, inputType: 'insertText', data: text }));
      target.dispatchEvent(new Event('change', { bubbles: true }));
    } else {
      navigator.clipboard?.writeText(text).catch(() => {});
    }
  }

  async function captureImages(adapter, promptText, knownFingerprints) {
    const anchor = findPromptAnchor(adapter, promptText);
    const candidates = collectImageCandidates(adapter, anchor);
    const scored = candidates
      .map(candidate => ({ ...candidate, score: scoreCandidate(candidate, anchor) }))
      .sort((a, b) => b.score - a.score);
    const result = [];
    const seen = new Set(knownFingerprints || []);
    for (const candidate of scored) {
      if (result.length >= 8) break;
      const captured = await captureCandidate(candidate, promptText, adapter.id);
      if (!captured) continue;
      if (seen.has(captured.fingerprint)) continue;
      seen.add(captured.fingerprint);
      result.push(captured);
    }
    return result;
  }

  function findPromptAnchor(adapter, promptText) {
    const normalizedPrompt = normalize(promptText);
    const candidates = (adapter.promptContainers?.() || [])
      .filter(isVisible)
      .map((el, index) => ({ el, index, rect: el.getBoundingClientRect(), text: visibleText(el) }))
      .filter(item => item.text.length >= 12);
    if (!normalizedPrompt) return candidates[candidates.length - 1] || null;
    let best = null;
    for (let i = candidates.length - 1; i >= 0; i -= 1) {
      const item = candidates[i];
      const score = fuzzyScore(normalizedPrompt, normalize(item.text));
      if (!best || score > best.score || (score === best.score && item.index > best.index)) {
        best = { ...item, score };
      }
      if (score >= 0.92) break;
    }
    return best && best.score >= 0.28 ? best : (candidates[candidates.length - 1] || null);
  }

  function collectImageCandidates(adapter, anchor) {
    const root = adapter.searchRoot?.() || document.body;
    const imgCandidates = [...root.querySelectorAll('img')].filter(img => {
      if (!isVisible(img)) return false;
      const w = img.naturalWidth || img.width || 0;
      const h = img.naturalHeight || img.height || 0;
      if (Math.max(w, h) < MIN_DIMENSION) return false;
      const src = img.currentSrc || img.src || '';
      if (!src || /^data:image\/svg/i.test(src) || /\.svg(\?|$)/i.test(src)) return false;
      return true;
    }).map((img, index) => ({ img, el: img, index, sourceUrl: img.currentSrc || img.src || '' }));
    const bgCandidates = [...root.querySelectorAll('div, button, a, span')].map((el, index) => {
      if (!isVisible(el)) return null;
      const rect = el.getBoundingClientRect();
      if (Math.max(rect.width, rect.height) < MIN_DIMENSION) return null;
      const bg = getComputedStyle(el).backgroundImage || '';
      const match = bg.match(/url\(["']?([^"')]+)["']?\)/i);
      if (!match || !match[1] || /\.svg(\?|$)/i.test(match[1])) return null;
      return { el, index: imgCandidates.length + index, sourceUrl: new URL(match[1], location.href).href, rect };
    }).filter(Boolean);
    const linkCandidates = [...root.querySelectorAll('a[href]')].map((el, index) => {
      if (!isVisible(el)) return null;
      const href = el.href || '';
      if (!/^https?:\/\//i.test(href) || !/\.(png|jpe?g|webp)(\?|$)/i.test(href)) return null;
      const rect = el.getBoundingClientRect();
      return { el, index: imgCandidates.length + bgCandidates.length + index, sourceUrl: href, rect };
    }).filter(Boolean);
    const all = [...imgCandidates, ...bgCandidates, ...linkCandidates];
    if (!anchor) {
      return all.map((item, index) => ({
        ...item,
        index,
        rect: item.rect || item.el.getBoundingClientRect(),
        afterAnchor: true
      }));
    }
    const anchorRect = anchor.rect;
    const mapped = all
      .map((item, index) => ({ ...item, index, rect: item.rect || item.el.getBoundingClientRect() }))
      .map(item => ({ ...item, afterAnchor: item.rect.top >= anchorRect.bottom - 20 }));
    const anchored = mapped.filter(item => item.rect.top >= anchorRect.top - 8);
    return anchored.length ? anchored : mapped.map(item => ({ ...item, afterAnchor: false, anchorFallback: true }));
  }

  function scoreCandidate(candidate, anchor) {
    const img = candidate.img || candidate.el;
    const w = img.naturalWidth || img.width || 0;
    const h = img.naturalHeight || img.height || 0;
    const rect = candidate.rect || img.getBoundingClientRect?.() || { width: 0, height: 0 };
    let score = 0;
    score += Math.min(30, Math.max(w, h, rect.width, rect.height) / 80);
    score += Math.min(20, Math.max(w * h, rect.width * rect.height) / 120000);
    if (candidate.afterAnchor) score += 30;
    if (anchor && candidate.rect?.top >= anchor.rect.bottom - 20) score += 15;
    score += candidate.index;
    return score;
  }

  async function captureCandidate(candidate, promptText, siteId) {
    const img = candidate.img || candidate.el;
    const sourceUrl = candidate.sourceUrl || img.currentSrc || img.src || '';
    const width = img.naturalWidth || img.width || 0;
    const height = img.naturalHeight || img.height || 0;
    let dataUrl = '';
    let byteSize = 0;
    let mimeType = 'image/png';
    try {
      if (sourceUrl.startsWith('blob:') || sourceUrl.startsWith('data:image/')) {
        const response = await fetch(sourceUrl);
        if (!response.ok) throw new Error(`${response.status} ${response.statusText}`);
        const blob = await response.blob();
        byteSize = blob.size;
        mimeType = blob.type || mimeType;
        dataUrl = await blobToDataUrl(blob);
      } else if (isSameOriginHttpUrl(sourceUrl)) {
        const response = await fetch(sourceUrl, {
          credentials: 'include',
          cache: 'no-store'
        });
        if (!response.ok) throw new Error(`${response.status} ${response.statusText}`);
        const blob = await response.blob();
        if (!blob || !blob.size) throw new Error('Empty image response');
        byteSize = blob.size;
        mimeType = normalizeImageMime(blob.type, mimeType);
        dataUrl = await blobToDataUrl(blob);
      }
    } catch (error) {
      dataUrl = sourceUrl.startsWith('data:image/') ? sourceUrl : '';
      byteSize = estimateDataUrlBytes(dataUrl);
      mimeType = dataUrl.match(/^data:([^;]+)/)?.[1] || mimeType;
      candidate.captureError = error?.message || String(error);
    }
    if (!dataUrl && !/^https?:\/\//i.test(sourceUrl)) return null;
    if (byteSize && byteSize < MIN_CAPTURE_BYTES && Math.max(width, height) < 1024) return null;
    const ext = mimeType.includes('webp') ? 'webp' : mimeType.includes('jpeg') || mimeType.includes('jpg') ? 'jpg' : 'png';
    return {
      dataUrl,
      sourceUrl,
      fingerprint: [sourceUrl, width, height, byteSize].join('|'),
      fileName: `webgen-image-${Date.now()}.${ext}`,
      mimeType,
      byteSize,
      width,
      height,
      source: 'generator-dom',
      siteId,
      conversationUrl: location.href,
      promptText,
      captureError: candidate.captureError || '',
      confidence: byteSize >= MIN_CAPTURE_BYTES ? 'high' : 'review'
    };
  }

  function isSameOriginHttpUrl(sourceUrl) {
    if (!/^https?:\/\//i.test(sourceUrl)) return false;
    try {
      return new URL(sourceUrl, location.href).origin === location.origin;
    } catch (_) {
      return false;
    }
  }

  function normalizeImageMime(type, fallback) {
    const mime = String(type || '').toLowerCase();
    if (mime.startsWith('image/')) return mime;
    return fallback || 'image/png';
  }

  function blobToDataUrl(blob) {
    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve(reader.result);
      reader.onerror = () => reject(reader.error);
      reader.readAsDataURL(blob);
    });
  }

  function estimateDataUrlBytes(dataUrl) {
    if (!dataUrl || !dataUrl.includes(',')) return 0;
    return Math.floor(dataUrl.split(',', 2)[1].length * 0.75);
  }

  function visibleText(el) {
    return (el.innerText || el.textContent || '').replace(/\s+/g, ' ').trim();
  }

  function normalize(text) {
    return String(text || '')
      .toLowerCase()
      .replace(/[\u200B-\u200D\uFEFF]/g, '')
      .replace(/[^\p{L}\p{N}]+/gu, ' ')
      .replace(/\s+/g, ' ')
      .trim();
  }

  function fuzzyScore(prompt, candidate) {
    if (!prompt || !candidate) return 0;
    if (candidate.includes(prompt) || prompt.includes(candidate)) return 1;
    const promptTokens = prompt.split(' ').filter(Boolean);
    const candidateTokens = new Set(candidate.split(' ').filter(Boolean));
    if (!promptTokens.length || !candidateTokens.size) return 0;
    let overlap = 0;
    for (const token of promptTokens) if (candidateTokens.has(token)) overlap += 1;
    const first = prompt.slice(0, 80);
    const last = prompt.slice(-80);
    let score = overlap / promptTokens.length;
    if (first && candidate.includes(first)) score += 0.25;
    if (last && candidate.includes(last)) score += 0.25;
    const lengthRatio = Math.min(prompt.length, candidate.length) / Math.max(prompt.length, candidate.length);
    return Math.min(1, score * 0.8 + lengthRatio * 0.2);
  }

  function isVisible(el) {
    if (!el) return false;
    const rect = el.getBoundingClientRect();
    const style = getComputedStyle(el);
    return rect.width > 0 && rect.height > 0 && style.display !== 'none' && style.visibility !== 'hidden' && style.opacity !== '0';
  }
})();
