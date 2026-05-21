# 代码审查计划 — 2026-05-21

> 审查范围：`6092f66 Migrate upper stream` 合并后到当前工作区的全部变更。
> 触发原因：合并上游 5/17-5/20 更新后引入多个 regressions，用户报告 Seedance/Seedream
> 轮询时持续 `render()` 全量重建 DOM，导致 textarea/input 焦点丢失，严重干扰用户工作。

## 审查文件

| 文件 | 变更量 | 说明 |
|------|--------|------|
| `static/canvas.html` | +839/-143 | 工具栏折叠、小地图、画笔增强、级联重构、画布素材管理 |
| `app/upstream.py` | +293 | VEO3.1 图片上传、视频图片校验、模型拉取 helper |
| `app/routes/generate.py` | +85/-? | 视频错误信息改进、Seedream/Seedance 端点 |
| `app/routes/provider.py` | +9 | `image_generation_endpoint` / `image_edit_endpoint` / `clear_key` |
| `app/routes/public.py` | +54 | `canvas-assets/check` + `canvas-assets/download` |
| `app/models.py` | +13 | `CanvasAssetCheckRequest` / `CanvasAssetDownloadRequest` / `random_enabled` |
| `app/providers.py` | +36 | `get_api_provider_exact` |
| `app/config.py` | +6 | 视频/图片 endpoint 配置 |
| `static/api-settings.html` | +111/-? | provider 表单字段扩展 |
| `static/comfyui-settings.html` | +105 | ComfyUI random 字段 |
| `static/theme.css` | +14 | update-available 样式 |

---

## 发现的问题

### 🔴 Issue 1 — `refreshNodes()` 函数未定义 (CRITICAL)

**根因**：上游定义了 `refreshNodes(ids=[])` 用于增量更新指定节点的 DOM，`6092f66` 只移植了调用点，遗漏了函数定义。

**影响**：24 处调用点在运行时会抛 `ReferenceError`，级联运行（一键运行/循环节点）全部崩溃。

**调用点分布**：
- `runNodeCascade` 并行循环路径（~15 处）
- `runNodeCascade` 串行循环路径（~4 处）
- `runOneCascadePass` / `retryNodeAndDownstream` / `cancelCascade`（~4 处）
- Output 节点删除按钮（1 处）

**修复**：从上游移植 `refreshNodes()` + `refreshRunNodes()` 函数体，插入 `render()` 定义之后。

---

### 🟡 Issue 2 — Seedance 轮询中 `render()` 全量重建 DOM 导致焦点丢失 (MAJOR)

**根因**：`checkSeedanceNode` 和 `scheduleSeedancePoll` 中调用全局 `render()`，它执行
`nodesEl.innerHTML = ''` 销毁全部节点 DOM 后重建。用户正在编辑的 textarea/input 随 DOM
消灭，焦点丢失。

**轮询周期**：
- 提交 Seedance 后 `3000ms` 首查
- 后续每 `5000ms`（手动 Check）或 `8000ms`（自动轮询）复查
- Seedance 视频生成通常耗时 1-15 分钟

**影响**：在此期间用户每隔 5~8 秒被打断一次，无法正常编辑 prompt 或其他节点。

**修复**：将 Seedance 轮询路径中的 `render()` 替换为 `refreshNodes()` 增量更新（只重绘
Seedance 节点及其 output 节点）。

注意：Seedream 节点 `runDedicatedImageNode` 同样使用 `render()`，但只在开始/完成/失败
时各调一次，非持续轮询，影响相对轻微。可一并修复但不影响可用性。

---

### 🟡 Issue 3 — `runOneCascadePass` 混用新旧渲染模式 (MAJOR)

**根因**：`runOneCascadePass` 函数大量使用旧式 `render()` 全量重建，成功/完成分支用了
新式 `refreshNodes()`。

**修复**：统一改为 `refreshNodes()` 增量更新（依赖 Issue 1 修复）。

---

### 🟡 Issue 4 — `seedancePollTimers` 未在节点删除时清理 (MINOR)

**根因**：`renderSeedanceBody` 在渲染时自动启动 `scheduleSeedancePoll`，但删除节点时
没有 `clearTimeout` + `delete`。

