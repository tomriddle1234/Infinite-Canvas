const STORAGE_KEY = 'webgen_bridge_session_v1';
const SOURCE = 'webgen-bridge-background';
const CONTENT_SCRIPT_FILES = [
  'adapters/chatgpt.js',
  'adapters/generic-chat-image.js',
  'content-generator.js',
  'content-canvas.js'
];

let state = {
  prompts: [],
  images: [],
  selectedPromptId: '',
  selectedImageId: '',
  participatingTabs: {},
  lastStatus: ''
};

chrome.sidePanel.setPanelBehavior({ openPanelOnActionClick: true }).catch(() => {});

loadState();

chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  handleMessage(message || {}, sender).then(sendResponse).catch(error => {
    sendResponse({ ok: false, error: error?.message || String(error) });
  });
  return true;
});

chrome.tabs.onRemoved.addListener(tabId => {
  if (state.participatingTabs[String(tabId)]) {
    delete state.participatingTabs[String(tabId)];
    saveState();
  }
});

async function handleMessage(message, sender) {
  switch (message.type) {
    case 'get-state':
      await registerActiveTab(false);
      return { ok: true, state };
    case 'register-active-tab':
      await registerActiveTab(true);
      return { ok: true, state };
    case 'add-prompt':
      addPrompt(message.prompt || {});
      return { ok: true, state };
    case 'update-prompt':
      updatePrompt(message.promptId, message.patch || {});
      return { ok: true, state };
    case 'select-prompt':
      state.selectedPromptId = message.promptId || '';
      await saveState();
      return { ok: true, state };
    case 'select-image':
      state.selectedImageId = message.imageId || '';
      await saveState();
      return { ok: true, state };
    case 'move-image':
      moveImage(message.imageId, message.promptId);
      return { ok: true, state };
    case 'remove-image':
      removeImage(message.imageId);
      return { ok: true, state };
    case 'remove-prompt':
      removePrompt(message.promptId);
      return { ok: true, state };
    case 'clear-imported':
      state.images = state.images.filter(image => image.status !== 'imported');
      syncPromptImageIds();
      await saveState();
      return { ok: true, state };
    case 'fill-input':
      return fillInput(message.promptId);
    case 'capture-current-tab':
      return captureCurrentTab(message.promptId);
    case 'capture-opened-tabs':
      return captureOpenedTabs(message.promptId);
    case 'create-node':
      return createNodeFromSelectedImage(message.imageId);
    case 'pull-selected-canvas-prompt':
      return pullSelectedCanvasPrompt();
    case 'get-active-context':
      return { ok: true, context: await activeTabContext(), state };
    default:
      return { ok: false, error: `未知消息类型 / Unknown message type: ${message.type || ''}` };
  }
}

function uid(prefix) {
  return `${prefix}_${Math.random().toString(36).slice(2, 10)}${Date.now().toString(36).slice(-4)}`;
}

async function loadState() {
  try {
    const result = await chrome.storage.session.get([STORAGE_KEY]);
    if (result[STORAGE_KEY]) {
      state = normalizeState(result[STORAGE_KEY]);
    }
  } catch (_) {
    // Session storage is a convenience, not a hard dependency.
  }
}

async function saveState() {
  state = normalizeState(state);
  try {
    await chrome.storage.session.set({ [STORAGE_KEY]: state });
  } catch (error) {
    state.lastStatus = `会话暂存失败，继续使用内存。/ Session storage skipped: ${error?.message || error}`;
  }
  broadcastState();
}

function normalizeState(next) {
  return {
    prompts: Array.isArray(next?.prompts) ? next.prompts : [],
    images: Array.isArray(next?.images) ? next.images : [],
    selectedPromptId: next?.selectedPromptId || '',
    selectedImageId: next?.selectedImageId || '',
    participatingTabs: next?.participatingTabs || {},
    lastStatus: next?.lastStatus || ''
  };
}

function broadcastState() {
  chrome.runtime.sendMessage({ source: SOURCE, type: 'state-updated', state }).catch(() => {});
}

function activePrompt() {
  return state.prompts.find(prompt => prompt.id === state.selectedPromptId) || state.prompts[state.prompts.length - 1] || null;
}

function promptById(promptId) {
  return state.prompts.find(prompt => prompt.id === promptId) || null;
}

