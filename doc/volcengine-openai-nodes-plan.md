# Volcengine Seedream/Seedance and GPT Image 2.0 Node Plan

Updated 2026-05-16: Initial migration plan based on `C:\src\ComfyUI-Jimeng-API`.

## Goal

Add a small, focused set of first-party nodes to Infinite Canvas:

- `GPT Image 2.0` node using the official OpenAI API.
- `Seedream` node using Volcengine Ark SDK.
- `Seedance` node using Volcengine Ark SDK.

This work targets the split application under `app/` and `static/`.
Do not modify `main.py`.

## Confirmed Decisions

- Use the Volcengine Ark SDK when available: `volcengine-python-sdk[ark]`.
- Do not port Jimeng branding or unrelated Jimeng-only features.
- Do not add broad third-party API registration UI for this work.
- Users should only need to enter API keys in Settings:
  - OpenAI API key for official `gpt-image-2`.
  - Volcengine Ark API key for Seedream and Seedance.
- The dedicated nodes do not use Comfly. Existing Comfly-related code can remain unless a later cleanup task explicitly removes it.
- ModelScope stays as-is and is not part of this project.
- Existing project features can remain, but this work should avoid relying on the generic provider system where that makes UX more complex.
- Build dedicated nodes rather than forcing these models into the existing generic API/provider nodes.
- Seedream versions required:
  - `doubao-seedream-4-0-250828`
  - `doubao-seedream-4-5-251128`
  - `doubao-seedream-5-0-260128`
- Seedance versions required:
  - `doubao-seedance-1-5-pro-251215`
  - `doubao-seedance-2-0-260128`
  - `doubao-seedance-2-0-fast-260128`
- GPT Image 2.0 must use OpenAI official API, not Volcengine.

## Proposed User-Facing Nodes

### GPT Image 2.0 Node

Purpose: official OpenAI image generation and image editing with `gpt-image-2`.

Recommended UI fields:

- Prompt.
- Input/reference images.
- Size / aspect ratio.
- Count.
- Quality if supported by current backend implementation.

Hidden/default behavior:

- Provider is always official OpenAI.
- Model is always `gpt-image-2`.
- API key is read from Settings / environment.

Official API notes checked 2026-05-16:

- OpenAI's image generation guide lists `gpt-image-2` as the latest GPT Image model.
- Use the Image API endpoints:
  - `POST /v1/images/generations` for prompt-only generation.
  - `POST /v1/images/edits` when reference images are provided.
- GPT Image responses return generated image data under `data[]`, with `b64_json` expected for GPT Image models.
- Use `OPENAI_API_KEY`; do not reuse `COMFLY_API_KEY`.

### Seedream Node

Purpose: Volcengine image generation with Seedream 4.0, 4.5, and 5.0.

Recommended UI fields:

- Prompt.
- Model version: `4.0`, `4.5`, `5.0`.
- Input/reference images.
- Size / aspect ratio.
- Count, mapped to the plugin's `generation_count` behavior.
- Seed.
- Watermark toggle.

Advanced fields to defer or hide initially:

- Sequential/group image generation.
- Web search for Seedream 5.0.
- Max images for group generation.

Implementation note: keep backend support extensible enough to add these later without changing the node contract heavily.

Count behavior: follow the ComfyUI plugin's default non-group behavior. `count` means repeated independent requests (`generation_count`). Do not enable Seedream 4/5 group/sequential generation in the first pass.

### Seedance Node

Purpose: Volcengine video generation with Seedance 1.5 Pro, 2.0, and 2.0 Fast.

Recommended UI fields:

- Prompt.
- Model version: `1.5 Pro`, `2.0`, `2.0 Fast`.
- First-frame image.
- Last-frame image.
- Reference images.
- Reference videos.
- Reference audio.
- Duration.
- Resolution.
- Aspect ratio.
- Seed.
- Generate audio.
- Non-blocking submit / later result retrieval.

Advanced fields to defer or hide initially:

- Draft mode / draft task reuse.
- Explicit quota management.
- Offline inference mode.
- Web search.

