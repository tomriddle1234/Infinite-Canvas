# Upstream Sync Playbook — hero8152/Infinite-Canvas

> 给未来 agent 的操作手册。用户每隔一段时间会要求"把原作者的更新拉过来"。
> 你**必须**先读这份再动手 —— 仓库关系特殊，普通 git 流程行不通。

最近一次完整同步：**2026-05-17**（见末尾"同步日志"）。下次同步直接从那个时间点之后的上游 commit 开始对比即可。

---

## 1. 仓库关系（关键事实，不要重新摸索）

| 项 | 值 |
|---|---|
| 本仓库 | `tomriddle1234/Infinite-Canvas`（GitHub fork） |
| 上游 | `hero8152/Infinite-Canvas` |
| 上游默认分支 | `main` |
| **共同 git 历史** | **没有**。两边都有独立的 Initial commit |
| 上游提交风格 | 全部通过 GitHub 网页上传，每次发版 = `Delete static directory` + `Add files via upload` 两个 commit。看 commit 历史没意义，要看**文件内容**。 |

**含义**：
- ❌ `git merge upstream/main` 会引发 `refusing to merge unrelated histories`，强 merge 也会冲突满屏。**不要做**。
- ❌ `git cherry-pick <上游 sha>` 无法定位语义对等的本地 commit，**不要做**。
- ❌ `git rebase upstream/main` 直接报错。**不要做**。
- ✅ **基于文件内容**手工对比 + 移植，每个功能一个 commit。

---

## 2. 用户分叉至今的"不可回退"改动

未来 agent 看到上游某文件被你改了一大堆"看起来很有用的代码"，先停下来检查它是不是只是把这些改动**反向回退**了。下列改动用户做过很大努力，不能被上游覆盖：

### 后端
- **`main.py` 已拆分进 `app/` 包**：`app/routes/{canvas,chat,conversation,generate,provider,public,workflow}.py`、`app/{providers,store,models,config,factory,ws,comfyui,imageproc,upstream,upstream_openai_image,upstream_volcengine}.py`。上游 main.py 改动**不要**回写到 `main.py`，要找到对应的 `app/` 文件改。
- **新增端点（用户的成果，上游没有）**：`/api/nodes/seedance`、`/api/nodes/seedance/status`、`/api/nodes/seedream`、`/api/nodes/gpt-image-2`、`/api/first-party-keys`（GET/PUT）、`/api/nodes/media-upload`。**不要删**。
- **`main_refactored.py`** 是新入口（实际 `main.py` 也指向同样的 app）。

### 前端
- **去 CDN**：所有 `static/*.html` 的 `<script src="https://cdn.tailwindcss.com">` 和 `<script src="https://unpkg.com/lucide@latest">` 都换成 `/static/vendor/tailwindcss.js` / `/static/vendor/lucide.min.js`。`@import url('https://fonts.googleapis.com/...')` 换成 `@import url('/static/vendor/fonts/fonts.css')`。`three.js` 也走 `/static/vendor/three.module.js` 的 importmap。**上游的 HTML 仍在用 CDN，移植内容时不要把这些回退。**
- 上游的 `<script src="/static/vendor/...">` 路径在上游不存在，不要把上游的 vendor 路径当作"应该回退"。

### 运行/工程
- **嵌入式 Python 已删**：`python/`、`packages/`、`get-pip.py`、`安装依赖.bat`、`说明.png`、`运行说明.txt`、`mac-*.sh` 全部移除。**不要从上游拉回来**。
- **`run.bat`** 改为 activate `OFX_dev` miniforge env。
- **`AGENTS.md`** + **`.agents/`** + **`doc/`** + **`tests/smoke_refactored_app.py`** + **`tests/unit_seedream_seedance.py`** + **`.gitignore`** 都是用户的，不在上游里 —— 出现在 diff 的 "added on my side" 一栏属正常。

### 上游推广广告
- 上游 `README.md` 顶部和 `static/api-settings.html` 有 `https://apimart.ai/register?aff=1uyAbb` affiliate 链接 + `'api.getApi'` i18n key + `.api-link-btn` CSS。用户 README 已去广告。**默认不拉**这些。

---

## 3. 同步操作步骤

### 3.1 添加上游 remote 并拉取

```bash
git remote add upstream https://github.com/hero8152/Infinite-Canvas.git 2>/dev/null
git fetch upstream
```

如果 `upstream` 已存在，`add` 报错忽略。

### 3.2 看大图

```bash
# 上游今天最新一次 push 时间（决定本次同步覆盖区间）
git log upstream/main -1 --format='%cI %h %s'

# 上游和本仓库的所有文件差异
git diff --name-status upstream/main..HEAD > /tmp/upstream-diff.txt
git diff --stat upstream/main..HEAD | tail -30
```

