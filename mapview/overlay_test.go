package mapview

import (
	"math"
	"testing"

	"github.com/mike-ward/go-gui/gui"
	"github.com/mike-ward/go-map/projection"
)

// fakeProjector is a tiny manual Projector used so overlay HitTest
// tests can fix screen coords without booting a window or running a
// full draw pass.
type fakeProjector struct {
	// pts maps each test LatLng back to a screen coordinate.
	pts map[projection.LatLng][2]float32
	mpp float32 // meters-per-pixel; 1 means radius in meters == pixels
}

func (f fakeProjector) LatLngToScreen(p projection.LatLng) (x, y float32) {
	if v, ok := f.pts[p]; ok {
		return v[0], v[1]
	}
	return 0, 0
}

func (f fakeProjector) MetersToPixels(_ float64, meters float64) float32 {
	return float32(meters) / f.mpp
}

func (f fakeProjector) Zoom() uint32 { return 10 }

func TestMarker_HitTest(t *testing.T) {
	p := projection.LatLng{Lat: 45, Lng: -122}
	m := &Marker{MarkerID: "m", Pos: p, Label: "home"}
	pr := fakeProjector{pts: map[projection.LatLng][2]float32{p: {100, 100}}, mpp: 1}
	if !m.HitTest(pr, 105, 105) {
		t.Error("near-center click should hit")
	}
	if m.HitTest(pr, 200, 100) {
		t.Error("far click should miss")
	}
}

func TestPolyline_HitTest(t *testing.T) {
	p0 := projection.LatLng{Lat: 0, Lng: 0}
	p1 := projection.LatLng{Lat: 0, Lng: 1}
	pl := &Polyline{LineID: "pl", Points: []projection.LatLng{p0, p1}, StrokeWidth: 2}
	pr := fakeProjector{pts: map[projection.LatLng][2]float32{
		p0: {10, 50},
		p1: {100, 50},
	}}
	if !pl.HitTest(pr, 55, 50) {
		t.Error("click on segment should hit")
	}
	if pl.HitTest(pr, 55, 200) {
		t.Error("click far from segment should miss")
	}
}

func TestPolygon_HitTest(t *testing.T) {
	a := projection.LatLng{Lat: 0, Lng: 0}
	b := projection.LatLng{Lat: 0, Lng: 1}
	c := projection.LatLng{Lat: 1, Lng: 1}
	d := projection.LatLng{Lat: 1, Lng: 0}
	pg := &Polygon{PolyID: "pg", Ring: []projection.LatLng{a, b, c, d}}
	pr := fakeProjector{pts: map[projection.LatLng][2]float32{
		a: {0, 0}, b: {100, 0}, c: {100, 100}, d: {0, 100},
	}}
	if !pg.HitTest(pr, 50, 50) {
		t.Error("click inside polygon should hit")
	}
	if pg.HitTest(pr, 200, 50) {
		t.Error("click outside polygon should miss")
	}
}

func TestCircle_HitTest_ScalesWithZoom(t *testing.T) {
	p := projection.LatLng{Lat: 45, Lng: -122}
	// mpp=1 ⇒ RadiusMeters 20 → 20 px.
	ci := &Circle{CircleID: "c", Center: p, RadiusMeters: 20}
	pr := fakeProjector{pts: map[projection.LatLng][2]float32{p: {100, 100}}, mpp: 1}
	if !ci.HitTest(pr, 110, 100) {
		t.Error("inside radius should hit")
	}
	if ci.HitTest(pr, 130, 100) {
		t.Error("outside radius should miss")
	}
}

// AddOverlay rejects overlays with empty IDs — stale-state bugs on
// frame-re-registration are the whole reason author IDs are required.
func TestAddOverlay_RequiresID(t *testing.T) {
	w := &gui.Window{}
	AddOverlay(w, "m", &Marker{MarkerID: "", Pos: projection.LatLng{}})
	if readOverlays(w, "m").Len() != 0 {
		t.Error("empty-ID overlay should be rejected")
	}
}