Important terminology note: quota, non-blocking mode, draft reuse, reference audio, and web search are feature names observed in `C:\src\ComfyUI-Jimeng-API`; they are not existing Infinite Canvas concepts. If we want them in our own nodes, we must explicitly implement them in this project.

For the first pass, implement non-blocking mode and reference audio for Seedance. Do not expose quota management, draft reuse, offline inference, or web search yet. Preserve backend structure so they can be added later.

Current-project fit checked 2026-05-16:

- Infinite Canvas has pending output placeholders and elapsed-time display while a node is running.
- It has ModelScope/Angle-style task polling/retry ideas, but no general canvas task persistence system.
- It has video output display, but no first-class audio node or audio upload pipeline.

Minimal implementation decision:

- Seedance owns its own task state instead of introducing a global task system.
- Seedance non-blocking is enabled by default.
- On submit, the node stores returned `task_id` values in the node state and output placeholder metadata.
- The same Seedance node exposes a "Check result" action that polls saved task IDs.
- Task IDs are saved with the canvas JSON so users can recover after reload.
- Reference videos and reference audio are uploaded/selected inside the Seedance node, not via new global Video/Audio input node types.

## Backend Plan

### Dependencies

Update `requirements.txt`:

- Add `volcengine-python-sdk[ark]>=5.0.12`.

Do not install against system Python. Follow `.agents/rules/cmdrule.md` and use the `OFX_dev` environment for verification.

### Settings And Secrets

Add first-party key handling for:

- `OPENAI_API_KEY`
- `VOLCENGINE_ARK_API_KEY`

Avoid asking users to configure model lists, base URLs, provider ids, or protocol modes for these nodes.

### Backend Modules

Preferred file placement:

- `app/upstream_openai_image.py` for official OpenAI `gpt-image-2`.
- `app/upstream_volcengine.py` for Ark SDK image/video calls.
- `app/models.py` for request/response models.
- `app/routes/generate.py` for HTTP endpoints.
- `app/config.py` for model constants and env names.

Do not add new framework layers. Follow the current split-app style.

### Proposed Endpoints

Add focused endpoints rather than overloading generic provider behavior too much:

- `POST /api/nodes/gpt-image-2`
- `POST /api/nodes/seedream`
- `POST /api/nodes/seedance`
- `POST /api/nodes/seedance/status`

Each endpoint should:

- Validate the API key exists.
- Normalize node UI fields into the upstream API shape.
- Download or save returned images/videos into the existing `output/` flow.
- Return the same kind of local URLs that canvas output nodes already understand.
- Include raw task/model metadata for debugging, but keep it out of the visible UI unless needed.

Response contracts:

- `POST /api/nodes/gpt-image-2` returns `{ "images": ["/output/..." ], "raw": {...}, "usage": {...} }`.
- `POST /api/nodes/seedream` returns `{ "images": ["/output/..." ], "raw": {...}, "usage": {...} }`.
- `POST /api/nodes/seedance` with `non_blocking: true` returns `{ "status": "submitted", "task_ids": ["..."], "raw": {...} }`.
- `POST /api/nodes/seedance` with `non_blocking: false` returns `{ "status": "succeeded", "videos": ["/output/..." ], "task_ids": ["..."], "raw": {...} }`.
- `POST /api/nodes/seedance/status` accepts `{ "task_ids": ["..."] }` and returns `{ "status": "pending" | "succeeded" | "failed", "videos": ["/output/..." ], "tasks": [...], "raw": {...} }`.

### Seedream Backend Mapping

Use model alias mapping:

- `4.0` -> `doubao-seedream-4-0-250828`
- `4.5` -> `doubao-seedream-4-5-251128`
- `5.0` -> `doubao-seedream-5-0-260128`

Port from `ComfyUI-Jimeng-API`:

- Size validation/mapping.
- Reference image conversion.
- Ark SDK `images.generate`.
- Streaming response handling where needed for Seedream 4/5.
- Local output saving.

Do not port:

- Seedream 3.
- Jimeng quota UI.
- Jimeng localization/UI assets.

### Seedance Backend Mapping

Use model alias mapping:

- `1.5 Pro` -> `doubao-seedance-1-5-pro-251215`
- `2.0` -> `doubao-seedance-2-0-260128`
- `2.0 Fast` -> `doubao-seedance-2-0-fast-260128`

Port from `ComfyUI-Jimeng-API`:

- `content_generation.tasks.create`.
- Task polling via `content_generation.tasks.get`.
- Content construction for text, first frame, last frame, reference images, reference videos, and reference audio.
- Duration / frames calculation.
- `resolution`, `ratio`, `seed`, `generate_audio`.
- Returned video download and local output saving.
- Non-blocking task submission and later result retrieval.
- Reference video behavior follows the plugin: up to 3 reference videos, each treated as `reference_video`.
- Reference audio behavior follows the plugin: up to 3 reference audios, each treated as `reference_audio`.

Defer:

- Draft task reuse.
- Quota estimation.
- Offline inference.
- Web search.

## Frontend Plan

### Settings UI

Keep Settings simple:

- Add an OpenAI key field if not already clearly present.
- Add a Volcengine Ark key field.
- Do not require model list setup for these nodes.
- Do not require user-created providers for these nodes.

Existing API provider UI can remain untouched unless a minimal key field must be added there.

### Canvas UI

Add three dedicated node creation menu entries:

- `GPT Image 2.0`
- `Seedream`
- `Seedance`

These should be visually and behaviorally consistent with existing canvas nodes, but they should not expose generic provider/model-provider complexity.

Node run behavior:

- GPT Image node calls `/api/nodes/gpt-image-2`.
- Seedream node calls `/api/nodes/seedream`.
- Seedance node calls `/api/nodes/seedance`.
- Seedance node supports both blocking generation and non-blocking submission.
- Seedance defaults to non-blocking mode because video tasks are long-running.
- Non-blocking Seedance runs return task IDs immediately and render a pending output card.
- The Seedance node stores task IDs in node state and exposes a "Check result" button.
- Checking results calls `/api/nodes/seedance/status`; succeeded videos are appended to the existing output node.
- Outputs should connect to existing output nodes.
- Input images/prompts should reuse existing canvas connection/source collection logic where possible.
- Reference video and reference audio are handled as upload/select fields inside the Seedance node.

Preview behavior follows the existing project style:

- Output nodes remain the primary result gallery.
- Dedicated generation nodes may show a small "latest result" thumbnail/player for convenience.
- Clicking a result thumbnail/player opens the existing `outputLightbox` preview.
- Do not add "double-click the node body to enlarge" in the first pass; that is not an existing canvas interaction pattern.
- Image/video enlarged preview should reuse the current Output lightbox behavior instead of introducing a new modal.

### Online Image Page

No major change required for the first pass unless we want GPT Image 2.0 node parity outside the canvas.

## Implementation Phases

### Phase 1: Foundation

- Add env/config constants.
- Add requirements entry for Ark SDK.
- Add backend request models.
- Add focused endpoints with placeholder validation.
- Add settings fields for OpenAI and Volcengine Ark keys.

### Phase 2: GPT Image 2.0 Node

- Implement official OpenAI request path.
- Add canvas node UI.
- Support prompt + reference images + local output saving.
- Verify import and endpoint behavior.

### Phase 3: Seedream Node

- Port minimal Ark SDK image generation.
- Support 4.0, 4.5, 5.0.
- Support prompt, references, size, count, seed, watermark.
- Save generated images to local output.

### Phase 4: Seedance Node

- Port minimal Ark SDK video task creation and polling.
- Support 1.5 Pro, 2.0, 2.0 Fast.
- Support prompt, first/last frame, image references, video references, audio references, duration, resolution, aspect ratio, seed, generated audio.
- Support non-blocking submit and later query/recovery, with task IDs persisted in canvas JSON.
- Save generated videos to local output.

### Phase 5: Polish And Verification

