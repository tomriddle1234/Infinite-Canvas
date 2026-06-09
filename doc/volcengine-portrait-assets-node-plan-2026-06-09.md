# Volcengine Portrait Assets Node Plan

Updated 2026-06-09: Initial design for adding Volcengine real-person portrait assets and virtual portrait assets to this project.

## Summary

Seedance 2.0 should use Volcengine portrait assets by passing the asset URI, not by downloading the preview image and resubmitting it as an ordinary reference image.

The correct inference-time value is:

```text
asset://<ASSET_ID>
```

That URI should be placed in the Seedance content item URL field, for example:

```json
{
  "type": "image_url",
  "role": "reference_image",
  "image_url": {
    "url": "asset://asset-20260318035710-xxxxx"
  }
}
```

Preview images returned by `ListAssets` or `GetAsset` are for UI display, selection, and local preview caching only. They should not be treated as the primary Seedance input for trusted portraits, because downloading them and sending them back as `data:image/...` or a normal HTTP image loses the trusted asset path and can re-trigger the same real-person/sensitive-content review path.

## Why Asset URI Instead Of Downloaded Image

Official Volcengine docs describe portrait assets as trusted assets that can be used directly by Seedance 2.0 after they become `Active`. The docs also say Seedance 2.0 does not support directly uploading reference images/videos containing real human faces, and that material IDs use the `asset://<ASSET_ID>` format.

Implications for this project:

- The node may download/cache the returned `URL` for thumbnails and detail preview.
- The node output consumed by Seedance must keep `url: "asset://..."`.
- `asset://` should be passed through by backend request construction unchanged.
- Prompt text should refer to material order as "图片 1", "图片 2", etc.; it should not ask the model to interpret raw asset IDs in natural language.

References:

- Volcengine virtual portrait library: https://www.volcengine.com/docs/82379/2223965
- Volcengine private virtual portrait asset library: https://www.volcengine.com/docs/82379/2333565
- Seedance 2.0 API reference: https://www.volcengine.com/docs/82379/1393047
- Seedance 2.0 SDK tutorial: https://www.volcengine.com/docs/82379/2291680
- Seedance 2.0 advanced creation entitlement: https://www.volcengine.com/docs/82379/2377608?lang=zh

## Prompt Reference Convention

Updated 2026-06-09: Verified against Volcengine docs and the installed `volcengine-python-sdk[ark]` type definitions.

The official convention is:

- API/content layer: pass the trusted asset URI in `content.<modality>_url.url`.
- Prompt layer: refer to materials by their order in the request body, such as `图片 1`, `图片 2`, `视频 1`, or `参考音频 1`.
- Do not put raw Asset IDs in the prompt.

The Volcengine private virtual portrait asset guide states that, in prompts, references should use "图片 1" / "视频 1" style names, with numbering based on the material order in the request body, and that Asset IDs should not be used directly in the prompt. The Seedance 2.0 SDK tutorial gives the same rule: Asset ID is only used to pass the material in `content.<modality>_url.url`; prompt text still uses a "material type + index" format. The tutorial labels "图片1中美妆博主" as the correct style and using `asset-...` as the character name as incorrect.

The virtual portrait library experience UI also uses an `@+素材名` pattern, for example `@图片 1 ... 参考 @图片 2 ...`. This is a UI/prompt-authoring convention. It is not an extra SDK field.

The installed SDK shape confirms this split. `volcenginesdkarkruntime.types.content_generation.create_task_content_param` defines content items such as:

```python
class CreateTaskContentImageParam(TypedDict):
    type: Literal["image_url"]
    image_url: CreateTaskContentImageDataParam
    role: str
```

There is no separate structured field for `@图片 1`. Therefore this project should treat `@图片 1` / `图片 1` as prompt text syntax and keep the actual material binding in the ordered `content` array.

Recommended UX rule:

- Show portrait assets as visible image cards and Seedance slots.
- Label slots as `图片 1`, `图片 2`, etc.
- Let users insert `图片 1` or `@图片 1` from the UI.
- Normalize or tolerate common variants such as `@参考图1`, `@图1`, and `图片1`.
- Submit the corresponding ordered `content` items with `url: "asset://..."`.

