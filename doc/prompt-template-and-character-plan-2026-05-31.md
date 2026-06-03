# Prompt 模板节点 + 镜头运动节点 + 角色定义节点组 设计方案 v2

**日期**: 2026-05-31
**版本**: v2（调研 ComfyUI-Jimeng-API 与 awesome-seedance-2-prompts 后重写）
**状态**: 待用户拍板，未开始实现

> v1 留存：v1 内容已合并进本文。v2 的核心增量是「图—角色对应」的权威结论、
> 角色组拼贴图处理、负向模板去留依据、最简镜头运动预览方案。

---

## 0. v2 关键结论（先讲事实再讲方案）

1. **Seedance API 不存在 `@图N` 这种 token 协议。** API 层面只有 `role` 元数据
   （`first_frame` / `last_frame` / `reference_image`），见
   `C:\src\ComfyUI-Jimeng-API\nodes\nodes_video.py:190` 的 `_append_image_content`。
   多个 `reference_image` 仅靠数组顺序传递，没有编号 token。
2. **`@图1` / `@Image1` / `Marco@Image 1` 是社区惯例的"自然语言锚点"**，
   见 `C:\src\awesome-seedance-2-prompts\README_zh.md:471` 与 `:784`。
   它不是 API 强制语法，是给大模型读的描述性引用；模型把它当文本理解。
   → 所以我们的设计**不能依赖 token 数量、序号正确性来跑通 API**，但
   **应当在 prompt 中生成清晰的图—角色文字映射**，提升模型对齐效果。
3. **原作者的负向词整体可保留。** 社区 Seedance 2.0 案例中也直接用「负面提示词：浮动镜头, CGI 平滑感, 角色不稳定, ...」
   （`README_zh.md:1446`），与原作者风格一致。判断时不再按 SD/非 SD 切分，
   按"是否描述了不希望出现的画面/动作"保留即可。
4. **角色组要面对两种典型形态**：
   - **多张独立图**（最常见）：脸正/脸侧/全身/服装细节 → 每张图给一个简短描述。
   - **单张拼贴图**：一张图带九宫格/三视图等分区 → 用一段"分区说明"文字。
5. **镜头运动不用 three.js。** 走最简：方向 + 速度 + 持续时间 + 文本预览，
   配 GIF/SVG 静态缩略图即可（详见 §6）。

---

## 1. 背景与立场

原作者新增「提示词模板库」用独立侧边面板实现，违背"一切皆节点"。
fork 决策：**不 cherry-pick 面板 UI，只复用 markdown 模板内容**，重新节点化。
顺手把同样有切面板问题的镜头运动也节点化，并补上原作者没有的角色定义。

---

## 2. 已查清的现状

### 2.1 fork 项目的 source 数据流

`static/canvas.html::generatorSources(gen)`（约 6680-6730 行）是所有下游节点
拉上游数据的统一入口。返回的源对象形状：

```js
{
  id: string,
  type: string,    // 'image' | 'prompt' | 'group' | 'promptGroup' | 'loop' | 'output' | 'outputImage' | 'loopImage' | 'groupImage' | 'groupPrompt'
  label: string,
  preview?: string,
  refs: [{url, name, role}],  // 注意：已有 role 字段（first_frame/last_frame/reference_image）
  prompt: string
}
```

**关键观察**：
- `refs` 和 `prompt` 正交，同一 source 可同时带图 + 文。
- `group` 节点已能产出多个 source —— 角色组沿用这套即可。
- 下游消费方（generator/seedream/seedance/gptimage/llm）只读这个形状，
  **新节点遵守此契约则零改造下游**。

### 2.2 Seedance 端的 role 协议（fork 已实现）

- `SEEDANCE_MAX_REF_IMAGES = 9`
- `app/upstream_volcengine.py` 已按 `ref.role || 自动分配` 发送：
  `{"type": "image", "image_url": ..., "role": role}`
- 与 ComfyUI-Jimeng-API 行为一致；可信。

### 2.3 prompt 节点现状

纯 textarea，**没有任何系统化的负向处理**。模板节点是首次定义"负向嵌入约定"
的时机。

### 2.4 原作者的"角色"功能

只是 `default_asset_library()` 里一个图片分类（`{"id":"characters","name":"角色","type":"image"}`），
**没有角色卡概念**。不复用。

### 2.5 原作者的提示词模板