function selectedImage() {
  return state.images.find(image => image.id === state.selectedImageId) || null;
}

function addPrompt(prompt) {
  const text = String(prompt.text || '').trim();
  if (!text) return;
  if (prompt.id) {
    const sameId = state.prompts.find(item => item.id === prompt.id);
    if (sameId) {
      Object.assign(sameId, {
        text,
        title: prompt.title || firstLine(text),
        source: prompt.source || sameId.source || 'manual',
        siteId: prompt.siteId || sameId.siteId || '',
        sourceTabId: prompt.sourceTabId || sameId.sourceTabId || '',
        conversationUrl: prompt.conversationUrl || sameId.conversationUrl || '',
        updatedAt: new Date().toISOString()
      });
      state.selectedPromptId = sameId.id;
      saveState();
      return;
    }
  }
  const existing = state.prompts.find(item => normalizeText(item.text) === normalizeText(text));
  if (existing) {
    state.selectedPromptId = existing.id;
    saveState();
    return;
  }
  const item = {
    id: prompt.id || uid('prompt'),
    text,
    title: prompt.title || firstLine(text),
    source: prompt.source || 'manual',
    siteId: prompt.siteId || '',
    sourceTabId: prompt.sourceTabId || '',
    conversationUrl: prompt.conversationUrl || '',
    createdAt: prompt.createdAt || new Date().toISOString(),
    imageIds: []
  };
  state.prompts.push(item);
  state.selectedPromptId = item.id;
  saveState();
}

function updatePrompt(promptId, patch) {
  const prompt = state.prompts.find(item => item.id === promptId);
  if (!prompt) return;
  Object.assign(prompt, patch);
  if (patch.text) prompt.title = patch.title || firstLine(patch.text);
  saveState();
}

function removePrompt(promptId) {
  state.prompts = state.prompts.filter(prompt => prompt.id !== promptId);
  state.images = state.images.map(image => image.promptId === promptId ? { ...image, promptId: '' } : image);
  if (state.selectedPromptId === promptId) state.selectedPromptId = state.prompts[0]?.id || '';
  syncPromptImageIds();
  saveState();
}

function removeImage(imageId) {
  state.images = state.images.filter(image => image.id !== imageId);
  if (state.selectedImageId === imageId) state.selectedImageId = state.images[0]?.id || '';
  syncPromptImageIds();
  saveState();
}

function moveImage(imageId, promptId) {
  const image = state.images.find(item => item.id === imageId);
  if (!image) return;
  image.promptId = promptId || '';
  syncPromptImageIds();
  saveState();
}

function syncPromptImageIds() {
  for (const prompt of state.prompts) {
    prompt.imageIds = state.images.filter(image => image.promptId === prompt.id).map(image => image.id);
  }
}

function firstLine(text) {
  const line = String(text || '').trim().split(/\r?\n/).find(Boolean) || 'Prompt';
  return line.length > 60 ? `${line.slice(0, 57)}...` : line;
}

function normalizeText(text) {
  return String(text || '').toLowerCase().replace(/\s+/g, ' ').trim();
}

async function activeTab() {
  const tabs = await chrome.tabs.query({ active: true, lastFocusedWindow: true });
  return tabs[0] || null;
}

async function activeTabContext() {
  const tab = await activeTab();
  if (!tab?.id || !isWebTab(tab)) return { kind: 'unsupported', tabId: tab?.id || 0, url: tab?.url || '', title: tab?.title || '' };
  try {
    const response = await sendTopFrameMessage(tab.id, { type: 'webgen-probe' });
    if (response?.kind === 'unsupported' && looksLikeGeneratorTab(tab.url || '')) {
      return { kind: 'generator', siteId: inferSiteIdFromUrl(tab.url || ''), tabId: tab.id, url: tab.url || '', title: tab.title || '', probeError: response.error || '' };
    }
    return { ...(response || {}), tabId: tab.id, url: tab.url || '', title: tab.title || '' };
  } catch (_) {
    return { kind: inferKindFromUrl(tab.url), tabId: tab.id, url: tab.url || '', title: tab.title || '' };
  }
}

function isWebTab(tab) {
  return !!tab.url && /^(https?:\/\/|file:\/\/)/i.test(tab.url);
}

