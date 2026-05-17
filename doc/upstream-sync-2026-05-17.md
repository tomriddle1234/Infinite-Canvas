# 上游同步差异报告 — hero8152/Infinite-Canvas (2026-05-17)

> 仅作分析，**未改动任何代码**。读完后再决定拉哪些。

## 0. 仓库关系

| 项 | 值 |
|---|---|
| 本仓库 | `tomriddle1234/Infinite-Canvas` (你的 fork) |
| 上游 | `hero8152/Infinite-Canvas` (`upstream/main`，已 fetch 进本仓库) |
| 共同 git 历史 | **无**（两边都有独立 Initial commit），无法用 merge/rebase/cherry-pick |
| 上游最新 push | 2026-05-17 01:25 UTC |
| `C:\src\Original-Infinite-Canvas` | 实际是从你 fork clone 的，**不是上游** |

操作方式：基于文件内容对比、手工移植。

---

## 1. 你的本地改动总览（保留方向，不要被回退）

来自你 commits `81cb30f` → `ba37088`：

1. **后端重构** — `main.py` 拆分进 `app/` 包（`routes/`、`providers.py`、`store.py`、`models.py`、`config.py`、`factory.py`、`ws.py`、`comfyui.py`、`imageproc.py`、`upstream.py`）。`main_refactored.py` 是新入口。
2. **去 CDN** — `static/vendor/` 本地化 Tailwind / Lucide / Three.js / 字体。所有 `static/*.html` 的 `<script src="https://cdn...">` 都被改为 `/static/vendor/...`。
3. **去嵌入式 Python** — 删除 `python/`、`packages/`、`get-pip.py`、`安装依赖.bat`、`说明.png`、`运行说明.txt`、mac `.sh` 脚本。`run.bat` 改为 activate miniforge `OFX_dev` env。
4. **Seedream / Seedance ARK API** — `app/upstream_volcengine.py`、`app/upstream_openai_image.py`、新增路由 `/api/nodes/seedance`、`/api/nodes/seedance/status`、`/api/nodes/seedream`、`/api/nodes/gpt-image-2`。
5. **新增** — `app/routes` 中还添加了 `/api/first-party-keys`（GET/PUT）、`/api/nodes/media-upload`。
6. **文档/工程** — `AGENTS.md`、`.agents/`、`doc/go-migration/`、`doc/volcengine-openai-nodes-plan.md`、`tests/smoke_refactored_app.py`、`tests/unit_seedream_seedance.py`、`requirements.txt` 现代化、`.gitignore`。
7. **README** — 重写为项目说明（去广告）。

---

## 2. 上游 5/15 - 5/17 的真正新增

按"功能 × 上游位置 × 移植目标 × 后端依赖 × 工作量 × 是否值得拉"维度列出。

### A. 历史图批量管理（**全新文件**）— `static/history-bulk-manager.js`

| 字段 | 值 |
|---|---|
| 上游文件 | `static/history-bulk-manager.js`（260 行，纯前端 IIFE） |
| 上游引用页 | `enhance.html`、`zimage.html`、`angle.html`、`online.html`、`klein.html`（在 `<script src="/static/theme.js"></script>` 之后插入） |
| 你目前是否有 | ❌ 完全没有 |
| 后端依赖 | 仅调用已有的 `/api/history` 和 `/api/history/delete`（你 `app/routes/public.py` 已有） |
| 移植步骤 | 1) 把 `history-bulk-manager.js` 整文件拷到 `static/`；2) 在上述 5 个 HTML 的 head 里加 `<script src="/static/history-bulk-manager.js?v=20260517"></script>`（位置在 theme.js / i18n.js 后） |
| 工作量 | 低（~10 分钟） |
| 是否值得 | ✅ 推荐 — 实用、无侵入、无后端改动 |
| 注意 | 上游脚本路径写的是 `?v=20260517`，cache-bust 不影响功能 |

### B. 大图预览模态（**全新文件**）— `static/image-preview.js`

