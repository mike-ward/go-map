package mapview

import (
	"math"
	"testing"

	"github.com/mike-ward/go-gui/gui"
	"github.com/mike-ward/go-map/projection"
)

func TestFireDecision_FirstFrameSeedsBaseline(t *testing.T) {
	s := MapState{Center: projection.LatLng{Lat: 1, Lng: 2}, Zoom: 5}
	next, fm, fz, _ := fireDecision(lastFired{}, s, projection.LatLng{}, false)
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
	next, fm, fz, _ := fireDecision(prev, s, projection.LatLng{}, false)
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
	next, fm, fz, _ := fireDecision(prev, now, projection.LatLng{}, false)
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
	_, fm, fz, _ := fireDecision(prev, now, projection.LatLng{}, false)
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
	_, fm, fz, _ := fireDecision(prev, now, projection.LatLng{}, false)
	if !fm || !fz {
		t.Errorf("both changed: move=%v zoom=%v", fm, fz)
	}
}

// Cursor entry seeds the hover baseline — no OnHover fire on the
// very first valid sample. Matches OnMove / OnZoomChange's first-
// frame-suppression rule so consumers do not see a synthetic event
// on every mouse-over.
func TestFireDecision_FirstHoverSeedsBaseline(t *testing.T) {
	ll := projection.LatLng{Lat: 40, Lng: -74}
	next, _, _, fh := fireDecision(
		lastFired{}, MapState{}, ll, true)
	if fh {
		t.Error("first hover sample must not fire OnHover")
	}
	if !next.HoverSet || next.HoverLL != ll {
		t.Errorf("hover baseline = %+v, want set with %+v", next, ll)
	}
}

// Same LatLng twice must not fire — a stationary cursor over a
// stationary map should stay silent.
func TestFireDecision_HoverUnchangedNoFire(t *testing.T) {
	ll := projection.LatLng{Lat: 40, Lng: -74}
	prev := lastFired{HoverLL: ll, HoverSet: true}
	_, _, _, fh := fireDecision(prev, MapState{}, ll, true)
	if fh {
		t.Error("unchanged hover must not fire")
	}
}

// Any LatLng difference fires — cursor moved, map panned under a
// stationary cursor, or both.
func TestFireDecision_HoverChangeFires(t *testing.T) {
	prev := lastFired{
		HoverLL:  projection.LatLng{Lat: 40, Lng: -74},
		HoverSet: true,
	}
	now := projection.LatLng{Lat: 41, Lng: -74}
	next, _, _, fh := fireDecision(prev, MapState{}, now, true)
	if !fh {
		t.Error("hover LatLng change must fire OnHover")
	}
	if next.HoverLL != now {
		t.Errorf("baseline hover = %+v, want %+v", next.HoverLL, now)
	}
}

// Cursor exit (hoverPresent=false) clears the baseline so the next
// entry seeds fresh — without this, a re-entry at the same LatLng
// as the last pre-exit sample would stay silent forever.
func TestFireDecision_HoverExitClearsBaseline(t *testing.T) {
	prev := lastFired{
		HoverLL:  projection.LatLng{Lat: 40, Lng: -74},
		HoverSet: true,
	}
	next, _, _, fh := fireDecision(
		prev, MapState{}, projection.LatLng{}, false)
	if fh {
		t.Error("cursor exit must not fire OnHover")
	}
	if next.HoverSet {
		t.Error("cursor exit must clear HoverSet")
	}
}

// MapState delta and hover delta are independent — a simultaneous
// pan + hover change fires both without either suppressing the
// other.
func TestFireDecision_MoveAndHoverFireTogether(t *testing.T) {
	prev := lastFired{
		State:    MapState{Center: projection.LatLng{Lat: 0, Lng: 0}, Zoom: 5},
		Set:      true,
		HoverLL:  projection.LatLng{Lat: 40, Lng: -74},
		HoverSet: true,
	}
	s := MapState{Center: projection.LatLng{Lat: 1, Lng: 0}, Zoom: 5}
	ll := projection.LatLng{Lat: 41, Lng: -74}
	_, fm, _, fh := fireDecision(prev, s, ll, true)
	if !fm || !fh {
		t.Errorf("both deltas: move=%v hover=%v", fm, fh)
	}
}

// fireCallbacks + the full registry plumbing: seed a map state,
// canvas size, and hover, then invoke fireCallbacks twice and
// confirm OnHover fires on the second call (baseline seeded first).
func TestFireCallbacks_OnHoverFiresOnDelta(t *testing.T) {
	w := &gui.Window{}
	nsWrite(w, nsCanvas, "m", canvasSize{W: 256, H: 256})
	nsWrite(w, nsHover, "m", hoverState{X: 64, Y: 64, Valid: true})
	s := MapState{
		Center: projection.LatLng{Lat: 40, Lng: -74},
		Zoom:   5,
	}
	nsWrite(w, nsState, "m", s)

	var fired int
	var gotLL projection.LatLng
	c := Cfg{
		ID: "m",
		OnHover: func(_ *gui.Window, ll projection.LatLng) {
			fired++
			gotLL = ll
		},
	}

	// First call seeds the hover baseline — no fire.
	fireCallbacks(w, c, s)
	if fired != 0 {
		t.Errorf("first fireCallbacks fired OnHover %d times, want 0", fired)
	}

	// Shift the hover sample so the projected LatLng differs.
	nsWrite(w, nsHover, "m", hoverState{X: 128, Y: 128, Valid: true})
	fireCallbacks(w, c, s)
	if fired != 1 {
		t.Errorf("second fireCallbacks fired OnHover %d times, want 1", fired)
	}
	if (gotLL == projection.LatLng{}) {
		t.Error("callback LatLng was zero; expected projected value")
	}
}

// Cursor outside canvas (hoverState.Valid=false) must not fire
// OnHover. Guards against a regression that projects a zero hover
// position as latlng (0,0) and fires on entry.
func TestFireCallbacks_OnHoverSilentWhenCursorOutsideCanvas(t *testing.T) {
	w := &gui.Window{}
	nsWrite(w, nsCanvas, "m", canvasSize{W: 256, H: 256})
	s := MapState{Zoom: 5}
	nsWrite(w, nsState, "m", s)
	// Explicit invalid hover — cursor left the canvas.
	nsWrite(w, nsHover, "m", hoverState{})

	var fired int
	c := Cfg{
		ID:      "m",
		OnHover: func(*gui.Window, projection.LatLng) { fired++ },
	}
	fireCallbacks(w, c, s)
	fireCallbacks(w, c, s)
	if fired != 0 {
		t.Errorf("OnHover fired %d times with invalid hover; want 0", fired)
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