`static/system-prompts/infinite-canvas-prompt-templates.md` 共 10 个预设，
每个含「正向 / 负向 / 平台参数建议」，整体可直接搬。

### 2.6 ComfyUI-Jimeng-API 的关键约束（trusted reference）

- `_raise_if_text_params`（`nodes_video.py:78`）：**Seedance 1.0 禁止在 prompt
  中出现 `--xxx` CLI 参数**（resolution/ratio/dur/frames/camerafixed/seed）。
  这些必须走 API 字段。→ 我们的模板设计不要在文本里塞 `--ar 16:9`。
- 首尾帧与参考图互斥：`first_frame` + `last_frame` 不能与 `reference_image`
  共存（`nodes_video.py:1313`）。→ 角色组（一堆 reference_image）连进
  Seedance 时若 Seedance 已设了首尾帧，需要 UI 提示冲突。

---

## 3. 节点总览

| 节点 type | 中文名 | 形态 | 输出 source |
|-----------|--------|------|-------------|
| `promptTemplate` | 提示词模板 | 单节点（库选择 + 模板选择 + 模式正/负 + 占位填充） | 1 个 prompt source |
| `cameraMotion` | 镜头运动 | 单节点（方向 + 速度 + 持续 + 预览） | 1 个 prompt source |
| `character` | 角色定义组 | 节点组（多 image + 1 description + 可选 1 collageMap） | 多个 image source + 1 prompt source |

三者输出都符合既有 source 契约，连到现有 prompt/generator/seedance/seedream
节点不需要任何修改。

---

## 4. `promptTemplate` 节点

### 4.1 数据结构

```js
{
  id, x, y, width, height,
  type: 'promptTemplate',
  library: 'composition',          // 'composition' | 'cameraMotion' | future
  templateId: 'preset_9grid',      // 库内模板键
  mode: 'positive',                // 'positive' | 'negative' | 'both'
  placeholders: {                  // 模板 [占位符] 填充
    '主体': '一位戴眼镜的男子',
    '主体详细描述': '...'
  },
  joinNegative: 'inline',          // 'inline'（嵌入正向尾部）| 'separate'（仅输出负向，给下游用）
}
```

### 4.2 模板源文件

`static/system-prompts/templates/`：
- `composition.json`（10 个：从原作者 .md 转换而来，字段：`{id, name, scene, positive, negative, params, placeholders}`）
- `cameraMotion.json`（自建，参考下面 §6）

转换脚本一次性跑，结果入仓。

### 4.3 输出 prompt 拼装规则

```
正向 + （若 mode=positive 且 joinNegative=inline）→
  正向 + "\n\n[避免/Negative]:\n" + 负向
```

- 模板里的 `[主体]` 等占位被 `placeholders` 替换；未填的留 `[主体]` 原样，
  让用户在下游 prompt 节点继续编辑也可以。
- `params`（平台参数建议）**不写进 prompt**，作为节点 UI 提示展示
  （根据 §2.6，Seedance 1.0 禁止 `--xxx`）。

### 4.4 UI 草图

```
┌──────────────────────────────┐
│ 提示词模板  [composition ▾] │
│ 模板: [角色设定参考表 ▾]    │
│ 模式: ⦿正向 ○负向 ○正+负   │
│ ─ 占位填充 ─                │
│ 主体:        [____________] │
│ 主体详细描述:[____________] │
│ ─ 预览 ─                    │
│ [展开预览拼装结果]           │
│ ⚠ 提示: 商业 API 平台参数  │
│   建议: --ar 16:9 (不写入)  │
└──────────────────────────────┘
   ● prompt out
```

### 4.5 向后兼容

新增 type，老工程没有此节点 → 无影响。
未来要给 prompt 节点加"模板下拉"也可以，但**节点形态保持单一**，确保
"画布上一目了然每段提示词的来源"。

---

## 5. `character` 角色定义节点组

### 5.1 形态

是一个 **group 容器**（沿用现有 `group` 节点的容器范式），内部固定包含：

```
[A] character group: 林夏
├── (1..9) image 节点 —— 角色参考图（组内自动编号 ①②③…）
├── 1 prompt 节点 —— 角色专属提示词（可用 {A1} {A2} 引用本组第 N 张图）
└── (0..1) collageMap 节点 —— 拼贴图分区说明（仅当某张图是拼贴）
```

