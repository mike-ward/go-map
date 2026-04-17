package mapview

import (
	"math"

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
	capMaps     = 16
)

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
	Zoom             uint32
	FocusedOverlayID string
	InfoOpen         bool
	InfoFocusIndex   int8
}

// panState tracks an in-progress drag pan. Stored in a separate
// namespace so MapState stays a clean snapshot. Moved flips true once
// the cursor has travelled past dragThresholdPx from the press point;
// panDragEnd uses it to distinguish a pan from a click.
type panState struct {
	Active    bool
	Moved     bool
	StartX    float32 // mouse down position (absolute window coords)
	StartY    float32
	LocalX    float32 // mouse down position (widget-local coords)
	LocalY    float32
	StartCtr  projection.LatLng // center at drag start
	StartZoom uint32
	CanvasW   float32
	CanvasH   float32
}

// lastFired records the MapState last passed to OnMove / OnZoomChange
// so the next frame can detect deltas. Set=false means "no baseline
// yet" and suppresses the synthetic first-frame change event.
type lastFired struct {
	State MapState
	Set   bool
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
	sm := gui.StateMap[string, MapState](w, nsState, capMaps)
	s, ok := sm.Get(id)
	if !ok {
		return
	}
	s.Center = c.Clamp()
	sm.Set(id, s)
}

// SetZoom updates the zoom level, clamped to [0, maxZoom]. Center
// unchanged. No-op if the map ID has not yet rendered.
func SetZoom(w *gui.Window, id string, zoom uint32) {
	sm := gui.StateMap[string, MapState](w, nsState, capMaps)
	s, ok := sm.Get(id)
	if !ok {
		return
	}
	if zoom > maxZoom {
		zoom = maxZoom
	}
	s.Zoom = zoom
	sm.Set(id, s)
}

// SetView replaces both center and zoom atomically.
func SetView(w *gui.Window, id string, c projection.LatLng, zoom uint32) {
	sm := gui.StateMap[string, MapState](w, nsState, capMaps)
	s, ok := sm.Get(id)
	if !ok {
		return
	}
	if zoom > maxZoom {
		zoom = maxZoom
	}
	s.Center = c.Clamp()
	s.Zoom = zoom
	sm.Set(id, s)
}

// capOverlaysPerMap is the FIFO eviction ceiling applied by the inner
// BoundedMap. Matches the plan's 10k-marker benchmark target; the
// 10001st AddOverlay evicts the first registered.
const capOverlaysPerMap = 10_000

// readOverlays returns the live overlay map for id, creating an empty
// BoundedMap if the registry has no entry yet. Mutators receive the
// same pointer the widget reads, so writes take effect on the next
// frame. BoundedMap.Range iterates in insertion order, which drives
// the deterministic z-order of drawOverlays and panDragEnd hit-test.
func readOverlays(w *gui.Window, id string) *gui.BoundedMap[string, Overlay] {
	sm := gui.StateMap[string, *gui.BoundedMap[string, Overlay]](
		w, nsOverlays, capMaps)
	if bm, ok := sm.Get(id); ok && bm != nil {
		return bm
	}
	bm := gui.NewBoundedMap[string, Overlay](capOverlaysPerMap)
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
}

// RemoveOverlay deletes the overlay with the given overlay-ID. No-op
// if absent.
func RemoveOverlay(w *gui.Window, id, overlayID string) {
	readOverlays(w, id).Delete(overlayID)
}

// ClearOverlays removes every overlay registered against id. Intended
// for layer switchers that repopulate markers on change.
func ClearOverlays(w *gui.Window, id string) {
	readOverlays(w, id).Clear()
}

// FitBounds centers the map on b and picks the largest integer zoom at
// which b fits inside (canvasW × canvasH) after inset by padding pixels
// on all sides. No-op if the map has not rendered, b is zero, b is
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
	if math.IsNaN(float64(padding)) || math.IsInf(float64(padding), 0) || padding < 0 {
		padding = 0
	}
	if b.NE.Lat < b.SW.Lat || b.NE.Lng < b.SW.Lng {
		return
	}
	availW := canvasW - 2*padding
	availH := canvasH - 2*padding
	if availW <= 0 || availH <= 0 {
		return
	}
	best := uint32(0)
	for z := uint32(0); z <= maxZoom; z++ {
		ne := projection.Project(b.NE, z)
		sw := projection.Project(b.SW, z)
		wpx := ne.X - sw.X
		hpx := sw.Y - ne.Y
		if wpx < 0 || hpx < 0 {
			// Projection surprised us (clamped-away polar inputs can
			// collapse the box). Stop searching rather than accept a
			// spurious "fits" result.
			break
		}
		if wpx <= float64(availW) && hpx <= float64(availH) {
			best = z
		} else {
			break
		}
	}
	s.Center = b.Center().Clamp()
	s.Zoom = best
	sm.Set(id, s)
}

// maxZoom is the global zoom ceiling. Tile sources may cap lower via
// their own MaxZoom(); input handlers consult that at call time.
const maxZoom uint32 = 22
