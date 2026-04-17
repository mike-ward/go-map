package mapview

import (
	"math"
	"testing"

	"github.com/mike-ward/go-gui/gui"
	"github.com/mike-ward/go-map/projection"
)

// onMouseScroll: a single wheel click (ScrollY=1) at gain=0.25 must
// advance zoom by exactly one scrollZoomStep (0.25). Pins the
// end-to-end wiring — gain scales the accumulator, accumulator fires
// one sub-tick, sub-tick writes fractional zoom. A refactor that
// dropped the `* gain` multiplication would slip past the factory-
// only `TestMap_*ScrollZoomGain` checks; this test catches it.
func TestOnMouseScroll_GainScalesAccumulator(t *testing.T) {
	w := &gui.Window{}
	id := "m"
	seed := MapState{Center: projection.LatLng{Lat: 0, Lng: 0}, Zoom: 10}
	readState(w, id, seed)

	h := onMouseScroll(id, nil, 0.25)
	h(&gui.Layout{Shape: &gui.Shape{Width: 400, Height: 300}},
		&gui.Event{ScrollY: 1, MouseX: 200, MouseY: 150}, w)

	got, _ := Snapshot(w, id)
	want := 10.25
	if math.Abs(got.Zoom-want) > 1e-6 {
		t.Errorf("Zoom = %g, want %g (gain=0.25 should advance 0.25/notch)",
			got.Zoom, want)
	}
}

// onMouseScroll: four consecutive wheel clicks at gain=0.25 must
// accumulate to one full integer zoom (4 × 0.25 = 1.0). Locks the
// multi-event integration: residual carries across events, not
// silently discarded.
func TestOnMouseScroll_GainAccumulatesAcrossEvents(t *testing.T) {
	w := &gui.Window{}
	id := "m"
	seed := MapState{Center: projection.LatLng{Lat: 0, Lng: 0}, Zoom: 10}
	readState(w, id, seed)

	h := onMouseScroll(id, nil, 0.25)
	for i := 0; i < 4; i++ {
		h(&gui.Layout{Shape: &gui.Shape{Width: 400, Height: 300}},
			&gui.Event{ScrollY: 1, MouseX: 200, MouseY: 150}, w)
	}

	got, _ := Snapshot(w, id)
	if math.Abs(got.Zoom-11) > 1e-6 {
		t.Errorf("Zoom = %g after 4 clicks, want 11", got.Zoom)
	}
}

// onMouseScroll: NaN / ±Inf ScrollY must be rejected at the handler
// boundary before the accumulator would latch the poison. scrollSteps
// also flushes NaN, so both guards currently mask each other; pin the
// handler-level guard directly so a removal regression can't hide.
func TestOnMouseScroll_NonFiniteScrollYIgnored(t *testing.T) {
	for _, sy := range []float32{
		float32(math.NaN()),
		float32(math.Inf(1)),
		float32(math.Inf(-1)),
	} {
		w := &gui.Window{}
		id := "m"
		seed := MapState{Center: projection.LatLng{Lat: 0, Lng: 0}, Zoom: 10}
		readState(w, id, seed)

		h := onMouseScroll(id, nil, 1)
		h(&gui.Layout{Shape: &gui.Shape{Width: 400, Height: 300}},
			&gui.Event{ScrollY: sy, MouseX: 200, MouseY: 150}, w)

		got, _ := Snapshot(w, id)
		if got.Zoom != 10 {
			t.Errorf("ScrollY=%v: Zoom changed to %g, want 10 (unchanged)",
				sy, got.Zoom)
		}
		// Accumulator must also stay clean — a latched NaN would
		// propagate through every subsequent scroll event.
		if acc := nsRead[float32](w, nsScroll, id); acc != 0 {
			t.Errorf("ScrollY=%v: accumulator = %g, want 0", sy, acc)
		}
	}
}

