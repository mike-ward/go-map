package mapview

import (
	"math"
	"time"

	"github.com/mike-ward/go-gui/gui"
	"github.com/mike-ward/go-map/projection"
)

// State-registry namespaces. Convention: "mapview.<purpose>", one
// namespace per distinct value type, all keyed by Cfg.ID. capMaps is
// the per-namespace map cap passed to gui.StateMap.
const (
	nsState     = "mapview.state"
	nsPan       = "mapview.pan"
	nsHover     = "mapview.hover"
	nsLastFired = "mapview.lastfired"
	nsScroll    = "mapview.scroll"
	nsOverlays  = "mapview.overlays"
	nsSeeded    = "mapview.seeded"
	nsInfoRect  = "mapview.inforect"
	nsVersion   = "mapview.version"
	nsA11y      = "mapview.a11y"
	nsLayers    = "mapview.layers"
	nsCanvas    = "mapview.canvas"
	capMaps     = 16
)

// canvasSize is the most recent OnDraw canvas dimensions reported by
// a map's DrawCanvas. Written each frame from the OnDraw closure (no
// version bump — render-loop read by the Overview widget on the next
// frame) and read by any consumer that needs pixel dims for viewport
// math. One-frame lag accepted; the alternative (piping dims out of
// layout) would widen the widget API for a locator-only use case.
type canvasSize struct{ W, H float32 }

// CanvasSize returns the last-rendered canvas dimensions of the map
// with the given ID. ok is false before the first frame; callers must
// check before trusting W / H — the zero value is not a valid size.
func CanvasSize(w *gui.Window, id string) (width, height float32, ok bool) {
	c := nsRead[canvasSize](w, nsCanvas, id)
	if c.W <= 0 || c.H <= 0 {
		return 0, 0, false
	}
	return c.W, c.H, true
}

// MapState is the persistent per-map state held in the Window state
// registry. Accessed by the widget factory each frame and mutated by
// package-level helpers (PanTo, SetZoom, ...).
//
// FocusedOverlayID is the Marker currently under keyboard focus (empty
// means viewport mode). InfoOpen is true when the InfoWindow popup is
// visible; only meaningful when FocusedOverlayID != "". InfoFocusIndex
// points at the keyboard-focused sub-element of an open popup:
// 0..len(Actions)-1 select an action; len(Actions) is the close button.
// Reset to 0 on every popup open; value is ignored when InfoOpen=false.
//
// Transient drag-tracking fields live on panState below to keep the
// snapshot type small.
type MapState struct {
	Center           projection.LatLng
	Zoom             float64
	FocusedOverlayID string
	InfoOpen         bool
	InfoFocusIndex   int8
}

// panState tracks an in-progress drag pan. Stored in a separate
// namespace so MapState stays a clean snapshot. Moved flips true once
// the cursor has travelled past dragThresholdPx from the press point;
// panDragEnd uses it to distinguish a pan from a click.
//
// Velocity fields (Last*, Vel*) sample a low-pass-filtered world-pixel
// velocity during drag so panDragEnd can launch a kinetic-pan fling
// that matches the cursor's exit momentum. LastT is time.Time directly
// (no unix-ns conversion) because panState lives in the state registry
// as a value and a time.Time field does not heap-escape across nsWrite.
type panState struct {
	Active    bool
	Moved     bool
	StartX    float32 // mouse down position (absolute window coords)
	StartY    float32
	LocalX    float32 // mouse down position (widget-local coords)
	LocalY    float32
	StartCtr  projection.LatLng // center at drag start
	StartZoom float64
	CanvasW   float32
	CanvasH   float32
	// Last* is the most recent mouse sample during the drag; seeded
	// on mouse-down, refreshed on every move that crosses the drag
	// threshold. Vel* is an EMA of world-pixel velocity in the
	// direction the map center is moving (screen-right drag → center
	// travels left → negative VelX).
	LastX, LastY float32
	LastT        time.Time
	VelX, VelY   float64
}

// lastFired records the MapState + hover LatLng last passed to
// OnMove / OnZoomChange / OnHover so the next frame can detect
// deltas. Set=false means "no MapState baseline yet" and suppresses
// the synthetic first-frame change event; HoverSet=false means
// "no hover baseline yet" and likewise suppresses the first hover
// sample on cursor entry, so OnHover fires only on real movement.
type lastFired struct {
	State    MapState
	Set      bool
	HoverLL  projection.LatLng
	HoverSet bool
}

// nsRead and nsWrite are the only state-registry primitives used by
// this package. Eight specialized read/write pairs collapsed into
// these two so callers can never accidentally bypass the namespace
// constants by reaching for gui.StateMap directly.
func nsRead[V any](w *gui.Window, ns, id string) V {
	var zero V
	return gui.StateReadOr[string, V](w, ns, id, zero)
}

