# 02 — Image Engine

## What we need

Pulled from `main.py`:

| Operation | Where in Python | Frequency |
|---|---|---|
| PNG → JPEG with white background flatten | `convert_output_to_jpg` (line 1189) | per output download |
| Resize / thumbnail (LANCZOS quality) | `reference_to_data_url`, `compress_data_url_image` (1217, 1243) | per AI request with refs |
| Format detection (PNG / JPEG / WebP) | various | per upload |
| Data URL encode / decode | `compress_data_url_image`, `modelscope_image_url` | per AI request |
| Alpha → opaque background | `convert_output_to_jpg` | per JPEG export |
| Future: text labels, watermarks, side-by-side composition | not yet implemented | TBD |

Plus the user wants the new stack to support **composition** for future
features (overlay nodes, batch grids, annotated outputs).

## Candidates

| Library | Maintained | CGO? | Resize quality | Composition | Notes |
|---|---|---|---|---|---|
| **`disintegration/imaging`** | dormant since 2021 but stable | no | Lanczos / Catmull-Rom / Linear | basic (Paste, Overlay) | Most-used pure-Go image lib. API is clean. |
| **`anthonynsimon/bild`** | active | no | Linear / Nearest / similar | blend modes, segmentation | Newer alternative. API is more verbose. |
| **`fogleman/gg`** | dormant but stable | no | n/a (drawing only) | full Cairo-like (paths, text, gradients, blend) | The composition tool of choice in Go. |
| **`h2non/bimg`** (libvips wrapper) | active | **yes** | excellent | excellent | 10x faster but needs libvips installed on target machine. Breaks single-exe story. |
| **`discord/lilliput`** (libjpeg-turbo + libpng) | active | **yes** | good | limited | Same CGO problem. |
| **`kolesa-team/go-webp`** | active | yes for encode, no for decode | n/a | n/a | WebP encoding wraps libwebp. Decoding via `golang.org/x/image/webp` is pure Go. |

## Decision

**Use `disintegration/imaging` + `fogleman/gg` together.**

- `imaging` for: resize, thumbnail, crop, blur, sharpen, color
  adjustments, format encode/decode.
- `gg` for: composition (overlays, side-by-side grids), 2D drawing
  (rectangles, lines, text), watermarks, future annotation features.

### Rationale

- Both are **pure Go** — preserves the single-exe goal.
- `imaging` is dormant but has zero open critical bugs and is used in
  production by thousands of projects. Lanczos quality is
  visually indistinguishable from Pillow's.
- `gg` is the de facto standard for 2D composition in Go, modeled on
  Cairo. Has text rendering with FreeType fonts.
- `bild` is newer and active but its API is less ergonomic and its
  resize quality is no better than `imaging`'s.
- libvips-based options (`bimg`, `lilliput`) are 5-10x faster but
  require libvips installed on the target — kills single-exe.

### When to revisit

If a future feature processes hundreds of images per second on the
same box, swap `imaging` for `bimg` and ship libvips DLLs alongside
the exe. The `imageproc/` package interface stays the same; only the
implementation file changes.

## Package interface sketch

```go
package imageproc

// Format is the detected source format.
type Format int

const (
    FormatPNG Format = iota
    FormatJPEG
    FormatWebP
    FormatUnknown
)

// Decode returns an image.Image and the detected format.
func Decode(b []byte) (image.Image, Format, error)

// EncodeJPEG flattens alpha onto a white background and JPEG-encodes.
// quality is 1-100, typical 88.
func EncodeJPEG(img image.Image, quality int) ([]byte, error)

// EncodePNG re-encodes losslessly.
func EncodePNG(img image.Image) ([]byte, error)

// Resize fits img inside (maxW, maxH) preserving aspect ratio,
// using Lanczos. Returns the original if it already fits.
func Resize(img image.Image, maxW, maxH int) image.Image

// ToDataURL encodes img as a data: URL.
// mediaType is "image/jpeg" or "image/png".
func ToDataURL(img image.Image, mediaType string, quality int) (string, error)

// FromDataURL decodes a data: URL back to image.Image + format.
func FromDataURL(s string) (image.Image, Format, error)

// CompositeGrid arranges images into a grid (cols x ceil(n/cols)).
// Used by future "batch view" features.
func CompositeGrid(images []image.Image, cols, cellW, cellH, gap int) image.Image
```

This matches what the Python helpers do today, but typed and reusable.

## Migration notes per Pillow call

| Pillow | imaging / gg |
|---|---|
| `Image.open(path)` | `imaging.Open(path)` |
| `img.convert("RGB")` | wrap in `imageproc.flattenAlpha(img, white)` |
| `img.thumbnail((w, h), Image.LANCZOS)` | `imaging.Fit(img, w, h, imaging.Lanczos)` |
| `img.save(p, "JPEG", quality=88)` | `imaging.Save(img, p, imaging.JPEGQuality(88))` |
| `Image.new("RGB", size, white)` | `imaging.New(w, h, color.White)` |
| `bg.paste(img, mask=alpha)` | `imaging.OverlayCenter(bg, img, 1.0)` |
| `ImageDraw.text(...)` | `gg.NewContextForImage(img).DrawStringAnchored(...)` |