由于无共同祖先，`A`/`D`/`M` 三类含义如下（diff 方向是 upstream → HEAD）：
- `A xxx` = 本仓库新增、上游没有（**多半是用户成果，不要回退**）。
- `D xxx` = 上游有、本仓库没有（**可能是用户主动删的（如 `python/`），也可能是上游真的新加了我们漏掉的，逐个判断**）。
- `M xxx` = 两边都有但内容不同（**核心战场**）。

### 3.3 过滤掉用户的纯增/纯删

用户的固定增删模式：

```bash
# 上游有、本仓库没有但属于"用户主动删除的旧资产"
git diff --name-status upstream/main..HEAD | grep -E '^D\s' | grep -E '(python/|packages/|get-pip\.py|mac-.*\.sh|安装依赖\.bat|说明\.png|运行说明\.txt)'
# ↑ 这些都是用户清理的，不要拉回

# 本仓库有、上游没有的，属于用户的新工程结构
git diff --name-status upstream/main..HEAD | grep -E '^A\s' | grep -E '(\.agents/|app/|doc/|tests/|static/vendor/|AGENTS\.md|main_refactored\.py|run_refactored\.bat|\.gitignore)'
# ↑ 这些是用户成果，不要被上游"覆盖回去"
```

剩下的 `M` 才是真正需要逐文件处理的目标。

### 3.4 i18n 是低成本入口

```bash
git diff upstream/main..HEAD -- static/i18n.js | grep '^-' | grep -v '^---' | head -50
```

`-` 开头的行 = 上游有、本仓库没有的 i18n key。每个 key 对应一个**用户功能差异**，可以从 key 名字快速定位上游新增了什么（例如 `canvas.loopImageToggle` → 图片循环、`canvas.outputConvertToInputGroup` → output 转输入组）。

### 3.5 后端路由对比

```bash
# 上游 main.py 所有路由
git show upstream/main:main.py | grep -E "^@app\.(get|post|put|delete|patch|websocket)"

# 本仓库所有路由（散落在 app/routes 和 app/ws.py）
grep -rE "^@(app|router|.+_router)\.(get|post|put|delete|patch|websocket)" app/ | sort
```

新端点要落到对应 `app/routes/<module>.py`；公共 helper 放 `app/upstream.py` / `app/providers.py` / `app/store.py` 等。**不要塞回 `main.py`**。

---

## 4. 移植规则（避免回退用户成果）

复制上游代码块时，按这张表"翻译"：

| 上游写法 | 移到本仓库时改成 |
|---|---|
| `<script src="https://cdn.tailwindcss.com">` | `<script src="/static/vendor/tailwindcss.js">` |
| `<script src="https://unpkg.com/lucide@latest">` | `<script src="/static/vendor/lucide.min.js">` |
| `@import url('https://fonts.googleapis.com/...')` | `@import url('/static/vendor/fonts/fonts.css')` |
| `from "three"` (importmap with `https://cdn.skypack.dev`) | importmap with `/static/vendor/three.module.js` |
| `main.py` 加新路由 `@app.post(...)` | 加到对应的 `app/routes/<module>.py` 的 `@router.post(...)` |
| `main.py` 加 helper | 看属性归属：HTTP 上游调用 → `app/upstream*.py`；存储 → `app/store.py`；providers → `app/providers.py`；config → `app/config.py`；图像 → `app/imageproc.py` |
| `main.py` import `from threading import Lock` | 直接在目标 `app/routes/*.py` 里 import |
| 上游用 `payload.dict()` / `model.dict()` (Pydantic v1) | 用户已升 Pydantic v2，改 `.model_dump()` |
| 上游 affiliate 链接 / `'api.getApi'` 翻译 / `.api-link-btn` | **不拉** |

---

## 5. 验证清单（每次同步收尾必跑）

```bash
# 1. Python 语法
python -c "import ast; [ast.parse(open(f, encoding='utf-8').read()) for f in ['app/routes/canvas.py','app/routes/generate.py','app/models.py','app/ws.py','main_refactored.py']]; print('OK')"

# 2. 路由 smoke test（route_count 必须 >= 上次记录的）
python tests/smoke_refactored_app.py

# 3. Seedream/Seedance 单元测试不能回归
python tests/unit_seedream_seedance.py

# 4. 启动 + curl 抽样（推荐封进一个临时 Python 脚本，host=127.0.0.1, port=3099 避免和真服务冲突）
#    必测：GET /、GET /api/config、GET /api/canvases、本次新加的路由各一发
#    建议：POST /api/canvases 创建一个画布、DELETE + /purge 清理掉，避免污染数据
```