func nsWrite[V any](w *gui.Window, ns, id string, v V) {
	gui.StateMap[string, V](w, ns, capMaps).Set(id, v)
	if invalidatesRender(ns) {
		bumpVersion(w, id)
	}
}

// invalidatesRender reports whether a write to ns must bump the
// DrawCanvas version. nsInfoRect writes come from inside OnDraw —
// bumping there would loop.
func invalidatesRender(ns string) bool {
	return ns == nsState || ns == nsHover || ns == nsLayers
}

// bumpVersion increments the per-map DrawCanvas cache key. The zero
// value is a valid first-frame version — no cache exists yet so the
// initial OnDraw runs regardless. Every MapState / hover / overlay
// mutation must funnel through here so the cached tessellation stays
// coherent with captured closure state. Wraparound at 2^64 is a
// non-event — the cache stores the last seen value, so any change
// misses the cache.
func bumpVersion(w *gui.Window, id string) {
	sm := gui.StateMap[string, uint64](w, nsVersion, capMaps)
	v, _ := sm.Get(id)
	sm.Set(id, v+1)
}

// readVersion returns the current DrawCanvas cache key for id. Zero
// when id has no prior state.
func readVersion(w *gui.Window, id string) uint64 {
	return nsRead[uint64](w, nsVersion, id)
}

// readState returns the current MapState for id, seeding it from seed
// if the registry has no entry yet. The seed branch is the reason
// readState exists — every other reader uses nsRead directly.
func readState(w *gui.Window, id string, seed MapState) MapState {
	sm := gui.StateMap[string, MapState](w, nsState, capMaps)
	if s, ok := sm.Get(id); ok {
		return s
	}
	sm.Set(id, seed)
	return seed
}

// Snapshot returns the current MapState for the map with the given
// ID. ok is false if the map has not yet rendered (in which case the
// returned MapState is the zero value); callers must check ok before
// trusting Center/Zoom — the zero value is a real point on the
// equator, not a sentinel.
func Snapshot(w *gui.Window, id string) (s MapState, ok bool) {
	return gui.StateMap[string, MapState](w, nsState, capMaps).Get(id)
}

// PanTo recenters the map on the given LatLng. Zoom is unchanged.
// No-op if the map ID has not yet rendered.
func PanTo(w *gui.Window, id string, c projection.LatLng) {
	if s, ok := Snapshot(w, id); ok {
		SetView(w, id, c, s.Zoom)
	}
}

// SetZoom updates the zoom level, clamped to [0, maxZoomF]. NaN/±Inf
// collapse to 0. Center unchanged. No-op if the map ID has not yet
// rendered.
func SetZoom(w *gui.Window, id string, zoom float64) {
	if s, ok := Snapshot(w, id); ok {
		SetView(w, id, s.Center, zoom)
	}
}

// SetView replaces both center and zoom atomically. Zoom is clamped
// via clampZoom so a stray NaN / ±Inf cannot reach MapState. Any in-
// flight kinetic fling is cancelled — a programmatic SetView is an
// explicit request to land at that center, not a suggestion to
// glide towards it.
func SetView(w *gui.Window, id string, c projection.LatLng, zoom float64) {
	sm := gui.StateMap[string, MapState](w, nsState, capMaps)
	s, ok := sm.Get(id)
	if !ok {
		return
	}
	cancelKineticPan(w, id)
	s.Center = c.Clamp()
	s.Zoom = clampZoom(zoom)
	sm.Set(id, s)
	bumpVersion(w, id)
}

// clampZoom coerces a zoom value into the renderable range. NaN and
// ±Inf collapse to 0 so MapState.Zoom is always a finite number in
// [0, maxZoomF] — this lets fireDecision compare states with plain
// struct equality and keeps every downstream projection call safe
// from poisoned inputs. The single helper replaces eight inline repeats
// of the same guard across the zoom write paths.
func clampZoom(z float64) float64 {
	if !isFinite(z) || z < 0 {
		return 0
	}
	if z > maxZoomF {
		return maxZoomF
	}
	return z
}

// capOverlaysPerMap is the FIFO eviction ceiling applied by the inner
// BoundedMap. Matches the plan's 10k-marker benchmark target; the
// 10001st AddOverlay evicts the first registered.
const capOverlaysPerMap = 10_000

// readOverlays returns the live overlay map for id. Mutators receive
// the same pointer the widget reads, so writes take effect on the next
// frame. BoundedMap.Range iterates in insertion order, which drives
// the deterministic z-order of drawOverlays and panDragEnd hit-test.
func readOverlays(w *gui.Window, id string) *gui.BoundedMap[string, Overlay] {
	return readRegistryMap[Overlay](w, nsOverlays, id, capOverlaysPerMap)
}

