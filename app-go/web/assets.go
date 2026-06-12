package web

import "embed"

// StaticFiles is the active Go-embedded frontend.
//
// Keep this as an explicit allowlist so ignored local prompt presets are
// never embedded into release binaries.
//
//go:embed static/*.css static/*.gif static/*.html static/*.js static/*.png static/system-prompts/templates/*.json static/vendor
var StaticFiles embed.FS
