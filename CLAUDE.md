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
mapview.Widget(Cfg{...}) → gui.View
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
- `tile.Source interface { Fetch(ctx, TileCoord) ([]byte, error); Attribution() string; MaxZoom() uint32 }`.
- `mapview.Cfg` — zero-initializable `*Cfg` pattern.

### Rendering Strategy

go-gui's `DrawContext` has no image/texture primitive. Tiles render as
child `gui.Image` views positioned via the float system (absolute
coords in the canvas). Overlays (markers, polylines, attribution) draw
via `OnDraw(*DrawContext)`.

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

## Unresolved

- Vector tiles (MVT/protobuf + glyph labels) deferred to v0.2+.
- MBTiles (offline SQLite) deferred to v0.2+.
- Default tile source: ship OSM direct, or force consumer to supply?
- Image primitive on `DrawContext` — push upstream to go-gui, or keep
  composition-via-child-Image approach indefinitely?
