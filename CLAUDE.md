# CLAUDE.md

Guidance for Claude Code when working in this repository.

## Commands

```
go test ./...                   # run all tests
go vet ./...                    # static analysis
golangci-lint run ./...         # full lint
go run ./cmd/a11ylint ./...     # overlay-Label a11y lint
go build ./...                  # build all packages
go run ./examples/basic         # run demo (requires SDL2)
```

## Architecture

Interactive slippy-tile map widget built on go-gui. Tile images load
asynchronously; `Window.RequestRedraw()` wakes the frame loop when a
tile lands.

```
mapview.Map(Cfg{...}) → gui.View
  → GenerateLayout() wraps gui.DrawCanvas with child Image views
  → OnDraw renders overlays (markers, polylines, attribution)
  → tile.Cache serves tiles; tile.Source fetches on miss
```

### Packages

- `projection/` — `LatLng`, `Bounds`, Web Mercator forward/inverse.
  Pure math, no deps, fully testable headless.
- `tile/` — `TileCoord{Z,X,Y}`, `Source` interface, `OSM()` adapter,
  LRU `Cache`. HTTP client isolated here; no tile I/O in other packages.
- `mapview/` — widget factory. Owns pan/zoom state, input handlers,
  tile-grid computation, overlay rendering.
- `examples/basic/` — minimal runnable demo.

### Core Types

- `projection.LatLng{Lat, Lng float64}` — WGS84.
- `projection.Bounds{NE, SW LatLng}`.
- `tile.TileCoord{Z, X, Y uint32}` — standard slippy indexing.
- `tile.Source interface { Fetch(ctx, Coord) ([]byte, error); URL(Coord) string; Attribution() string; MaxZoom() uint32 }`.
- `tile.HTTPFetcher interface { HTTPFetcher() func(ctx, url) (*http.Response, error) }` — optional, type-asserted by consumers for `gui.WindowCfg.ImageFetcher`.
- `mapview.Cfg` — zero-initializable `*Cfg` pattern.

### Rendering Strategy

Tiles render via `dc.Image(x, y, w, h, url, ...)` on the DrawCanvas
(go-gui v0.12.2+). Overlays draw via `OnDraw(*DrawContext)` on the
same canvas. No child-View composition; single Shape.

Remote tile fetches go through `gui.WindowCfg.ImageFetcher` by
default, but each layer's own `tile.HTTPFetcher` overrides it per
draw via `gui.DrawContext.ImageWithFetcher` (go-gui v0.12.4+). So
OSM + WMS stacked in one window carry their own policy-compliant
User-Agents without a shared composite fetcher. Consumers that wire
only one source can still rely on `cfg.ImageFetcher` alone.

### Async Tiles

`tile.Source.Fetch` runs on a goroutine. On completion, the widget
stores the decoded image and calls `w.RequestRedraw()` to re-layout.
Inflight requests are deduped by `TileCoord`.

### Attribution Requirement

`mapview` always renders source attribution (bottom-right by default).
Do not add an option to hide it — OSM tile policy forbids.

## Coding Conventions

- **No variable shadowing.** Use `=` for existing variables, not `:=`.
- **Clean lint and format.** `golangci-lint run ./...` and `gofmt` must
  pass with zero issues.
- Comments wrap at 90 columns when practical.
- Performance improvements should favor reducing heap allocations.
- All widgets follow the go-gui `*Cfg` struct convention.
- Event callbacks: `func(*gui.Layout, *gui.Event, *gui.Window)`; set
  `e.IsHandled = true` when consumed.

## Pre-Commit Checks

Always run `gofmt -l .`, `golangci-lint run ./...`, and
`go run ./cmd/a11ylint ./...` before committing. The a11ylint tool
flags mapview overlay composite literals missing a non-empty `Label`
field — accessibility is a constraint, not a feature.

## Gotchas

- **Root Layout sizing.** A Layout with `Sizing: FillFill` at the
  view-generator root stays 0×0 — no parent to fill. `renderDrawCanvas`
  bails in `rectsOverlap` and `OnDraw` never fires. Use
  `mapview.FullWindow(w, v)` for single-widget demos; write a sized
  `gui.Row` / `gui.Column` by hand for multi-pane windows. See
  `examples/basic/main.go` for the FullWindow form,
  `examples/stacked-map/main.go` for a sidebar layout.
- **DrawCanvas cache.** `DrawCanvasCfg.Version` gates OnDraw re-exec.
  `mapview` bumps Version per frame via `w.FrameCount()` so pan/zoom
  state is never cached stale. Cost: one OnDraw per frame regardless
  of state change. Revisit in Phase 2 with a state-version counter.
- **ImageFetcher UA.** go-gui's default fetcher sends `go-gui/<version>`.
  OSM policy requires app-identifying UA. `mapview.drawLayerTiles`
  auto-threads each layer Source's `tile.HTTPFetcher` via
  `dc.ImageWithFetcher` (go-gui v0.12.4+), so wiring
  `cfg.ImageFetcher` is only needed when the Source does not
  implement `HTTPFetcher`.
- **Gallery thumbnail UA.** `Gallery` forwards `ThumbnailURL` to
  `gui.Image`, which uses the window-level `ImageFetcher`. If the
  thumbnail URL lives on the same tile server as the Source, the UA
  applies; CDN-hosted previews are typically UA-agnostic. Prefer
  static CDN thumbnails over live tile endpoints.

## Unresolved

- Vector tiles (MVT/protobuf + glyph labels) deferred to v0.2+.
- MBTiles (offline SQLite) deferred to v0.2+.
- Self-wrapping Widget vs consumer-wrapping — single-widget demos
  use `mapview.FullWindow(w, v)`; multi-pane layouts still wrap by
  hand. No plan to push self-sizing into `mapview.Map` itself.