// AddOverlay + RemoveOverlay round-trip through the registry; Remove
// leaves the namespace entry so later Adds still land in the same
// BoundedMap.
func TestAddOverlay_RoundTrip(t *testing.T) {
	w := &gui.Window{}
	m := &Marker{MarkerID: "x", Pos: projection.LatLng{Lat: 1, Lng: 2}}
	AddOverlay(w, "m", m)
	if got, ok := readOverlays(w, "m").Get("x"); !ok || got != m {
		t.Errorf("lookup after Add = %v ok=%v, want %v true", got, ok, m)
	}
	RemoveOverlay(w, "m", "x")
	if _, ok := readOverlays(w, "m").Get("x"); ok {
		t.Error("entry still present after Remove")
	}
}

func TestClearOverlays(t *testing.T) {
	w := &gui.Window{}
	AddOverlay(w, "m", &Marker{MarkerID: "a", Pos: projection.LatLng{}})
	AddOverlay(w, "m", &Marker{MarkerID: "b", Pos: projection.LatLng{}})
	ClearOverlays(w, "m")
	if n := readOverlays(w, "m").Len(); n != 0 {
		t.Errorf("after Clear len = %d, want 0", n)
	}
}

// FitBounds picks the largest zoom at which the box fits. A 10°-wide
// box inset with 20 px padding on a 400×400 canvas should land at a
// zoom the projection says it fits in.
func TestFitBounds_SelectsFittingZoom(t *testing.T) {
	w := &gui.Window{}
	readState(w, "m", MapState{})
	b := projection.Bounds{
		NE: projection.LatLng{Lat: 5, Lng: 5},
		SW: projection.LatLng{Lat: -5, Lng: -5},
	}
	FitBounds(w, "m", b, 20, 400, 400)
	s, _ := Snapshot(w, "m")
	if s.Zoom == 0 {
		t.Error("FitBounds picked zoom 0; should fit at a higher zoom")
	}
	// At the chosen zoom, the projected box must fit in 360x360.
	ne := projection.Project(b.NE, s.Zoom)
	sw := projection.Project(b.SW, s.Zoom)
	if wpx := ne.X - sw.X; wpx > 360 {
		t.Errorf("box width %g exceeds avail 360 at chosen zoom %d", wpx, s.Zoom)
	}
	// Growing zoom by 1 must overflow — otherwise FitBounds left a
	// larger fit on the table.
	if s.Zoom < maxZoom {
		ne2 := projection.Project(b.NE, s.Zoom+1)
		sw2 := projection.Project(b.SW, s.Zoom+1)
		if wpx := ne2.X - sw2.X; wpx <= 360 {
			t.Errorf("zoom %d also fits (w=%g); FitBounds under-shot", s.Zoom+1, wpx)
		}
	}
}

// Marker.OnClick must fire even when Cfg.OnPOISelect is nil — hoisting
// the hit-test out of the OnPOISelect gate is a regression-prone
// refactor, so lock it in with a test.
func TestPanDragEnd_MarkerOnClick_WithoutOnPOISelect(t *testing.T) {
	w := &gui.Window{}
	id := "m"
	readState(w, id, MapState{Center: projection.LatLng{Lat: 45, Lng: -122}, Zoom: 10})
	fired := false
	marker := &Marker{
		MarkerID: "target",
		Pos:      projection.LatLng{Lat: 45, Lng: -122},
		OnClick:  func(*gui.Window) { fired = true },
	}
	AddOverlay(w, id, marker)

	canvasW, canvasH := float32(200), float32(200)
	cx, cy := canvasW/2, canvasH/2
	// Align Start* with Local* so the up-point translation in
	// panDragEnd (StartX-LocalX offset) degenerates to identity.
	nsWrite(w, nsPan, id, panState{
		Active: true, Moved: false,
		StartX: cx, StartY: cy,
		LocalX: cx, LocalY: cy,
		CanvasW: canvasW, CanvasH: canvasH,
	})
	up := &gui.Event{MouseX: cx, MouseY: cy}
	panDragEnd(Cfg{ID: id})(nil, up, w)
	if !fired {
		t.Error("Marker.OnClick did not fire when OnPOISelect was nil")
	}
}

