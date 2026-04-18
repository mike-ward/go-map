package mapview

import (
	"math"
	"testing"

	"github.com/mike-ward/go-gui/gui"
	"github.com/mike-ward/go-map/projection"
)

// bumpVersion monotonically increments. Zero is the pre-mutation
// baseline (first-frame render sees no cache entry regardless).
func TestBumpVersion_Monotonic(t *testing.T) {
	w := &gui.Window{}
	if v := readVersion(w, "m"); v != 0 {
		t.Errorf("initial readVersion = %d, want 0", v)
	}
	bumpVersion(w, "m")
	if v := readVersion(w, "m"); v != 1 {
		t.Errorf("after 1 bump = %d, want 1", v)
	}
	bumpVersion(w, "m")
	bumpVersion(w, "m")
	if v := readVersion(w, "m"); v != 3 {
		t.Errorf("after 3 bumps = %d, want 3", v)
	}
}

// Versions are keyed by id so two maps in the same window advance
// independently.
func TestBumpVersion_PerID(t *testing.T) {
	w := &gui.Window{}
	bumpVersion(w, "a")
	bumpVersion(w, "a")
	bumpVersion(w, "b")
	if va := readVersion(w, "a"); va != 2 {
		t.Errorf("a = %d, want 2", va)
	}
	if vb := readVersion(w, "b"); vb != 1 {
		t.Errorf("b = %d, want 1", vb)
	}
}

// Every public state mutator must bump so the DrawCanvas cache
// invalidates on the next frame. No-op branches (unknown id) must
// not bump — no captured state changed.
func TestPublicMutators_BumpVersion(t *testing.T) {
	cases := []struct {
		name string
		fn   func(w *gui.Window)
	}{
		{"PanTo", func(w *gui.Window) {
			PanTo(w, "m", projection.LatLng{Lat: 1, Lng: 2})
		}},
		{"SetZoom", func(w *gui.Window) { SetZoom(w, "m", 5) }},
		{"SetView", func(w *gui.Window) {
			SetView(w, "m", projection.LatLng{Lat: 1, Lng: 2}, 5)
		}},
		{"AddOverlay", func(w *gui.Window) {
			AddOverlay(w, "m", &Marker{MarkerID: "x",
				Pos: projection.LatLng{Lat: 1, Lng: 2}})
		}},
		{"RemoveOverlay", func(w *gui.Window) {
			AddOverlay(w, "m", &Marker{MarkerID: "x",
				Pos: projection.LatLng{Lat: 1, Lng: 2}})
			RemoveOverlay(w, "m", "x")
		}},
		{"ClearOverlays", func(w *gui.Window) {
			AddOverlay(w, "m", &Marker{MarkerID: "x",
				Pos: projection.LatLng{Lat: 1, Lng: 2}})
			ClearOverlays(w, "m")
		}},
		{"FitBounds", func(w *gui.Window) {
			FitBounds(w, "m", projection.Bounds{
				NE: projection.LatLng{Lat: 1, Lng: 1},
				SW: projection.LatLng{Lat: -1, Lng: -1},
			}, 0, 512, 512)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := &gui.Window{}
			readState(w, "m", MapState{Zoom: 10})
			before := readVersion(w, "m")
			c.fn(w)
			after := readVersion(w, "m")
			if after <= before {
				t.Errorf("%s: version %d → %d, want increment",
					c.name, before, after)
			}
		})
	}
}

// PanTo / SetZoom / SetView / FitBounds on an unseeded id are
// no-ops. No state changed → no bump.
func TestNoOpMutators_DoNotBump(t *testing.T) {
	cases := []struct {
		name string
		fn   func(w *gui.Window)
	}{
		{"PanTo", func(w *gui.Window) {
			PanTo(w, "missing", projection.LatLng{Lat: 1, Lng: 2})
		}},
		{"SetZoom", func(w *gui.Window) { SetZoom(w, "missing", 5) }},
		{"SetView", func(w *gui.Window) {
			SetView(w, "missing", projection.LatLng{Lat: 1, Lng: 2}, 5)
		}},
		{"FitBounds_Unseeded", func(w *gui.Window) {
			FitBounds(w, "missing", projection.Bounds{
				NE: projection.LatLng{Lat: 1, Lng: 1},
				SW: projection.LatLng{Lat: -1, Lng: -1},
			}, 0, 512, 512)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := &gui.Window{}
			if v := readVersion(w, "missing"); v != 0 {
				t.Fatalf("baseline = %d, want 0", v)
			}
			c.fn(w)
			if v := readVersion(w, "missing"); v != 0 {
				t.Errorf("%s bumped on no-op: %d", c.name, v)
			}
		})
	}
}