- Add friendly Chinese error messages for missing key, invalid model access, invalid media, task failure, and timeout.
- Run `OFX_dev` import checks.
- Boot the split app and test endpoints.
- Browser-test canvas node creation and run flow when API keys are available.

## Files Expected To Change

- `requirements.txt`
- `app/config.py`
- `app/models.py`
- `app/upstream_openai_image.py`
- `app/upstream_volcengine.py`
- `app/routes/generate.py`
- `app/routes/provider.py` if Settings key save/load needs adjustment
- `static/canvas.html`
- `static/api-settings.html`
- `static/i18n.js` if new labels need translations

Do not modify:

- `main.py`
- ModelScope behavior unless needed to avoid regressions
- Existing Go migration docs

## Deferred Features

These are useful but not needed for the first implementation:

- Quota management.
- Draft task reuse.
- Seedream group/sequential generation UI.
- Seedream web search UI.
- Seedance web search UI.
- Any Jimeng-only branding, docs, icons, or locale files.

## Open Questions

No open questions are blocking implementation at this point.

Implementation assumptions now fixed:

- OpenAI key name: `OPENAI_API_KEY`.
- Volcengine key name: `VOLCENGINE_ARK_API_KEY`.
- Seedance reference videos: first pass supported, plugin-style, up to 3.
- Seedance reference audio: first pass supported, plugin-style, up to 3.
- Seedance reference video/audio UI: upload/select fields inside the Seedance node.
- Seedance non-blocking: default on, task IDs persisted in node/canvas state.
- Seedream count: independent repeated requests, not group/sequential generation.
- Advanced fields: omitted until requested, not hidden behind an advanced toggle.

Remaining implementation-time checks:

- Confirm installed `openai` SDK version supports current Images API behavior; otherwise use direct HTTP.
- Confirm installed `volcengine-python-sdk[ark]` version exposes the same Ark SDK methods used by the ComfyUI plugin.

## Implementation Update - 2026-05-16

Initial implementation is now in place without touching `main.py`.

Implemented:

- Added dedicated backend endpoints:
  - `POST /api/nodes/gpt-image-2`
  - `POST /api/nodes/seedream`
  - `POST /api/nodes/seedance`
  - `POST /api/nodes/seedance/status`
- Added `POST /api/nodes/media-upload` for Seedance reference video/audio uploads.
- Added Settings API for first-party keys:
  - `GET /api/first-party-keys`
  - `PUT /api/first-party-keys`
- Added Settings page panel for `OPENAI_API_KEY` and `VOLCENGINE_ARK_API_KEY`.
- Added canvas nodes:
  - `gptimage`: GPT Image 2.0 through OpenAI official API.
  - `seedream`: Seedream 4.0 / 4.5 / 5.0 through Volcengine Ark SDK.
  - `seedance`: Seedance 1.5 Pro / 2.0 / 2.0 Fast through Volcengine Ark SDK.
- Seedance is non-blocking in the UI: submit stores `taskIds`; `Check result` polls and saves completed videos.
- Seedance reference video/audio are uploaded inside the Seedance node, up to 3 each.
- Generated images/videos append to the existing Output node and the dedicated node stores a latest-output preview.

Implementation notes:

- OpenAI Images is implemented with direct `httpx` calls to `OPENAI_API_BASE_URL` because this project did not already depend on the OpenAI Python SDK.
- Volcengine Ark SDK is imported lazily so the app can still import before the new dependency is installed, and missing SDK errors point at `requirements.txt`.
- `requirements.txt` now declares `volcengine-python-sdk[ark]>=5.0.12`.
- The first pass uses local data URLs for Seedance reference video/audio when uploaded from the canvas. If Ark rejects data URLs for video references in real API testing, the next increment should switch only that part to Ark file upload / asset upload while keeping the node UI unchanged.

Verification performed:

- Python syntax check passed for modified backend files using `py_compile`.
- `static/canvas.html` main inline script syntax check passed with bundled Node.js.
- `static/api-settings.html` main inline script syntax check passed with bundled Node.js.