// Drag past the threshold must NOT fire any click callbacks.
func TestPanDragEnd_DragSuppressesClick(t *testing.T) {
	w := &gui.Window{}
	id := "m"
	readState(w, id, MapState{})
	clicked := false
	nsWrite(w, nsPan, id, panState{
		Active: true, Moved: true,
		LocalX: 10, LocalY: 10,
		CanvasW: 100, CanvasH: 100,
	})
	c := Cfg{
		ID:      id,
		OnClick: func(*gui.Window, projection.LatLng) { clicked = true },
	}
	panDragEnd(c)(nil, nil, w)
	if clicked {
		t.Error("OnClick fired despite Moved=true")
	}
}

// Polyline passing through (0, 0) as the first vertex must still
// include the subsequent points in its bounds — the legacy
// Extend(zero) sentinel reset the box to the second vertex and
// dropped the rest.
func TestPolyline_Bounds_FirstVertexAtZero(t *testing.T) {
	pl := &Polyline{
		LineID: "pl",
		Points: []projection.LatLng{
			{Lat: 0, Lng: 0},
			{Lat: 10, Lng: 20},
			{Lat: -5, Lng: -15},
		},
	}
	b := pl.Bounds()
	if b.NE.Lat != 10 || b.NE.Lng != 20 || b.SW.Lat != -5 || b.SW.Lng != -15 {
		t.Errorf("Bounds = %+v, want NE{10,20} SW{-5,-15}", b)
	}
}

// overlayVisible must render an overlay whose real position is
// visible through an antimeridian-straddling viewport. Before the
// world-pixel shift fix, the lat/lng cull test incorrectly rejected
// overlays on the east side (lng ≈ -170) when the viewport wrapped
// past lng = 180.
func TestOverlayVisible_AntimeridianStraddle(t *testing.T) {
	m := &Marker{MarkerID: "dateline-east", Pos: projection.LatLng{Lat: 0, Lng: -179}}
	// Build a viewport whose left edge sits near lng=+170 and whose
	// right edge wraps past the dateline (world X > worldSize).
	const z uint32 = 3
	worldPx := projection.WorldSize(z)
	leftLng := 170.0
	leftX := (leftLng + 180) / 360 * worldPx
	vpW := 400.0
	minX, maxX := leftX, leftX+vpW
	if maxX <= worldPx {
		t.Fatalf("test precondition: maxX=%g must exceed worldPx=%g",
			maxX, worldPx)
	}
	// Marker at Lat=0 projects to y = worldPx/2; center the viewport y
	// range on that so only the X-axis straddle fix is under test.
	midY := worldPx / 2
	if !overlayVisible(m, z, worldPx, minX, maxX, midY-100, midY+100) {
		t.Error("overlay at lng=-179 culled by straddling viewport")
	}
}

// Conversely, an overlay far from the viewport stays culled so the
// antimeridian shift does not turn culling into a no-op.
func TestOverlayVisible_OutOfView(t *testing.T) {
	m := &Marker{MarkerID: "faraway", Pos: projection.LatLng{Lat: 0, Lng: 0}}
	const z uint32 = 5
	worldPx := projection.WorldSize(z)
	// Viewport well to the east of the prime meridian, non-wrapping,
	// with a y range that intersects the marker's projected y so the
	// cull decision is forced to come from the X axis only.
	minX, maxX := worldPx*0.6, worldPx*0.6+200
	midY := worldPx / 2
	if overlayVisible(m, z, worldPx, minX, maxX, midY-100, midY+100) {
		t.Error("far-off marker should be culled")
	}
}

