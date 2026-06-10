# 07 — Volcengine Ark Go SDK Research

Updated 2026-06-10.

This note records whether Volcengine blocks the planned Go migration.
Short answer: it does not. The project uses Ark for Seedream,
Seedance, and portrait asset-library browsing; the Go side has enough
official SDK coverage for generation, and the remaining asset calls can
be ported from the current hand-signed Python implementation.

## Current Python surface

Tracked files:

- `requirements.txt` declares `volcengine-python-sdk[ark]>=5.0.12`.
- `app/upstream_volcengine.py` uses `volcenginesdkarkruntime.Ark`.
- `app/upstream_volcengine_assets.py` uses `httpx` plus a local
  Volcengine HMAC-SHA256 Signature V4 helper.

Current capabilities:

| Area | Python implementation | Notes |
|---|---|---|
| Seedream image generation | `client.images.generate(...)` | Requests b64 output and optionally streams. |
| Seedance video task create | `client.content_generation.tasks.create(...)` | Sends text, image, video, audio refs. |
| Seedance task get/list | `client.content_generation.tasks.get/list(...)` | Normalizes status, videos, missing tasks. |
| Ark asset groups | hand-signed `ListAssetGroups` | Uses AK/SK, project, region. |
| Ark assets | hand-signed `ListAssets`, `GetAsset` | Also caches previews locally. |
| Preset virtual portraits | hand-signed `ListMediaAssetGroup` | Normalizes platform asset-group response. |

## Go SDK status

Official repositories / docs checked on 2026-06-10:

- `github.com/volcengine/volcengine-go-sdk`
  - Repository: <https://github.com/volcengine/volcengine-go-sdk>
  - Go package docs: <https://pkg.go.dev/github.com/volcengine/volcengine-go-sdk/service/arkruntime>
- `github.com/volcengine/volc-sdk-golang`
  - Repository: <https://github.com/volcengine/volc-sdk-golang>
  - This appears to be an older / broader service SDK. For Ark runtime
    work, prefer `volcengine-go-sdk`.
- Ark API docs:
  - SDK install / upgrade:
    <https://www.volcengine.com/docs/82379/1541595?lang=zh>
  - Image / video generation API index:
    <https://www.volcengine.com/docs/82379/1520757?redirect=1&lang=zh>

The `arkruntime` package exposes the methods we need:

| Needed by project | Go SDK method |
|---|---|
| Seedream image generation | `GenerateImages` |
| Seedream streaming image generation | `GenerateImagesStreaming` |
| Seedance create task | `CreateContentGenerationTask` |
| Seedance get task | `GetContentGenerationTask` |
| Seedance list tasks | `ListContentGenerationTasks` |
| Optional delete task | `DeleteContentGenerationTask` |

The package also supports:

- `NewClientWithApiKey(apiKey, ...)`
- `NewClientWithAkSk(ak, sk, ...)`
- `WithBaseUrl(...)`
- `WithRegion(...)`
- `WithTimeout(...)`
- `WithProjectName(...)`

That is enough to reproduce the current Ark runtime path without
keeping Python around.

## Proposed Go packages

```
internal/upstream/volcengine.go
internal/upstream/volcengine_assets.go
internal/upstream/volcengine_sign.go
internal/upstream/volcengine_test.go
internal/upstream/testdata/
  seedream_generate_response.json
  seedance_task_create.json
  seedance_task_get_succeeded.json
  volcengine_asset_get.json
```

Suggested boundaries:

- `volcengine.go` owns Ark runtime generation:
  - `GenerateSeedreamOnce(ctx, req, index)`
  - `SubmitSeedance(ctx, req)`
  - `PollSeedance(ctx, taskIDs)`
  - `ListSeedanceTasks(ctx, filter)`
- `volcengine_assets.go` owns user / preset asset browsing:
  - `ListAssetGroups(ctx, req)`
  - `ListAssets(ctx, req)`
  - `GetAsset(ctx, id)`
  - `SearchPresetVirtualPortraits(ctx, req)`
  - `CachedAssetPreview(ctx, assetID, refresh)`
- `volcengine_sign.go` owns Signature V4 request signing:
  - `SignV4Headers(ak, sk, action, body, region)`
  - `AssetCall(ctx, action, body)`

## Porting notes

Do not let SDK-specific structs leak into handlers. Keep the frontend
response shape identical to Python:

```json
{
  "status": "succeeded",
  "task_ids": ["..."],
  "tasks": [],
  "videos": [],
  "raw": {}
}
```

For Seedream, preserve:

- model alias validation from `config.SEEDREAM_MODELS`
- `response_format: "b64_json"`
- `watermark`
- `seed + index`
- reference image conversion to `data:` URLs when local
- pass-through for `http://`, `https://`, `data:`, and `asset://`

For Seedance, preserve:

- content item roles (`first_frame`, `last_frame`, `reference_image`,
  `reference_video`, `reference_audio`)
- media validation limits from Python
- duration caps for Seedance 1.5 vs newer models
- pending / succeeded / failed normalization
- fallback from direct task get to list lookup when a task is missing

For assets, preserve:

- default region `cn-beijing`
- default project `default`
- `ark.<region>.volcengineapi.com`
- cache path under `assets/cache/volcengine_assets`
- `asset://` URI passthrough

## Migration recommendation

Port Volcengine after the core server, store, static serving, and image
helpers are stable, but before the final cutover. Generation endpoints
are user-visible and should be proven in Go while Python remains
available for parity testing.

Good milestone:

1. Start Python on port 3000 and Go on port 8080.
2. Send the same Seedream request to both.
3. Verify both return an image and the Go response keeps the same
   frontend-facing fields.
4. Submit a Seedance task from Go.
5. Poll it from both Python and Go using the same task id.
6. Browse one asset group and preview one asset from Go.

Once those pass, Volcengine should be considered migrated enough for
the Go release branch.
