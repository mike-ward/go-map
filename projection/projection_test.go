package projection

import (
	"math"
	"testing"
)

const tol = 1e-6

func approxEq(a, b float64) bool { return math.Abs(a-b) < tol }

func TestProjectUnprojectRoundTrip(t *testing.T) {
	cases := []LatLng{
		{Lat: 0, Lng: 0},
		{Lat: 47.6062, Lng: -122.3321}, // Seattle
		{Lat: -33.8688, Lng: 151.2093}, // Sydney
		{Lat: 51.5074, Lng: -0.1278},   // London
	}
	for _, z := range []uint32{0, 5, 10, 18} {
		for _, p := range cases {
			got := Unproject(Project(p, z), z)
			if !approxEq(got.Lat, p.Lat) || !approxEq(got.Lng, p.Lng) {
				t.Errorf("z=%d in=%+v got=%+v", z, p, got)
			}
		}
	}
}

func TestProjectOriginTopLeft(t *testing.T) {
	pt := Project(LatLng{Lat: 85.05112878, Lng: -180}, 0)
	if !approxEq(pt.X, 0) || pt.Y > 1 || pt.Y < -1 {
		t.Errorf("NW corner expected (0,0), got %+v", pt)
	}
}

func TestClampWrapsLongitude(t *testing.T) {
	p := LatLng{Lat: 0, Lng: 200}.Clamp()
	if !approxEq(p.Lng, -160) {
		t.Errorf("wrap 200 -> -160, got %v", p.Lng)
	}
}

func TestWorldSizeDoublesPerZoom(t *testing.T) {
	for z := uint32(0); z < 20; z++ {
		want := 256.0 * math.Pow(2, float64(z))
		if got := WorldSize(z); !approxEq(got, want) {
			t.Errorf("z=%d want=%v got=%v", z, want, got)
		}
	}
}

// Non-finite inputs to Clamp must collapse to 0; otherwise NaN/Inf
// propagates through Project/Unproject and silently corrupts every
// downstream viewport calculation.
func TestClamp_NaNCoercesToZero(t *testing.T) {
	cases := []LatLng{
		{Lat: math.NaN(), Lng: 0},
		{Lat: 0, Lng: math.NaN()},
		{Lat: math.NaN(), Lng: math.NaN()},
	}
	for _, c := range cases {
		got := c.Clamp()
		if math.IsNaN(got.Lat) || math.IsNaN(got.Lng) {
			t.Errorf("in=%+v Clamp produced NaN: %+v", c, got)
		}
	}
}

func TestClamp_InfCoercesToZero(t *testing.T) {
	cases := []LatLng{
		{Lat: math.Inf(1), Lng: 0},
		{Lat: math.Inf(-1), Lng: 0},
		{Lat: 0, Lng: math.Inf(1)},
		{Lat: 0, Lng: math.Inf(-1)},
	}
	for _, c := range cases {
		got := c.Clamp()
		if math.IsInf(got.Lat, 0) || math.IsInf(got.Lng, 0) ||
			math.IsNaN(got.Lat) || math.IsNaN(got.Lng) {
			t.Errorf("in=%+v Clamp produced non-finite: %+v", c, got)
		}
	}
}

// BoundsOf must include a (0,0) first vertex — the prior zero-Bounds
// sentinel caused Extend to reset on the second vertex and silently
// drop the origin point.
func TestBoundsOf_IncludesOriginFirstVertex(t *testing.T) {
	b := BoundsOf(
		LatLng{Lat: 0, Lng: 0},
		LatLng{Lat: 10, Lng: 20},
		LatLng{Lat: -5, Lng: -15},
	)
	if b.NE != (LatLng{Lat: 10, Lng: 20}) {
		t.Errorf("NE = %+v, want {10, 20}", b.NE)
	}
	if b.SW != (LatLng{Lat: -5, Lng: -15}) {
		t.Errorf("SW = %+v, want {-5, -15}", b.SW)
	}
}

func TestBoundsOf_Empty(t *testing.T) {
	if !BoundsOf().IsZero() {
		t.Error("BoundsOf() with no points should be zero")
	}
}

// BoundsOf with a single point must return a degenerate box at that
// point — callers (e.g. Marker.Bounds) rely on it.
func TestBoundsOf_SinglePoint(t *testing.T) {
	p := LatLng{Lat: 47.6, Lng: -122.3}
	b := BoundsOf(p)
	// Clamp's math.Mod introduces sub-ULP drift on Lng; compare with
	// tolerance rather than direct equality.
	if !approxEq(b.NE.Lat, p.Lat) || !approxEq(b.NE.Lng, p.Lng) ||
		!approxEq(b.SW.Lat, p.Lat) || !approxEq(b.SW.Lng, p.Lng) {
		t.Errorf("BoundsOf(p) = %+v, want degenerate at %+v", b, p)
	}
}

func TestBounds_Center(t *testing.T) {
	b := Bounds{
		NE: LatLng{Lat: 10, Lng: 20},
		SW: LatLng{Lat: -2, Lng: 6},
	}
	c := b.Center()
	if c.Lat != 4 || c.Lng != 13 {
		t.Errorf("Center = %+v, want {4, 13}", c)
	}
}

// Extend must preserve existing corners when the new point lies
// strictly inside the box — a regression swapping < for <= could
// shrink the box silently.
func TestBounds_Extend_PropagatesExistingCorners(t *testing.T) {
	b := Bounds{
		NE: LatLng{Lat: 10, Lng: 20},
		SW: LatLng{Lat: -10, Lng: -20},
	}
	got := b.Extend(LatLng{Lat: 1, Lng: 2})
	if got != b {
		t.Errorf("Extend on inside-point changed b: got %+v, want %+v", got, b)
	}
}

// Latitudes outside the Web Mercator range must be pinned to the
// representable extreme, not silently passed through.
func TestClamp_LatAboveMercatorClamps(t *testing.T) {
	const want = 85.05112878
	for _, in := range []float64{86, 90, 90.0001, 1e6} {
		got := LatLng{Lat: in}.Clamp().Lat
		if !approxEq(got, want) {
			t.Errorf("Lat=%g clamped to %v, want %v", in, got, want)
		}
	}
	for _, in := range []float64{-86, -90, -90.0001, -1e6} {
		got := LatLng{Lat: in}.Clamp().Lat
		if !approxEq(got, -want) {
			t.Errorf("Lat=%g clamped to %v, want %v", in, got, -want)
		}
	}
}