// Hostile Circle inputs (NaN/Inf radius) must not reach HitTest or
// the draw backend; the sub-pixel cull was bypassed by NaN before
// hardening (NaN <= 0.5 is false).
func TestCircle_HitTest_IgnoresNonFiniteRadius(t *testing.T) {
	p := projection.LatLng{Lat: 45, Lng: -122}
	pr := fakeProjector{pts: map[projection.LatLng][2]float32{p: {100, 100}}, mpp: 1}
	for _, r := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), 0, -5} {
		ci := &Circle{CircleID: "c", Center: p, RadiusMeters: r}
		if ci.HitTest(pr, 100, 100) {
			t.Errorf("radius %v hit-test should reject", r)
		}
	}
}

// Polyline/Polygon vertex counts above maxOverlayPoints must skip
// rendering so a pathological author-supplied slice does not allocate
// gigabytes per frame.
func TestPolyline_HitTest_SkipsOverCap(t *testing.T) {
	pts := make([]projection.LatLng, maxOverlayPoints+1)
	pl := &Polyline{LineID: "pl", Points: pts}
	// If the cap is not respected the loop walks every vertex and the
	// fakeProjector returns (0,0) for each — slow but harmless.
	// Assertion: HitTest returns false without touching any vertex.
	if pl.HitTest(fakeProjector{}, 0, 0) {
		t.Error("oversize polyline should skip hit-test")
	}
}

// Polyline.Draw must also honour the cap. Nil DrawContext / Projector
// is acceptable here because the guard runs first; if the cap ever
// regresses, the nil deref inside Draw will panic and the defer
// surfaces it as a test failure.
func TestPolyline_Draw_SkipsOverCap(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("oversize polyline draw panicked: %v", r)
		}
	}()
	pts := make([]projection.LatLng, maxOverlayPoints+1)
	(&Polyline{LineID: "pl", Points: pts}).Draw(nil, nil)
}

func TestPolygon_Draw_SkipsOverCap(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("oversize polygon draw panicked: %v", r)
		}
	}()
	pts := make([]projection.LatLng, maxOverlayPoints+1)
	(&Polygon{PolyID: "pg", Ring: pts}).Draw(nil, nil)
}

func TestPolygon_HitTest_SkipsOverCap(t *testing.T) {
	pts := make([]projection.LatLng, maxOverlayPoints+1)
	pg := &Polygon{PolyID: "pg", Ring: pts}
	if pg.HitTest(fakeProjector{}, 0, 0) {
		t.Error("oversize polygon should skip hit-test")
	}
}

// A polyline whose two vertices collapse to the same point must still
// hit-test against that point — segmentDistSq's lenSq==0 branch is the
// only thing stopping the division from blowing up.
func TestPolyline_HitTest_DegenerateSegment(t *testing.T) {
	p := projection.LatLng{Lat: 1, Lng: 2}
	pl := &Polyline{LineID: "pl", Points: []projection.LatLng{p, p}}
	pr := fakeProjector{pts: map[projection.LatLng][2]float32{p: {50, 50}}}
	if !pl.HitTest(pr, 50, 50) {
		t.Error("degenerate polyline at the hit point should match")
	}
	if pl.HitTest(pr, 500, 500) {
		t.Error("degenerate polyline far from hit point should miss")
	}
}

// Giant radii must not produce non-canonical lat/lng corners. The cap
// keeps culling decisions stable and avoids projection overflow.
func TestCircle_Bounds_CapsHugeRadius(t *testing.T) {
	c := &Circle{CircleID: "c", Center: projection.LatLng{Lat: 0, Lng: 0}, RadiusMeters: 1e20}
	b := c.Bounds()
	if b.NE.Lat != 90 || b.SW.Lat != -90 {
		t.Errorf("Lat corners = %v / %v, want ±90", b.NE.Lat, b.SW.Lat)
	}
	if b.NE.Lng != 180 || b.SW.Lng != -180 {
		t.Errorf("Lng corners = %v / %v, want ±180", b.NE.Lng, b.SW.Lng)
	}
}