**组内节点都不需要新 type**，全部用既有 `image` / `prompt` 节点；
container 本身是新 type `character`（继承 `group` 渲染但限制内容）。

### 5.2 角色组标识 + 组内图序号（核心设计）

#### 5.2.1 组标识 A/B/C/…

- 画布上每个角色组**自动分配一个字母标识** `A` `B` `C` … `Z`。
- 分配规则：按创建时间。删除某组后**不回收**字母——保持稳定，
  避免引用错乱（如 A 被删后新建的组得 `D` 而不是 `A`）。
- 标识渲染：
  - 角色组标题栏左侧大号方框徽标 **`[A]`**（28×28px 紫底白字）。
  - 角色组**边框颜色按字母循环**（A=紫 / B=蓝 / C=绿 / D=橙 / E=粉 / F=青 …），
    多组连 Seedance 时一眼分辨。
- 用户**可手动改字母**（点击徽标弹小输入框），但必须单字母且唯一；
  改动会自动重写画布上所有引用此组的 prompt 文本（`{A1}` → `{D1}`）。

#### 5.2.2 组内图序号

- 角色组内的 image 节点**自动分配 1 开始的序号**，缩略图左上角显示角标
  **① ② ③ …**，颜色 = 组色。
- 序号由 image 节点在组内的**位置顺序**决定（从左到右、从上到下）；
  拖拽调整位置即重编号。
- 范围 1–9（Seedance 上限）；标题栏右侧显示 `3/9`，到 9 后禁止再加图。

#### 5.2.3 用户在 prompt 节点引用图片

**统一引用语法**：`{字母+数字}`，如 `{A1}` 表示"A 组第 1 张图"。

| 场景 | 写法 | 替换示例 |
|------|------|----------|
| 组内 prompt 引用本组 | `{1}` 简写（系统自动补字母）或 `{A1}` 完整写法 | `{1}` → `图 A1(正面面部)` |
| 下游 prompt 引用任意组 | 必须 `{A1}` 完整写法 | `{B2}` → `图 B2(全身)` |

示例：A 组（角色"林夏"）内的 prompt——

```
{1}为该角色正面面部，{2}为侧面，{3}为全身三视图拼贴。
角色：25岁短发女生，穿白衬衫，表情参考{1}，体型参考{3}。
```

组内合成后输出（带组标识）——

```
图 A1(正面面部特写)为该角色正面面部，图 A2(侧面轮廓)为侧面，图 A3(三视图拼贴)为全身三视图拼贴。
角色：25岁短发女生，穿白衬衫，表情参考图 A1(正面面部特写)，体型参考图 A3(三视图拼贴)。
```

到 Seedance 节点再做"A1 → 全局图 1"的最终重写（见 §5.4）。

每张图的描述来自该 image 节点的 `name` 字段。

#### 5.2.4 prompt 节点 UI 辅助

**组内 prompt 节点**工具栏 `+图▾` 弹出本组图片列表：

```
┌─ 插入引用 (A 组) ─┐
│ ① 正面面部       │
│ ② 侧面轮廓       │
│ ③ 三视图拼贴     │
└──────────────────┘
```

点击插入 `{1}`（本组简写）。

**下游主画布 prompt 节点**`+图▾` 弹出**所有上游角色组**的两级列表：

```
┌─ 插入引用 ────┐
│ A 林夏         │
│   ① 正面面部  │
│   ② 侧面轮廓  │
│   ③ 三视图    │
│ B 张三         │
│   ① 全身      │
│   ② 背面      │
└────────────────┘
```

点击插入 `{A1}` / `{B2}`，强制带组标识。

### 5.3 输出 source

| source | type | 内容 |
|--------|------|------|
| 每张图一个 | `characterImage` | `refs: [{url, name, role: 'reference_image'}]`, `prompt: 该图描述`, `groupTag: 'A'`, `charImageIndex: N` |
| 合并描述一个 | `characterPrompt` | `refs: []`, `prompt: 含 A1/A2 形式引用的角色描述`, `groupTag: 'A'` |

**关键字段**：
- `role='reference_image'` 所有角色组图统一标记。
- `groupTag` 是组字母（`A`/`B`/…），`charImageIndex` 是组内序号（1–9）。
- 下游 Seedance 用 `(groupTag, charImageIndex)` 元组做重映射（§5.4）。
- `characterPrompt` 里组内简写 `{1}` 已替换为 `图 A1(描述)`，**保留字母**，
  方便下游做最终全局序号重写。

