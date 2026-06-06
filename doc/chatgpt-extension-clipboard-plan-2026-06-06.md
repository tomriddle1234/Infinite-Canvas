# Chat Image Extension Clipboard Bridge Plan - 2026-06-06

## Goal

Build a small Chrome extension that acts as a manual, user-confirmed clipboard bridge between chat-style web image generation pages and Infinite Canvas.

ChatGPT is the first target page, but the extension should not be hard-coded as a ChatGPT-only tool. The same clipboard and capture model should work for other conversational image-generation web UIs that follow the common pattern:

- User prompt message.
- Generated assistant/result area below the prompt.
- One or more visible images after generation finishes.

The extension is not intended to automate any website as an API replacement. It should help the user move prompts and generated images between already-open browser tabs:

- Generator tabs: receive prompts, detect/capture generated images, and keep prompt/image pairings visible.
- Infinite Canvas tab: send prompts to the extension and receive selected images as new image nodes.

## Product Shape

The extension opens a right-side in-page panel when the Chrome extension button is clicked. The same panel design should appear on supported generator pages and Infinite Canvas pages, but available actions depend on the current page.

The panel behaves like a specialized clipboard manager:

- No setup screen in the first version.
- No account login or remote backend.
- No background batch generation.
- No automatic ChatGPT submission.
- Manual confirmation for importing images into the canvas.
- No long-term history in the MVP.
- A browser restart should reset the extension clipboard to a fresh empty state.

## Reference: Prompt Holder

User-provided reference source:

- `C:\src\Prompt-Holder`
- License: AGPL-3.0
- Relevant files:
  - `Extension/manifest.json`
  - `Extension/js/storage.js`
  - `Extension/js/common.js`
  - `Extension/js/background.js`
  - `Extension/js/content_script.js`
  - `Extension/js/side_panel.js`
  - `Extension/pages/side_panel.html`

Useful ideas to borrow conceptually:

- Manifest V3 structure with `background.service_worker`.
- Chrome `sidePanel` support for a stable right-side browser UI.
- Local-first storage via `chrome.storage.local`.
- Storage-change listener to keep side panel views fresh.
- Prompt records that can include image attachments.
- Message routing from side panel -> background -> active tab content script.
- Generic input detection across AI chat sites.
- Multi-strategy prompt/image insertion: contenteditable, textarea/input, clipboard, file input, paste event, and drop event.

Important license note:

- Prompt Holder is AGPL-3.0. Do not copy source code directly into this project unless we intentionally accept and comply with AGPL obligations.
- Use it as architecture and UX reference only for the MVP unless the user explicitly decides to incorporate AGPL code.

Design impact:

- Prefer Chrome's native side panel as the first UI container if it satisfies the desired right-side workflow.
- Keep an injected in-page panel as a fallback only if native side panel cannot access enough page context or does not feel integrated enough.
- Reuse the idea of a small storage abstraction, but create our own data model for prompt/image pairings, source tabs, and Infinite Canvas import status.
- Do not copy Prompt Holder's long-term library/history model for the MVP.
- Reuse the idea of a generic input-target finder, but route page-specific logic through our adapter layer.

## Core UI

Use a compact three-column table-like layout.

### Column 1: Prompts

Purpose: show the prompt queue and provide a stable anchor for pairing images.

Each prompt row should include:

- Prompt text preview.
- Source indicator: Canvas, ChatGPT captured, or manual paste.
- Created time.
- Active/selected state.
- Optional short label generated from the first line or first N characters.

Actions:

- Add prompt manually.
- Paste from clipboard.
- Fill current ChatGPT input with this prompt.
- Mark prompt as active target for the next captured image.
- Delete prompt from extension clipboard.

### Column 2: Images

Purpose: show images captured from ChatGPT and allow correction when pairing is wrong.

Each prompt row can contain zero, one, or many images.

Each image item should include:

- Thumbnail preview.
- Approximate file size when known.
- Resolution when known.
- Capture time.
- Status: captured, ready, imported, failed.
- A small warning state if the image was captured by heuristic rather than explicit selection.

Required interaction:

- Images must be draggable between prompt rows.
- A prompt row must accept multiple images.
- Image cards should support selection.
- Image cards should support remove/unpair without deleting the prompt.