// sanitizeStroke must clamp NaN / Inf / negative to the fallback so no
// non-finite width reaches the gui draw layer.
func TestSanitizeStroke(t *testing.T) {
	cases := []struct {
		in   float32
		want float32
	}{
		{float32(math.NaN()), 2},
		{float32(math.Inf(1)), 2},
		{float32(math.Inf(-1)), 2},
		{-1, 2},
		{0, 2},
		{5, 5},
		{1e9, 1024},
	}
	for _, c := range cases {
		if got := sanitizeStroke(c.in, 2); got != c.want {
			t.Errorf("sanitizeStroke(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

// FitBounds must no-op on inverted bounds rather than parking the
// zoom at the ceiling (the old "wpx <= availW" test trivially passed
// on a negative width).
func TestFitBounds_RejectsInvertedBounds(t *testing.T) {
	w := &gui.Window{}
	seed := MapState{Zoom: 3, Center: projection.LatLng{Lat: 10, Lng: 20}}
	readState(w, "m", seed)
	// NE east of SW but latitude inverted.
	bad := projection.Bounds{
		NE: projection.LatLng{Lat: -5, Lng: 5},
		SW: projection.LatLng{Lat: 5, Lng: -5},
	}
	FitBounds(w, "m", bad, 0, 400, 400)
	got, _ := Snapshot(w, "m")
	if got != seed {
		t.Errorf("inverted bounds mutated state: got %+v, want %+v", got, seed)
	}
}

func TestFitBounds_NoOpOnNonFiniteCanvas(t *testing.T) {
	w := &gui.Window{}
	seed := MapState{Zoom: 4}
	readState(w, "m", seed)
	b := projection.Bounds{
		NE: projection.LatLng{Lat: 1, Lng: 1},
		SW: projection.LatLng{Lat: 0, Lng: 0},
	}
	FitBounds(w, "m", b, 0, float32(math.NaN()), 400)
	got, _ := Snapshot(w, "m")
	if got != seed {
		t.Errorf("NaN canvas width mutated state: got %+v", got)
	}
}

func TestFitBounds_NoOpOnUnknownID(t *testing.T) {
	w := &gui.Window{}
	b := projection.Bounds{
		NE: projection.LatLng{Lat: 1, Lng: 1},
		SW: projection.LatLng{Lat: 0, Lng: 0},
	}
	FitBounds(w, "missing", b, 0, 100, 100)
	if _, ok := Snapshot(w, "missing"); ok {
		t.Error("FitBounds on unknown id created state")
	}
}

// Zero-valued bounds take the same early-return path as IsZero. Locked
// down separately so a refactor that drops the IsZero guard still gets
// caught.
func TestFitBounds_NoOpOnZeroBounds(t *testing.T) {
	w := &gui.Window{}
	seed := MapState{Zoom: 7, Center: projection.LatLng{Lat: 1, Lng: 2}}
	readState(w, "m", seed)
	FitBounds(w, "m", projection.Bounds{}, 0, 200, 200)
	got, _ := Snapshot(w, "m")
	if got != seed {
		t.Errorf("zero bounds mutated state: got %+v, want %+v", got, seed)
	}
}

// Negative / non-finite padding must be coerced to 0 rather than
// shrink availW below zero and bail out silently.
func TestFitBounds_NegativePaddingCoercedToZero(t *testing.T) {
	b := projection.Bounds{
		NE: projection.LatLng{Lat: 5, Lng: 5},
		SW: projection.LatLng{Lat: -5, Lng: -5},
	}
	ref := &gui.Window{}
	readState(ref, "m", MapState{})
	FitBounds(ref, "m", b, 0, 400, 400)
	want, _ := Snapshot(ref, "m")

	for _, pad := range []float32{-5, float32(math.NaN()), float32(math.Inf(1))} {
		w := &gui.Window{}
		readState(w, "m", MapState{})
		FitBounds(w, "m", b, pad, 400, 400)
		got, _ := Snapshot(w, "m")
		if got != want {
			t.Errorf("padding %v → %+v, want %+v (coerced to 0)", pad, got, want)
		}
	}
}

// Map panics at construction when any InitialOverlay has an empty ID —
// the widget cannot use the registry without a key.
func TestMap_PanicsOnInitialOverlayWithoutID(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Map did not panic on empty-ID overlay")
		}
	}()
	Map(Cfg{
		ID: "m",
		InitialOverlays: []Overlay{
			&Marker{MarkerID: "", Pos: projection.LatLng{}},
		},
	})
}

func TestMap_PanicsOnNilInitialOverlay(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Map did not panic on nil overlay")
		}
	}()
	Map(Cfg{ID: "m", InitialOverlays: []Overlay{nil}})
}

