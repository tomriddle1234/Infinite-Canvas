# 主无限画布上游同步实现计划（2026-05-23）

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` 或 `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 只同步原作者 2026-05-17 之后对“主无限画布 classic canvas”有实际价值的修复，明确跳过 Smart Canvas、LTX、RunningHub、自更新和素材库大系统。

**Architecture:** 当前项目不是上游单文件 `main.py` 架构，而是 `main_refactored.py` + `app/` 拆分架构；上游后端改动必须翻译到 `app/routes/*`、`app/ws.py`、`app/models.py`、`app/store.py`。前端不能整文件覆盖 `static/canvas.html`，只能按函数/菜单/文案小块移植，避免覆盖 Seedream/Seedance、去 CDN、本地 vendor 和已修过的焦点问题。

**Tech Stack:** FastAPI, Pydantic v2, plain HTML/CSS/JS, browser BroadcastChannel/WebSocket, file-backed JSON canvas store.

---

## 0. 上游与当前项目实现差异

### 0.1 仓库关系

- 原作者项目：`C:\src\Original-Infinite-Canvas`
- 当前项目：`C:\src\Infinite-Canvas`
- 两边没有可靠共同 git 历史，不能 merge/rebase/cherry-pick。
- 只能做文件内容对比和手工移植。

### 0.2 后端结构差异

原作者：

- 主要逻辑集中在 `main.py`。
- 路由写法是 `@app.get(...)` / `@app.post(...)`。
- `ConnectionManager`、canvas CRUD、asset endpoints、provider 逻辑都在同一个文件里。

当前项目：

- 入口是 `main_refactored.py:1-10`。
- FastAPI app 在 `app/factory.py:37-63` 创建。
- 路由由 `app/routes/__init__.py:9-20` 聚合。
- Canvas CRUD 在 `app/routes/canvas.py:13-87`。
- Canvas asset check/download 在 `app/routes/public.py:51-96`。
- WebSocket manager 在 `app/ws.py:33-82`。
- Canvas 数据模型在 `app/models.py:177-198`。
- Canvas 持久化在 `app/store.py:173-260`。

因此：

- 上游 `main.py` 的 `@app.put("/api/canvases/{canvas_id}")` 改动要落到 `app/routes/canvas.py:update_canvas()`。
- 上游 `manager.broadcast_canvas_updated()` 要落到 `app/ws.py:ConnectionManager`。
- 上游 request model 字段要落到 `app/models.py:CanvasCreateRequest` / `CanvasSaveRequest`。
- 上游 `/api/canvas-assets/*` 逻辑如果有 bugfix，要落到 `app/routes/public.py`。

### 0.3 前端结构差异

原作者 2026-05-23 上游：

- 把 `theme.js`、`i18n.js` 放到 `static/js/`，CSS 放到 `static/css/`。
- 新增 `static/smart-canvas.html`。
- 新增 `static/ltx-director-timeline.js`。
- 在 `canvas.html` 里加入 Smart Canvas / LTX / RunningHub 相关 UI 和逻辑。

当前项目：

- 仍使用 `static/i18n.js`、`static/theme.js`、`static/theme.css`。
- 使用本地 vendor：`/static/vendor/tailwindcss.js`、`/static/vendor/lucide.min.js`、`/static/vendor/three.module.js`。
- 已有本地 Seedream/Seedance/gpt-image-2 节点和接口。
- `static/canvas.html` 已经有 2026-05-17 同步过的主画布功能，但存在几个未对齐点。

禁止：

- 禁止把上游 `/static/js/i18n.js` 路径照搬进当前 HTML。
- 禁止把上游 CDN/vendor 结构覆盖当前本地 vendor。
- 禁止整文件替换 `static/canvas.html`。

## 1. 明确排除的上游功能

本计划不实现以下内容：

- Smart Canvas：`static/smart-canvas.html`、`kind:'smart'` 新建入口、`smart.*` i18n、Smart Canvas 专用素材库 UI。
- LTX Director：`static/ltx-director-timeline.js`、`addLTXDirectorNode()`、`ltxDirector` node、`canvas.ltx*` i18n、LTX workflow 常量。
- RunningHub：provider protocol、默认模型、后端调用、logo、设置页选项。
- 自更新：`/api/app-info`、`/api/update-from-github`、backup/rollback、版本检查 UI。
- `asset-library` 整套 API：它主要服务 Smart Canvas 素材库，当前不需要。
- ComfyUI 专属 bind_prompt/workflow 绑定改动：当前用户明确不依赖 ComfyUI 模块，除非后续发现它修复主画布通用 bug。

## 2. 本轮真正要修的主画布缺口

已经通过当前文件读取确认，当前项目不是“完全没同步”，而是有以下具体缺口：