### 5.4 下游 Seedance 的参考图序号重映射

#### 5.4.1 映射规则

1. 按**连线顺序**（先连的组先排）拼接：
   - A 组 ①②③ → 全局参考图 1/2/3
   - B 组 ①② → 全局参考图 4/5
2. 非 character 来源的 image 节点按原有逻辑排在角色组之后。
3. 总数超 9 → 超出部分不发送，Seedance 节点头部红色警告
   `⚠ 参考图 9/9 已满，超出 N 张未发送`。
4. 若 Seedance 已设首/尾帧 → 起始槽位相应后移；冲突时弹提示。

#### 5.4.2 prompt 中的序号重写

Seedance 节点构建映射表 `(groupTag, charImageIndex) → globalSlot`：

```
('A', 1) → 1     ('B', 1) → 4
('A', 2) → 2     ('B', 2) → 5
('A', 3) → 3
```

对合并后的 prompt 文本做正则替换 `图\s*([A-Z])(\d)` → `图 {global}`：

```
原文: 表情参考图 A1(正面面部)，体型参考图 A3(三视图)，背面参考图 B2(背面)
重写: 表情参考图 1(正面面部)，体型参考图 3(三视图)，背面参考图 5(背面)
```

模型最终看到的是清晰的"图 1/2/3/4/5"自然语言引用，与 API 实际发送顺序对齐。

#### 5.4.3 Seedance 节点 UI 槽位显示

Seedance 节点的 9 个参考图槽位每格都显示来源徽标 `[组]组内序号 + 全局序号 + 组名`：

```
┌─ Seedance 参考图 (5/9) ──────────────────────────────────┐
│  全局 1     全局 2     全局 3     全局 4     全局 5      │
│  [A]①       [A]②       [A]③       [B]①       [B]②       │
│  ┌─────┐  ┌─────┐  ┌─────┐  ┌─────┐  ┌─────┐         │
│  │正面 │  │侧面 │  │三视 │  │全身 │  │背面 │         │
│  └─────┘  └─────┘  └─────┘  └─────┘  └─────┘         │
│  紫·林夏   紫·林夏   紫·林夏   蓝·张三   蓝·张三       │
│                                                          │
│  全局 6     7         8         9                        │
│  ┌─空─┐  ┌─空─┐  ┌─空─┐  ┌─空─┐                    │
└──────────────────────────────────────────────────────────┘
```

槽位左上小徽标 `[A]①` 颜色 = 组色；下方一行 `紫·林夏` 重复组色 + 组名。
用户对照 prompt 里"图 4"立即知道是"B 组① 张三 全身"。

### 5.5 拼贴图处理

组内某张图如果是拼贴图（如九宫格角色设定表），用户点该 image 节点的
`拼贴` 标记，展开分区描述面板：

```
┌─ 图 A③ 拼贴分区 ─────┐
│ 分区数: [3]            │
│ 1. 左上: 正面表情      │
│ 2. 右上: 侧面          │
│ 3. 下排: 全身三视图    │
└────────────────────────┘
```

分区描述拼入该图的 description：

```
图 A3 (拼贴图): 共 3 个分区 — 左上: 正面表情; 右上: 侧面; 下排: 全身三视图
```

拼贴图**只占 1 个参考图槽位**（API 单图发送）；分区说明纯文本帮助模型理解。

### 5.6 UI 草图

```
┌[A] 角色: 林夏 ────────────────────────┐  ← 紫色边框
│ 参考图 (3/9)  [+添加图片]              │
│                                        │
│ ① ┌──────┐  ② ┌──────┐  ③ ┌──────┐  │  ← 角标紫色
│   │ 正面 │    │ 侧面 │    │三视图│     │
│   │ 面部 │    │ 轮廓 │    │拼贴📌│    │
│   └──────┘    └──────┘    └──────┘    │
│                                        │
│ 角色描述:                              │
│ ┌──────────────────────────────────┐  │
│ │ {1}为正面面部，{2}为侧面，       │  │
│ │ {3}为三视图拼贴。               │  │
│ │ 角色：25岁短发女生...            │  │
│ │                        [+图▾]   │  │
│ └──────────────────────────────────┘  │
│                                        │
│ ● image×3              ● prompt        │
└────────────────────────────────────────┘
```

