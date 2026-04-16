package mapview

import (
	"math"
	"testing"

	"github.com/mike-ward/go-map/projection"
)

// Seattle: a convenient non-trivial point that lands cleanly inside a
// single tile at zoom 11.
var seattle = projection.LatLng{Lat: 47.6062, Lng: -122.3321}

func TestComputeViewport_ZoomZero(t *testing.T) {
	// World at z=0 is 256×256 px. (0,0) LatLng maps to world-px
	// (128,128) (the center of the single tile). A 200×200 canvas
	// centered there should origin at (28,28) and see only tile 0.
	vp := computeViewport(200, 200, MapState{
		Center: projection.LatLng{Lat: 0, Lng: 0},
		Zoom:   0,
	})
	if vp.Z != 0 {
		t.Fatalf("Zoom = %d, want 0", vp.Z)
	}
	if math.Abs(float64(vp.OriginX-28)) > 1e-3 ||
		math.Abs(float64(vp.OriginY-28)) > 1e-3 {
		t.Fatalf("Origin = (%v,%v), want ~(28,28)", vp.OriginX, vp.OriginY)
	}
	if vp.MinTX != 0 || vp.MaxTX != 0 {
		t.Errorf("X range = [%d,%d], want [0,0]", vp.MinTX, vp.MaxTX)
	}
	if vp.MinTY != 0 || vp.MaxTY != 0 {
		t.Errorf("Y range = [%d,%d], want [0,0]", vp.MinTY, vp.MaxTY)
	}
}

func TestComputeViewport_Seattle(t *testing.T) {
	vp := computeViewport(900, 650, MapState{Center: seattle, Zoom: 11})
	// Center tile at zoom 11 for Seattle is (328, 715). Verify the
	// range includes it.
	if vp.MinTX > 328 || vp.MaxTX < 328 {
		t.Errorf("X range = [%d,%d] missing tile 328", vp.MinTX, vp.MaxTX)
	}
	if vp.MinTY > 715 || vp.MaxTY < 715 {
		t.Errorf("Y range = [%d,%d] missing tile 715", vp.MinTY, vp.MaxTY)
	}
	// 900×650 viewport at z11 spans roughly 4×3 tiles.
	xSpan := vp.MaxTX - vp.MinTX + 1
	ySpan := vp.MaxTY - vp.MinTY + 1
	if xSpan < 3 || xSpan > 5 {
		t.Errorf("X span %d outside expected [3,5]", xSpan)
	}
	if ySpan < 2 || ySpan > 4 {
		t.Errorf("Y span %d outside expected [2,4]", ySpan)
	}
}

func TestViewport_TileScreenPosRoundtrip(t *testing.T) {
	vp := computeViewport(900, 650, MapState{Center: seattle, Zoom: 11})
	// The screen position of the tile containing the center, plus the
	// center's intra-tile offset, must equal the canvas center.
	ts := float64(projection.TileSize)
	tx := int32(math.Floor(vp.CtrPx.X / ts))
	ty := int32(math.Floor(vp.CtrPx.Y / ts))
	sx, sy := vp.tileScreenPos(tx, ty)
	intraX := float32(vp.CtrPx.X - float64(tx)*ts)
	intraY := float32(vp.CtrPx.Y - float64(ty)*ts)
	gotX := sx + intraX
	gotY := sy + intraY
	// Tolerance reflects the float32 truncation inside OriginX/Y:
	// world-px values ~1e5 at z=11 lose <0.01 px of precision when
	// converted to float32.
	if math.Abs(float64(gotX-vp.W/2)) > 0.05 {
		t.Errorf("center X = %v, want %v", gotX, vp.W/2)
	}
	if math.Abs(float64(gotY-vp.H/2)) > 0.05 {
		t.Errorf("center Y = %v, want %v", gotY, vp.H/2)
	}
}

func TestViewport_ScreenToLatLngCenter(t *testing.T) {
	vp := computeViewport(900, 650, MapState{Center: seattle, Zoom: 11})
	got := vp.screenToLatLng(vp.W/2, vp.H/2)
	// Origin is stored float32; tolerance is the ~0.01 sub-pixel
	// drift scaled through Unproject at z=11 (~500k world px).
	const tol = 1e-4
	if math.Abs(got.Lat-seattle.Lat) > tol {
		t.Errorf("Lat = %v, want %v (tol %g)", got.Lat, seattle.Lat, tol)
	}
	if math.Abs(got.Lng-seattle.Lng) > tol {
		t.Errorf("Lng = %v, want %v (tol %g)", got.Lng, seattle.Lng, tol)
	}
}

func TestWrapTileX(t *testing.T) {
	cases := []struct {
		tx, maxN int32
		want     uint32
	}{
		// zoom 0: single world tile, everything wraps to 0
		{0, 1, 0},
		{-1, 1, 0},
		{7, 1, 0},
		// zoom 2: maxN = 4
		{0, 4, 0},
		{3, 4, 3},
		{4, 4, 0},  // off-by-one at the seam
		{-1, 4, 3}, // dateline-straddle west side
		{-4, 4, 0}, // full world west
		{-5, 4, 3},
		{8, 4, 0},
		{11, 4, 3},
		// zoom 11: maxN = 2048
		{-1, 2048, 2047},
		{2048, 2048, 0},
		{-2049, 2048, 2047},
	}
	for _, c := range cases {
		got := wrapTileX(c.tx, c.maxN)
		if got != c.want {
			t.Errorf("wrapTileX(%d, %d) = %d, want %d",
				c.tx, c.maxN, got, c.want)
		}
	}
}

func TestViewport_AntimeridianRange(t *testing.T) {
	// Center near +180°; viewport overlaps the seam. MinTX should go
	// negative (or >= maxN), forcing wrapTileX to produce tiles from
	// the opposite side of the world.
	vp := computeViewport(800, 600, MapState{
		Center: projection.LatLng{Lat: 0, Lng: 179.9},
		Zoom:   2,
	})
	maxN := int32(1) << vp.Z // 4
	seenWraps := false
	for tx := vp.MinTX; tx <= vp.MaxTX; tx++ {
		if tx < 0 || tx >= maxN {
			seenWraps = true
			wrapped := wrapTileX(tx, maxN)
			if wrapped >= uint32(maxN) {
				t.Errorf("wrapTileX(%d,%d)=%d out of range", tx, maxN, wrapped)
			}
		}
	}
	if !seenWraps {
		t.Fatalf("expected dateline-straddling range, got [%d,%d]",
			vp.MinTX, vp.MaxTX)
	}
}