1. **WebSocket canvas update 后端广播缺失**
   - 当前前端已经监听 `canvas_updated`：`static/canvas.html:2029-2041`、`2076-2082`。
   - 当前 `app/ws.py` 没有 `broadcast_canvas_updated()`。
   - 当前 `app/routes/canvas.py:update_canvas()` 保存后只 `return {"canvas": canvas}`，没有广播。
   - 结果：多 tab 同步主要靠 2.5s polling，不能实时收到保存广播。

2. **Canvas 保存 payload 与模型不完全对齐**
   - 当前前端 `saveCanvas()` 发送 `base_updated_at`，但没有发送 `client_id`：`static/canvas.html:1863-1867`。
   - 当前 `CanvasSaveRequest` 有 `base_updated_at`，但没有 `client_id` 和 `settings`：`app/models.py:182-189`。
   - 上游最新保存 payload 带 `client_id: CLIENT_ID`，后端广播时用它避免本 tab 回收自己的更新。

3. **Output 右键菜单没有暴露“下载全部图片”按钮**
   - 当前 `downloadOutputNodeImages()` 已存在：`static/canvas.html:3237-3263`。
   - 当前 `openOutputNodeMenu()` 只渲染“转输入组/复制输入组”：`static/canvas.html:3301-3329`。
   - 上游菜单新增了文件分组和 `data-output-download` 按钮。
   - 当前功能代码存在但用户无法从右键菜单触发。

4. **i18n 缺失导致菜单文案为空或 fallback 生硬**
   - 当前 `static/i18n.js` 没有：
     - `canvas.outputGroupActions`
     - `canvas.outputFileActions`
     - `canvas.outputDownloadAllImages`
     - `canvas.outputDownloadEmpty`
     - `canvas.missingFile`
   - 当前 `canvas.html` 里缺失文件中文 fallback 是 `????`：`static/canvas.html:7560-7562`。

5. **Smoke test 没覆盖已有 canvas-assets 路由**
   - 当前 `tests/smoke_refactored_app.py:21-45` 缺 `/api/canvas-assets/check` 和 `/api/canvas-assets/download`。
   - 这两个路由是主画布 Output 批量下载和缺失素材检测的后端依赖。

## 3. 文件修改清单

### Modify: `app/ws.py`

职责：WebSocket 连接管理。新增 `broadcast_canvas_updated()`，让后端保存画布后广播给其它 tab。

目标位置：`ConnectionManager` 类中，放在 `broadcast_new_image()` 后、`send_personal_message()` 前。

新增方法：

```python
    async def broadcast_canvas_updated(self, canvas_id: str, updated_at: int, client_id: str = ""):
        data = json.dumps({
            "type": "canvas_updated",
            "canvas_id": canvas_id,
            "updated_at": updated_at,
            "client_id": client_id or "",
        })
        for connection in self.active_connections[:]:
            try:
                await connection.send_text(data)
            except Exception as e:
                print(f"Broadcast canvas update error: {e}")
                self.active_connections.remove(connection)
```

### Modify: `app/models.py`

职责：请求模型。扩展 canvas 创建/保存请求字段。

目标：

```python
class CanvasCreateRequest(BaseModel):
    title: str = "未命名画布"
    icon: str = "🧩"

class CanvasSaveRequest(BaseModel):
    title: str = "未命名画布"
    icon: str = "🧩"
    nodes: List[Dict[str, Any]] = []
    connections: List[Dict[str, Any]] = []
    viewport: Dict[str, Any] = {}
    logs: List[Dict[str, Any]] = []
    base_updated_at: Optional[int] = None
```

改成：

```python
class CanvasCreateRequest(BaseModel):
    title: str = "未命名画布"
    icon: str = "🧩"
    kind: str = "classic"

class CanvasSaveRequest(BaseModel):
    title: str = "未命名画布"
    icon: str = "🧩"
    nodes: List[Dict[str, Any]] = []
    connections: List[Dict[str, Any]] = []
    viewport: Dict[str, Any] = {}
    logs: List[Dict[str, Any]] = []
    settings: Dict[str, Any] = {}
    base_updated_at: Optional[int] = None
    client_id: Optional[str] = None
```

说明：

- `kind` 只作为兼容字段，不接 Smart Canvas。创建时会强制 classic。
- `settings` 只保存已有画布设置，当前前端不发送也不影响。
- `client_id` 用于广播回避发起 tab。

### Modify: `app/store.py`

职责：canvas JSON 文件持久化。

目标函数：`new_canvas()`、`canvas_record()`。

修改点：

1. `new_canvas()` 增加 `kind` 参数，但只允许 classic：

