# 05 — Risks and Mitigations

Things that look easy on paper but tend to bite during a port.

## 1. JSON shape compatibility

**Risk.** The frontend was built against the Python responses. Any
field rename, casing change, or null-vs-omitted difference will break
the UI silently (no compile-time check on the JS side).

**Mitigation.**

- Every Go DTO uses `json` tags that exactly match the Python
  serialization (`snake_case`).
- For each ported endpoint, run both servers and `diff` the JSON of
  the same request before considering the port complete.
- Be deliberate about `omitempty`: Python's `json.dumps` includes
  `None` as `null`; Go's default also includes the zero value.
  Use pointer types or explicit `omitempty` to match.

## 2. "Probe many JSON paths" extraction code

**Risk.** Functions like `extract_image(data)` and
`text_from_chat_response(data)` probe 5-10 candidate paths in a
free-form dict to handle differences across upstream providers
(OpenAI vs Apimart vs ModelScope vs ComfyUI). In Python this is
`data.get("a", {}).get("b") or data.get("c")[0].get("url")`.
In Go this becomes verbose nested type assertions.

**Mitigation.**

- Put all such probing in `internal/upstream/extract.go`.
- Use a small helper like `dig(m map[string]any, path ...string) (any, bool)`
  to flatten the verbosity.
- Cover each probing function with a table-driven test using captured
  real upstream responses (drop them in `internal/upstream/testdata/`).

## 3. SSE streaming for LLM chat

**Risk.** FastAPI's `StreamingResponse` makes SSE trivial. In Gin you
must:

1. Set headers manually (`Content-Type: text/event-stream`,
   `Cache-Control: no-cache`, `Connection: keep-alive`).
2. Write directly to `c.Writer` and call `c.Writer.Flush()` after
   every chunk.
3. Honor `c.Request.Context().Done()` to stop on client disconnect.

**Mitigation.**

- One `server.SSE(c *gin.Context, ch <-chan string)` helper that does
  this correctly once. All chat handlers use it.

## 4. Image encoder output is not byte-identical

**Risk.** JPEG encoded by Go's `image/jpeg` will not produce identical
bytes to Pillow's libjpeg encoder at the same `quality=88`. Tests that
diff bytes will fail.

**Mitigation.**

- Don't byte-diff images. Diff dimensions, mode, and a perceptual
  hash. Or eyeball a few samples.
- Document that re-running the same generation on the Go server may
  produce a different (but visually equivalent) output file.

## 5. Lanczos resize implementation differences

**Risk.** `imaging.Lanczos` uses a 3-tap Lanczos. Pillow's
`Image.LANCZOS` also defaults to 3-tap. Output dimensions match;
pixel values are within ~1% on edges.

**Mitigation.**

- For thumbnails (where this matters least), accept the difference.
- For reference image preprocessing fed back to an AI model, the
  difference is far below the model's input sensitivity. No-op.

## 6. WebSocket back-pressure

**Risk.** Python's `ConnectionManager.broadcast_new_image` iterates
all connections and `await`s each send. A slow client blocks the
broadcast for everyone briefly.

In Go you can either replicate this (sequential send under a mutex)
or fix the bug at the same time (each client gets its own send
goroutine + buffered channel; drop on overflow).

**Mitigation.**

- Take the chance to fix it. Per-client buffered channel of e.g. 64
  messages. If full, drop the oldest "stats" message but never drop
  "new_image" — instead, mark the connection dead and close it. The
  client will reconnect.

## 7. Lock granularity

**Risk.** Python uses module-level locks
(`QUEUE_LOCK`, `HISTORY_LOCK`, `CONVERSATION_LOCK`, `CANVAS_LOCK`,
`LOAD_LOCK`, `GLOBAL_CONFIG_LOCK`). Some are coarser than they need
to be.

**Mitigation.**

- `store.LockSet` keyed by `(resource, id)` (e.g.,
  `("canvas", "abc-123")`) gives per-record locking for free.
- For genuinely global locks (history append, providers file), use
  named `sync.Mutex` in the relevant store.

## 8. Goroutine leaks on polling loops

**Risk.** `wait_for_image_task` polls every N seconds. If the HTTP
client cancels the request (e.g., browser closes the tab), Python's
asyncio cancellation propagates automatically. In Go you must
propagate `ctx` and check it in the loop.

**Mitigation.**

- Every upstream polling function takes `ctx context.Context` as the
  first argument.
- `select { case <-ctx.Done(): return ctx.Err(); case <-time.After(...): }`
  instead of `time.Sleep`.
- Code review checklist item.

## 9. Embedded workflows vs user-saved workflows

**Risk.** Embedded defaults (compiled in) and user edits (on disk) can
diverge. Users expect their edits to win.

**Mitigation.**

- On startup, list `web/workflows/*.json` (embedded names) and
  `./workflows/*.json` (on-disk names) — union them, disk wins on
  conflict.
- Document that "factory defaults" can be restored by deleting the
  on-disk file.

## 10. Time zone and timestamp format

**Risk.** Python's `time.time()` returns seconds as float; the
codebase also uses `now_ms()` which returns `int(time.time() * 1000)`.
Go's `time.Now().UnixMilli()` returns int64. JSON encoding may differ
(scientific notation, trailing zeros).

**Mitigation.**

- Always use `int64` ms in DTOs. `time.Now().UnixMilli()` everywhere.
- Custom JSON marshaling not needed since Go encodes int64 as plain
  digits.

## 11. The `safe_user_id` and request fingerprinting

**Risk.** `safe_user_id` derives an ID from headers / cookies / IP.
The exact derivation matters because canvas / conversation
ownership keys off it.

**Mitigation.**

- Port the function bit-for-bit. Add a test that feeds it identical
  requests and compares outputs to the Python version.
- Don't "clean up" the algorithm during the port.

## 12. Validation error message format

**Risk.** `friendly_validation_error` formats Pydantic v2 errors into
a Chinese-localized message. The frontend probably depends on that
format.

**Mitigation.**

- Don't try to match Pydantic's error structure 1:1. Instead, write a
  small Gin error handler that produces the same outer shape
  (`{"error": "...", "details": [...]}`) the frontend expects.
- Grep the frontend for how it consumes validation errors before
  finalizing the format.