如果用户改了前端，也要把 i18n.js / 各 HTML / canvas.html 的内联 JS 用 Node 解析过一遍：

```bash
node -e "const c=require('fs').readFileSync('static/canvas.html','utf8'); const re=/<script(?![^>]*\bsrc\b)[^>]*>([\s\S]*?)<\/script>/g; let m,total=0,errors=0; while((m=re.exec(c))){const body=m[1]; if(!body.trim()) continue; total+=body.length; try{new Function(body);}catch(e){errors++; console.error(e.message);}} console.log('inline JS bytes:',total,'errors:',errors);"
```

errors 必须为 0。

---

## 6. Commit 规范

- **每个上游功能一个 commit**，便于用户单独回滚某个特性。
- 标题用 `sync(upstream): §X <短描述>`，§X 是对应的"区块编号"（参考 `doc/upstream-sync-2026-05-17.md` 的 A-K 编号习惯，或新建本次的编号）。
- Commit body 至少写明：上游来源（README 提到的"X 月 X 日更新" / 上游某 commit sha）、touch 了哪些本仓库文件、是否依赖另一个 §X、是否需要用户手动跑端到端验证。

---

## 7. 同步日志（按时间倒序，每次同步追加一节，不要删旧的）

### 2026-05-23 — 同步上游主画布补丁

本次同步只移植主无限画布相关内容。继续跳过 Smart Canvas、LTX、RunningHub、self-update、asset-library、ComfyUI 方向功能和 affiliate/广告内容。

拉取了：
- 后端画布保存契约：`CanvasSaveRequest` 接收 `settings`、`client_id`，`CanvasCreateRequest` 接收 `kind`；`app/store.py` 的新画布和列表记录保留 `kind`、`settings`、`logs`。
- 多 tab 同步广播：`app/ws.py` 新增 `broadcast_canvas_updated()`，`app/routes/canvas.py` 在保存画布后广播 `canvas_updated`，并回传来源 `client_id` 供前端忽略自身更新。
- 画布资产端点 smoke 覆盖：`tests/smoke_refactored_app.py` 增加 `/api/canvases/{canvas_id}/meta`、`/api/canvas-assets/check`、`/api/canvas-assets/download`。
- Output 节点文件操作：`static/canvas.html` 的右键菜单恢复“下载全部图片”，复用现有 `/api/canvas-assets/download`；同时保存画布时发送 `settings` 和 `client_id`。
- i18n 补齐：`canvas.outputGroupActions`、`canvas.outputFileActions`、`canvas.outputDownloadAllImages`、`canvas.outputDownloadEmpty`、`canvas.missingFile`。

验证情况：
- ✅ `python -c "import ast; [ast.parse(open(f, encoding='utf-8').read()) for f in ['app/routes/canvas.py','app/routes/public.py','app/routes/generate.py','app/models.py','app/ws.py','main_refactored.py']]; print('OK')"` 通过。
- ✅ `python tests/smoke_refactored_app.py` 通过，`route_count: 57`，`missing_paths: []`。
- ✅ `python -m pytest tests/test_canvas_sync_contract.py -v` 通过，2 tests passed。
- ✅ `python tests/unit_seedream_seedance.py` 通过，`failure_count: 0`。
- ✅ `node --check` 校验 `static/canvas.html` 内联脚本合并产物和 `static/i18n.js` 通过。

未端到端验证（需浏览器手动确认）：
- 打开同一画布的两个 tab，修改并保存其中一个，另一个应通过 WebSocket `canvas_updated` 提示并同步。
- 右键 Output 节点，确认“下载全部图片”在存在本地 `/output/` 或 `/assets/` 图片时可点击并下载 zip，无本地图片时禁用。

### 2026-05-17（下午） — 同步上游 5/17 下午追加更新

上游最新 commit：`e0c838c Add files via upload`（2026-05-17 13:53 +0800；上一次同步基线是 `8afea2f` 09:25 +0800）。
上游本批改动总量：`main.py +33/-8`、`static/api-settings.html +9/-1`、`static/index.html +1/-1`，只有**一个功能改动**。

拉取了：
- **§H**（`cb81885`+`e0c838c`）API 设置页"未保存就能拉模型"：
  - 后端新增 `POST /api/providers/fetch-models`（接收 `TestConnectionPayload`，按表单当前 base_url/api_key 拉取，api_key 为空且 provider_id 已存在时回落到 env）→ 落进 `app/routes/provider.py`，**不写回 `main.py`**。
  - 抽出共享 helper `_fetch_models_from_upstream(base_url, api_key)`，GET 与 POST 共用；原 `GET /api/providers/{id}/fetch-models` 改用 `providers.get_api_provider_exact()`（新加到 `app/providers.py`）—— 不再静默 fallback 到首选 provider，避免新增平台没保存时拉错模型清单。
  - 前端 `fetchModels()` 改成 POST 调新端点，前置 `syncEditor()` + "请先填写请求地址" 校验。
  - `static/index.html` iframe 缓存破坏版本：`?v=20260514-provider-protocol` → `?v=20260517-provider-fetch-form2`。