// FitBounds has four guard paths (zero bounds, non-finite canvas,
// inverted bounds, negative dx/dy post-project) that return without
// mutating state. None must bump — a misplaced bump here would
// invalidate the tessellation cache on every degenerate caller.
func TestFitBounds_DegenerateDoesNotBump(t *testing.T) {
	valid := projection.Bounds{
		NE: projection.LatLng{Lat: 1, Lng: 1},
		SW: projection.LatLng{Lat: -1, Lng: -1},
	}
	inverted := projection.Bounds{
		NE: projection.LatLng{Lat: -1, Lng: -1},
		SW: projection.LatLng{Lat: 1, Lng: 1},
	}
	cases := []struct {
		name string
		fn   func(w *gui.Window)
	}{
		{"ZeroBounds", func(w *gui.Window) {
			FitBounds(w, "m", projection.Bounds{}, 0, 512, 512)
		}},
		{"InvertedBounds", func(w *gui.Window) {
			FitBounds(w, "m", inverted, 0, 512, 512)
		}},
		{"NonFiniteCanvasW", func(w *gui.Window) {
			FitBounds(w, "m", valid, 0,
				float32(math.NaN()), 512)
		}},
		{"NonPositiveCanvasH", func(w *gui.Window) {
			FitBounds(w, "m", valid, 0, 512, 0)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := &gui.Window{}
			readState(w, "m", MapState{Zoom: 10})
			before := readVersion(w, "m")
			c.fn(w)
			if got := readVersion(w, "m"); got != before {
				t.Errorf("%s: version %d → %d, want no change",
					c.name, before, got)
			}
		})
	}
}

// readState's seed branch writes via sm.Set, deliberately bypassing
// nsWrite so the first-frame seed does NOT bump. If a future refactor
// routes readState through nsWrite, the seed would invalidate a cache
// that doesn't yet exist — harmless today but a silent contract drift.
func TestReadState_SeedDoesNotBump(t *testing.T) {
	w := &gui.Window{}
	readState(w, "m", MapState{Zoom: 7})
	if v := readVersion(w, "m"); v != 0 {
		t.Errorf("seed bumped version to %d, want 0", v)
	}
}

// RemoveOverlay on an absent id and ClearOverlays on an empty map
// are both no-ops and must not bump — spurious bumps would force an
// OnDraw re-run for nothing.
func TestOverlayMutators_NoOpDoNotBump(t *testing.T) {
	t.Run("RemoveOverlay_Absent", func(t *testing.T) {
		w := &gui.Window{}
		readState(w, "m", MapState{})
		before := readVersion(w, "m")
		RemoveOverlay(w, "m", "never-added")
		if got := readVersion(w, "m"); got != before {
			t.Errorf("version %d → %d, want no change", before, got)
		}
	})
	t.Run("ClearOverlays_Empty", func(t *testing.T) {
		w := &gui.Window{}
		readState(w, "m", MapState{})
		before := readVersion(w, "m")
		ClearOverlays(w, "m")
		if got := readVersion(w, "m"); got != before {
			t.Errorf("version %d → %d, want no change", before, got)
		}
	})
}

// Writes to namespaces that do NOT affect OnDraw output must not
// bump. A spurious bump there would invalidate the tessellation
// cache every drag tick / callback fire / scroll accumulation
// step — the whole point of the counter is to skip those.
//
// nsInfoRect is the nastiest case: it is written from inside
// drawFocus each frame the popup is open. Bumping there would be
// a render-next-frame feedback loop.
func TestNonInvalidatingNamespaces_DoNotBump(t *testing.T) {
	cases := []struct {
		name string
		fn   func(w *gui.Window)
	}{
		{"nsLastFired", func(w *gui.Window) {
			nsWrite(w, nsLastFired, "m", lastFired{Set: true})
		}},
		{"nsScroll", func(w *gui.Window) {
			nsWrite(w, nsScroll, "m", float32(1.5))
		}},
		{"nsPan", func(w *gui.Window) {
			nsWrite(w, nsPan, "m", panState{Active: true})
		}},
		{"nsSeeded", func(w *gui.Window) {
			nsWrite(w, nsSeeded, "m", true)
		}},
		{"nsInfoRect", func(w *gui.Window) {
			nsWrite(w, nsInfoRect, "m", infoRectState{})
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := &gui.Window{}
			c.fn(w)
			if v := readVersion(w, "m"); v != 0 {
				t.Errorf("%s bumped version to %d", c.name, v)
			}
		})
	}
}

// nsState / nsHover writes through the generic nsWrite path must
// auto-bump. The auto-bump is how 15 input.go call sites stay in
// sync without per-site instrumentation.
func TestInvalidatingNamespaces_AutoBump(t *testing.T) {
	t.Run("nsState", func(t *testing.T) {
		w := &gui.Window{}
		nsWrite(w, nsState, "m", MapState{Zoom: 5})
		if v := readVersion(w, "m"); v != 1 {
			t.Errorf("nsState write did not bump: version = %d", v)
		}
	})
	t.Run("nsHover", func(t *testing.T) {
		w := &gui.Window{}
		nsWrite(w, nsHover, "m", hoverState{X: 1, Y: 2, Valid: true})
		if v := readVersion(w, "m"); v != 1 {
			t.Errorf("nsHover write did not bump: version = %d", v)
		}
	})
	t.Run("nsLayers", func(t *testing.T) {
		w := &gui.Window{}
		nsWrite(w, nsLayers, "m",
			gui.NewBoundedMap[string, Layer](capLayersPerMap))
		if v := readVersion(w, "m"); v != 1 {
			t.Errorf("nsLayers write did not bump: version = %d", v)
		}
	})
}