function inferKindFromUrl(url = '') {
  if (/^https?:\/\/(127\.0\.0\.1|localhost)(:\d+)?\//i.test(url)) return 'canvas';
  if (/^https?:\/\//i.test(url)) return 'generator';
  return 'unsupported';
}

async function registerActiveTab(markParticipating) {
  const tab = await activeTab();
  if (tab?.id && isWebTab(tab)) {
    await ensureContentScripts(tab.id);
  }
  const context = await activeTabContext();
  if (markParticipating && context.kind === 'generator' && context.tabId) {
    state.participatingTabs[String(context.tabId)] = {
      tabId: context.tabId,
      siteId: context.siteId || 'generic',
      title: context.title || '',
      url: context.url || '',
      lastSeenAt: new Date().toISOString()
    };
    await saveState();
  }
  return context;
}

async function fillInput(promptId) {
  const prompt = state.prompts.find(item => item.id === promptId) || activePrompt();
  if (!prompt) return { ok: false, error: '未选择提示词 / No prompt selected' };
  const tab = await activeTab();
  if (!tab?.id || !isWebTab(tab)) return { ok: false, error: '没有可用的当前网页 / No active web tab' };
  await sendTopFrameMessage(tab.id, { type: 'webgen-fill-input', promptText: prompt.text });
  state.lastStatus = '已填入当前网页。/ Prompt filled into active tab.';
  await saveState();
  return { ok: true, state };
}

async function captureCurrentTab(promptId = '') {
  const tab = await activeTab();
  if (!tab?.id || !isWebTab(tab)) return { ok: false, error: '没有可用的生成网页 / No active generator tab' };
  const prompt = promptById(promptId) || activePrompt();
  if (prompt) state.selectedPromptId = prompt.id;
  let targetTab = tab;
  const context = await activeTabContext();
  if (context.kind !== 'generator') {
    const generatorTabs = await getOpenGeneratorTabs();
    if (generatorTabs.length) targetTab = generatorTabs[0];
  }
  const summary = await captureFromTab(targetTab.id, targetTab, prompt);
  state.lastStatus = summarizeCapture([summary]);
  await saveState();
  return { ok: true, state, summaries: [summary] };
}

async function captureOpenedTabs(promptId = '') {
  const prompt = promptById(promptId) || activePrompt();
  if (prompt) state.selectedPromptId = prompt.id;
  const registeredEntries = Object.values(state.participatingTabs || {});
  const openTabs = await getOpenGeneratorTabs();
  const tabMap = new Map();
  for (const tab of openTabs) {
    tabMap.set(String(tab.id), {
      tabId: tab.id,
      siteId: inferSiteIdFromUrl(tab.url || ''),
      title: tab.title || '',
      url: tab.url || '',
      lastSeenAt: new Date(tab.lastAccessed || Date.now()).toISOString()
    });
  }
  for (const entry of registeredEntries) tabMap.set(String(entry.tabId), entry);
  const entries = [...tabMap.values()];
  const summaries = [];
  for (const entry of entries) {
    try {
      const tab = await chrome.tabs.get(Number(entry.tabId));
      summaries.push(await captureFromTab(tab.id, tab, prompt));
    } catch (error) {
      summaries.push({ tabId: entry.tabId, title: entry.title || '', found: 0, skipped: true, error: error?.message || String(error) });
    }
  }
  state.lastStatus = summarizeCapture(summaries);
  await saveState();
  return { ok: true, state, summaries };
}

async function getOpenGeneratorTabs() {
  const tabs = await chrome.tabs.query({});
  return (tabs || [])
    .filter(tab => tab?.id && isWebTab(tab) && looksLikeGeneratorTab(tab.url || ''))
    .sort((a, b) => Number(b.lastAccessed || 0) - Number(a.lastAccessed || 0));
}

function looksLikeGeneratorTab(url = '') {
  if (/^https:\/\/(chatgpt\.com|chat\.openai\.com)\//i.test(url)) return true;
  if (/^https?:\/\/(127\.0\.0\.1|localhost)(:\d+)?\//i.test(url)) return false;
  if (!/^https?:\/\//i.test(url)) return false;
  return /(generate|image|chat|conversation|prompt|ai|studio)/i.test(url);
}

function inferSiteIdFromUrl(url = '') {
  if (/^https:\/\/(chatgpt\.com|chat\.openai\.com)\//i.test(url)) return 'chatgpt';
  return 'generic';
}

async function captureFromTab(tabId, tabInfo, prompt = null) {
  const knownFingerprints = state.images.map(image => image.fingerprint).filter(Boolean);
  const response = await sendTopFrameMessage(tabId, {
    type: 'webgen-capture',
    promptText: prompt?.text || '',
    selectedPromptId: prompt?.id || '',
    knownFingerprints
  });
  const candidates = Array.isArray(response?.images) ? response.images : [];
  let added = 0;
  let downloadFailed = 0;
  let lastDownloadError = '';
  for (const candidate of candidates) {
    if (!candidate || isDuplicate(candidate)) continue;
    const hydrated = await hydrateCandidate(candidate, tabId);
    if (!hydrated || !hydrated.dataUrl || isDuplicate(hydrated)) {
      downloadFailed += 1;
      lastDownloadError = hydrated?.captureError || candidate.captureError || lastDownloadError;
      continue;
    }
    const promptId = resolvePromptId(candidate, prompt);
    const image = {
      id: uid('image'),
      promptId,
      dataUrl: hydrated.dataUrl || '',
      sourceUrl: hydrated.sourceUrl || '',
      fingerprint: hydrated.fingerprint || fingerprint(hydrated),
      fileName: hydrated.fileName || `webgen-image-${Date.now()}.png`,
      mimeType: hydrated.mimeType || 'image/png',
      byteSize: hydrated.byteSize || 0,
      width: hydrated.width || 0,
      height: hydrated.height || 0,
      source: hydrated.source || 'generator-dom',
      siteId: response?.siteId || hydrated.siteId || 'generic',
      sourceTabId: tabId,
      sourceTabTitle: tabInfo?.title || '',
      conversationUrl: tabInfo?.url || hydrated.conversationUrl || '',
      promptText: hydrated.promptText || prompt?.text || '',
      confidence: hydrated.confidence || 'review',
      createdAt: new Date().toISOString(),
      status: hydrated.dataUrl ? 'ready' : 'captured'
    };
    state.images.push(image);
    state.selectedImageId = image.id;
    if (promptId) state.selectedPromptId = promptId;
    added += 1;
  }
  syncPromptImageIds();
  if (response?.siteId && tabId) {
    state.participatingTabs[String(tabId)] = {
      tabId,
      siteId: response.siteId,
      title: tabInfo?.title || '',
      url: tabInfo?.url || '',
      lastSeenAt: new Date().toISOString()
    };
  }
  return {
    tabId,
    title: tabInfo?.title || '',
    found: candidates.length,
    added,
    downloadFailed,
    downloadError: lastDownloadError,
    error: response?.error || ''
  };
}

function resolvePromptId(candidate, fallbackPrompt) {
  if (candidate.promptText) {
    const existing = state.prompts.find(prompt => normalizeText(prompt.text) === normalizeText(candidate.promptText));
    if (existing) return existing.id;
  }
  return candidate.promptId || fallbackPrompt?.id || '';
}

function isDuplicate(candidate) {
  const key = candidate.fingerprint || fingerprint(candidate);
  return state.images.some(image => image.fingerprint === key || (candidate.sourceUrl && image.sourceUrl === candidate.sourceUrl));
}

async function hydrateCandidate(candidate, tabId = 0) {
  if (candidate.dataUrl) return candidate;
  const sourceUrl = candidate.sourceUrl || '';
  if (!/^https?:\/\//i.test(sourceUrl)) return candidate.dataUrl ? candidate : null;
  try {
    const response = await fetch(sourceUrl, { credentials: 'include', cache: 'no-store' });
    if (!response.ok) throw new Error(`${response.status} ${response.statusText}`);
    const blob = await response.blob();
    if (!blob || !blob.size) return null;
    const mimeType = blob.type || candidate.mimeType || 'image/png';
    const dataUrl = await blobToDataUrl(blob);
    const ext = mimeType.includes('webp') ? 'webp' : mimeType.includes('jpeg') || mimeType.includes('jpg') ? 'jpg' : 'png';
    return {
      ...candidate,
      dataUrl,
      mimeType,
      byteSize: blob.size,
      fileName: candidate.fileName || `webgen-image-${Date.now()}.${ext}`,
      fingerprint: [sourceUrl, candidate.width || '', candidate.height || '', blob.size].join('|'),
      confidence: blob.size >= 1.5 * 1024 * 1024 ? 'high' : (candidate.confidence || 'review')
    };
  } catch (error) {
    const pageWorld = await fetchImageInPageWorld(tabId, sourceUrl).catch(pageError => ({
      ok: false,
      error: pageError?.message || String(pageError)
    }));
    if (pageWorld?.ok && pageWorld.dataUrl) {
      return {
        ...candidate,
        dataUrl: pageWorld.dataUrl,
        mimeType: pageWorld.mimeType || candidate.mimeType || 'image/png',
        byteSize: pageWorld.byteSize || candidate.byteSize || 0,
        fileName: candidate.fileName || `webgen-image-${Date.now()}.${extensionForMime(pageWorld.mimeType || candidate.mimeType)}`,
        fingerprint: [sourceUrl, candidate.width || '', candidate.height || '', pageWorld.byteSize || ''].join('|'),
        confidence: pageWorld.byteSize >= 1.5 * 1024 * 1024 ? 'high' : (candidate.confidence || 'review'),
        source: `${candidate.source || 'generator-dom'}+page-fetch`
      };
    }
    return {
      ...candidate,
      dataUrl: '',
      captureError: `${error?.message || String(error)}${pageWorld?.error ? `; page fetch: ${pageWorld.error}` : ''}`,
      confidence: 'review'
    };
  }
}

async function fetchImageInPageWorld(tabId, sourceUrl) {
  if (!tabId || !chrome.scripting?.executeScript || !sourceUrl) return { ok: false, error: 'Page-world fetch unavailable' };
  const [result] = await chrome.scripting.executeScript({
    target: { tabId },
    world: 'MAIN',
    args: [sourceUrl],
    func: async url => {
      function readAsDataUrl(blob) {
        return new Promise((resolve, reject) => {
          const reader = new FileReader();
          reader.onload = () => resolve(reader.result);
          reader.onerror = () => reject(reader.error);
          reader.readAsDataURL(blob);
        });
      }
      const response = await fetch(url, {
        credentials: 'include',
        cache: 'no-store'
      });
      if (!response.ok) throw new Error(`${response.status} ${response.statusText}`);
      const blob = await response.blob();
      if (!blob || !blob.size) throw new Error('Empty image response');
      return {
        ok: true,
        dataUrl: await readAsDataUrl(blob),
        mimeType: blob.type || 'image/png',
        byteSize: blob.size
      };
    }
  });
  return result?.result || { ok: false, error: 'No page-world result' };
}

function extensionForMime(mimeType = '') {
  const mime = String(mimeType || '').toLowerCase();
  if (mime.includes('webp')) return 'webp';
  if (mime.includes('jpeg') || mime.includes('jpg')) return 'jpg';
  return 'png';
}

function blobToDataUrl(blob) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(reader.result);
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(blob);
  });
}

function fingerprint(item) {
  return [
    item.sourceUrl || '',
    item.width || '',
    item.height || '',
    item.byteSize || ''
  ].join('|');
}

function summarizeCapture(summaries) {
  const added = summaries.reduce((sum, item) => sum + Number(item.added || 0), 0);
  const found = summaries.reduce((sum, item) => sum + Number(item.found || 0), 0);
  const downloadFailed = summaries.reduce((sum, item) => sum + Number(item.downloadFailed || 0), 0);
  const skipped = summaries.filter(item => item.skipped || item.error).length;
  const lastError = summaries.map(item => item.downloadError || item.error || '').find(Boolean);
  return `抓取完成 / Capture: ${added} added, ${found} found${downloadFailed ? `, ${downloadFailed} download failed` : ''}${skipped ? `, ${skipped} skipped/failed` : ''}${lastError ? ` (${lastError})` : ''}`;
}

async function pullSelectedCanvasPrompt() {
  const tab = await activeTab();
  if (!tab?.id || !isWebTab(tab)) return { ok: false, error: '没有可用的画布页 / No active canvas tab' };
  const response = await sendCanvasFrameMessage(tab.id, { type: 'canvas-get-selected-prompt' });
  if (!response?.prompt?.text) return { ok: false, error: '没有选中的提示词节点 / No selected prompt-like node found' };
  addPrompt({
    text: response.prompt.text,
    title: response.prompt.name || firstLine(response.prompt.text),
    source: 'canvas',
    sourceTabId: tab.id,
    conversationUrl: tab.url || ''
  });
  return { ok: true, state };
}

async function createNodeFromSelectedImage(imageId = '') {
  const image = state.images.find(item => item.id === imageId) || selectedImage();
  if (!image) return { ok: false, error: '未选择图片 / No image selected' };
  if (!image.dataUrl) return { ok: false, error: '选中的图片没有文件数据 / Selected image has no captured file data' };
  const tab = await findCanvasTargetTab();
  if (!tab?.id || !isWebTab(tab)) return { ok: false, error: '没有打开的无限画布页面 / No open Infinite Canvas tab' };
  const prompt = state.prompts.find(item => item.id === image.promptId);
  const response = await sendCanvasFrameMessage(tab.id, {
    type: 'canvas-create-image-node',
    image: {
      ...image,
      prompt: prompt?.text || image.promptText || '',
      promptId: prompt?.id || image.promptId || ''
    }
  });
  if (!response?.ok) return { ok: false, error: response?.error || '创建节点失败 / Create node failed' };
  image.status = 'imported';
  image.localUrl = response.file?.url || response.node?.url || '';
  state.lastStatus = `已创建图片节点 / Created image node ${response.node?.id || ''}`;
  await saveState();
  return { ok: true, state, node: response.node, file: response.file };
}

async function findCanvasTargetTab() {
  const tab = await activeTab();
  if (tab?.id && isWebTab(tab)) {
    const context = await activeTabContext();
    if (context.kind === 'canvas') return tab;
  }
  const tabs = await getOpenCanvasTabs();
  return tabs[0] || null;
}

async function getOpenCanvasTabs() {
  const tabs = await chrome.tabs.query({});
  const candidates = (tabs || []).filter(tab => tab?.id && isWebTab(tab) && /^https?:\/\/(127\.0\.0\.1|localhost)(:\d+)?\//i.test(tab.url || ''));
  candidates.sort((a, b) => Number(b.lastAccessed || 0) - Number(a.lastAccessed || 0));
  return candidates;
}

async function sendCanvasFrameMessage(tabId, message) {
  const frameId = await findCanvasFrameId(tabId);
  if (Number.isInteger(frameId)) {
    try {
      return await sendTabMessage(tabId, message, { frameId });
    } catch (error) {
      if (!String(error?.message || error).includes('Receiving end does not exist')) throw error;
    }
  }
  return sendTabMessage(tabId, message);
}

async function sendTopFrameMessage(tabId, message) {
  return sendTabMessage(tabId, message, { frameId: 0 });
}

async function findCanvasFrameId(tabId) {
  try {
    const frames = await chrome.webNavigation.getAllFrames({ tabId });
    const canvasFrame = (frames || []).find(frame => /\/static\/canvas\.html/i.test(frame.url || ''));
    if (canvasFrame) return canvasFrame.frameId;
    const localTop = (frames || []).find(frame => frame.frameId === 0 && /^https?:\/\/(127\.0\.0\.1|localhost)(:\d+)?\//i.test(frame.url || ''));
    return localTop?.frameId;
  } catch (_) {
    return null;
  }
  return null;
}

async function sendTabMessage(tabId, message, options = undefined) {
  try {
    return await chrome.tabs.sendMessage(tabId, message, options);
  } catch (error) {
    if (!isMissingReceiverError(error)) throw error;
    await ensureContentScripts(tabId);
    return chrome.tabs.sendMessage(tabId, message, options);
  }
}

function isMissingReceiverError(error) {
  const text = String(error?.message || error || '');
  return text.includes('Receiving end does not exist') || text.includes('Could not establish connection');
}

async function ensureContentScripts(tabId) {
  if (!chrome.scripting?.executeScript) return;
  try {
    await chrome.scripting.executeScript({
      target: { tabId, allFrames: true },
      files: CONTENT_SCRIPT_FILES
    });
  } catch (error) {
    // Some frames can be extension/CSP/protected pages. Static content_scripts still cover normal pages.
    console.warn('Webgen Bridge: script injection fallback failed', error);
  }
}