### Column 3: Controls

Purpose: keep the workflow explicit and low-friction.

Initial controls:

- Capture image: scan the current ChatGPT page and capture the most likely latest generated image.
- Create node: send the selected image to Infinite Canvas and create an image node.
- Open/Focus Canvas: optional helper if no Infinite Canvas tab is available.
- Refresh page scan: re-run prompt/image DOM detection.
- Clear imported: optional cleanup for already-imported images.

Page-dependent behavior:

- On supported generator pages, Capture image is enabled.
- On Infinite Canvas pages, Create node is enabled when an image is selected.
- On other pages, Create node should be disabled or become a no-op with a small inline message.

Capture controls should include two modes:

- Capture current tab: scan only the active generator tab.
- Capture opened generator tabs: ask all participating generator tabs where the extension panel has been opened to scan for new images.

## Manual Version Workflow

### Prompt To Generator Page

1. User selects or creates a prompt in Infinite Canvas.
2. Infinite Canvas content script sends the prompt to the extension clipboard.
3. User opens or focuses a supported generator page.
4. User opens the extension panel.
5. User clicks Fill input on a prompt row.
6. Extension inserts the prompt into the page's chat input when the page adapter supports it.
7. User manually clicks the site's send/generate button.

### Generator Image To Canvas

1. User waits for image generation to finish.
2. User opens the extension panel on one or more generator tabs.
3. User clicks Capture image.
4. Extension scans the current tab or all opened participating generator tabs, depending on the selected capture mode.
5. Captured image appears in Column 2 under the best prompt match.
6. User corrects pairing by dragging the image to another prompt row if needed.
7. User selects the image and goes to the Infinite Canvas tab.
8. User clicks Create node.
9. Extension sends the original image file/blob plus metadata to Infinite Canvas.
10. Infinite Canvas creates an image node.

## Capture Heuristic

The first version should avoid private/internal site APIs and rely only on visible page DOM and user-visible image resources.

Initial heuristic:

1. Locate the relevant prompt text in the page's conversation DOM.
2. Starting from that prompt message, scan forward/downward through later DOM nodes.
3. Prefer image candidates that appear after the prompt and before the next user prompt.
4. Prefer candidates with a retrievable file/blob size larger than 1.5 MB.
5. Prefer the newest image candidate if multiple candidates pass.
6. Ignore small UI images, avatars, icons, and thumbnails.

Candidate filters:

- Element is `img`, `picture img`, canvas export target, or download-link image target.
- Natural width and height are above a practical threshold, for example 512 x 512.
- File size is above 1.5 MB when known.
- Element is visible in the viewport or near the current conversation area.
- Element source is not an SVG, icon, avatar, tracking pixel, or site asset.

Fallbacks:

- If size cannot be known because of CORS or blob restrictions, use resolution and DOM position.
- If no confident match exists, show a "needs review" state and let the user choose from detected candidates.
- If direct blob fetch fails, provide a "paste image from clipboard" fallback after the user uses the site's copy/download affordance.

## DOM Location Strategy

The first implementation should make prompt anchoring explicit and predictable. The best default strategy is to search from the bottom of the conversation upward, because users usually want the latest generated result.

### Prompt Anchor Search

Input:

- The active prompt row selected in the extension, or the newest prompt row if no row is selected.
- The current page document.
- Optional site adapter hints for message containers.

Algorithm:

1. Normalize the prompt text from the extension:
   - Trim whitespace.
   - Collapse repeated whitespace.
   - Remove invisible control characters.
   - Keep punctuation, but compute a second comparison string with punctuation simplified.
   - Limit matching text to a practical length, for example the first and last 500-1000 characters.
2. Collect candidate text containers from the page:
   - Prefer adapter-provided user-message containers.
   - Else inspect visible `article`, `[role="article"]`, `[data-message-author-role]`, main chat blocks, and large text blocks.
   - Ignore hidden elements, nav/sidebar text, buttons, inputs, code-only blocks, and very small labels.
3. Traverse candidate containers from bottom to top.
4. Score each candidate against the prompt using fuzzy matching:
   - Exact normalized substring match gets highest score.
   - Candidate contains the first meaningful prompt segment.
   - Candidate contains the last meaningful prompt segment.
   - Token overlap is high.
   - Length ratio is plausible.