// seedOverlaysOnce must run exactly once per map ID. Resurrecting a
// removed overlay on the next frame would silently defeat
// RemoveOverlay for any overlay that happened to be in InitialOverlays.
func TestSeedOverlaysOnce_DoesNotResurrectRemoved(t *testing.T) {
	w := &gui.Window{}
	c := Cfg{
		ID: "m",
		InitialOverlays: []Overlay{
			&Marker{MarkerID: "a", Pos: projection.LatLng{Lat: 1, Lng: 2}},
		},
	}
	seedOverlaysOnce(w, c)
	RemoveOverlay(w, "m", "a")
	seedOverlaysOnce(w, c)
	if _, ok := readOverlays(w, "m").Get("a"); ok {
		t.Error("second seedOverlaysOnce call resurrected removed overlay")
	}
}

// A LatLng at the viewport center maps to the canvas center — the
// contract viewport exposes to overlays via the Projector interface.
func TestViewport_LatLngToScreen_CenterRoundTrip(t *testing.T) {
	s := MapState{
		Center: projection.LatLng{Lat: 47.6062, Lng: -122.3321},
		Zoom:   11,
	}
	vp := computeViewport(800, 600, s)
	x, y := vp.LatLngToScreen(s.Center)
	if math.Abs(float64(x-400)) > 1e-3 || math.Abs(float64(y-300)) > 1e-3 {
		t.Errorf("center projected to (%v, %v), want (400, 300)", x, y)
	}
}

// MetersToPixels at a valid zoom produces a finite positive result;
// exercises the finite-check gate used by Circle.Draw.
func TestViewport_MetersToPixels(t *testing.T) {
	vp := computeViewport(400, 400, MapState{Zoom: 5})
	got := vp.MetersToPixels(0, 1000)
	if got <= 0 || math.IsNaN(float64(got)) || math.IsInf(float64(got), 0) {
		t.Errorf("MetersToPixels returned %v, want finite positive", got)
	}
}

// Antimeridian-exact: a marker sitting at lng=180 must render when the
// viewport straddles the dateline — shift=+worldPx catches it from the
// east side.
func TestOverlayVisible_ExactlyAtAntimeridian(t *testing.T) {
	m := &Marker{MarkerID: "dateline", Pos: projection.LatLng{Lat: 0, Lng: 180}}
	const z uint32 = 4
	worldPx := projection.WorldSize(z)
	// Viewport centered on the antimeridian.
	leftX := worldPx - 100
	minX, maxX := leftX, leftX+200
	midY := worldPx / 2
	if !overlayVisible(m, z, worldPx, minX, maxX, midY-50, midY+50) {
		t.Error("marker exactly at lng=180 should be visible in straddling viewport")
	}
}

// panDragMove swallows sub-threshold motion: the pan baseline stays
// unseeded, the map center does not move, and Moved remains false.
func TestPanDragMove_SwallowsBelowThreshold(t *testing.T) {
	w := &gui.Window{}
	id := "m"
	seed := MapState{Center: projection.LatLng{Lat: 10, Lng: 20}, Zoom: 5}
	readState(w, id, seed)
	nsWrite(w, nsPan, id, panState{
		Active: true, StartX: 0, StartY: 0,
		StartCtr: seed.Center, StartZoom: seed.Zoom,
	})
	// 2 px hypotenuse — below the 4 px threshold.
	e := &gui.Event{MouseX: 2, MouseY: 2}
	panDragMove(id)(nil, e, w)

	if p := nsRead[panState](w, nsPan, id); p.Moved {
		t.Error("Moved flipped on sub-threshold motion")
	}
	if got, _ := Snapshot(w, id); got.Center != seed.Center {
		t.Errorf("center drifted on sub-threshold motion: %+v", got.Center)
	}
}

