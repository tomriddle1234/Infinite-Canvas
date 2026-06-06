let currentState = null;
let currentContext = null;

const els = {
  pageContext: document.getElementById('pageContext'),
  rows: document.getElementById('rows'),
  status: document.getElementById('status'),
  refreshBtn: document.getElementById('refreshBtn'),
  pullCanvasPromptBtn: document.getElementById('pullCanvasPromptBtn'),
  createNodeBtn: document.getElementById('createNodeBtn'),
  clearImportedBtn: document.getElementById('clearImportedBtn')
};

init();

chrome.runtime.onMessage.addListener(message => {
  if (message?.type === 'state-updated') {
    currentState = message.state;
    render();
  }
});

async function init() {
  bindEvents();
  await command('register-active-tab');
  await refresh();
  setInterval(refreshContextOnly, 1500);
}

function bindEvents() {
  els.refreshBtn.addEventListener('click', refresh);
  els.pullCanvasPromptBtn.addEventListener('click', async () => {
    const res = await command('pull-selected-canvas-prompt');
    setStatus(res.ok ? '已从画布读取提示词。/ Prompt pulled from canvas.' : res.error);
  });
  els.createNodeBtn.addEventListener('click', async () => {
    setStatus('正在创建画布图片节点... / Creating image node...');
    const res = await command('create-node');
    setStatus(res.ok ? '图片节点已创建。/ Image node created.' : res.error);
  });
  els.clearImportedBtn.addEventListener('click', () => command('clear-imported'));
}

async function refresh() {
  const res = await command('get-state');
  if (res.ok) currentState = res.state;
  await refreshContextOnly();
  render();
}

async function refreshContextOnly() {
  const res = await command('get-active-context');
  if (res.ok) {
    currentContext = res.context;
    updateContext();
  }
}

function updateContext() {
  const kind = currentContext?.kind || 'unsupported';
  const title = currentContext?.title || currentContext?.url || '';
  const label = kind === 'canvas' ? '无限画布 / Infinite Canvas' : kind === 'generator' ? '生成网页 / Generator' : '不支持页面 / Unsupported';
  els.pageContext.textContent = `${label}: ${title}`.slice(0, 120);
  els.createNodeBtn.disabled = !currentState?.selectedImageId;
  els.pullCanvasPromptBtn.disabled = kind !== 'canvas';
}

function render() {
  if (!currentState) return;
  updateContext();
  const prompts = currentState.prompts || [];
  const images = currentState.images || [];
  els.rows.innerHTML = '';
  if (!prompts.length) {
    const empty = document.createElement('div');
    empty.className = 'empty';
    empty.textContent = '添加提示词，或从无限画布读取选中的提示词。/ Add a prompt or pull one from Infinite Canvas.';
    els.rows.appendChild(empty);
  }
  const unassignedImages = images.filter(image => !image.promptId);
  for (const prompt of prompts) {
    els.rows.appendChild(renderRow(prompt, images.filter(image => image.promptId === prompt.id)));
  }
  if (unassignedImages.length) {
    els.rows.appendChild(renderRow({ id: '', title: '未分配 / Unassigned', text: '没有匹配到提示词的已抓取图片。/ Captured images without a prompt match.', source: 'system' }, unassignedImages));
  }
  els.status.textContent = currentState.lastStatus || '仅当前会话暂存。Chrome 重启后清空。/ Session clipboard only.';
}

function renderRow(prompt, images) {
  const row = document.createElement('div');
  row.className = 'row';

  const promptCell = document.createElement('div');
  promptCell.className = `prompt-cell ${currentState.selectedPromptId === prompt.id ? 'selected' : ''}`;
  promptCell.innerHTML = `
    <div class="prompt-title">${escapeHtml(prompt.title || firstLine(prompt.text))}</div>
    <div class="prompt-text">${escapeHtml(prompt.text || '')}</div>
    <div class="meta">${escapeHtml(sourceLabel(prompt.source || ''))}${prompt.imageIds?.length ? ` · ${prompt.imageIds.length} 张图 / image(s)` : ''}</div>
  `;
  promptCell.addEventListener('click', () => prompt.id && command('select-prompt', { promptId: prompt.id }));

  const imagesCell = document.createElement('div');
  imagesCell.className = 'images-cell';
  imagesCell.dataset.promptId = prompt.id || '';
  imagesCell.addEventListener('dragover', event => {
    event.preventDefault();
    imagesCell.classList.add('drag-over');
  });
  imagesCell.addEventListener('dragleave', () => imagesCell.classList.remove('drag-over'));
  imagesCell.addEventListener('drop', event => {
    event.preventDefault();
    imagesCell.classList.remove('drag-over');
    const imageId = event.dataTransfer.getData('text/plain');
    command('move-image', { imageId, promptId: prompt.id || '' });
  });

  for (const image of images) {
    imagesCell.appendChild(renderImageCard(image));
  }

  const controls = document.createElement('div');
  controls.className = 'row-controls';
  controls.innerHTML = `
    <button data-action="fill" ${prompt.id ? '' : 'disabled'}>填入 <span>Fill</span></button>
    <button data-action="capture-current" ${prompt.id ? '' : 'disabled'}>抓当前 <span>Capture</span></button>
    <button data-action="capture-opened" ${prompt.id ? '' : 'disabled'}>抓已开 <span>Opened</span></button>
    <button data-action="delete" ${prompt.id ? '' : 'disabled'}>删除 <span>Delete</span></button>
  `;
  controls.querySelector('[data-action="fill"]').addEventListener('click', async () => {
    if (!prompt.id) return;
    const res = await command('fill-input', { promptId: prompt.id });
    setStatus(res.ok ? '已填入当前生成网页。/ Prompt filled.' : res.error);
  });
  controls.querySelector('[data-action="capture-current"]').addEventListener('click', async () => {
    if (!prompt.id) return;
    setStatus('正在扫描当前页... / Scanning current tab...');
    const res = await command('capture-current-tab', { promptId: prompt.id });
    setStatus(res.ok ? (res.state?.lastStatus || '抓取完成。/ Capture finished.') : res.error);
  });
  controls.querySelector('[data-action="capture-opened"]').addEventListener('click', async () => {
    if (!prompt.id) return;
    setStatus('正在扫描已打开的生成页... / Scanning opened generator tabs...');
    const res = await command('capture-opened-tabs', { promptId: prompt.id });
    setStatus(res.ok ? (res.state?.lastStatus || '多页抓取完成。/ Multi-tab capture finished.') : res.error);
  });
  controls.querySelector('[data-action="delete"]').addEventListener('click', () => prompt.id && command('remove-prompt', { promptId: prompt.id }));

  row.appendChild(promptCell);
  row.appendChild(imagesCell);
  row.appendChild(controls);
  return row;
}