Scope note:

- Prompt reference ordering is a Seedance-node concern, not a Portrait Asset node concern.
- The Portrait Asset node should not own prompt text, prompt insertion, or reference-number semantics.
- The Portrait Asset node only emits one selected portrait asset reference. Seedance receives one or more upstream references and decides the visible slot order and final content order.

## Authorized Real-Person Portrait Usage

Updated 2026-06-09: Verified from the Seedance 2.0 SDK tutorial section "使用已授权真人素材".

Official behavior:

- After real-person verification and authorization, images, videos, and audio for that person can be uploaded to Volcengine.
- After the material is successfully admitted into the asset library, each material receives an independent Asset ID.
- In Seedance requests, the material is used by placing `asset://<asset ID>` in the relevant `content.<modality>_url.url` field.
- Real-person image assets use `type: "image_url"` and `role: "reference_image"`.
- Real-person video assets use `type: "video_url"` and `role: "reference_video"`.
- Real-person audio assets use `type: "audio_url"` and `role: "reference_audio"`.
- Prompt text must still use the "素材类型+序号" convention. For example, `图片 n` refers to the nth `type="image_url"` item in the content array, counting only image content items in request order. Asset IDs are not supported as prompt references.

Official-style request shape:

```json
{
  "content": [
    {
      "type": "text",
      "text": "图片 1 中的人物走进画面，参考视频 1 的场景运动，使用参考音频 1 的声音节奏。"
    },
    {
      "type": "image_url",
      "image_url": {
        "url": "asset://<asset ID>"
      },
      "role": "reference_image"
    },
    {
      "type": "video_url",
      "video_url": {
        "url": "asset://<asset ID>"
      },
      "role": "reference_video"
    },
    {
      "type": "audio_url",
      "audio_url": {
        "url": "asset://<asset ID>"
      },
      "role": "reference_audio"
    }
  ]
}
```

Design implication:

- This project must preserve `AssetType` in the data model because authorized real-person libraries can contain image, video, and audio assets.
- The Portrait Asset node is still one portrait/person per node. Multiple people should be represented by multiple Portrait Asset nodes.
- For the first implementation pass, the required browsing scope is both portrait library families: real-person (`LivenessFace`) and virtual portrait (`AIGC`).
- Seedance's reference UI, not the Portrait Asset node, should map selected portrait image assets into the final reference-image slot order. Future increments can add trusted `asset://` video and audio assets to Seedance's reference-video/reference-audio controls.
- Any prompt helper or `图片 1` / `视频 1` / `参考音频 1` mapping belongs in the Seedance node or Seedance documentation, not in the Portrait Asset node implementation.

## Asset Library Types

Volcengine Assets API uses Asset Groups and Assets.

Planned group types:

- `LivenessFace`: real-person portrait assets. These are the user/account's registered real-person portrait materials.
- `AIGC`: virtual portrait assets. This covers virtual portrait materials exposed through the Assets API, including private virtual portraits where available.

The user-facing UI should call these:

- 真人认证人像
- 虚拟人像

Do not collapse both into a generic "avatar" concept internally. The group type matters for API filtering, permissions, status display, and future upload/registration workflows.

Updated 2026-06-09 after user review:

- The target account is expected to have the entitlement needed for both asset families.
- `AIGC` should be usable for virtual portrait assets.
- `LivenessFace` will only return useful data after real-person portraits have been registered.
- This implementation is application-only: browse and use existing assets. It does not include real-person verification, portrait registration, or new asset creation.
- Keep both families visible in the same Portrait Asset node, but preserve their group type in state, filters, badges, and request payloads.

## Asset Discovery And Search Strategy

Official docs confirm the following Assets API search capabilities:

- `ListAssetGroups` supports `Filter.GroupType`, `Filter.Name`, and `Filter.GroupIds`.
- `ListAssets` supports `Filter.GroupType`, `Filter.GroupIds`, `Filter.Statuses`, and `Filter.Name`.
- `GetAsset` returns a single asset with fields such as `Id`, `Name`, `URL`, `Status`, `GroupId`, `AssetType`, and project metadata.