```python
def new_canvas(title: str = "未命名画布", icon: str = "layers", kind: str = "classic") -> dict:
    timestamp = now_ms()
    canvas = {
        "id": uuid.uuid4().hex,
        "title": (title or "未命名画布")[:80],
        "icon": (icon or "🧩")[:32],
        "kind": "classic",
        "created_at": timestamp,
        "updated_at": timestamp,
        "nodes": [],
        "connections": [],
        "viewport": {"x": 0, "y": 0, "scale": 1},
        "logs": [],
        "settings": {},
    }
    save_canvas(canvas)
    return canvas
```

2. `canvas_record()` 返回 `kind`，让前端如果未来遇到上游 smart 数据也能识别，但当前 UI 不创建 smart：

```python
def canvas_record(data: dict) -> dict:
    return {
        "id": data.get("id"),
        "title": data.get("title", "未命名画布"),
        "icon": data.get("icon", "🧩"),
        "kind": data.get("kind", "classic"),
        "created_at": data.get("created_at", 0),
        "updated_at": data.get("updated_at", 0),
        "deleted_at": data.get("deleted_at", 0),
        "node_count": len(data.get("nodes", [])),
    }
```

### Modify: `app/routes/canvas.py`

职责：canvas CRUD。

修改点：

1. imports 从：

```python
from .. import store
```

改成：

```python
from .. import store, ws
```

2. `create_canvas()` 从：

```python
@router.post("/api/canvases")
async def create_canvas(payload: CanvasCreateRequest):
    return {"canvas": store.new_canvas(payload.title, payload.icon)}
```

改成：

```python
@router.post("/api/canvases")
async def create_canvas(payload: CanvasCreateRequest):
    return {"canvas": store.new_canvas(payload.title, payload.icon, payload.kind)}
```

3. `get_canvas_meta()` 增加 `kind` 返回：

```python
@router.get("/api/canvases/{canvas_id}/meta")
async def get_canvas_meta(canvas_id: str):
    canvas = store.load_canvas(canvas_id)
    return {
        "id": canvas.get("id"),
        "updated_at": canvas.get("updated_at", 0),
        "title": canvas.get("title", "未命名画布"),
        "icon": canvas.get("icon", "layers"),
        "kind": canvas.get("kind", "classic"),
    }
```

4. `update_canvas()` 保存 `settings`，保存后广播：

当前：

```python
    canvas["logs"] = payload.logs[-500:]
    store.save_canvas(canvas)
    return {"canvas": canvas}
```

改成：

```python
    canvas["logs"] = payload.logs[-500:]
    canvas["settings"] = payload.settings or {}
    if not canvas.get("kind"):
        canvas["kind"] = "classic"
    store.save_canvas(canvas)
    await ws.manager.broadcast_canvas_updated(
        canvas_id,
        int(canvas.get("updated_at") or store.now_ms()),
        payload.client_id or "",
    )
    return {"canvas": canvas}
```

### Modify: `static/canvas.html`

职责：主无限画布 UI。

修改点 1：保存 payload 增加 `client_id`，保留现有 `base_updated_at`。

当前 `saveCanvas()` 请求体位于 `static/canvas.html:1863-1867`：

```js
body:JSON.stringify({ title:canvas.title, icon:canvas.icon || '🧩', nodes, connections, viewport, logs:canvas.logs || [], base_updated_at: baseUpdatedAt })
```

改成：

```js
body:JSON.stringify({ title:canvas.title, icon:canvas.icon || '🧩', nodes, connections, viewport, logs:canvas.logs || [], settings:canvas.settings || {}, client_id:CLIENT_ID, base_updated_at: baseUpdatedAt })
```

修改点 2：Output 右键菜单加入下载入口。

当前 `openOutputNodeMenu()` 位于 `static/canvas.html:3301-3329`，菜单只包含：

```js
const imageCount = outputImageUrls(node).length;
outputNodeMenu.innerHTML = `
    <button class="menu-btn" data-output-convert="${escapeAttr(nodeId)}" ${imageCount ? '' : 'disabled'}><i data-lucide="replace" class="w-4 h-4"></i><span>${tr('canvas.outputConvertToInputGroup')}</span></button>
    <button class="menu-btn" data-output-copy="${escapeAttr(nodeId)}" ${imageCount ? '' : 'disabled'}><i data-lucide="copy-plus" class="w-4 h-4"></i><span>${tr('canvas.outputCopyToInputGroup')}</span></button>
`;
```

改成：