// readRegistryMap returns the live BoundedMap[string, V] for id in ns,
// creating an empty one (capped at maxEntries) if absent. Shared by
// overlay and layer registries so the create-on-demand pattern exists
// once.
func readRegistryMap[V any](w *gui.Window, ns, id string, maxEntries int) *gui.BoundedMap[string, V] {
	sm := gui.StateMap[string, *gui.BoundedMap[string, V]](w, ns, capMaps)
	if bm, ok := sm.Get(id); ok && bm != nil {
		return bm
	}
	bm := gui.NewBoundedMap[string, V](maxEntries)
	sm.Set(id, bm)
	return bm
}

// AddOverlay registers o on the map identified by id. o.ID() must be
// non-empty; duplicates replace the prior entry in place. Safe to call
// before the first frame — state is created on demand.
func AddOverlay(w *gui.Window, id string, o Overlay) {
	if o == nil || o.ID() == "" {
		return
	}
	readOverlays(w, id).Set(o.ID(), o)
	bumpVersion(w, id)
}

// RemoveOverlay deletes the overlay with the given overlay-ID. No-op
// if absent; the version bump is skipped on a no-op so a stale
// RemoveOverlay in a hot callback path cannot force per-frame OnDraw
// re-runs.
func RemoveOverlay(w *gui.Window, id, overlayID string) {
	bm := readOverlays(w, id)
	if !bm.Contains(overlayID) {
		return
	}
	bm.Delete(overlayID)
	bumpVersion(w, id)
}

// ClearOverlays removes every overlay registered against id. Intended
// for layer switchers that repopulate markers on change. No-op (and
// no version bump) when already empty.
func ClearOverlays(w *gui.Window, id string) {
	bm := readOverlays(w, id)
	if bm.Len() == 0 {
		return
	}
	bm.Clear()
	bumpVersion(w, id)
}

// FitBounds centers the map on b and picks the fractional zoom at
// which b fits inside (canvasW × canvasH) after inset by padding pixels
// on all sides. Analytical: zoom = min(log2(availW/dx0), log2(availH/dy0))
// where dx0, dy0 are the bounds extent in world pixels at z=0. Clamped
// to [0, maxZoomF]. No-op if the map has not rendered, b is zero, b is
// inverted (NE below/west of SW), or any dimension is non-finite /
// non-positive. Antimeridian-straddling bounds are not supported.
func FitBounds(w *gui.Window, id string, b projection.Bounds, padding float32, canvasW, canvasH float32) {
	sm := gui.StateMap[string, MapState](w, nsState, capMaps)
	s, ok := sm.Get(id)
	if !ok || b.IsZero() {
		return
	}
	if !finitePositive(float64(canvasW)) || !finitePositive(float64(canvasH)) {
		return
	}
	if !isFiniteF32(padding) || padding < 0 {
		padding = 0
	}
	if b.NE.Lat < b.SW.Lat || b.NE.Lng < b.SW.Lng {
		return
	}
	availW := canvasW - 2*padding
	availH := canvasH - 2*padding
	// availW / availH <= 0 (padding consumes the whole canvas) is NOT
	// an early return — log2 of a non-positive ratio produces NaN or
	// -Inf, which clampZoom collapses to 0 below. Plan §5a calls for
	// Zoom=0 on that input so the map re-centers on bounds even when
	// the author asked for more padding than fits.
	// Measure bounds extent at z=0 (one tile-size world) so the log2
	// ratios yield zoom directly. A degenerate bounds (both corners
	// project to the same point — e.g. a single-point Bounds) makes
	// dx/dy zero; treat that as "fits at max zoom" rather than
	// dividing by zero.
	ne := projection.ProjectF(b.NE, 0)
	sw := projection.ProjectF(b.SW, 0)
	dx := ne.X - sw.X
	dy := sw.Y - ne.Y
	if dx < 0 || dy < 0 {
		return
	}
	// Degenerate (dx==0 or dy==0) axes go straight to max zoom so the
	// other axis still gets to tighten the fit. availW / availH <= 0
	// (padding consumes canvas) makes the ratio non-positive; log2
	// returns NaN / -Inf which propagates through math.Min and
	// collapses to 0 in clampZoom — matches plan §5a "padding consumes
	// entire canvas → Zoom=0".
	zx := maxZoomF
	if dx > 0 {
		zx = math.Log2(float64(availW) / dx)
	}
	zy := maxZoomF
	if dy > 0 {
		zy = math.Log2(float64(availH) / dy)
	}
	cancelKineticPan(w, id)
	s.Center = b.Center().Clamp()
	s.Zoom = clampZoom(math.Min(zx, zy))
	sm.Set(id, s)
	bumpVersion(w, id)
}

// maxZoom is the global zoom ceiling for tile paths (tile.Coord.Z
// stays uint32). maxZoomF is the float clamp used by MapState.Zoom and
// every fractional write path. They track the same numeric value;
// maxZoomF is derived so bumping maxZoom never leaves the float clamp
// lagging.
const maxZoom uint32 = 22
const maxZoomF = float64(maxZoom)
