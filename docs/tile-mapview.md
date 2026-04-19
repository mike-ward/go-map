# Deep Dive: tile package + mapview state system

**Date:** 2026-04-19  
**Files covered:** `tile/tile.go`, `tile/cache.go`, `tile/osm.go`, `tile/wms/wms.go`, `mapview/state.go`

---

## Overview

This covers the plumbing behind a slippy-tile map widget: how tiles are addressed,
cached, fetched (OSM and WMS), and how the widget's per-frame render state is
tracked and invalidated.

---

## 1. Tile Coordinate System (`tile/tile.go`)

### What

A "slippy tile" is a 256×256 pixel PNG identified by `{Z}/{X}/{Y}`. Z is zoom level;
X is the column (west → east); Y is the row (north → south). At zoom 0 there is one
tile; at zoom 1 there are 4 (2×2); at zoom N there are `2^N × 2^N` tiles.

```go
type Coord struct {
    Z uint32
    X uint32
    Y uint32
}
```

### The `Valid()` bit-shift trick

```go
func (c Coord) Valid() bool {
    n := uint32(1) << c.Z
    return c.X < n && c.Y < n
}
```

`uint32(1) << c.Z` gives the grid size at zoom Z in one operation. The interesting edge:
when `c.Z >= 32`, Go's shift semantics produce **0** (defined behaviour — not UB as in
C). So `Valid()` returns false for Z ≥ 32, which is exactly right: no real tile server
supports zoom 32.

**Why uint32 not int?** Tile indices are never negative; using unsigned avoids a class
of off-by-one bugs where a negative X/Y would wrap to a giant positive number and pass
a signed check.

### `String()` without fmt

```go
func (c Coord) String() string {
    var buf [32]byte
    b := strconv.AppendUint(buf[:0], uint64(c.Z), 10)
    b = append(b, '/')
    b = strconv.AppendUint(b, uint64(c.X), 10)
    ...
    return string(b)
}
```

`strconv.AppendUint` into a stack-allocated `[32]byte` avoids heap allocation.
`fmt.Sprintf` allocates once per call regardless of string length. For something
called on every visible tile every frame, this matters.

**Pattern:** use `strconv.Append*` + stack buffer for hot-path string building.
`string(b)` at the end copies the stack bytes onto the heap exactly once.

### Resources

