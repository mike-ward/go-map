# CLAUDE.md

Guidance for Claude Code when working in this repository.

## Commands

```
go test ./...                   # run all tests
go vet ./...                    # static analysis
golangci-lint run ./...         # full lint
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

Remote tile fetches go through `gui.WindowCfg.ImageFetcher`. Callers
supplying a `tile.Source` that implements `tile.HTTPFetcher` should
wire `cfg.ImageFetcher = src.HTTPFetcher()` so OSM-policy-compliant
User-Agent headers reach the fetch path. Without this wiring,
downloads use `go-gui/<version>` UA.

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

Always run `gofmt -l .` and `golangci-lint run ./...` before committing.

## Gotchas

- **Root Layout sizing.** A Layout with `Sizing: FillFill` at the
  view-generator root stays 0×0 — no parent to fill. `renderDrawCanvas`
  bails in `rectsOverlap` and `OnDraw` never fires. Wrap
  `mapview.Map` in `gui.Column(ContainerCfg{Sizing: FixedFixed,
  Width/Height: float32(w.WindowSize())})` or equivalent. See
  `examples/basic/main.go`. Matches go-gui's own `examples/draw_canvas`
  and `examples/particles` pattern.
- **DrawCanvas cache.** `DrawCanvasCfg.Version` gates OnDraw re-exec.
  `mapview` bumps Version per frame via `w.FrameCount()` so pan/zoom
  state is never cached stale. Cost: one OnDraw per frame regardless
  of state change. Revisit in Phase 2 with a state-version counter.
- **ImageFetcher UA.** go-gui's default fetcher sends `go-gui/<version>`.
  OSM policy requires app-identifying UA. Consumer must wire
  `cfg.ImageFetcher` from a `tile.HTTPFetcher`-implementing source.

## Unresolved

- Vector tiles (MVT/protobuf + glyph labels) deferred to v0.2+.
- MBTiles (offline SQLite) deferred to v0.2+.
- Self-wrapping Widget vs consumer-wrapping — current API requires
  consumer to wrap for root sizing. Consider `mapview.FullWindow(...)`
  helper that bundles Column+FixedFixed.