The public docs and installed Ark SDK do not expose a separate SDK method for the Volcengine Experience Center's natural-language virtual portrait search. The virtual portrait library docs describe the web UI as supporting natural-language and conditional filtering, but the published Assets API docs emphasize name/group/status filtering. Therefore this project should not assume that the official natural-language search UI can be replicated by one documented API call.

Implementation decision:

- Use Volcengine Assets API as the authoritative source of asset IDs and preview URLs.
- Page through asset metadata; do not eagerly download all preview images.
- Build a local text index from returned metadata fields such as `Name`, `Title`, `Description`, tags if returned, and any catalog/bio fields exposed by the API.
- Add local UI filters for common portrait attributes: gender, age band, nationality/region, and profession/role.
- If the public/prebuilt virtual portrait library is not returned by `ListAssets` for the current account/project, inspect the Volcengine console experience page network requests and add a documented adapter only if the request shape is stable and appropriate to use. Do not scrape DOM as the primary data source.
- If no real-person assets exist in `LivenessFace`, treat the library as empty rather than as an error.

Expected result shape for a portrait search result:

```json
{
  "asset_id": "asset-20260224225721-prllw",
  "asset_uri": "asset://asset-20260224225721-prllw",
  "name": "荷兰女性花卉种植员",
  "group_type": "AIGC",
  "status": "Active",
  "preview_url": "/api/volcengine/assets/preview/asset-20260224225721-prllw",
  "tags": ["荷兰", "女性", "多肉/花卉种植员"],
  "bio": "22岁荷兰女性，是郁金香种植员，每天在花田里除草剪枝忙到日落。工作日休息时就举着运动相机拍花田的云雀，摘花时总哼荷兰民谣。"
}
```

The node's direct search results should be portrait cards. After the user selects a card, the node should show the portrait preview plus text metadata: portrait tags, asset ID/URI, and biography/description where available.

## Current Project Gap

Current project state:

- `app/upstream_volcengine.py` supports Seedream and Seedance through `volcenginesdkarkruntime`.
- Current Seedance reference images are converted to data URLs for local files.
- The project does not yet have Volcengine AK/SK configuration for Assets API.
- The project does not yet have `ListAssetGroups`, `ListAssets`, `GetAsset`, `CreateAssetGroup`, or `CreateAsset` wrappers.
- The project does not yet have an image-like canvas node whose output is a trusted `asset://` URI.

Original upstream reference:

- `C:\src\Original-Infinite-Canvas\main.py` has a partial trusted asset implementation using `CreateAssetGroup`, `CreateAsset`, and `GetAsset`.
- It also stores `asset://` registration results in asset-library metadata and has a `trusted_asset` path in the video request flow.
- That implementation is useful as reference for request signing and asset registration, but it is not a dedicated two-library browser/search portrait node.

## Product Shape

Add a new canvas node:

```text
人像资产 / Portrait Asset
```

The node is an image-like source node. It outputs one selected portrait asset as an `AIReference`-compatible object:

```json
{
  "url": "asset://asset-...",
  "name": "asset display name",
  "role": "reference_image",
  "kind": "portrait_asset",
  "asset_id": "asset-...",
  "group_id": "group-...",
  "group_type": "LivenessFace",
  "status": "Active",
  "preview_url": "/api/volcengine/assets/preview-cache/asset-..."
}
```

Seedance should see it exactly like an image reference, except the URL remains `asset://...`.

## Backend Plan

### 1. Configuration

Add first-party settings for Assets API:

- `VOLCENGINE_ACCESS_KEY_ID`
- `VOLCENGINE_SECRET_ACCESS_KEY`
- `VOLCENGINE_PROJECT_NAME`, default `default`
- `VOLCENGINE_REGION`, default `cn-beijing`

Keep `VOLCENGINE_ARK_API_KEY` for inference. Assets API uses AK/SK signing, while Seedance task creation uses the Ark API key path already present in this project.

