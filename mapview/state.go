package mapview

import (
	"github.com/mike-ward/go-gui/gui"
	"github.com/mike-ward/go-map/projection"
)

// Namespace and capacity for mapview state stored in the per-Window
// gui.StateRegistry. Keyed by Cfg.ID.
const (
	nsState = "mapview.state"
	capMaps = 16
)

// MapState is the persistent per-map state held in the Window state
// registry. Accessed by the widget factory each frame and mutated by
// package-level helpers (PanTo, SetZoom, ...).
//
// Transient drag-tracking fields live on panState below to keep the
// snapshot type small.
type MapState struct {
	Center projection.LatLng
	Zoom   uint32

	// Seeded tracks whether Cfg.Initial* values have been written. On
	// first frame the factory seeds Center/Zoom from Cfg and sets
	// Seeded=true. Subsequent frames read the registry verbatim.
	Seeded bool
}

// panState tracks an in-progress drag pan. Stored in a separate
// namespace so MapState stays a clean snapshot.
type panState struct {
	Active    bool
	StartX    float32 // mouse down position (window coords)
	StartY    float32
	StartCtr  projection.LatLng // center at drag start
	StartZoom uint32
}

const nsPan = "mapview.pan"

// readState returns the current MapState for id, seeding it from seed
// if the registry has no entry yet. Callers treat the result as a
// value snapshot; writes go through writeState.
func readState(w *gui.Window, id string, seed MapState) MapState {
	sm := gui.StateMap[string, MapState](w, nsState, capMaps)
	if s, ok := sm.Get(id); ok {
		return s
	}
	seed.Seeded = true
	sm.Set(id, seed)
	return seed
}

func readPan(w *gui.Window, id string) panState {
	return gui.StateReadOr[string, panState](w, nsPan, id, panState{})
}

func writePan(w *gui.Window, id string, p panState) {
	gui.StateMap[string, panState](w, nsPan, capMaps).Set(id, p)
}

// Snapshot returns the current MapState for the map with the given
// ID. Returns the zero value if the map has not yet rendered.
func Snapshot(w *gui.Window, id string) MapState {
	return gui.StateReadOr[string, MapState](w, nsState, id, MapState{})
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

// maxZoom is the global zoom ceiling. Tile sources may cap lower via
// their own MaxZoom(); input handlers consult that at call time.
const maxZoom uint32 = 22