跳过：
- 无（上游本批没有 CDN/affiliate 类需要"翻译"的内容）。

验证情况：
- ✅ `python _check_ast.py`（临时脚本）通过——`app/routes/{canvas,generate,provider}.py`、`app/providers.py`、`app/models.py`、`app/ws.py`、`main_refactored.py` 全部语法 OK。
- ⚠️ **当前 worktree 跑在不同机器上**（用户 `AMD-WS`，无 `OFX_dev` conda env、无 `node`），所以 `tests/smoke_refactored_app.py` / `tests/unit_seedream_seedance.py` / 内联 JS `new Function` 校验**未跑**。下次回到 DAN 主机上的 `OFX_dev` 环境，建议跑一遍这三项确认无回归。

未端到端验证（需用户手动跑）：
- API 设置页新增一个 provider（不保存）→ 填 base_url + api_key → 点「拉取模型」，应能拉到清单。
- 已保存的 provider → 同样点「拉取模型」，应仍可工作（走相同的 POST 端点）。
- 旧的 `GET /api/providers/{id}/fetch-models` 直接 curl 已保存 provider 仍应可用（保留作向后兼容）。

### 2026-05-17 — 同步上游 5/15-5/17 更新

上游最新 commit：`8afea2f Add files via upload`（2026-05-17 01:25 UTC）。
基础分析报告：[`doc/upstream-sync-2026-05-17.md`](upstream-sync-2026-05-17.md)。

拉取了：
- **§A**（`fbf569a`）`static/history-bulk-manager.js`（260 行，历史批量管理）+ 在 enhance/zimage/angle/online/klein.html 注入 script 标签
- **§B**（同 `fbf569a`）`static/image-preview.js`（149 行，大图预览模态）
- **§D**（`af707cd`）后端 `POST /api/canvas-image-tasks` + `GET /api/canvas-image-tasks/{id}` + `CANVAS_TASKS` 进程内队列；将 `online_image` 拆出 `build_online_image_result` 复用
- **§E 后端**（`4e40181`）`GET /api/canvases/{id}/meta` + `update_canvas` 的 `base_updated_at` 409 冲突检测
- **§E 前端**（`e88a498`）canvas.html 加 6 个函数（applyRemoteCanvasData / syncRemoteCanvasNow / checkRemoteCanvasVersion / startCanvasRemotePolling / stopCanvasRemotePolling / handleCanvasUpdatedMessage）+ saveCanvas 加 base_updated_at + visibilitychange 触发同步；polling 间隔 2500ms；通过 BroadcastChannel 接收 `canvas_updated`（上游也未用 WebSocket 推送）
- **§C 主体**（`2b44e35`）loop 节点 imageInput + imageBatchSize 状态、loop-image-panel UI、`imageRefsFromNode` + `loopInputImageRefs` helper、`generatorSources` 中 loop 分支返回 loopImage 虚拟源、canConnect 允许 image/group/output → loop；i18n 8 个新 key
- **§C 收尾**（`e823b58`）Output 节点右键菜单 `转为输入组` / `复制为新输入组`（contextmenu hook + `convertOutputNodeToInputGroup` / `copyOutputNodeToInputGroup` / `createInputGroupFromOutput`）

**跳过**：
- §F apimart affiliate 推广按钮（用户 README 已去广告）
- §G `theme.js` v=20260516-theme-sync 小调整（无症状不拉）
- §J 各 HTML 其它"CDN→vendor 切换造成的"diff（无功能差异）

**未端到端验证**（需用户手动跑）：
- 真实并发图片循环（loop 节点 imageInput → 接 image/group/output → 接 generator → 触发一键运行）
- 多 tab 画布同步（开两 tab 编辑同一画布，验证 ~2.5s 内自动同步）
- Output 右键 → 转/复制为输入组，确认下游连接保留

### 模板（下次同步追加用）

```markdown
### YYYY-MM-DD — 同步上游 X/X-X/X 更新

上游最新 commit：`<sha> <message>`（<UTC 时间>）。

拉取了：
- §X（`<commit>`）<说明>

跳过：
- §X <原因>

未端到端验证（需用户手动跑）：
- <清单>
```