- [Slippy map tile names — OpenStreetMap Wiki](https://wiki.openstreetmap.org/wiki/Slippy_map_tilenames)
- [Go spec: shift expressions](https://go.dev/ref/spec#Operators)
- [strconv package](https://pkg.go.dev/strconv)

---

## 2. LRU Cache with Read Lock (`tile/cache.go`)

### What

A fixed-capacity Least-Recently-Used cache: when it's full, the oldest entry is
evicted. Standard data structure for tile caches — a viewport visits ~25 tiles;
panning reuses most of them.

### Implementation: doubly-linked list + hash map

```
items: map[Coord]*list.Element   ← O(1) lookup
order: *list.List                ← O(1) LRU tracking
```

`list.List` is Go's standard doubly-linked list (`container/list`). Each `list.Element`
is a node; `PushFront` inserts at the "newest" end, `Back()` is the oldest.

On `Put`:

1. Hit → update value, `MoveToFront` (refresh recency).
2. Miss + at capacity → remove `Back()` from list and map, then `PushFront`.
3. Miss + under capacity → just `PushFront`.

### The read-lock optimization

Standard LRU promotes entries on `Get` (moves the accessed entry to the front).
This cache **does not**:

```go
// Get uses RLock, not Lock
func (c *Cache) Get(key Coord) (data []byte, ok bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    ...
}
```

**Why:** Under map-panning workloads, tiles are written once (on fetch completion)
and read many times per frame. Promoting on read would require a write lock on
every `Get`, turning `RLock` into `Lock` and serialising concurrent tile fetches.

The comment in the source explains the reasoning: "reads are dominated by cache
hits from a recently panned viewport (all Puts), so read-promotion has negligible
impact on hit rate." This is an example of tailoring a generic algorithm to a
specific access pattern.

**Trade-off:** Entries are evicted in Put order, not access order. In practice,
panning a map moves the visible region in bulk, so LRU-by-Put still works well.

### `sync.RWMutex` primer

- `RLock` / `RUnlock` — multiple goroutines can hold read locks simultaneously.
- `Lock` / `Unlock` — exclusive; blocks until all readers release.
- Use `RWMutex` when reads are frequent and writes are rare.

### Resources

- [container/list](https://pkg.go.dev/container/list)
- [sync.RWMutex](https://pkg.go.dev/sync#RWMutex)
- [LRU Cache — Wikipedia](https://en.wikipedia.org/wiki/Cache_replacement_policies#LRU)

---

## 3. OSM HTTP Source (`tile/osm.go`)

### What

Fetches tile PNG bytes from `tile.openstreetmap.org`. Implements `tile.Source` and
`tile.HTTPFetcher`.

### Semaphore concurrency control

```go
sem *semaphore.Weighted   // from golang.org/x/sync/semaphore

func (s *osmSource) Fetch(ctx context.Context, c Coord) ([]byte, error) {
    if err := s.sem.Acquire(ctx, 1); err != nil {
        return nil, err
    }
    defer s.sem.Release(1)
    ...
}
```

`semaphore.Weighted` is a counting semaphore: `Acquire(ctx, n)` blocks until n
"slots" are free. Here capacity is `DefaultConcurrency = 12`, so at most 12 tile
fetches run simultaneously. This prevents saturating the tile server or the local
network connection.

**Why not `chan struct{}`?** A buffered channel of empty structs is the idiomatic
Go semaphore for simple cases. `semaphore.Weighted` adds context cancellation
support: if the user pans away before a tile loads, the `ctx` is cancelled and
`Acquire` returns immediately rather than waiting for a slot that would never be
used.

### Header injection prevention

```go
func SanitizeHeader(s string) string {
    r := strings.NewReplacer("\r", "", "\n", "")
    out := strings.TrimSpace(r.Replace(s))
    if len(out) > MaxUserAgentLen {
        out = out[:MaxUserAgentLen]
    }
    return out
}
```

HTTP headers are line-delimited (`\r\n`). A User-Agent containing a newline would
split the header, allowing an attacker to inject arbitrary headers. This is
[CWE-113 Header Injection](https://cwe.mitre.org/data/definitions/113.html).
`SanitizeHeader` strips `\r` and `\n` before the string ever reaches `req.Header.Set`.

The `MaxUserAgentLen = 512` cap prevents a malformed or adversarial UA from growing
unboundedly in memory or hitting HTTP/2 header size limits.

### PNG magic-byte validation

```go
var pngMagic = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

func IsPNG(b []byte) bool {
    return len(b) >= len(pngMagic) && bytes.Equal(b[:len(pngMagic)], pngMagic)
}
```

OSM tile servers return a 200 OK with an HTML error page under load. Without the
check, `go-gui` would decode "unknown format", cache the garbage on disk, and log
errors on every subsequent frame — permanent until the cache is cleared.

The PNG signature is defined in RFC 2083 §3.1. It's designed to be
self-identifying: the `\x89` byte catches 7-bit ASCII transfers; `\r\n` detects
CRLF line-ending conversions; `\x1a` is DOS end-of-file.

### `io.LimitReader` body cap

```go
body, err := io.ReadAll(io.LimitReader(resp.Body, maxTileBytes))
```

`io.ReadAll` without a limit reads until EOF — a hostile server could send infinite
bytes and exhaust RAM. `io.LimitReader` wraps the reader and returns EOF after
`maxTileBytes` (4 MiB). A real tile is a few KB; 4 MiB is generous enough for
high-DPI tiles and tight enough to bound memory.

### Resources

- [OSM Tile Usage Policy](https://operations.osmfoundation.org/policies/tiles/)
- [golang.org/x/sync/semaphore](https://pkg.go.dev/golang.org/x/sync/semaphore)
- [PNG spec — RFC 2083](https://www.rfc-editor.org/rfc/rfc2083)
- [HTTP header injection — OWASP](https://owasp.org/www-community/attacks/HTTP_Response_Splitting)

---

## 4. WMS Source (`tile/wms/wms.go`)

### What

WMS (Web Map Service) is an OGC standard protocol for requesting georeferenced map
images from a server that does rendering. Unlike OSM (static PNG tiles), WMS renders
on demand from vector/raster geodata.

### Pre-baked URL template

```go
// In New(): build urlPrefix once
buf.WriteString(cfg.Endpoint)
buf.WriteString("?service=WMS&request=GetMap&version=1.3.0")
buf.WriteString("&layers=...")
buf.WriteString("&bbox=")  // ← trailing prefix, BBOX appended per-tile

// In URL(c):
return s.urlPrefix + bboxFor(c)   // one string concat per tile
```

Every parameter except BBOX is the same for all tiles from the same source. Baking
them into `urlPrefix` at construction time means `URL(Coord)` is a single
concatenation — no `fmt.Sprintf`, no allocations beyond the final string.

**Pattern:** Pre-compute invariant parts of hot-path strings at initialisation time.

### EPSG:3857 BBOX calculation

```go
func bboxFor(c tile.Coord) string {
    size := 2 * mercatorR / float64(uint64(1)<<c.Z)
    minX := -mercatorR + float64(c.X)*size
    ...
}
```

WMS 1.3.0 with EPSG:3857 expects the bounding box in **meters**, not degrees.
`mercatorR = 20037508.34...` is half the equatorial circumference in meters
(the Earth projected onto a flat square).

At zoom Z, the world is divided into `2^Z` columns. Each column is `2 * mercatorR / 2^Z`
meters wide. Column X starts at `-mercatorR + X * size`.

`strconv.AppendFloat` with precision `-1` produces the shortest decimal that round-trips
through IEEE 754 — the server's parser reconstructs the exact tile corner without
floating-point drift introducing sub-pixel gaps.

### `querySep` — defensive URL joining

```go
func querySep(endpoint string) string {
    if !strings.Contains(endpoint, "?") { return "?" }
    if strings.HasSuffix(endpoint, "?") || strings.HasSuffix(endpoint, "&") { return "" }
    return "&"
}
```

Some WMS deployments ship with a partial query string already in the endpoint URL
(e.g. `?map=foo.map` for MapServer). This function handles all three cases:
no query, query already has a trailing separator, or query needs `&`.

### Resources

- [OGC WMS 1.3.0 spec](https://www.ogc.org/standard/wms/)
- [EPSG:3857 — epsg.io](https://epsg.io/3857)
- [Web Mercator projection — Wikipedia](https://en.wikipedia.org/wiki/Web_Mercator_projection)

---

## 5. MapView State Registry (`mapview/state.go`)

### What

`mapview` widgets live inside a `gui.Window`. Multiple maps can exist in one window.
The state registry (`gui.StateMap`) is the window's per-widget persistent storage,
keyed by `(namespace, id)`. This file defines all namespaces, their value types,
and the rules for when a write triggers a redraw.

### Namespace pattern

```go
const (
    nsState   = "mapview.state"   // MapState (pan/zoom/focus/popup)
    nsPan     = "mapview.pan"     // panState (drag tracking)
    nsHover   = "mapview.hover"   // hoverState
    nsVersion = "mapview.version" // uint64 draw-cache key
    ...
)
```

Each namespace is a string prefix + purpose. All reads/writes go through `nsRead`
/ `nsWrite` — a package-level fence that prevents call sites from bypassing the
version bump:

```go
func nsWrite[V any](w *gui.Window, ns, id string, v V) {
    gui.StateMap[string, V](w, ns, capMaps).Set(id, v)
    if invalidatesRender(ns) {
        bumpVersion(w, id)
    }
}
```

**Why:** `DrawCanvas` caches the output of `OnDraw` using a version counter. If any
code mutates `MapState` without bumping the version, the draw cache returns stale
output and the map doesn't update. The fence makes that impossible.

### Version counter as cache key

```go
func bumpVersion(w *gui.Window, id string) {
    sm := gui.StateMap[string, uint64](w, nsVersion, capMaps)
    v, _ := sm.Get(id)
    sm.Set(id, v+1)
}
```

`DrawCanvas` re-executes `OnDraw` only when its version parameter changes. The
counter approach means:

- No-op frames (no state change) skip `OnDraw` and replay the cached tessellation.
- Any state change (pan, zoom, new tile loaded) bumps the counter → `OnDraw` runs.
- Overflow at 2^64 is safe: the cache stores the last-seen value, so any change
  misses the cache regardless of the value's magnitude.

**Why not a boolean `dirty` flag?** A counter survives the case where two mutations
happen between frames — the second bump is visible as a changed value. A boolean
would also work, but would need explicit reset after each draw.

### The infinite-loop trap

The code comment calls out two namespaces that must **never** invalidate render:

```
nsInfoRect  // written inside OnDraw — must not loop
nsCanvas    // written inside OnDraw — must not loop
```

If `nsCanvas` were in the `invalidatesRender` whitelist:

1. `OnDraw` writes canvas size.
2. Write bumps version.
3. `DrawCanvas` sees new version → re-runs `OnDraw`.
4. Go to 1. (infinite loop)

This is a subtle API contract: namespaces written from inside a draw callback can
never also trigger a redraw. The whitelist in `invalidatesRender` encodes this
constraint statically.

### `clampZoom` — defensive numeric hygiene

```go
func clampZoom(z float64) float64 {
    if !isFinite(z) || z < 0 { return 0 }
    if z > maxZoomF { return maxZoomF }
    return z
}
```

`MapState.Zoom` is always a finite number in `[0, 22]`. This makes `MapState`
comparable with plain struct equality (NaN != NaN) and keeps downstream
projection math safe from poisoned inputs. Every zoom write path funnels through
`clampZoom` — a single chokepoint rather than 8 scattered guards.

### `FitBounds` — analytical zoom calculation

```go
zx := math.Log2(float64(availW) / dx)
zy := math.Log2(float64(availH) / dy)
zoom = clampZoom(math.Min(zx, zy))
```

At zoom Z the world is `2^Z × tileSize` pixels. To fit bounds of pixel-width `dx`
into `availW` pixels: solve `availW = dx * 2^Z` → `Z = log2(availW / dx)`.
The minimum of the two axes (width and height) is the zoom at which both fit.
`clampZoom` handles degenerate cases (NaN from log of 0 or negative, ±Inf).

### Resources

- [math.Log2 — Go docs](https://pkg.go.dev/math#Log2)
- [IEEE 754 NaN behaviour in Go](https://go.dev/blog/laws-of-reflection) (background)
- [sync.Map vs StateMap pattern](https://pkg.go.dev/sync#Map)

---

## Concept Map

```
tile.Coord  ──────────────────────────────────────────────┐
  ↓ Valid()                                               │
tile.Source ─── tile.HTTPFetcher                          │
  ↓ Fetch()                              ↓ HTTPFetcher()  │
osmSource / wms.source                   gui.ImageFetcher │
  semaphore → concurrency cap            (per-layer UA)   │
  LimitReader → memory cap                                │
  IsPNG / IsJPEG → body validation                        │
                                                          │
tile.Cache  ─── LRU eviction ◄────────────────────────────┘
  RWMutex → concurrent Get without write-lock

mapview/state.go
  nsWrite → invalidatesRender? → bumpVersion
  bumpVersion → DrawCanvas.Version changes → OnDraw re-runs
  clampZoom → MapState.Zoom always finite
```

---

## Learning Path

1. Read: [Effective Go — Interfaces](https://go.dev/doc/effective_go#interfaces)  
   The `tile.Source` interface is a textbook example of small, composable interfaces.

2. Read: [Go blog — The Laws of Reflection](https://go.dev/blog/laws-of-reflection)  
   Background for understanding `type assertion` on `tile.HTTPFetcher`.

3. Practice: implement a `tile.Source` for a local MBTiles SQLite file.  
   You'll need `Fetch`, `URL` (return `""`), `Attribution`, `MaxZoom`.

4. Read: [web.dev — LRU caches](https://web.dev/articles/storage-for-the-web) (concept)  
   Then compare with Go's `container/list`-based implementation here.

5. Experiment: change `c.mu.RLock()` to `c.mu.Lock()` in `Cache.Get`, run the bench
   tests (`go test -bench=. ./tile/...`), and measure the contention difference.