function renderImageCard(image) {
  const card = document.createElement('div');
  card.className = `image-card ${currentState.selectedImageId === image.id ? 'selected' : ''} ${image.status === 'imported' ? 'imported' : ''} ${image.confidence === 'review' ? 'review' : ''}`;
  card.draggable = true;
  card.dataset.imageId = image.id;
  card.innerHTML = `
    <img src="${escapeAttr(image.dataUrl || image.localUrl || '')}" alt="" draggable="false">
    <span title="${escapeAttr(image.sourceTabTitle || image.conversationUrl || '')}">${formatBytes(image.byteSize)} ${image.siteId || ''}</span>
    <button class="image-create-btn" data-action="create-image">创建 <span>Create</span></button>
  `;
  card.addEventListener('click', () => command('select-image', { imageId: image.id }));
  card.addEventListener('dragstart', event => {
    event.dataTransfer.clearData();
    event.dataTransfer.effectAllowed = 'copyMove';
    event.dataTransfer.setData('application/x-webgen-bridge-image', JSON.stringify(imageDragPayload(image)));
    event.dataTransfer.setData('text/plain', image.id);
  });
  card.querySelector('[data-action="create-image"]').addEventListener('click', async event => {
    event.stopPropagation();
    await command('select-image', { imageId: image.id });
    setStatus('正在创建画布图片节点... / Creating image node...');
    const res = await command('create-node', { imageId: image.id });
    setStatus(res.ok ? '图片节点已创建。/ Image node created.' : res.error);
  });
  return card;
}

function imageDragPayload(image) {
  const prompt = (currentState?.prompts || []).find(item => item.id === image.promptId);
  return {
    id: image.id,
    dataUrl: image.dataUrl || '',
    localUrl: image.localUrl || '',
    sourceUrl: image.sourceUrl || '',
    fileName: image.fileName || `webgen-image-${Date.now()}.png`,
    name: image.fileName || image.sourceTabTitle || 'webgen image',
    mimeType: image.mimeType || 'image/png',
    byteSize: image.byteSize || 0,
    width: image.width || 0,
    height: image.height || 0,
    prompt: prompt?.text || image.promptText || '',
    promptId: prompt?.id || image.promptId || '',
    sourceSite: image.siteId || '',
    sourceTabTitle: image.sourceTabTitle || '',
    conversationUrl: image.conversationUrl || ''
  };
}

function command(type, payload = {}) {
  return chrome.runtime.sendMessage({ type, ...payload }).then(response => {
    if (response?.state) currentState = response.state;
    render();
    return response || { ok: false, error: '没有响应 / No response' };
  }).catch(error => ({ ok: false, error: error?.message || String(error) }));
}

function setStatus(text) {
  els.status.textContent = text || '';
}

function firstLine(text) {
  const line = String(text || '').trim().split(/\r?\n/).find(Boolean) || '提示词 / Prompt';
  return line.length > 60 ? `${line.slice(0, 57)}...` : line;
}

function formatBytes(value) {
  const bytes = Number(value || 0);
  if (!bytes) return '';
  if (bytes > 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)}MB`;
  return `${Math.round(bytes / 1024)}KB`;
}

function escapeHtml(text) {
  return String(text || '').replace(/[&<>"']/g, ch => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[ch]));
}

function escapeAttr(text) {
  return escapeHtml(text).replace(/`/g, '&#96;');
}

function sourceLabel(source) {
  const map = {
    manual: '手动 / Manual',
    canvas: '画布 / Canvas',
    generator: '生成页 / Generator',
    system: '系统 / System'
  };
  return map[source] || source;
}