API settings UI:

- Add `Volcengine Access Key ID` and `Volcengine Secret Access Key` fields to the project's existing API Settings panel.
- These are user-managed settings, like the existing Ark API key.
- The fields should display masked previews after saving.
- The backend must never log the full AK/SK, return the full SK to the browser after save, or include it in error messages.
- `C:\src\Infinite-Canvas\API\s.txt` currently contains sensitive Volcengine credentials and is already gitignored. Do not read, print, copy, commit, or embed this file's contents. It can be used by the user as their private source when manually filling the settings panel, but the implementation should not depend on parsing that file.

### 2. New Module

Create `app/upstream_volcengine_assets.py`.

Responsibilities:

- Sign Volcengine OpenAPI requests.
- Call `ListAssetGroups`.
- Call `ListAssets`.
- Call `GetAsset`.
- Later: call `CreateAssetGroup`, `CreateAsset`, `UpdateAsset`, `DeleteAsset`.
- Normalize every active asset to a stable local shape.

Suggested functions:

```python
def asset_uri(asset_id: str) -> str
async def list_asset_groups(group_type: str, query: str, page: int, page_size: int) -> dict
async def list_assets(group_type: str, group_id: str, query: str, statuses: list[str], page: int, page_size: int) -> dict
async def get_asset(asset_id: str, project_name: str = "") -> dict
async def cached_asset_preview(asset_id: str, refresh: bool = False) -> str
def build_portrait_search_index(items: list[dict]) -> list[dict]
```

### 3. New Routes

Add an assets router, for example `app/routes/volcengine_assets.py`.

Initial endpoints:

```text
POST /api/volcengine/assets/groups
POST /api/volcengine/assets/list
POST /api/volcengine/assets/detail
GET  /api/volcengine/assets/preview/{asset_id}
```

Second phase endpoints:

```text
POST /api/volcengine/assets/group/create
POST /api/volcengine/assets/create
POST /api/volcengine/assets/status
```

### 4. Preview Cache

Use two caches.

Metadata cache:

```text
data/volcengine_assets_cache.json
```

Cache key should include:

- project name
- group type
- group id
- query
- statuses
- page
- page size

Recommended TTL:

- `ListAssetGroups`: 5-15 minutes
- `ListAssets`: 1-5 minutes
- `GetAsset`: 30-60 seconds for non-Active assets, 5-15 minutes for Active assets

Preview cache:

```text
assets/cache/volcengine_assets/<asset_id>.<ext>
```

The preview cache is populated from the temporary `URL` returned by `ListAssets` or `GetAsset`. It is only for display. If refresh fails but a local cached preview exists, the UI may keep showing it with a stale badge.

Updated 2026-06-09 after user review:

- Preview images should be cached long term on the user's local machine because the virtual portrait library is not expected to change frequently.
- Metadata does not need long-term caching if it is small enough to fetch quickly; keep only lightweight session/short-TTL metadata cache unless real API testing shows it is slow.
- Missing or expired preview images should degrade gracefully: show an empty thumbnail or placeholder in the node, not a blocking error.

### 5. Seedance Pass-Through

Update `app/upstream_volcengine.py` so `_data_url()` explicitly passes through trusted asset URIs:

```python
if url.startswith("asset://"):
    return url
```

Do not call `imageproc.reference_to_data_url()` for asset URIs.

## Frontend Plan

### 1. Add Node Type

Add `portraitAsset` to `static/canvas.html`.

Menu label:

```text
人像资产
```

Node controls:

- Library segmented control: 真人认证 / 虚拟人像.
- Search input with debounce.
- Asset group selector.
- Status filter: Active / Processing / Failed / All.
- Attribute filters for portrait browsing: gender, age band, nationality/region, profession/role.
- Sort selector.
- Lazy grid with thumbnails.
- Load more button or scroll pagination.
- Detail preview with name, tags, biography/description, asset ID, URI, status, group, update time.
- Role selector: reference image / first frame / last frame.
- Selection model: exactly one portrait per node.