// TestZoomToward_Invariant: the LatLng under the cursor must not
// move when zoom changes. This is the defining property of
// zoom-to-cursor pan.
func TestZoomToward_Invariant(t *testing.T) {
	s := MapState{
		Center: projection.LatLng{Lat: 47.6062, Lng: -122.3321},
		Zoom:   11,
	}
	widgetW, widgetH := float32(900), float32(650)

	cases := []struct {
		name       string
		cx, cy     float32
		newZoom    float64
		tolDegrees float64
	}{
		{"center_zoom_in", 450, 325, 12, 1e-9},
		{"center_zoom_out", 450, 325, 10, 1e-9},
		{"top_left_zoom_in", 50, 50, 13, 1e-6},
		{"bottom_right_zoom_out", 850, 600, 9, 1e-6},
		{"off_center_in", 700, 200, 14, 1e-6},
		{"two_steps_in", 200, 400, 13, 1e-6},
		{"fractional_zoom_in", 450, 325, 12.5, 1e-6},
		{"fractional_zoom_out", 450, 325, 10.25, 1e-6},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// LatLng under the cursor BEFORE the zoom change.
			oldPt := projection.ProjectF(s.Center, s.Zoom)
			cursorPxOld := projection.Point{
				X: oldPt.X + float64(c.cx-widgetW/2),
				Y: oldPt.Y + float64(c.cy-widgetH/2),
			}
			want := projection.UnprojectF(cursorPxOld, s.Zoom)

			// Compute new center, then resolve the LatLng at the SAME
			// screen position under the new zoom.
			newCtr := zoomToward(s, c.newZoom,
				c.cx, c.cy, widgetW, widgetH)
			newPt := projection.ProjectF(newCtr, c.newZoom)
			cursorPxNew := projection.Point{
				X: newPt.X + float64(c.cx-widgetW/2),
				Y: newPt.Y + float64(c.cy-widgetH/2),
			}
			got := projection.UnprojectF(cursorPxNew, c.newZoom)

			if dLat := math.Abs(got.Lat - want.Lat); dLat > c.tolDegrees {
				t.Errorf("Lat drift %g > tol %g (got %v, want %v)",
					dLat, c.tolDegrees, got.Lat, want.Lat)
			}
			if dLng := math.Abs(got.Lng - want.Lng); dLng > c.tolDegrees {
				t.Errorf("Lng drift %g > tol %g (got %v, want %v)",
					dLng, c.tolDegrees, got.Lng, want.Lng)
			}
		})
	}
}

// TestZoomToward_FractionalToFractional: zoom-to-cursor invariant
// must hold when BOTH source and target zoom are fractional — the
// previously-covered invariant tests start from integer s.Zoom. A
// regression that implicitly floored s.Zoom on entry would survive
// integer-start tests but break here.
func TestZoomToward_FractionalToFractional(t *testing.T) {
	s := MapState{
		Center: projection.LatLng{Lat: 40.0, Lng: -74.0},
		Zoom:   10.3,
	}
	widgetW, widgetH := float32(800), float32(600)
	cx, cy := float32(600), float32(200) // off-center cursor
	for _, newZ := range []float64{11.7, 9.25, 10.3} {
		// LatLng under cursor before the zoom.
		oldPt := projection.ProjectF(s.Center, s.Zoom)
		cursorPxOld := projection.Point{
			X: oldPt.X + float64(cx-widgetW/2),
			Y: oldPt.Y + float64(cy-widgetH/2),
		}
		want := projection.UnprojectF(cursorPxOld, s.Zoom)

		newCtr := zoomToward(s, newZ, cx, cy, widgetW, widgetH)
		newPt := projection.ProjectF(newCtr, newZ)
		cursorPxNew := projection.Point{
			X: newPt.X + float64(cx-widgetW/2),
			Y: newPt.Y + float64(cy-widgetH/2),
		}
		got := projection.UnprojectF(cursorPxNew, newZ)
		if math.Abs(got.Lat-want.Lat) > 1e-6 {
			t.Errorf("z=%g: Lat drift got %v, want %v",
				newZ, got.Lat, want.Lat)
		}
		if math.Abs(got.Lng-want.Lng) > 1e-6 {
			t.Errorf("z=%g: Lng drift got %v, want %v",
				newZ, got.Lng, want.Lng)
		}
	}
}

// TestZoomToward_SameZoomNoop: zooming to the current zoom level
// must leave the center unchanged.
func TestZoomToward_SameZoomNoop(t *testing.T) {
	s := MapState{
		Center: projection.LatLng{Lat: 40.7128, Lng: -74.0060},
		Zoom:   10,
	}
	got := zoomToward(s, s.Zoom, 400, 300, 800, 600)
	if math.Abs(got.Lat-s.Center.Lat) > 1e-9 {
		t.Errorf("Lat changed on no-op zoom: got %v, want %v",
			got.Lat, s.Center.Lat)
	}
	if math.Abs(got.Lng-s.Center.Lng) > 1e-9 {
		t.Errorf("Lng changed on no-op zoom: got %v, want %v",
			got.Lng, s.Center.Lng)
	}
}

// TestZoomToward_CursorAtCenter: when the cursor sits exactly at the
// canvas center, the map center must not move regardless of zoom
// delta.
func TestZoomToward_CursorAtCenter(t *testing.T) {
	s := MapState{
		Center: projection.LatLng{Lat: 51.5074, Lng: -0.1278},
		Zoom:   8,
	}
	widgetW, widgetH := float32(1024), float32(768)
	for _, newZ := range []float64{5, 7, 9, 12, 16, 11.5, 8.75} {
		got := zoomToward(s, newZ, widgetW/2, widgetH/2,
			widgetW, widgetH)
		// Mercator Unproject(Project(p, z), z) preserves within ~1e-12
		// at these latitudes; allow 1e-9.
		if math.Abs(got.Lat-s.Center.Lat) > 1e-9 {
			t.Errorf("z=%g: Lat drift got %v, want %v",
				newZ, got.Lat, s.Center.Lat)
		}
		if math.Abs(got.Lng-s.Center.Lng) > 1e-9 {
			t.Errorf("z=%g: Lng drift got %v, want %v",
				newZ, got.Lng, s.Center.Lng)
		}
	}
}