// panDragMove flips Moved and updates center once travel crosses the
// drag threshold.
func TestPanDragMove_FlipsMovedAtThreshold(t *testing.T) {
	w := &gui.Window{}
	id := "m"
	seed := MapState{Center: projection.LatLng{Lat: 0, Lng: 0}, Zoom: 5}
	readState(w, id, seed)
	nsWrite(w, nsPan, id, panState{
		Active: true, StartX: 0, StartY: 0,
		StartCtr: seed.Center, StartZoom: seed.Zoom,
	})
	// 10 px east — comfortably past threshold.
	e := &gui.Event{MouseX: 10, MouseY: 0}
	panDragMove(id)(nil, e, w)

	if p := nsRead[panState](w, nsPan, id); !p.Moved {
		t.Error("Moved did not flip on above-threshold motion")
	}
	if got, _ := Snapshot(w, id); got.Center == seed.Center {
		t.Error("center unchanged after above-threshold pan")
	}
}

// OnClick receives a LatLng derived from the release point; the
// previous regression hoist was checked only for Marker.OnClick. This
// asserts the map-level OnClick callback path end-to-end.
func TestPanDragEnd_OnClickDeliversLatLng(t *testing.T) {
	w := &gui.Window{}
	id := "m"
	seed := MapState{Center: projection.LatLng{Lat: 45, Lng: -122}, Zoom: 10}
	readState(w, id, seed)
	const canvasW, canvasH float32 = 200, 200
	cx, cy := canvasW/2, canvasH/2
	nsWrite(w, nsPan, id, panState{
		Active: true, Moved: false,
		StartX: cx, StartY: cy,
		LocalX: cx, LocalY: cy,
		CanvasW: canvasW, CanvasH: canvasH,
	})
	var got projection.LatLng
	var fired bool
	c := Cfg{
		ID: id,
		OnClick: func(_ *gui.Window, ll projection.LatLng) {
			got = ll
			fired = true
		},
	}
	up := &gui.Event{MouseX: cx, MouseY: cy}
	panDragEnd(c)(nil, up, w)
	if !fired {
		t.Fatal("OnClick did not fire")
	}
	// Release at the canvas center → LatLng ≈ map center. Tolerance
	// reflects the float32 round-trip through viewport.screenToLatLng.
	if math.Abs(got.Lat-seed.Center.Lat) > 1e-4 ||
		math.Abs(got.Lng-seed.Center.Lng) > 1e-4 {
		t.Errorf("OnClick LatLng = %+v, want %+v", got, seed.Center)
	}
}

// When two overlays overlap at the click point, panDragEnd must
// report the one drawn last (topmost). The hit-test walks BoundedMap
// insertion order and keeps the final match — regression guard for
// that iteration direction.
func TestPanDragEnd_TopmostOverlayWins(t *testing.T) {
	w := &gui.Window{}
	id := "m"
	center := projection.LatLng{Lat: 45, Lng: -122}
	readState(w, id, MapState{Center: center, Zoom: 10})

	bottom := &Marker{MarkerID: "bottom", Pos: center,
		OnClick: func(*gui.Window) {}}
	top := &Marker{MarkerID: "top", Pos: center,
		OnClick: func(*gui.Window) {}}
	AddOverlay(w, id, bottom)
	AddOverlay(w, id, top)

	var selected Overlay
	c := Cfg{
		ID:          id,
		OnPOISelect: func(_ *gui.Window, o Overlay) { selected = o },
	}

	canvasW, canvasH := float32(200), float32(200)
	cx, cy := canvasW/2, canvasH/2
	nsWrite(w, nsPan, id, panState{
		Active: true, Moved: false,
		StartX: cx, StartY: cy, LocalX: cx, LocalY: cy,
		CanvasW: canvasW, CanvasH: canvasH,
	})
	panDragEnd(c)(nil, &gui.Event{MouseX: cx, MouseY: cy}, w)

	if selected != top {
		t.Errorf("selected = %v, want topmost %v", selected, top)
	}
}