**修复**：在 `deleteNode` 中增加定时器清理逻辑。

---

### 🟢 Issue 5 — `ws.GLOBAL_LOOP` 空值保护 (MINOR)

**根因**：`node_gpt_image_2` 和 `node_seedream` 在完成时调用
`asyncio.run_coroutine_threadsafe(ws.manager.broadcast_new_image(result), ws.GLOBAL_LOOP)`
未检查 `GLOBAL_LOOP is not None`。

**影响**：若 lifespan 尚未设置 loop 时收到请求会静默失败。

**修复**：调用前加 `if ws.GLOBAL_LOOP is not None:` 保护。

---

## 修复计划

| 顺序 | Issue | 文件 | 操作 |
|------|-------|------|------|
| 1 | Issue 1 | `static/canvas.html` | 插入 `refreshNodes()` + `refreshRunNodes()` 函数定义 |
| 2 | Issue 2 | `static/canvas.html` | 将 `checkSeedanceNode` / `scheduleSeedancePoll` 中的 `render()` 替换为 `refreshNodes()` |
| 3 | Issue 3 | `static/canvas.html` | 将 `runOneCascadePass` / `retryNodeAndDownstream` 中的 `render()` 替换为 `refreshNodes()` |
| 4 | Issue 4 | `static/canvas.html` | 在 `deleteNode` 中清理 `seedancePollTimers` |
| 5 | Issue 5 | `app/routes/generate.py` | `ws.GLOBAL_LOOP` 空值保护 |

修复完成后按上游同步 playbook §5 验证清单跑 Python 语法检查 + 内联 JS 语法检查。

---

## 用户确认修复

所有问题已列出。用户将从以下选项中选择修复范围：
- A. 修复全部问题
- B. 只修复 Critical (Issue 1)
- C. 只修复体验问题 (Issue 2)
- D. 修复 Issue 1 + 2

---

## 修复日志（2026-05-21）

用户选择了 **A. 修复全部问题**。全部修复已完成：

| Issue | 状态 | 变更摘要 |
|-------|------|----------|
| Issue 1 | ✅ 已修复 | `static/canvas.html`：在 `render()` 之后插入 `refreshNodes()` + `refreshRunNodes()` 完整的 28 行函数定义（从上游 `hero8152/Infinite-Canvas` 移植） |
| Issue 2 | ✅ 已修复 | `static/canvas.html`：`checkSeedanceNode` 4 处 `render()` → `refreshNodes([nodeId])` / `refreshNodes([nodeId, out?.id])`；`scheduleSeedancePoll` 超时路径 1 处替换 |
| Issue 3 | ✅ 已修复 | `static/canvas.html`：`runOneCascadePass` 2 处 `render()` → `refreshNodes(order)` / `refreshNodes([id])`（`retryNodeAndDownstream` 和 `cancelCascade` 已用 `refreshNodes`，无需改） |
| Issue 4 | ✅ 已修复 | `static/canvas.html`：`deleteNode` 开头增加 `seedancePollTimers` 的 `clearTimeout` + `delete` |
| Issue 5 | ❌ 误报 | `app/routes/generate.py` 中所有 6 处 `ws.GLOBAL_LOOP` 调用已带有 `if ws.GLOBAL_LOOP:` 保护，无需改动 |

修复后 `canvas.html` 的 `render()` 调用从 94 处减少：Seedance 轮询路径不再全量重建 DOM，改用 `refreshNodes()` 增量更新。

### 验证状态

当前 sandbox（AMD-WS）无 Python/Node 运行时，**需在 DAN 主机上验证**：

```powershell
# Python 语法检查
C:\Users\DAN\.conda\envs\OFX_dev\python.exe -c "import ast; [ast.parse(open(f,encoding='utf-8').read()) for f in ['app/routes/generate.py','app/upstream.py','main_refactored.py']]; print('OK')"

# JS 内联语法检查
node -e "const c=require('fs').readFileSync('static/canvas.html','utf8'); const re=/<script(?![^>]*\bsrc\b)[^>]*>([\s\S]*?)<\/script>/g; let m,errors=0; while((m=re.exec(c))){try{new Function(m[1]);}catch(e){errors++;console.error(e.message);}} console.log('errors:',errors);"
```