### 2. Dynamic Loading

Do not load the entire library. The node should request one page at a time:

```text
page_size = 24 or 48
```

Search and filters should reset pagination. Thumbnail images should use browser lazy loading and the backend preview cache URL.

Text metadata may be cached more aggressively than preview media. If the virtual portrait catalog is large, the project can page through metadata to build a local searchable text catalog, but preview images should still load on demand.

### 3. Canvas Source Contract

Treat `portraitAsset` as an image-like source for Seedance.

Add it to source collection paths used by:

- `generatorSources()`
- `orderedSources()`
- `seedanceImagePayload()`
- connection validation
- copy/paste serialization

The source preview should use `preview_url`; the source payload should use `url: asset://...`.

### 4. Seedance Slot UI

Seedance slots should display:

- cached thumbnail
- asset name
- badge: 真人认证 or 虚拟人像
- badge: Active only; Processing/Failed should block submission

When connected to Seedance, the payload should become:

```json
{
  "url": "asset://asset-...",
  "name": "人像名称",
  "role": "reference_image"
}
```

Seedance-side prompt order rule:

- The prompt's `图片 1`, `图片 2`, etc. refer to the Seedance node's visible reference-image slot order.
- If the user reorders slots in the Seedance node UI, the generated request content order must follow that reordered UI order.
- This rule is documented here only to prevent integration ambiguity. It is not implemented by the Portrait Asset node.
- The Portrait Asset node itself has no prompt editor and does not modify user prompt text.
- The Seedance node may display the slot-to-asset mapping, but it should not auto-insert or rewrite prompt references.

## Implementation Phases

### Phase 1: Browse And Use Existing Assets

Goal: use existing Volcengine portrait assets in Seedance.

Tasks:

- Add AK/SK/project settings.
- Add Assets API signed request helper.
- Add group and asset listing routes.
- Add preview cache route.
- Add `portraitAsset` node.
- Allow `asset://` pass-through in Seedance.
- Connect `portraitAsset` to Seedance as a reference image source.
- Support both `AIGC` virtual portrait assets and `LivenessFace` real-person portrait assets.
- Treat empty `LivenessFace` results as a normal empty state.

Verification:

- Query `LivenessFace` groups and assets.
- Query `AIGC` groups and assets.
- Select an Active asset.
- Submit Seedance request and verify backend payload contains `asset://...`.

### Phase 2: Search, Cache, And UX Polish

Goal: make large libraries usable.

Tasks:

- Search debounce.
- Pagination / infinite loading.
- Group filters.
- Status and stale-cache badges.
- Preview cache invalidation.
- Manual refresh button.
- Error states for missing AK/SK, missing IAM permission, missing entitlement.

### Phase 3: Asset Registration

Goal: register new assets from local library/canvas.

Out of scope for the first implementation. This phase is retained only as a later roadmap item.

Tasks:

- Create asset group.
- Upload/create asset by public URL.
- Poll `GetAsset` until Active/Failed.
- Store registration metadata in local asset library.
- Support separate flows for `LivenessFace` and `AIGC`.

Important: local files must be exposed through a public URL before `CreateAsset`, or uploaded by an official supported upload path if Volcengine adds one.

### Phase 4: Prompt Assistance

Goal: reduce user confusion about how to refer to selected assets.

Tasks:

- Display "图片 1", "图片 2" mapping in Seedance.
- Provide visible slot mapping only; do not add prompt controls to the Portrait Asset node.
- Do not inject raw `asset://` into prompt text.
- Do not automatically rewrite the user's prompt.

Out of scope for the Portrait Asset node. Any work here should be treated as a Seedance node UI/documentation improvement.

## Open Questions