| 字段 | 值 |
|---|---|
| 上游文件 | `static/image-preview.js`（149 行，纯前端 IIFE，挂 `window.StudioPreview`） |
| 上游引用页 | 同上 5 个 HTML |
| 你目前是否有 | ❌ 没有 |
| 后端依赖 | 无 |
| 移植步骤 | 拷文件 + 在同样的 HTML 中追加 `<script src="/static/image-preview.js?v=20260516"></script>` |
| 工作量 | 低（~5 分钟） |
| 是否值得 | ✅ 推荐 |

### C. 循环节点：图片输入批次模式（**canvas.html 大改动**）— 5/15 README 主打功能

| 字段 | 值 |
|---|---|
| 上游位置 | `canvas.html` 4240-4310 行的 `loop-toggle-row`、`loop-image-panel`；5488-5489 行 `type:'loopImage'` 节点；6145-6209 行的批次/循环执行逻辑 |
| 你目前是否有 | ❌ 你已有"提示词循环"，没有"图片循环 + 批次" |
| 新 i18n keys（已在我的 i18n.js 缺失）| `canvas.loopImageToggle`、`canvas.loopImageStart`、`canvas.loopBatchSize`、`canvas.loopImageWillOutput`、`canvas.loopImageEmpty`、`canvas.loopImageLabel`、`canvas.outputConvertToInputGroup`、`canvas.outputCopyToInputGroup`（zh + en 各 8 个） |
| 后端依赖 | **依赖新的异步任务接口（见下 §D）**——循环开 N 路并发时，前端走 `/api/canvas-image-tasks` 提交、轮询 `/api/canvas-image-tasks/{task_id}`，而不是阻塞式 `/api/online-image` |
| 移植难度 | **高** — 移植 8 处 canvas.html 代码块 + 整个并发调度逻辑 + 后端配套（§D）|
| 工作量 | 中-高（~半天到 1 天，含验证） |
| 是否值得 | 取决于你是否需要并发批量生成。这是上游的主推功能，但代码量大且耦合循环节点 |

### D. 异步画布图任务系统（**后端 + 前端**）— 配合 §C 必备

| 字段 | 值 |
|---|---|
| 上游后端 | `main.py` 新增 `CANVAS_TASKS` dict + `CANVAS_TASK_LOCK` + `async def run_canvas_image_task` + `POST /api/canvas-image-tasks` + `GET /api/canvas-image-tasks/{task_id}` |
| 上游前端 | `canvas.html` 6558-6620 行轮询逻辑 |
| 你目前是否有 | ❌ 你只有阻塞式 `/api/online-image` |
| 移植目标 | `app/routes/generate.py`（与 online-image 同模块），可能需要在 `app/store.py` 或单独 `app/canvas_tasks.py` 放 `CANVAS_TASKS` 状态 |
| 移植难度 | 中 — 逻辑独立，但是要复用你已有的 `build_online_image_result` 等价物（你拆分到 `app/upstream.py` / `app/providers.py`）|
| 工作量 | 中（~半天） |
| 是否值得 | 仅当你要 §C 才值得；否则不拉 |

### E. 画布元数据轻量端点（**后端**）— 配合多 tab 防覆盖

| 字段 | 值 |
|---|---|
| 上游后端 | `main.py` 新增 `GET /api/canvases/{canvas_id}/meta`（只返回 `id/updated_at/title/icon`） |
| 上游前端 | `canvas.html` 2023 行：周期性 fetch meta，比对 `updated_at`，发现别处更新就刷新 |
| 你目前是否有 | ❌ 你的 `app/routes/canvas.py` 没有 `/meta` |
| 移植目标 | `app/routes/canvas.py` 新增 1 个 endpoint，~10 行 |
| 工作量 | 低 |
| 是否值得 | ✅ 推荐 — 防多 tab/多窗口画布覆盖，独立小特性 |
| 注意 | 上游 `PUT /api/canvases/{canvas_id}` 还增加了 `base_updated_at` 冲突检测（409）。你的 `update_canvas` 没有这段。一起拉 |