5. Pick the highest-scoring bottom-most candidate above a confidence threshold.
6. If confidence is low, fall back to the active prompt row and scan for the latest large image in the visible conversation area, marking the capture as needs review.

This handles common cases where the user edited the prompt before sending it, the site rewrote line breaks, or the displayed prompt truncates whitespace.

### Search Below Prompt

Once a prompt anchor is found:

1. Find the closest message/conversation container for the anchor.
2. Start scanning forward through following sibling blocks and descendants.
3. Stop at the next likely user prompt container, unless the adapter says the site nests multiple result blocks differently.
4. Collect visible image candidates in that range.
5. If no candidates are found in the bounded range, expand to later visible conversation blocks below the anchor.
6. Prefer images that are lower than the prompt anchor on screen or later in DOM order.

This should match the common chat layout where generated images appear below the prompt that produced them.

### Image Candidate Scoring

Score each image candidate with a weighted heuristic:

- Large natural dimensions.
- Known byte size above 1.5 MB.
- Appears after the matched prompt anchor.
- Appears in a result/assistant block rather than user input/sidebar/navigation.
- Newer DOM position.
- Has a downloadable or fetchable source/blob.
- Not already captured by the extension.

Low-confidence candidates should still be shown in a review list rather than silently discarded.

### Duplicate Detection

Before adding a captured image, compare it against existing extension images.

Deduplication keys, from cheapest to strongest:

1. Same source tab id plus same DOM image `src` or download URL.
2. Same conversation URL plus same normalized source URL.
3. Same width, height, byte size, and near-identical capture time/source block.
4. Same blob byte hash when the blob is available.

If a duplicate is detected:

- Do not create a new image entry.
- Optionally update the existing image's source metadata if the new scan has better information.
- Surface a small "already captured" result in the capture summary.

For the MVP, URL/dimension/byte-size dedupe is enough. Byte hashing can be added later when blob capture is reliable.

## Site Adapter Model

The extension should use a small adapter layer rather than hard-coding all DOM logic into one content script.

Recommended adapter interface:

```js
{
  id: "chatgpt",
  label: "ChatGPT",
  matches: ["https://chatgpt.com/*"],
  findPromptMessages(root) {},
  findImageCandidates(root, promptMessage) {},
  fillInput(promptText) {},
  getConversationUrl() {},
  getPageTaskLabel() {}
}
```

First supported adapters:

- ChatGPT.
- Generic chat-image fallback adapter.

Generic fallback behavior:

- Detect visible large images in the main document area.
- Detect nearby editable/chat input fields for prompt fill only when confidence is high.
- Pair images to the nearest preceding large text block or active prompt row.
- Mark captures as lower-confidence so the user reviews them before import.

Later adapters can be added for other chat-style generation sites without changing the panel, storage, or Infinite Canvas import flow.

## Multi-Tab Capture

Users may run several image-generation chats in parallel. The extension should support a manual multi-tab capture mode without background automation.

Terminology:

- Participating generator tab: a supported generator tab where the extension panel has been opened during the current browser session.
- Active capture request: a user-clicked scan command from the extension panel.

Behavior:

1. Each content script registers its tab as participating when the panel opens.
2. `background.js` keeps a lightweight registry of participating generator tabs, including tab id, adapter id, page title, conversation URL, and last seen time.
3. When the user clicks Capture opened generator tabs, `background.js` sends a one-time scan message to each participating generator tab.
4. Each tab scans only its currently loaded visible page DOM and returns image candidates.
5. The panel merges returned candidates into the shared clipboard, grouped by source tab and best prompt match.
6. The user reviews and imports selected images manually.

Constraints:

- Do not continuously poll tabs in the background.
- Do not auto-submit prompts in any tab.
- Do not scan tabs where the extension panel has never been opened, unless the user later explicitly enables a broader permission mode.
- If a tab is sleeping, discarded, closed, logged out, or not reachable, show it as skipped rather than failing the whole capture.
- Deduplicate images by a stable fingerprint when possible, such as source URL plus dimensions plus byte size, or a later byte hash if the blob is available.

