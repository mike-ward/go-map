# go-map Architecture

Interactive slippy-tile map widget for [go-gui](https://github.com/mike-ward/go-gui).
Tiles load asynchronously; `gui.Window.RequestRedraw()` wakes the frame loop on arrival.

---

## Package Overview

```
go-map/
├── projection/          Web Mercator math (no deps, fully testable headless)
├── tile/                Tile coordinates, sources, LRU cache, HTTP adapters
│   └── wms/             WMS 1.3.0 source adapter
├── mapview/             Interactive map widget: state, rendering, input, overlays
├── cmd/a11ylint/        Static analyzer: flags overlay literals missing Label
└── examples/
    ├── basic/           Minimal OSM map
    ├── stacked-map/     WMS reference layer + legend
    ├── gallery-map/     Base-layer picker via Gallery widget
    ├── full-map/        FullWindow helper demo
    └── reference-map/   Stacked reference layers demo
```

### Dependency graph

```
mapview ──► projection
mapview ──► tile
tile/wms ──► tile
examples ──► mapview, tile, tile/wms
```

`projection` and `tile` are independently importable. No package imports `mapview`.

---

## projection

Pure math — no I/O, no GUI deps.

```go
type LatLng struct{ Lat, Lng float64 }   // WGS-84; Lat ∈ [-85.05°, 85.05°]
type Point  struct{ X, Y float64 }       // World pixels (origin top-left)
type Bounds struct{ NE, SW LatLng }

const TileSize = 256  // pixels per tile edge at any zoom

func Project(ll LatLng, z uint32) Point      // integer zoom
func ProjectF(ll LatLng, z float64) Point    // fractional zoom
func Unproject(p Point, z uint32) LatLng
func UnprojectF(p Point, z float64) LatLng
```

World width at zoom _z_ = `TileSize × 2^z`. Non-finite inputs (NaN/±Inf)
collapse to zero before any arithmetic. Longitude wraps at ±180°.

---

## tile

### Coord

```go
type Coord struct{ Z, X, Y uint32 }   // standard slippy-map indices

func (c Coord) Valid() bool            // z ≤ 31, x/y < 2^z
```

### Source interface

```go
type Source interface {
    Fetch(ctx context.Context, c Coord) ([]byte, error)
    URL(c Coord) string
    Attribution() string
    MaxZoom() uint32
}
```

`Fetch` is called on a goroutine; must be safe for concurrent use.
`URL` is called on the render goroutine (read-only, no I/O).

### HTTPFetcher (optional)

```go
type HTTPFetcher interface {
    HTTPFetcher() func(ctx context.Context, url string) (*http.Response, error)
}
```

Sources that implement `HTTPFetcher` carry their own HTTP policy (User-Agent,
auth headers). `mapview` type-asserts each source and passes the result to
`gui.DrawContext.ImageWithFetcher`, so a stacked OSM + WMS window never needs
a shared composite fetcher.

### Built-in sources

| Package    | Constructor                              | Notes                                           |
| ---------- | ---------------------------------------- | ----------------------------------------------- |
| `tile`     | `OSM()`                                  | Public OpenStreetMap; default UA `go-map/<ver>` |
| `tile`     | `OSMWithUserAgent(name, contact string)` | Custom UA string                                |
| `tile/wms` | `New(cfg WmsCfg)`                        | WMS 1.3.0, EPSG:3857, PNG/JPEG                  |

**OSM**: 15 s HTTP timeout, semaphore-gated concurrency (default 12 parallel
fetches). `IsPNG` / `IsJPEG` magic-byte guards reject malformed responses.

**WMS** (`WmsCfg`):

```go
type WmsCfg struct {
    BaseURL     string
    Layers      string
    Format      string   // "image/png" | "image/jpeg"
    Transparent bool
    UserAgent   string
    Attribution string
    MaxZoom     uint32
}
```

WMS fetches `GetMap` with `WIDTH=256&HEIGHT=256&CRS=EPSG:3857` and a bounding
box derived from `tile.Coord` via the Web Mercator tile extent formula. Timeout
is 20 s.

### Cache

```go
func NewCache(capacity int) *Cache
func (c *Cache) Get(key Coord) ([]byte, bool)
func (c *Cache) Put(key Coord, data []byte)
func (c *Cache) Len() int
```

Fixed-capacity LRU backed by a doubly-linked list and a `map[Coord]*node`.
Protected by a single `sync.Mutex`.

`Get` acquires a read-locked lookup but does **not** promote the entry to
most-recently-used. During active panning every access is a `Put` (new tile),
so promotion would add lock contention with no cache-hit benefit. When the
cache is full, `Put` evicts the least-recently-used entry.

---

## mapview

### Cfg (widget factory)

```go
type Cfg struct {
    ID             string                // Required — state registry namespace key
    InitialCenter  projection.LatLng     // Clamped to valid Web Mercator range
    InitialZoom    float64               // Clamped to [0, 19]
    Source         tile.Source           // Base layer (nil → checkerboard placeholder)
    InitialLayers  []Layer               // Stacked reference layers
    InitialOverlays []Overlay
    OnMove         func(*gui.Window, projection.LatLng)
    OnZoomChange   func(*gui.Window, float64)
    OnHover        func(*gui.Window, projection.LatLng)
    ScrollZoomGain float32               // Wheel zoom multiplier; default 1.0
    // … sizing, focus, background, A11Y fields
}
```

`*Cfg` zero-initialises safely; `ID` is the only required field.

### MapState

The single serialisable piece of map state, stored in the go-gui state registry
under `(Cfg.ID, nsState)`:

```go
type MapState struct {
    Center          projection.LatLng
    Zoom            float64
    FocusedOverlayID string    // "" = viewport navigation mode
    InfoOpen        bool
    InfoFocusIndex  int8       // sub-focus: action button index or close btn
}
```

### State registry namespaces

| Namespace    | Type                          | Bumps render version?      |
| ------------ | ----------------------------- | -------------------------- |
| `nsState`    | `MapState`                    | yes                        |
| `nsPan`      | `panState`                    | no                         |
| `nsScroll`   | `scrollState`                 | no                         |
| `nsHover`    | `hoverState`                  | yes                        |
| `nsLayers`   | `BoundedMap[string, Layer]`   | yes                        |
| `nsOverlays` | `BoundedMap[string, Overlay]` | no (manual bump)           |
| `nsVersion`  | `uint64`                      | — (is the version)         |
| `nsCanvas`   | `canvasSize`                  | no (written inside OnDraw) |

`nsWrite` is the single write path: it calls `bumpVersion` automatically for
render-invalidating namespaces. Direct registry writes bypass this; do not use
them for state that affects the drawn frame.

### Render version counter

```go
func bumpVersion(w *gui.Window, id string)
```

Increments the `uint64` stored at `(id, nsVersion)` and sets it as the
`DrawCanvasCfg.Version`. go-gui re-executes `OnDraw` only when `Version`
changes. No-op frames (cursor movement over empty space, spurious redraws)
replay the cached tessellation without calling `OnDraw`.

---

## Rendering pipeline

```
GenerateLayout(w, cfg)
│
├─ read MapState from registry (seed on first frame)
├─ fire delta callbacks: OnMove, OnZoomChange, OnHover
│     (callbacks may call PanTo/SetZoom → nsWrite → bumpVersion)
├─ re-read MapState (may have changed above)
├─ snapshot overlays + layers by value into OnDraw closure
└─ return DrawCanvas{Version: nsVersion, OnDraw: closure}

OnDraw(dc *DrawContext)
│
├─ stashCanvasSize(dc.Width, dc.Height)   [nsCanvas, no version bump]
├─ computeViewport(W, H, MapState)
│     → viewport{Z, TileZ, TileScale, CtrPx, OriginX,
│                MinTX, MaxTX, MinTY, MaxTY}
├─ drawTiles(dc, vp, layers)
│     └─ for each layer → drawLayerTiles(dc, vp, layer)
│           for y ∈ [MinTY, MaxTY]:
│             for x ∈ [MinTX, MaxTX]:
│               coord = wrapTileX({TileZ, x, y})
│               url   = source.URL(coord)
│               if HTTPFetcher → dc.ImageWithFetcher(url, fetcher, …)
│               else           → dc.Image(url, …)
├─ drawOverlays(dc, vp, overlays)
│     for each Overlay → o.Draw(dc, projector)
│     for focused Overlay → draw focus ring
└─ drawScaleBar(dc, MapState)
      attribution text (bottom-right)
      scale bar + distance label
      coordinate readout (bottom-left)
```

Tile images are rendered via `dc.Image` / `dc.ImageWithFetcher`. go-gui
deduplicates in-flight fetches by URL and caches decoded images internally;
`tile.Cache` stores raw bytes for offline/re-decode reuse.

---

## Overlay system

### Overlay interface

```go
type Overlay interface {
    ID() string
    Bounds() projection.Bounds
    Draw(dc *gui.DrawContext, pr Projector)
    HitTest(pr Projector, sx, sy float32) bool
}
```

### Projector interface (passed to Draw and HitTest)

```go
type Projector interface {
    LatLngToScreen(p projection.LatLng) (x, y float32)
    Zoom() float64
}
```

### Built-in overlays

**Marker** — point of interest

```go
type Marker struct {
    MarkerID string
    Pos      projection.LatLng
    Label    string                  // Required for a11y
    Title    string
    Body     string
    Color    color.NRGBA
    OnClick  func(*gui.Window)
    Actions  []InfoWindowAction      // max 4 (MaxInfoActions)
}
```

Drawn as a filled circle (6 px radius) with 2 px white outline.
HitTest radius: 8 px. Clicking an `InfoOpen` marker focuses the info-window;
clicking elsewhere collapses it.

**Polyline** — connected path

```go
type Polyline struct {
    PolylineID  string
    Points      []projection.LatLng
    Stroke      color.NRGBA
    StrokeWidth float32
    OnClick     func(*gui.Window)
}
```

HitTest uses quadratic distance-to-segment, 8 px tolerance.

**InfoWindowAction** — button in marker popup

```go
type InfoWindowAction struct {
    Label   string
    OnClick func(*gui.Window)   // popup closes before firing
}
```

### Attribution

`drawScaleBar` always renders the active base layer's `Attribution()` string
in the bottom-right corner. There is intentionally no option to suppress it —
OSM tile usage policy requires attribution.

---

## Input handling

### Pan (mouse/touch drag)

1. `onMouseDown` — records start position, requests `MouseLock`.
2. `panDragMove` — samples velocity with exponential moving average (EMA);
   recomputes `MapState.Center` on each move event.
3. `panDragEnd` — if pointer speed exceeds `kineticStartSpeed` (100 px/s),
   launches a kinetic fling animation.

Drag threshold: 4 px (`dragThresholdPx`). Moves below this are treated as
clicks (overlay hit-test + `OnClick` dispatch).

### Kinetic fling

Custom `gui.Animation` with exponential velocity decay:

```
v(t) = v₀ · exp(−t / τ)     τ = 300 ms
```

Each animation frame:

1. Compute `dt` since previous frame.
2. Apply `decay = exp(−dt/τ)` to `(vx, vy)`.
3. Convert pixel delta → LatLng delta via `UnprojectF`.
4. Update `MapState.Center`, bump version.
5. Stop when speed < `kineticStopSpeed` (5 px/s) or elapsed > 3 s.

### Scroll zoom

Wheel delta accumulates in `nsScroll`. When the absolute accumulated value
reaches ≥ 0.15 zoom units, a smooth zoom animation is launched toward the
cursor's geographic position.

### Keyboard

| Key             | Viewport mode | Overlay-focused mode             |
| --------------- | ------------- | -------------------------------- |
| ←↑→↓            | Pan 40 px     | —                                |
| Home            | Reset to seed | —                                |
| Tab / Shift+Tab | —             | Cycle overlay focus              |
| Enter           | —             | Open popup / activate action     |
| Escape          | —             | Close popup / return to viewport |

---

## Public API

```go
// Widget factory
func Map(cfg Cfg) gui.View
func FullWindow(w *gui.Window, v gui.View) gui.View   // single-widget demo helper

// Programmatic control (safe to call from callbacks or other goroutines)
func PanTo(w *gui.Window, id string, ll projection.LatLng)
func SetZoom(w *gui.Window, id string, z float64)
func SetView(w *gui.Window, id string, ll projection.LatLng, z float64)
func AddOverlay(w *gui.Window, id string, o Overlay)
func RemoveOverlay(w *gui.Window, id string, overlayID string)

// Read-only inspection
func Snapshot(w *gui.Window, id string) (MapState, bool)
func CanvasSize(w *gui.Window, id string) (width, height float32, ok bool)
```

All write functions call `nsWrite` internally, which bumps the render version
and schedules a redraw via `w.RequestRedraw()`.

---

## a11ylint

`cmd/a11ylint` is a Go analysis pass (compatible with `go vet -vettool` and
`golangci-lint`). It inspects composite literals for `mapview.Marker` and
`mapview.InfoWindowAction` and reports any that omit a non-empty `Label`
field. Accessibility is a constraint enforced at compile time.

```
go run ./cmd/a11ylint ./...
```

---

## Key constraints and gotchas

**Root layout sizing** — a `gui.Layout` with `Sizing: FillFill` at the
view-generator root stays 0×0 (no parent to fill from). Use
`mapview.FullWindow(w, v)` for single-widget demos; hand-write a sized
`gui.Row` / `gui.Column` for multi-pane windows.

**DrawCanvas version** — `OnDraw` only re-executes when `Version` changes.
Any code path that mutates rendered state must call `bumpVersion` (or go
through `nsWrite`). Forgetting this causes stale frames.

**ImageFetcher UA** — go-gui's default fetcher sends `go-gui/<version>`.
OSM tile usage policy requires an identifying User-Agent. `drawLayerTiles`
automatically threads each source's `HTTPFetcher` through
`dc.ImageWithFetcher`. Consumers that supply only one source may instead wire
`gui.WindowCfg.ImageFetcher` directly.

**WMS vs OSM stacking** — because each source carries its own fetcher,
stacked sources never share a fetcher. Composite-fetcher workarounds are
not required.

**Variable shadowing** — per project convention, existing variables are
updated with `=`, not `:=`. Shadowed variables are a lint error.