- Resolved: the account is expected to have access to the needed asset families. `AIGC` should be available, while `LivenessFace` depends on registered real-person data.
- Resolved: one `portraitAsset` node selects exactly one portrait. Multiple portraits should be represented by multiple nodes connected to Seedance.
- Resolved: this implementation is application-only. Do not implement real-person verification, registration, or asset creation in the first pass.
- Resolved: prompt image numbering is not owned by the Portrait Asset node. If needed, it belongs to Seedance node UI behavior.
- Resolved: long-term local cache is acceptable for preview images; metadata can remain short-lived unless API testing says otherwise.
- Resolved: missing real-person assets and missing preview images are not hard errors.
- Remaining API task: inspect the Volcengine console virtual portrait page network requests at `https://console.volcengine.com/ark/region:ark+cn-beijing/experience/vision?modelId=doubao-seedance-2-0-260128&tab=GenVideo` to confirm whether the prebuilt virtual portrait library is exposed by a documented/stable request shape or only by the console's private experience API.

## Volcengine AK/SK Location

The Assets API requires Access Key ID and Secret Access Key (AK/SK), not only the Ark API key used for Seedance inference.

Official guidance:

- Go to the Volcengine console.
- Open the top-right account avatar menu.
- Enter `API访问密钥` / `密钥管理`.
- Create or view an Access Key ID and Secret Access Key.
- Main accounts and IAM users can have access keys; roles cannot.
- For an IAM user, create/view keys from IAM user details under the `密钥` tab, and grant the minimum permissions needed for Ark Assets operations.

References:

- Access Key management: https://www.volcengine.com/docs/6291/65568
- Access key list API: https://www.volcengine.com/docs/6291/65579



## Decision

For this project, implement portrait assets as trusted asset URI references.

The UI may cache and show preview images, but the Seedance request must pass `asset://<asset_id>` through unchanged. Pulling down the preview image and sending it as a normal reference image is the fallback path only for non-trusted ordinary image nodes, not for the new portrait asset node.

## Update 2026-06-09: Remove Paid Private AIGC Asset Browsing

Follow-up testing showed that calling `ListAssets` / `ListAssetGroups` with `GroupType=AIGC` hits the paid private virtual portrait asset capability and can return `SubscriptionRequired AIGC asset capability is not available on your current plan`. This is not the same thing as the free/platform-provided preset virtual portrait library visible in the Ark experience center.

Implementation direction is corrected as follows:

- Do not support paid private `AIGC` virtual portrait asset browsing in this project.
- The Portrait Asset node's virtual mode should represent platform preset virtual portraits copied from the Ark experience center.
- Until the experience center's preset virtual portrait list/search API is captured and confirmed, virtual mode uses a manual `asset ID` / `asset://` entry field.
- The backend Assets API browsing endpoints should only support `LivenessFace` real-person portrait assets.
- Seedance usage remains unchanged: pass `asset://<asset_ID>` in the reference image slot, while prompt wording is still managed by the Seedance node/user prompt order.

## Update 2026-06-09: Node Must Show Portrait Results and Output a Reference

The manual `asset ID` / `asset://` entry-only design is not acceptable for the product UI. It hides the actual portrait choice from the user and makes the Portrait Asset node feel like a note field instead of a source node.

Corrected design:

- The Portrait Asset node must have an output port.
- The node must show searchable portrait cards with preview images inside the node UI.
- Selecting a portrait card sets `selectedAsset` and makes the node output `asset://<asset_ID>` to connected Seedance nodes.
- The preview image is only for user inspection and local UI cache. Seedance still receives the trusted asset URI, not the downloaded preview image.
- Prompt text remains outside this node. Reference order and "图片 1 / 图片 2" wording belong to the Seedance node/input order, not to the Portrait Asset node.

Implementation note:

- `GetAsset` works for known platform preset virtual portrait asset IDs and can return a real preview URL; the existing preview route can cache that image locally under `assets/cache/volcengine_assets/`.
- `ListAssets(GroupType=AIGC)` still must not be used for the platform preset library because it targets the paid private AIGC asset capability.
- Until the Ark experience center's private list/search endpoint is captured, the preset virtual search endpoint reads a local catalog (`data/volcengine_preset_portraits.json`) plus a small built-in seed list of known public examples. This makes the node UI and output contract correct now, while keeping the data source replaceable once the real console catalog endpoint is confirmed.