- 组徽标 `[A]` 紫底白字 28×28，标题栏左侧。点击改字母。
- 组边框 = 组色；组内图角标 ①②③ 同色。
- `📌` 标记该图已配置拼贴分区。
- `+图▾` 弹出本组图片列表，插入 `{N}` 简写。
- 拖拽图片节点松手后自动重编号 + 同步 prompt 里的 `{N}`。

### 5.7 后续可选

- 角色库（左侧资源）：存好的角色组 JSON，拖入画布实例化。本期不做。
- 多角色：放多个 `character` 组，Seedance 按连线顺序合并 + 重映射（§5.4）。
- 序号手动锁定（跳号 1,2,5）：首版不做。
- 26 个字母用完后用 AA/AB：极端场景，首版限制最多 26 组。

---

## 6. `cameraMotion` 节点（最简版，无 three.js）

### 6.1 数据结构

```js
{
  type: 'cameraMotion',
  motion: 'dolly_in',      // 见下表
  speed: 'slow',           // slow/medium/fast
  durationSec: 5,
  customNote: ''           // 用户附加描述
}
```

### 6.2 内置 motion 表（首批 12 种，覆盖原作者 angle.html 主要项）

| key | 中文 | 英文 prompt 片段 | 预览图 |
|-----|------|------------------|--------|
| dolly_in | 推镜头 | slow dolly-in toward subject | dolly_in.svg |
| dolly_out | 拉镜头 | dolly-out pulling away from subject | dolly_out.svg |
| pan_left | 左摇 | smooth pan from right to left | pan_left.svg |
| pan_right | 右摇 | smooth pan from left to right | pan_right.svg |
| tilt_up | 上摇 | tilt up revealing scene above | tilt_up.svg |
| tilt_down | 下摇 | tilt down revealing scene below | tilt_down.svg |
| orbit_cw | 环绕(顺) | clockwise orbit around subject | orbit_cw.svg |
| orbit_ccw | 环绕(逆) | counter-clockwise orbit around subject | orbit_ccw.svg |
| crane_up | 升镜头 | crane shot rising up | crane_up.svg |
| crane_down | 降镜头 | crane shot descending | crane_down.svg |
| handheld | 手持 | subtle handheld camera shake | handheld.svg |
| static | 固定 | locked tripod static shot | static.svg |

### 6.3 prompt 输出格式

```
[Camera]
{中文} ({english}), {speed} speed, {durationSec}s
{customNote}
```

### 6.4 UI 预览：极简方案

- 12 张 60×60 SVG 缩略图（箭头 + 简单几何），网格列出。
- 选中后右边显示当前选择 + 拼装好的 prompt 文本。
- **没有 3D 渲染，没有动画**。后续若需要可单独加 GIF 文件，但首版不做。

```
┌─ 镜头运动 ────────────────────┐
│ [→][←][↑][↓][↻][↺]           │
│ [▣→][▣←][▣↑][▣↓][〰][▣]      │
│ 当前: 推镜头 dolly_in         │
│ 速度: ○慢 ⦿中 ○快           │
│ 时长: [5] 秒                  │
│ 附加: [________________]      │
│ ─ 预览 ─                       │
│ [Camera]                       │
│ 推镜头 (slow dolly-in...)     │
│                                │
│ ● prompt out                  │
└────────────────────────────────┘
```

### 6.5 路径选择

`cameraMotion` **不复用** `promptTemplate` 节点，理由：
- 它有强制结构化字段（motion/speed/duration），不是字符串占位填充。
- 预览是图形化的，不是文本预览。
- 未来可能加更多结构（轨迹关键帧），独立 type 更利于演进。

但内部仍把 motion 表存在 `static/system-prompts/templates/cameraMotion.json`，
便于不写代码也能扩。

---

## 7. 与下游节点的对接

### 7.1 prompt 节点

- 不改 prompt 节点 UI。
- prompt 节点收到上游若干 source，按"先 character 描述 → 再 template
  正向 → 再 cameraMotion → 再用户自填 → 末尾追加 template 负向"的顺序拼装。
- 排序规则放在 prompt 节点的 `mergeSources()` 里实现；类型已在 source.type 上。

### 7.2 Seedance 节点