```js
const imageCount = outputImageUrls(node).length;
const downloadableCount = outputDownloadableImageUrls(node).length;
outputNodeMenu.innerHTML = `
    <div class="menu-section-title">${tr('canvas.outputGroupActions')}</div>
    <button class="menu-btn" data-output-convert="${escapeAttr(nodeId)}" ${imageCount ? '' : 'disabled'}><i data-lucide="replace" class="w-4 h-4"></i><span>${tr('canvas.outputConvertToInputGroup')}</span></button>
    <button class="menu-btn" data-output-copy="${escapeAttr(nodeId)}" ${imageCount ? '' : 'disabled'}><i data-lucide="copy-plus" class="w-4 h-4"></i><span>${tr('canvas.outputCopyToInputGroup')}</span></button>
    <div class="menu-divider"></div>
    <div class="menu-section-title">${tr('canvas.outputFileActions')}</div>
    <button class="menu-btn" data-output-download="${escapeAttr(nodeId)}" ${downloadableCount ? '' : 'disabled'}><i data-lucide="download" class="w-4 h-4"></i><span>${tr('canvas.outputDownloadAllImages')}</span></button>
`;
```

在 copyBtn 绑定后加入：

```js
const downloadBtn = outputNodeMenu.querySelector('[data-output-download]');
if(downloadBtn){
    downloadBtn.onclick = e => {
        e.stopPropagation();
        downloadOutputNodeImages(nodeId);
        closeOutputNodeMenu();
    };
}
```

修改点 3：下载 zip 文件名使用画布标题，减少多个 Output 下载时难分辨。

当前 `downloadOutputNodeImages()` 位于 `static/canvas.html:3237-3263`。

将 body 的 filename 和 link download 改为同一个安全短文件名：

```js
const zipName = `${(canvas?.title || 'canvas-output').slice(0, 48)}-${node?.id || 'output'}.zip`;
```

函数改成：

```js
async function downloadOutputNodeImages(nodeId){
    const node = nodes.find(n => n.id === nodeId);
    const urls = outputDownloadableImageUrls(node);
    if(!node || !urls.length){
        alert(tr('canvas.outputDownloadEmpty'));
        return;
    }
    const zipName = `${(canvas?.title || 'canvas-output').slice(0, 48)}-${node.id}.zip`;
    try {
        const res = await fetch('/api/canvas-assets/download', {
            method:'POST',
            headers:{'Content-Type':'application/json'},
            body:JSON.stringify({urls, filename:zipName})
        });
        if(!res.ok) throw new Error(await responseErrorMessage(res, tr('canvas.outputDownloadEmpty')));
        const blob = await res.blob();
        const a = document.createElement('a');
        const href = URL.createObjectURL(blob);
        a.href = href;
        a.download = zipName;
        document.body.appendChild(a);
        a.click();
        a.remove();
        setTimeout(() => URL.revokeObjectURL(href), 1200);
    } catch(err) {
        alert(err.message || tr('canvas.outputDownloadEmpty'));
    }
}
```

修改点 4：缺失文件中文文案从 `????` 改为 i18n。

当前 `static/canvas.html:7560-7562`：

```js
function missingAssetHtml(url, compact=false){
    return `<div class="missing-asset ${compact ? 'compact' : ''}" title="${escapeAttr(url || '')}"><i data-lucide="image-off" class="${compact ? 'w-4 h-4' : 'w-6 h-6'}"></i><span>${langIsEn() ? 'Missing file' : '????'}</span></div>`;
}
```

改成：

```js
function missingAssetHtml(url, compact=false){
    return `<div class="missing-asset ${compact ? 'compact' : ''}" title="${escapeAttr(url || '')}"><i data-lucide="image-off" class="${compact ? 'w-4 h-4' : 'w-6 h-6'}"></i><span>${tr('canvas.missingFile')}</span></div>`;
}
```

### Modify: `static/i18n.js`

职责：多语言字典。只补主画布需要的 key，不导入 Smart/LTX/RunningHub。

在 zh 的 canvas key 区域，`canvas.outputConvertToInputGroup` / `canvas.outputCopyToInputGroup` 附近加入：

```js
'canvas.outputGroupActions': '输出组',
'canvas.outputFileActions': '文件',
'canvas.outputDownloadAllImages': '下载全部图片',
'canvas.outputDownloadEmpty': '没有可下载的本地图片',
'canvas.missingFile': '文件缺失',
```

在 en 的 canvas key 区域加入：

```js
'canvas.outputGroupActions': 'Output group',
'canvas.outputFileActions': 'Files',
'canvas.outputDownloadAllImages': 'Download all images',
'canvas.outputDownloadEmpty': 'No local images to download',
'canvas.missingFile': 'Missing file',
```

### Modify: `tests/smoke_refactored_app.py`

职责：确认 refactored app 仍注册关键路由。

在 `EXPECTED_PATHS` 中加入：

```python
    "/api/canvases/{canvas_id}/meta",
    "/api/canvas-assets/check",
    "/api/canvas-assets/download",
```