UI additions:

- Capture current tab button.
- Capture opened tabs button.
- Source tab badge on each captured image.
- Filter or grouping by source tab.
- Per-tab result summary: found, skipped, failed, no match.

Pairing additions:

- Prompt rows should store optional `sourceTabId`, `siteId`, and `conversationUrl`.
- Image rows should store the tab they came from.
- If multiple tabs use the same prompt, keep images under the same prompt row when the prompt text matches closely, but keep source-tab badges visible.
- If the same prompt exists as separate intentional tasks, allow users to split or duplicate prompt rows later.

## Pairing Rules

Pairing should be helpful but always user-correctable.

Default pairing order:

1. If a prompt row is selected as active, attach the captured image there.
2. Else, match the nearest visible generator prompt text above the image.
3. Else, attach to the newest prompt in the extension clipboard.
4. Else, create an "Unassigned" prompt row.

Correction:

- Drag image between prompt rows.
- Allow one prompt to own multiple images.
- Allow unassigned images.
- Preserve the original capture metadata even after reassignment.

## Session Data Model

Recommended in-memory shape:

```json
{
  "prompts": [
    {
      "id": "prompt_...",
      "text": "Prompt text",
      "source": "canvas|generator|manual",
      "siteId": "chatgpt|generic|...",
      "sourceTabId": 123,
      "createdAt": "2026-06-06T00:00:00.000Z",
      "conversationUrl": "https://chatgpt.com/...",
      "imageIds": ["image_..."]
    }
  ],
  "images": [
    {
      "id": "image_...",
      "promptId": "prompt_...",
      "objectUrl": "blob:...",
      "dataUrl": null,
      "fileName": "webgen-image-20260606-000001.png",
      "mimeType": "image/png",
      "byteSize": 2000000,
      "width": 1024,
      "height": 1024,
      "source": "generator-dom|clipboard|manual-file",
      "siteId": "chatgpt|generic|...",
      "sourceTabId": 123,
      "conversationUrl": "https://chatgpt.com/...",
      "createdAt": "2026-06-06T00:00:00.000Z",
      "status": "captured|ready|imported|failed"
    }
  ],
  "selectedPromptId": "prompt_...",
  "selectedImageId": "image_..."
}
```

The MVP should be session-first:

- Keep clipboard state in the extension service worker and panel runtime.
- Use `chrome.storage.session` if available and needed to survive service-worker suspension.
- Do not use `chrome.storage.local` for prompts, images, or capture history in the MVP.
- Do not persist data across Chrome restart.
- Keep object URLs or in-memory blobs only for the active browser session.
- Send images to Infinite Canvas output storage quickly when the user clicks Create node.
- If session state is lost because Chrome restarts, the user starts with an empty clipboard.

This intentionally avoids building a prompt library, asset archive, or long-term history system in the first version.

## Extension Architecture

### Project Location

Recommended location inside this repository:

```text
extension/
```

For the MVP, put the Chrome extension manifest directly under `extension/`:

```text
extension/
  manifest.json
  background.js
  content-generator.js
  content-canvas.js
  panel.html
  panel.js
  panel.css
  adapters/
    chatgpt.js
    generic-chat-image.js
  icons/
```

Why this location:

- The extension is related to Infinite Canvas, but it is not part of the FastAPI static web app.
- Keeping it outside `static/` avoids mixing browser-extension files with served frontend assets.
- Chrome can load it directly with "Load unpacked" by selecting the `extension/` folder.
- If this repository later contains multiple extensions, this can be changed to `extension/webgen-bridge/`, but that extra nesting is not needed for the first version.

### Files

Expected MVP files:

- `manifest.json`
- `background.js`
- `content-generator.js`
- `content-canvas.js`
- `adapters/chatgpt.js`
- `adapters/generic-chat-image.js`
- `panel.js`
- `panel.css`
- `panel.html` or injected panel template

### Responsibilities

`background.js`:

- Own clipboard state.
- Route messages between tabs.
- Track known Infinite Canvas tabs.
- Track participating generator tabs.
- Expose capture/import commands to content scripts.
- Treat state as session-only, not durable history.

`content-generator.js`:

- Inject panel on supported generator pages.
- Fill the generator page input after user clicks Fill input.
- Scan visible conversation DOM for prompt/image candidates.
- Capture selected image candidates as blobs when browser permissions allow.
- Use site adapters for page-specific selectors and fallback heuristics.

`adapters/*.js`:

- Keep per-site DOM selectors and fill-input behavior isolated.
- Start with `chatgpt` and `generic-chat-image`.

`content-canvas.js`:

- Inject panel on Infinite Canvas pages.
- Send selected canvas prompt text to extension.
- Receive image import requests from the extension.
- Call the existing Infinite Canvas image-node creation path.

`panel.js`:

- Render the shared three-column UI.
- Handle prompt selection, image selection, drag/drop, and command buttons.
- Show page-aware disabled states.

## Infinite Canvas Integration

The extension should not depend on fragile internal DOM interactions if the app can expose a stable browser-side API.

Recommended minimal app bridge:

```js
window.InfiniteCanvasBridge = {
  addPromptToExtensionClipboard(promptPayload) {},
  createImageNodeFromExtension(imagePayload) {}
};
```

If adding a global bridge is undesirable, use `window.postMessage` with a strict message namespace:

- `infinite-canvas:extension:prompt`
- `infinite-canvas:extension:create-image-node`
- `infinite-canvas:extension:image-node-created`
- `infinite-canvas:extension:error`

Image import payload should include:

- Blob or data URL.
- File name.
- MIME type.
- Width and height if known.
- Prompt text.
- Prompt id.
- Generator source URL.
- Capture timestamp.

## Image File Management

Captured generator images must be copied into Infinite Canvas' normal generated-image output storage before a canvas image node is created. The extension should not leave the node pointing at a generator-site URL, a temporary blob URL, a data URL, or `chrome.storage.local`.

Current project storage conventions:

- Generated outputs use the local output category managed by `app.imageproc.output_path_for(filename, "output")`.
- That category currently maps to `assets/output/` and is exposed as `/assets/output/<filename>`.
- Existing older `/output/...` URLs are also supported by asset lookup helpers, but new web-generator imports should prefer the current `/assets/output/...` convention.
- Reference/input uploads such as `/api/nodes/media-upload` save into the input category. That is useful for source media, but generated results imported from web pages should be treated as output artifacts.

Recommended backend endpoint:

```text
POST /api/extension/webgen-import
multipart/form-data:
  file: original image blob
  prompt: prompt text
  prompt_id: extension prompt id
  source_url: current generator conversation URL
  source_site: adapter/site id
  source_tab_title: optional source tab title
  captured_at: ISO timestamp
  width: optional
  height: optional
  byte_size: optional
```

Recommended response:

```json
{
  "url": "/assets/output/webgen_....png",
  "filename": "webgen_....png",
  "content_type": "image/png",
  "width": 1024,
  "height": 1024,
  "byte_size": 2345678,
  "metadata": {
    "prompt": "Prompt text",
    "source": "chat-image-web-extension",
    "source_site": "chatgpt",
    "source_url": "https://chatgpt.com/..."
  }
}
```

Import flow:

1. Extension captures or receives the original image blob.
2. User clicks Create node on an Infinite Canvas tab.
3. The canvas-side content script posts the image blob to `/api/extension/webgen-import`.
4. The backend validates MIME type and size, assigns a safe filename, and writes the original bytes into the output category.
5. The backend returns the local `/assets/output/...` URL.
6. The canvas bridge creates an image node using that local URL.
7. The node metadata keeps the original prompt, capture metadata, and local output URL.

Filename convention:

- Use a prefix such as `webgen_`.
- Include a short timestamp or UUID segment.
- Preserve a safe extension derived from MIME type: `.png`, `.jpg`, or `.webp`.
- Never trust the remote filename from the generator site directly.

Validation:

- Accept only image MIME types needed for the first version: PNG, JPEG, WebP.
- Reject empty files.
- Set a maximum accepted file size. The exact limit can be decided during implementation, but it should comfortably allow high-resolution ChatGPT outputs.
- Optionally verify dimensions with Pillow before writing metadata.
- Do not transcode in the first version unless format validation fails; preserve the original image bytes whenever possible.