### F. "获取API"affiliate 推广按钮 — `static/api-settings.html`

| 字段 | 值 |
|---|---|
| 上游内容 | 一个 `<a href="https://apimart.ai/register?aff=1uyAbb">` 推广链接 + 对应 CSS + i18n `api.getApi` |
| 是否值得 | ❌ **不建议** — 这是原作者的 affiliate 引流，对你没价值，你的 README 重写时也已经移除了广告 |

### G. theme.js 小改动

| 字段 | 值 |
|---|---|
| 差异 | 上游版本在切换主题时**不再**给 `<html>` 加 `theme-dark` class（只保留 body 上的）；上游版本号 `v=20260516-theme-sync`，你是 `v=20260509` |
| 是否值得 | 边界小修。如果跨页面 dark mode 同步出过问题再拉；目前没观察到症状可以不拉 |

### H. 各页面 HTML 头部脚本顺序变化

上游 HTML 们把 `image-preview.js` 和 `history-bulk-manager.js` 加在 `i18n.js` 之后。你需要在所有这 5 个 HTML 同位置插入两行 `<script>`，但**绝对不要**把 `<script src="/static/vendor/...">` 换回 CDN（这是你的 vendor 化成果）。

### I. enhance.html / zimage.html / online.html / klein.html / angle.html 内部逻辑

`enhance.html` 和 `zimage.html` 分别有 1827 / 1203 行 diff，大部分其实是 CSS 微调 + bulk-manager 钩子（line 486 `if(document.body.classList.contains('history-bulk-selecting'))` 之类的小判断）。**单独看**：

| 文件 | 主要新增 | 移植 |
|---|---|---|
| `enhance.html` | bulk-selecting 判断 + 预览 hook（依赖 §A §B） | 拉 §A §B 之后，把 click 处理改 3-5 行 |
| `zimage.html` | 同上 | 同上 |
| `online.html` | 同上 + 少量 UI 调整 | 同上 |
| `klein.html` | 主要也是 hook | 同上 |
| `angle.html` | 主要也是 hook | 同上 |

### J. comfyui-settings.html / gpt-chat.html / index.html

`gpt-chat.html` 19 行差异、`comfyui-settings.html` 17 行差异 — 多数是 CDN 切换造成的，没有功能性差异。`index.html` 121 行差异，需要单独 review，但目测也是登录/欢迎页样式微调。

### K. main.py 后端其它差异

我对比了 endpoint 列表：除了 §D § E 提到的，上游 `main.py` 与你 `app/routes/` 端点是**对齐的**。你**多出来**的：`/api/nodes/{gpt-image-2, seedance, seedance/status, seedream}`、`/api/first-party-keys`、`/api/nodes/media-upload`。上游没有 — 这些是你的成果，不会被覆盖。

---

## 3. 推荐拉取顺序

按"高价值低风险"排序：

1. **§A 历史批量管理** + **§B 大图预览** — 两个独立 JS + 5 个 HTML 各加 2 行 script。零后端改动，立竿见影。
2. **§E 画布 meta 端点 + 冲突检测** — 后端 1 路由 + 1 函数加固，前端 1 个 fetch 即可。
3. **§D 异步任务系统** — 仅当你计划做 §C。
4. **§C 循环图片批次** — 工作量最大，且只对"批量并发生成"用户有意义。建议**先拉 1-2-3**，跑顺再单独评估 4。

**不建议拉**：§F affiliate 按钮、§J 美化 diff、§G theme.js 小改（除非有 bug）。

---

## 4. 下一步动作

按你之前的回答："新建合并分支" + "先列差异不动代码"——

- 这份报告交给你了。
- 等你点头后，我会从 `main` 切出 `sync-upstream-2026-05-17` 分支，按你勾选的项目逐个移植，**每个功能一个 commit**，便于你回滚单个特性。
- 移植过程中**保留**所有你的 vendor 化、app/ 重构、Seedream/Seedance、AGENTS.md 等改动不动。