注意：FastAPI route path 对 path param 使用 `{canvas_id}`，不是实际 ID。

### Create: `tests/test_canvas_sync_contract.py`

职责：不用启动真实服务器，直接验证后端契约：

- `CanvasSaveRequest` 接受 `client_id` 和 `settings`。
- `ConnectionManager.broadcast_canvas_updated()` 发送正确 JSON。

文件内容：

```python
import json

from app.models import CanvasSaveRequest
from app.ws import ConnectionManager


class DummySocket:
    def __init__(self):
        self.messages = []

    async def send_text(self, message):
        self.messages.append(message)


def test_canvas_save_request_accepts_client_id_and_settings():
    payload = CanvasSaveRequest(
        title="demo",
        icon="layers",
        nodes=[],
        connections=[],
        viewport={},
        logs=[],
        settings={"snap": True},
        base_updated_at=123,
        client_id="canvas_abc",
    )

    assert payload.settings == {"snap": True}
    assert payload.client_id == "canvas_abc"


async def test_broadcast_canvas_updated_message():
    manager = ConnectionManager()
    socket = DummySocket()
    manager.active_connections.append(socket)

    await manager.broadcast_canvas_updated("canvas1", 456, "canvas_abc")

    assert len(socket.messages) == 1
    assert json.loads(socket.messages[0]) == {
        "type": "canvas_updated",
        "canvas_id": "canvas1",
        "updated_at": 456,
        "client_id": "canvas_abc",
    }
```

If the repo does not have pytest async plugin installed, replace the async test with `asyncio.run(...)`:

```python
def test_broadcast_canvas_updated_message():
    async def run():
        manager = ConnectionManager()
        socket = DummySocket()
        manager.active_connections.append(socket)
        await manager.broadcast_canvas_updated("canvas1", 456, "canvas_abc")
        return socket.messages

    messages = asyncio.run(run())
    assert len(messages) == 1
    assert json.loads(messages[0]) == {
        "type": "canvas_updated",
        "canvas_id": "canvas1",
        "updated_at": 456,
        "client_id": "canvas_abc",
    }
```

Use the `asyncio.run` version if no async pytest support is present.

## 4. Implementation tasks

### Task 1: Add canvas update broadcast backend contract

**Files:**

- Modify: `app/ws.py:64-80`
- Modify: `app/models.py:177-198`
- Modify: `app/store.py:180-225`
- Modify: `app/routes/canvas.py:7-61`
- Create: `tests/test_canvas_sync_contract.py`
- Modify: `tests/smoke_refactored_app.py:21-45`

- [ ] **Step 1: Create backend contract tests**

Create `tests/test_canvas_sync_contract.py` with the `asyncio.run` version to avoid requiring pytest async plugins:

```python
import asyncio
import json

from app.models import CanvasSaveRequest
from app.ws import ConnectionManager


class DummySocket:
    def __init__(self):
        self.messages = []

    async def send_text(self, message):
        self.messages.append(message)


def test_canvas_save_request_accepts_client_id_and_settings():
    payload = CanvasSaveRequest(
        title="demo",
        icon="layers",
        nodes=[],
        connections=[],
        viewport={},
        logs=[],
        settings={"snap": True},
        base_updated_at=123,
        client_id="canvas_abc",
    )

    assert payload.settings == {"snap": True}
    assert payload.client_id == "canvas_abc"


def test_broadcast_canvas_updated_message():
    async def run():
        manager = ConnectionManager()
        socket = DummySocket()
        manager.active_connections.append(socket)
        await manager.broadcast_canvas_updated("canvas1", 456, "canvas_abc")
        return socket.messages

    messages = asyncio.run(run())

    assert len(messages) == 1
    assert json.loads(messages[0]) == {
        "type": "canvas_updated",
        "canvas_id": "canvas1",
        "updated_at": 456,
        "client_id": "canvas_abc",
    }
```

- [ ] **Step 2: Run test and verify it fails**

Run:

```bash
python -m pytest tests/test_canvas_sync_contract.py -v
```

Expected before implementation:

- `test_canvas_save_request_accepts_client_id_and_settings` may fail because `CanvasSaveRequest` lacks `settings` / `client_id`.
- `test_broadcast_canvas_updated_message` fails because `ConnectionManager` lacks `broadcast_canvas_updated`.

- [ ] **Step 3: Add `broadcast_canvas_updated()` to `app/ws.py`**

Insert inside `ConnectionManager`, after `broadcast_new_image()`:

```python
    async def broadcast_canvas_updated(self, canvas_id: str, updated_at: int, client_id: str = ""):
        data = json.dumps({
            "type": "canvas_updated",
            "canvas_id": canvas_id,
            "updated_at": updated_at,
            "client_id": client_id or "",
        })
        for connection in self.active_connections[:]:
            try:
                await connection.send_text(data)
            except Exception as e:
                print(f"Broadcast canvas update error: {e}")
                self.active_connections.remove(connection)
```

- [ ] **Step 4: Extend `CanvasCreateRequest` and `CanvasSaveRequest` in `app/models.py`**

Replace the two classes with:

```python
class CanvasCreateRequest(BaseModel):
    title: str = "未命名画布"
    icon: str = "🧩"
    kind: str = "classic"


class CanvasSaveRequest(BaseModel):
    title: str = "未命名画布"
    icon: str = "🧩"
    nodes: List[Dict[str, Any]] = []
    connections: List[Dict[str, Any]] = []
    viewport: Dict[str, Any] = {}
    logs: List[Dict[str, Any]] = []
    settings: Dict[str, Any] = {}
    base_updated_at: Optional[int] = None
    client_id: Optional[str] = None
```

- [ ] **Step 5: Preserve classic canvas shape in `app/store.py`**

Change `new_canvas()` to:

```python
def new_canvas(title: str = "未命名画布", icon: str = "layers", kind: str = "classic") -> dict:
    timestamp = now_ms()
    canvas = {
        "id": uuid.uuid4().hex,
        "title": (title or "未命名画布")[:80],
        "icon": (icon or "🧩")[:32],
        "kind": "classic",
        "created_at": timestamp,
        "updated_at": timestamp,
        "nodes": [],
        "connections": [],
        "viewport": {"x": 0, "y": 0, "scale": 1},
        "logs": [],
        "settings": {},
    }
    save_canvas(canvas)
    return canvas
```

Change `canvas_record()` to include `kind`:

```python
def canvas_record(data: dict) -> dict:
    return {
        "id": data.get("id"),
        "title": data.get("title", "未命名画布"),
        "icon": data.get("icon", "🧩"),
        "kind": data.get("kind", "classic"),
        "created_at": data.get("created_at", 0),
        "updated_at": data.get("updated_at", 0),
        "deleted_at": data.get("deleted_at", 0),
        "node_count": len(data.get("nodes", [])),
    }
```

- [ ] **Step 6: Wire broadcast into `app/routes/canvas.py`**

Change import:

```python
from .. import store, ws
```

Change `create_canvas()`:

```python
@router.post("/api/canvases")
async def create_canvas(payload: CanvasCreateRequest):
    return {"canvas": store.new_canvas(payload.title, payload.icon, payload.kind)}
```

Change `get_canvas_meta()` to return `kind`:

```python
@router.get("/api/canvases/{canvas_id}/meta")
async def get_canvas_meta(canvas_id: str):
    canvas = store.load_canvas(canvas_id)
    return {
        "id": canvas.get("id"),
        "updated_at": canvas.get("updated_at", 0),
        "title": canvas.get("title", "未命名画布"),
        "icon": canvas.get("icon", "layers"),
        "kind": canvas.get("kind", "classic"),
    }
```

Change tail of `update_canvas()`:

```python
    canvas["logs"] = payload.logs[-500:]
    canvas["settings"] = payload.settings or {}
    if not canvas.get("kind"):
        canvas["kind"] = "classic"
    store.save_canvas(canvas)
    await ws.manager.broadcast_canvas_updated(
        canvas_id,
        int(canvas.get("updated_at") or store.now_ms()),
        payload.client_id or "",
    )
    return {"canvas": canvas}
```

- [ ] **Step 7: Add smoke route coverage**

In `tests/smoke_refactored_app.py`, add these strings to `EXPECTED_PATHS`:

```python
    "/api/canvases/{canvas_id}/meta",
    "/api/canvas-assets/check",
    "/api/canvas-assets/download",
```

- [ ] **Step 8: Run backend tests**

Run:

```bash
python -m pytest tests/test_canvas_sync_contract.py -v
python tests/smoke_refactored_app.py
```

Expected:

- pytest: 2 passed.
- smoke: JSON has `"ok": true` and `missing_paths: []`.

### Task 2: Restore Output menu download action and i18n

**Files:**

- Modify: `static/canvas.html:1863-1867`
- Modify: `static/canvas.html:3237-3263`
- Modify: `static/canvas.html:3301-3329`
- Modify: `static/canvas.html:7560-7562`
- Modify: `static/i18n.js`

- [ ] **Step 1: Add `client_id` to canvas save payload**

In `static/canvas.html`, replace:

```js
body:JSON.stringify({ title:canvas.title, icon:canvas.icon || '🧩', nodes, connections, viewport, logs:canvas.logs || [], base_updated_at: baseUpdatedAt })
```

with:

```js
body:JSON.stringify({ title:canvas.title, icon:canvas.icon || '🧩', nodes, connections, viewport, logs:canvas.logs || [], settings:canvas.settings || {}, client_id:CLIENT_ID, base_updated_at: baseUpdatedAt })
```

- [ ] **Step 2: Update `downloadOutputNodeImages()`**

Replace the whole function at `static/canvas.html:3237-3263` with:

```js
async function downloadOutputNodeImages(nodeId){
    const node = nodes.find(n => n.id === nodeId);
    const urls = outputDownloadableImageUrls(node);
    if(!node || !urls.length){
        alert(tr('canvas.outputDownloadEmpty'));
        return;
    }
    const zipName = `${(canvas?.title || 'canvas-output').slice(0, 48)}-${node.id}.zip`;
    try {
        const res = await fetch('/api/canvas-assets/download', {
            method:'POST',
            headers:{'Content-Type':'application/json'},
            body:JSON.stringify({urls, filename:zipName})
        });
        if(!res.ok) throw new Error(await responseErrorMessage(res, tr('canvas.outputDownloadEmpty')));
        const blob = await res.blob();
        const a = document.createElement('a');
        const href = URL.createObjectURL(blob);
        a.href = href;
        a.download = zipName;
        document.body.appendChild(a);
        a.click();
        a.remove();
        setTimeout(() => URL.revokeObjectURL(href), 1200);
    } catch(err) {
        alert(err.message || tr('canvas.outputDownloadEmpty'));
    }
}
```

- [ ] **Step 3: Add download button to `openOutputNodeMenu()`**

In `static/canvas.html:3301-3329`, change the menu body to include `downloadableCount`, section labels, divider, and download button:

```js
const imageCount = outputImageUrls(node).length;
const downloadableCount = outputDownloadableImageUrls(node).length;
outputNodeMenu.innerHTML = `
    <div class="menu-section-title">${tr('canvas.outputGroupActions')}</div>
    <button class="menu-btn" data-output-convert="${escapeAttr(nodeId)}" ${imageCount ? '' : 'disabled'}><i data-lucide="replace" class="w-4 h-4"></i><span>${tr('canvas.outputConvertToInputGroup')}</span></button>
    <button class="menu-btn" data-output-copy="${escapeAttr(nodeId)}" ${imageCount ? '' : 'disabled'}><i data-lucide="copy-plus" class="w-4 h-4"></i><span>${tr('canvas.outputCopyToInputGroup')}</span></button>
    <div class="menu-divider"></div>
    <div class="menu-section-title">${tr('canvas.outputFileActions')}</div>
    <button class="menu-btn" data-output-download="${escapeAttr(nodeId)}" ${downloadableCount ? '' : 'disabled'}><i data-lucide="download" class="w-4 h-4"></i><span>${tr('canvas.outputDownloadAllImages')}</span></button>
`;
```

After copy button handler, add:

```js
const downloadBtn = outputNodeMenu.querySelector('[data-output-download]');
if(downloadBtn){
    downloadBtn.onclick = e => {
        e.stopPropagation();
        downloadOutputNodeImages(nodeId);
        closeOutputNodeMenu();
    };
}
```

- [ ] **Step 4: Replace missing file fallback**

Replace `missingAssetHtml()` with:

```js
function missingAssetHtml(url, compact=false){
    return `<div class="missing-asset ${compact ? 'compact' : ''}" title="${escapeAttr(url || '')}"><i data-lucide="image-off" class="${compact ? 'w-4 h-4' : 'w-6 h-6'}"></i><span>${tr('canvas.missingFile')}</span></div>`;
}
```

- [ ] **Step 5: Add zh i18n keys**

In `static/i18n.js`, near existing `canvas.outputConvertToInputGroup` / `canvas.outputCopyToInputGroup`, add:

```js
'canvas.outputGroupActions': '输出组',
'canvas.outputFileActions': '文件',
'canvas.outputDownloadAllImages': '下载全部图片',
'canvas.outputDownloadEmpty': '没有可下载的本地图片',
'canvas.missingFile': '文件缺失',
```

- [ ] **Step 6: Add en i18n keys**

In the English dictionary, add:

```js
'canvas.outputGroupActions': 'Output group',
'canvas.outputFileActions': 'Files',
'canvas.outputDownloadAllImages': 'Download all images',
'canvas.outputDownloadEmpty': 'No local images to download',
'canvas.missingFile': 'Missing file',
```

- [ ] **Step 7: Run frontend syntax check**

Run:

```bash
node -e "const c=require('fs').readFileSync('static/canvas.html','utf8'); const re=/<script(?![^>]*\bsrc\b)[^>]*>([\s\S]*?)<\/script>/g; let m,total=0,errors=0; while((m=re.exec(c))){const body=m[1]; if(!body.trim()) continue; total+=body.length; try{new Function(body);}catch(e){errors++; console.error(e.message);}} console.log('inline JS bytes:',total,'errors:',errors); process.exit(errors ? 1 : 0);"
```

Expected: `errors: 0`.

### Task 3: Full validation and documentation update

**Files:**

- Modify: `doc/upstream-sync-playbook.md`

- [ ] **Step 1: Run backend validation**

Run:

```bash
python -c "import ast; [ast.parse(open(f, encoding='utf-8').read()) for f in ['app/routes/canvas.py','app/routes/public.py','app/routes/generate.py','app/models.py','app/ws.py','main_refactored.py']]; print('OK')"
python tests/smoke_refactored_app.py
python -m pytest tests/test_canvas_sync_contract.py -v
python tests/unit_seedream_seedance.py
```

Expected:

- AST prints `OK`.
- smoke reports `"ok": true`.
- pytest passes.
- Seedream/Seedance tests pass.

- [ ] **Step 2: Run frontend syntax validation**

Run the node inline script check from Task 2 Step 7.

Expected: `errors: 0`.

- [ ] **Step 3: Manual browser validation**

Start the app with the project’s normal command, then verify:

1. Open main infinite canvas.
2. Create a classic canvas.
3. Add an Output node with at least one `/output/` or `/assets/` image.
4. Right-click Output node.
5. Confirm menu shows:
   - 输出组 / Output group
   - 转为输入组
   - 复制为新的输入组
   - 文件 / Files
   - 下载全部图片
6. Click “下载全部图片”; browser should download a zip.
7. Open the same canvas in two browser tabs.
8. Save in tab A; tab B should update via websocket or polling without overwriting active local edits.
9. Temporarily remove or rename one local output image; reload canvas and confirm missing placeholder says “文件缺失”, not `????`.
10. Add/execute a Seedream or Seedance node enough to confirm its UI still renders and routes are not broken.

- [ ] **Step 4: Update sync playbook log**

Append to `doc/upstream-sync-playbook.md` under sync log:

```markdown
### 2026-05-23 — 主无限画布范围同步计划与小修

上游最新 commit：`3b42541 Merge pull request #23 from xiaohongai/feature/ltx-director`（2026-05-23 10:14 +0800）。

本轮用户明确排除：
- Smart Canvas
- LTX Director
- RunningHub
- 自更新系统
- asset-library 素材库
- ComfyUI 专属 workflow 绑定

拉取/修复：
- 主画布保存增加 `client_id`，后端保存后通过 WebSocket 广播 `canvas_updated`，补齐前端已存在的多 tab 同步接收逻辑。
- Output 右键菜单恢复“下载全部图片”入口，复用已存在的 `/api/canvas-assets/download`。
- 补齐主画布 Output 下载与缺失素材显示 i18n key。
- 修复缺失素材中文 fallback 从 `????` 到 `文件缺失`。
- smoke test 增加 canvas-assets 路由覆盖。

跳过：
- 上述所有被明确排除功能。

验证：
- `<填写实际运行结果>`
```

Do not leave `<填写实际运行结果>` in the committed version; replace with actual commands and outcomes.

## 5. Commit plan

Only commit if the user explicitly asks for commits.

Recommended commit split if committing:

1. `sync(upstream): 2026-05-23 主画布同步计划`
   - `doc/upstream-sync-plan-2026-05-23.md`

2. `sync(upstream): restore canvas update broadcast`
   - `app/ws.py`
   - `app/models.py`
   - `app/store.py`
   - `app/routes/canvas.py`
   - `tests/test_canvas_sync_contract.py`
   - `tests/smoke_refactored_app.py`

3. `sync(upstream): restore output download menu`
   - `static/canvas.html`
   - `static/i18n.js`

4. `doc: record 2026-05-23 upstream sync scope`
   - `doc/upstream-sync-playbook.md`

## 6. Self-review checklist

- [ ] No Smart Canvas files or `smart.*` keys added.
- [ ] No LTX files, `ltxDirector`, or `canvas.ltx*` keys added.
- [ ] No RunningHub provider/model/logo/API code added.
- [ ] No app update endpoints added.
- [ ] No `asset-library` endpoints added.
- [ ] No CDN paths restored.
- [ ] Seedream/Seedance references in `static/canvas.html`, `app/routes/generate.py`, and tests remain intact.
- [ ] `static/i18n.js` only gets main canvas keys.
- [ ] `app/routes/canvas.py` broadcasts after `store.save_canvas()`, so `updated_at` is current.
- [ ] `client_id` is included in save payload and broadcast response.
- [ ] Output menu displays download action only when local downloadable images exist.
