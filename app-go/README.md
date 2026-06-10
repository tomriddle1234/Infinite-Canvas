# Infinite Canvas Go Backend

This is the temporary Gin port that runs side-by-side with the existing
Python backend.

Current scope:

- Serves the copied frontend from `web/static` on `http://127.0.0.1:8080/`.
- Serves existing runtime files from the repository root: `data/`,
  `output/`, `assets/`, `workflows/`, and `API/.env`.
- Implements the Phase 1 read-only endpoints from `doc/go-migration`.

Run:

```bat
run_go.bat
```

Or:

```powershell
go run ./cmd/server
```

## Packaging

Updated 2026-06-10: use `package_go.bat` to build the standalone Go package:

```bat
package_go.bat
```

The output is written to `dist/Infinite-Canvas-Go/` and includes
`Start-Go.bat`. Runtime files intentionally keep the same layout and JSON
formats as the Python backend:

- `API/.env`
- `data/api_providers.json`
- `data/canvases/`
- `data/conversations/`
- `data/seedance_tasks.json`
- `history.json`
- `global_config.json`
- `assets/`
- `output/`
- `workflows/`

By default the package creates these directories but does not copy local user
data or API keys. For a private migration package, run:

```bat
package_go.bat -IncludeUserData
```