- 现有 9 槽位逻辑沿用。
- 角色组连入：按 `role='reference_image'` 自动填到非首/尾帧槽位。
- **参考图序号重映射**（见 §5.4）：按连线顺序拼接各角色组的图，
  构建 `(groupTag, charImageIndex) → globalSlot` 映射表，应用到所有
  characterPrompt 文本的 `图 A1` / `图 B2` 引用上（正则替换为 `图 N` 全局序号）。
- **每个槽位 UI 显示来源徽标**（见 §5.4.3）：
  小角标 `[A]①` + 组色 + 组名（如"紫·林夏"）。
  让用户对照 prompt 文本里"图 4"指向哪张图一目了然。
- 数量超 9 → 红色徽标 `⚠ 9/9 已满，超出 N 张未发送`。
- 与已设的首/尾帧冲突时禁用首/尾帧槽位并提示。

### 7.3 generator / seedream / gptimage / llm / msgen / video / comfy

零改动；它们只读 source 形状。

---

## 8. 实施步骤（建议顺序）

1. **模板转换器**：写一个 Python 小脚本，把 `infinite-canvas-prompt-templates.md`
   转为 `composition.json`。先跑通这一步，输出文件入仓后续才方便。
2. **`promptTemplate` 节点**：JSON 加载 + 节点 UI + 占位填充 + 拼装输出。
   先只支持 `composition` 一个库。
3. **prompt 节点的 source 合并排序**：在 prompt 节点接收端加类型感知合并。
4. **`cameraMotion` 节点 + SVG 缩略图**：12 个 motion + 预览。
5. **`character` 容器节点**：基于现有 `group` 改造（限制内部节点类型，
   提供两类 source 输出，自动生成"图—角色"说明文字）。
6. **Seedance 节点对角色组的 9 张限制与冲突提示**。
7. **回归测试**：打开 3 个旧工程文件，确认新节点不存在时一切正常；
   新建一个含三类新节点的工程，连到 Seedance 出图。

---

## 9. 未决/需用户确认事项

- **拼贴图分区配置 UI**：是否需要可视化拉框？还是文本列表即可？
  （建议首版先文本列表，后续再加可视化。）
- **prompt 节点合并顺序** 是否允许用户在 prompt 节点 UI 里调？
  （建议首版固定顺序：角色描述 → 模板正向 → cameraMotion → 用户自填 → 负向，避免 UI 复杂度。）
- **模板平台参数** 仅作 UI 提示不写入 prompt —— 是否同意？
  （依据 ComfyUI-Jimeng-API 约束，强烈建议如此。）
- **组内图片拖拽排序后自动重编号** —— 是否接受？（拖拽松手后所有
  ①②③角标和 prompt 里的 `{N}` 引用都会自动更新，用户无需手动同步。）
- **组字母改后自动重写引用**：用户把 A 改成 D 时，所有 prompt 里的
  `{A1}` → `{D1}` 自动同步，是否接受？

---

## 附录 A：调研证据索引

- `C:\src\ComfyUI-Jimeng-API\nodes\nodes_video.py:78` — `_raise_if_text_params`
- `C:\src\ComfyUI-Jimeng-API\nodes\nodes_video.py:190` — `_append_image_content`（确认 role-only）
- `C:\src\ComfyUI-Jimeng-API\nodes\nodes_video.py:1290-1320` — 多 ref 收集与 role 分配
- `C:\src\ComfyUI-Jimeng-API\nodes\nodes_video.py:1313` — 首尾帧与 ref 互斥
- `C:\src\awesome-seedance-2-prompts\README_zh.md:471` — `@/image1` 自然语言用法
- `C:\src\awesome-seedance-2-prompts\README_zh.md:784` — `Marco@Image 1` 命名+引用
- `C:\src\awesome-seedance-2-prompts\README.md:3071` — `'@Image1' as opening frame`
- `C:\src\awesome-seedance-2-prompts\README_zh.md:1446` — Seedance 2.0 直接用「负面提示词：...」
- `C:\src\Original-Infinite-Canvas\static\system-prompts\infinite-canvas-prompt-templates.md` — 10 个待移植模板
- `C:\src\Infinite-Canvas\static\canvas.html:6680` — `generatorSources(gen)` 契约
- `C:\src\Infinite-Canvas\app\upstream_volcengine.py` — fork 端 role 发送实现