Verification not completed:

- Full FastAPI import/boot was not completed because the configured `OFX_dev` activation path and direct interpreter path were unavailable in this shell, and fallback Python installations did not have FastAPI installed.
- Real OpenAI / Volcengine API calls were not run because keys and network access were not available in this session.

## Bugfix Update - 2026-05-16

Seedream 4.5 returned:

- `InvalidParameter`
- `image size must be at least 3686400 pixels`

Fix:

- New Seedream nodes now default to `2K` instead of `1K`.
- Backend now normalizes every Seedream request size before calling Ark:
  - if the requested `WxH` is below `3,686,400` pixels, it scales up while preserving aspect ratio;
  - if the requested size cannot be parsed, it falls back to `2048x2048`.

Reason:

- Existing saved nodes may still contain `resolution: 1k`, so the backend guard is required even after changing the UI default.

## SDK Requirement Review Update - 2026-05-16

After reviewing the Volcengine-facing implementation against `C:\src\ComfyUI-Jimeng-API`, more SDK constraints were moved into this project as backend guards.

Additional fixes:

- Seedream size normalization is now model-specific:
  - Seedream 4.0 minimum: `1280x720` pixel count.
  - Seedream 4.5 minimum: `3,686,400` pixels.
  - Seedream 4.x maximum: `4096x4096` pixel count.
  - Seedream 5.0 minimum: `3,686,400` pixels.
  - Seedream 5.0 maximum: `10,404,496` pixels.
- Seedream seed `-1` now means random/omitted, matching the ComfyUI plugin behavior instead of sending `-1 + index`.
- Seedance `generate_audio` now defaults to `true`, matching the ComfyUI plugin.
- Seedance duration is now clamped by model family:
  - Seedance 1.5 Pro: `4-12` seconds.
  - Seedance 2.0 / 2.0 Fast: `4-15` seconds.
- Seedance reference video validation now follows the plugin's accepted formats:
  - MP4 or MOV only.
  - Local file size must not exceed `50MB`.
- Seedance reference audio validation now follows the plugin's practical request path:
  - MP3 or WAV only for uploaded files.
  - Local file size must not exceed `15MB`.
  - Audio references require at least one visual reference image or reference video.
- Seedance image roles now avoid mixing first/last-frame mode with reference-media mode:
  - if there are only 1-2 connected images and no uploaded reference video/audio, images are treated as first/last frame;
  - if there are 3+ images or any reference video/audio, connected images are treated as `reference_image`.

Remaining risk:

- The ComfyUI plugin uploads reference videos to an externally reachable ComfyAPI URL before passing `video_url` to Ark. This project currently converts local uploaded videos to data URLs. If Ark rejects video data URLs, the next fix should replace only the reference-video transport with a public/Ark-supported upload path while keeping the Seedance node UI unchanged.

## Bugfix Update - 2026-05-16 Seedance Polling

Observed during Seedance 1.5 Pro testing:

- After `POST /api/nodes/seedance` returned `200`, the node badge stayed at `queued`.
- No `/api/nodes/seedance/status` calls were made until the user manually clicked `Check result`.
- Multiple quick clicks on `Check result` created multiple concurrent status/download requests, so the same completed task could be appended to Output multiple times.

Fix:

- Renamed the post-submit UI status from `queued` to `submitted` / `已提交`.
- Added automatic frontend polling after Seedance submit.
- Existing saved canvases with pending `taskIds` also resume polling when rendered.
- Added a per-node `checking` lock so `Check result` cannot be clicked repeatedly while one query is in flight.
- Added frontend duplicate-output filtering before appending videos to Output.
- Added backend in-memory result cache keyed by `task_ids`, so repeated status calls for the same completed task return the same saved local URLs instead of re-downloading/re-saving.
- Added server log lines for Seedance submit and status checks.

Reason:

- Non-blocking should mean "submit quickly, then poll in the background", not "submit and wait for a manual button forever".
- Status/result endpoints should be idempotent for completed tasks.
