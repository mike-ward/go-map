package mapview

import (
	"math"
	"testing"

	"github.com/mike-ward/go-map/projection"
)

func TestFireDecision_FirstFrameSeedsBaseline(t *testing.T) {
	s := MapState{Center: projection.LatLng{Lat: 1, Lng: 2}, Zoom: 5}
	next, fm, fz := fireDecision(lastFired{}, s)
	if !next.Set || next.State != s {
		t.Errorf("baseline = %+v, want Set=true State=%+v", next, s)
	}
	if fm || fz {
		t.Errorf("first frame must not fire (move=%v zoom=%v)", fm, fz)
	}
}

func TestFireDecision_NoOpWhenStateEqual(t *testing.T) {
	s := MapState{Center: projection.LatLng{Lat: 1, Lng: 2}, Zoom: 5}
	prev := lastFired{State: s, Set: true}
	next, fm, fz := fireDecision(prev, s)
	if next != prev {
		t.Errorf("baseline drifted: %+v -> %+v", prev, next)
	}
	if fm || fz {
		t.Errorf("equal state must not fire (move=%v zoom=%v)", fm, fz)
	}
}

func TestFireDecision_CenterOnlyChange(t *testing.T) {
	prev := lastFired{
		State: MapState{Center: projection.LatLng{Lat: 1, Lng: 2}, Zoom: 5},
		Set:   true,
	}
	now := MapState{Center: projection.LatLng{Lat: 9, Lng: 9}, Zoom: 5}
	next, fm, fz := fireDecision(prev, now)
	if !fm {
		t.Error("center change must fire OnMove")
	}
	if fz {
		t.Error("zoom unchanged must not fire OnZoomChange")
	}
	if next.State != now {
		t.Errorf("baseline = %+v, want %+v", next.State, now)
	}
}

func TestFireDecision_ZoomOnlyChange(t *testing.T) {
	prev := lastFired{
		State: MapState{Center: projection.LatLng{Lat: 1, Lng: 2}, Zoom: 5},
		Set:   true,
	}
	now := MapState{Center: projection.LatLng{Lat: 1, Lng: 2}, Zoom: 7}
	_, fm, fz := fireDecision(prev, now)
	if fm {
		t.Error("center unchanged must not fire OnMove")
	}
	if !fz {
		t.Error("zoom change must fire OnZoomChange")
	}
}

func TestFireDecision_BothChange(t *testing.T) {
	prev := lastFired{
		State: MapState{Center: projection.LatLng{Lat: 0, Lng: 0}, Zoom: 1},
		Set:   true,
	}
	now := MapState{Center: projection.LatLng{Lat: 5, Lng: 5}, Zoom: 9}
	_, fm, fz := fireDecision(prev, now)
	if !fm || !fz {
		t.Errorf("both changed: move=%v zoom=%v", fm, fz)
	}
}

// NaN must flush the accumulator: returning NaN as residual would
// permanently jam the wheel because every future event would compute
// NaN+x = NaN and the consume loops would never fire.
func TestScrollSteps_NaNFlushesToZero(t *testing.T) {
	delta, residual := scrollSteps(float32(math.NaN()))
	if delta != 0 {
		t.Errorf("delta = %g, want 0", delta)
	}
	if math.IsNaN(float64(residual)) {
		t.Errorf("residual = NaN; must be flushed to 0")
	}
	if residual != 0 {
		t.Errorf("residual = %g, want 0", residual)
	}
}

// A single huge ScrollY (or many events between consumes) must not
// yield an excessive delta. The accumulator cap binds the input to
// ±maxScrollAccum before the step computation.
func TestScrollSteps_AccumCapped(t *testing.T) {
	for _, in := range []float32{1e6, 1e9, math.MaxFloat32} {
		delta, _ := scrollSteps(in)
		if delta > maxScrollAccum {
			t.Errorf("scrollSteps(%g) delta = %g exceeds cap %g",
				in, delta, maxScrollAccum)
		}
	}
	for _, in := range []float32{-1e6, -1e9, -math.MaxFloat32} {
		delta, _ := scrollSteps(in)
		if delta < -maxScrollAccum {
			t.Errorf("scrollSteps(%g) delta = %g below cap -%g",
				in, delta, maxScrollAccum)
		}
	}
}

// Sub-integer step (0.25) turns a sub-1 accumulator into a fractional
// zoom delta — the smooth-wheel promise of slice 5b. A full
// notch-worth of scroll still lands on an integer delta
// (1.0 = 4 × 0.25) so mouse-wheel UX is unchanged at the notch level.
func TestScrollSteps(t *testing.T) {
	cases := []struct {
		in              float32
		wantD, wantResi float32
	}{
		{0, 0, 0},
		{0.1, 0, 0.1},     // sub-step: nothing fires
		{0.25, 0.25, 0},   // exactly one sub-step
		{0.3, 0.25, 0.05}, // sub-step + residual
		{0.5, 0.5, 0},     // two sub-steps
		{1.0, 1.0, 0},     // one notch: four sub-steps, integer delta
		{2.7, 2.5, 0.2},   // ten sub-steps + residual
		{-0.1, 0, -0.1},   // negative sub-step
		{-0.5, -0.5, 0},
		{-1.0, -1.0, 0},
		{-3.2, -3.0, -0.2},
	}
	for _, c := range cases {
		gotD, gotR := scrollSteps(c.in)
		if d := gotD - c.wantD; d > 1e-6 || d < -1e-6 {
			t.Errorf("steps(%g) delta = %g, want %g", c.in, gotD, c.wantD)
		}
		if d := gotR - c.wantResi; d > 1e-6 || d < -1e-6 {
			t.Errorf("steps(%g) residual = %g, want %g", c.in, gotR, c.wantResi)
		}
	}
}

// Invariant across uncapped accumulator values: delta + residual
// reconstructs the input exactly. Catches refactors that lose
// precision on large values. Capped inputs reconstruct to the cap,
// not the raw input — the last case pins that explicitly.
func TestScrollSteps_DeltaPlusResidualReconstructsInput(t *testing.T) {
	cases := []struct {
		in, want float32
	}{
		{0.1, 0.1},
		{0.25, 0.25},
		{0.3, 0.3},
		{0.5, 0.5},
		{1.0, 1.0},
		{2.7, 2.7},
		{-1.5, -1.5},
		{-3.2, -3.2},
		{5.5, 5.5},
		{1e6, maxScrollAccum},   // capped: reconstruct to cap
		{-1e6, -maxScrollAccum}, // capped negative
	}
	for _, c := range cases {
		d, r := scrollSteps(c.in)
		got := d + r
		if x := got - c.want; x > 1e-6 || x < -1e-6 {
			t.Errorf("scrollSteps(%g): delta+residual=%g, want %g",
				c.in, got, c.want)
		}
	}
}