History and cleanup:

- Imported ChatGPT images should be treated like other generated output assets for project portability and canvas asset checks.
- If the canvas node is later deleted, file deletion should follow the same policy as other generated outputs; do not invent a special extension-only cleanup rule in the MVP.
- The extension can mark the image as imported after receiving the local URL during the current session, but Infinite Canvas remains the source of truth for the stored file.
- After Chrome restart, the extension does not need to remember which images were imported.

## UX Details

Panel behavior:

- Fixed to the right side of the page.
- Collapsible.
- Width around 420-560 px.
- Resizable later, but fixed width is enough for MVP.
- Keep rows dense and scannable.
- Avoid a settings-heavy first version.

Important states:

- No prompts yet.
- Prompt selected but no images.
- Image captured but not paired confidently.
- Image selected but no Infinite Canvas tab is active.
- Create node succeeded.
- Create node failed.
- Capture found no suitable image.
- Multi-tab capture partially succeeded.
- Multi-tab capture skipped closed, sleeping, or unsupported tabs.

Safety affordances:

- Never auto-submit ChatGPT prompts.
- Never auto-create canvas nodes without a click.
- Never scrape hidden/private endpoints.
- Do not keep trying in the background when a generator page is not active or has not participated.

## Open Questions

- Should the panel be an in-page overlay, a Chrome side panel, or both?
- Should prompt rows come only from Infinite Canvas first, or should generator-side prompt detection create prompt rows too?
- Should image node placement use current viewport center, cursor position, or a drop target in the canvas?
- Should imported image files be copied into the project's existing upload/static asset pipeline?
- Should imported images be deduplicated by hash?
- During the same session, should an image imported to canvas stay visible or move to a small Imported section?
- Which generator sites should get first-class adapters after ChatGPT?
- Should multi-tab capture include only tabs where the panel is open, or all supported generator tabs after an explicit permission prompt?

## Suggested MVP Scope

Phase 1 should include:

- Extension loads on ChatGPT, generic supported generator pages, and Infinite Canvas pages.
- Shared right-side panel renders.
- Manual prompt add/paste.
- Prompt fill into ChatGPT input and high-confidence generic chat inputs.
- Capture current tab using DOM scan.
- Capture opened generator tabs using one-time user-triggered scan.
- Image preview under prompt.
- Drag image between prompt rows.
- Create node button on Infinite Canvas tab.
- Basic success/error messages.
- Session-only state that clears after Chrome restart.

Phase 1 should exclude:

- Batch generation.
- Automatic ChatGPT sending.
- Background polling.
- Durable image library.
- Persistent prompt/image history.
- Multi-user sync.
- Internal generator-site API calls.
- Complex settings UI.

## Implementation Milestones

1. Build static panel UI with prompt/image/control columns.
2. Add extension storage and message bus.
3. Add prompt transfer from Infinite Canvas to extension.
4. Add site adapter layer with ChatGPT and generic fallback adapters.
5. Add Fill input on ChatGPT.
6. Add Capture current tab heuristic.
7. Add participating-tab registry.
8. Add Capture opened generator tabs.
9. Add drag/drop reassignment.
10. Add image payload transfer to Infinite Canvas.
11. Add create image node integration.
12. Test manually across ChatGPT tabs, another generator-style page if available, Infinite Canvas tab, and unrelated pages.

## Verification Checklist

- Extension panel opens on ChatGPT.
- Extension panel opens on at least one generic generator-style page or gracefully falls back.
- Extension panel opens on Infinite Canvas.
- Extension panel either does not open or shows disabled actions on unrelated pages.
- Prompt can be added manually.
- Prompt can be sent from Infinite Canvas to extension.
- Prompt can be filled into ChatGPT input.
- Capture image ignores small UI images.
- Capture image can find a generated image after a prompt.
- Capture opened generator tabs can collect candidates from multiple participating tabs.
- Multi-tab capture reports skipped/failed tabs without losing successful captures.
- Captured image preview is visible in Column 2.
- Image can be dragged to another prompt row.
- One prompt can hold multiple images.
- Create node does nothing outside Infinite Canvas.
- Create node creates an image node inside Infinite Canvas.
- Imported node preserves prompt metadata.
