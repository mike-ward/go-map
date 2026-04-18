package mapview

import (
	"strings"
	"testing"

	"github.com/mike-ward/go-gui/gui"
	"github.com/mike-ward/go-map/projection"
)

// Overview must reject an empty MapID; factory is the only place the
// check lives so a later refactor of overviewView cannot skip it.
func TestOverview_PanicsOnEmptyMapID(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Overview did not panic on empty MapID")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "MapID") {
			t.Errorf("panic = %v, want message mentioning MapID", r)
		}
	}()
	Overview(OverviewCfg{ID: "over"})
}

func TestOverview_PanicsOnEmptyID(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Overview did not panic on empty ID")
		}
	}()
	Overview(OverviewCfg{MapID: "target"})
}

// syncOverviewRect with no target snapshot must be a no-op — the
// target has not rendered yet and there is nothing to mirror.
func TestSyncOverviewRect_NoSnapshotIsNoop(t *testing.T) {
	w := &gui.Window{}
	syncOverviewRect(w, OverviewCfg{ID: "over", MapID: "target"})
	bm := readOverlays(w, "over")
	if bm.Len() != 0 {
		t.Errorf("overlays = %d, want 0 without a target snapshot", bm.Len())
	}
}

// syncOverviewRect must also skip writing the rectangle when the
// target has a snapshot but no canvas size yet (target rendered
// before nsCanvas was populated — impossible in practice, but the
// guard prevents a degenerate 0×0 rectangle slipping through).
func TestSyncOverviewRect_NoCanvasIsNoop(t *testing.T) {
	w := &gui.Window{}
	nsWrite(w, nsState, "target", MapState{
		Center: projection.LatLng{Lat: 47.6, Lng: -122.3},
		Zoom:   10,
	})
	syncOverviewRect(w, OverviewCfg{ID: "over", MapID: "target"})
	bm := readOverlays(w, "over")
	if bm.Len() != 0 {
		t.Errorf("overlays = %d, want 0 without canvas size", bm.Len())
	}
}

// syncOverviewRect must write a 4-corner Polygon ring matching the
// target's unprojected viewport bounds. The ring winds NW → NE → SE →
// SW per the reference-map demo convention; downstream draw code
// relies on that orientation.
func TestSyncOverviewRect_WritesPolygon(t *testing.T) {
	w := &gui.Window{}
	nsWrite(w, nsState, "target", MapState{
		Center: projection.LatLng{Lat: 0, Lng: 0},
		Zoom:   2,
	})
	nsWrite(w, nsCanvas, "target", canvasSize{W: 512, H: 256})

	syncOverviewRect(w, OverviewCfg{ID: "over", MapID: "target"})

	bm := readOverlays(w, "over")
	o, ok := bm.Get(defaultOverviewRectID)
	if !ok {
		t.Fatal("no overlay written for default rect id")
	}
	poly, ok := o.(*Polygon)
	if !ok {
		t.Fatalf("overlay = %T, want *Polygon", o)
	}
	if got := len(poly.Ring); got != 4 {
		t.Fatalf("ring len = %d, want 4", got)
	}
	// At Z=2 and a 512×256 canvas centered at (0,0), the viewport
	// spans ±128 world-px horizontally and ±64 vertically out of the
	// 1024 px world. Corners should be outside (0,0) in all four
	// quadrants — a loose check that survives projection round-trip
	// error without hard-coding transcendental constants.
	ne := poly.Ring[1] // index 1 = NE per ring order
	sw := poly.Ring[3] // index 3 = SW
	if ne.Lat <= 0 || ne.Lng <= 0 || sw.Lat >= 0 || sw.Lng >= 0 {
		t.Errorf("ring NE=%v SW=%v not in expected quadrants", ne, sw)
	}
}

// A target viewport that wraps the antimeridian (or spans >= 360°)
// cannot be represented as a single 4-corner ring. syncOverviewRect
// must remove any stale rectangle in that frame rather than paint
// nonsense.
func TestSyncOverviewRect_OverspanClearsRect(t *testing.T) {
	w := &gui.Window{}
	// Seed an existing rectangle so we can observe the removal.
	AddOverlay(w, "over", &Polygon{
		PolyID: defaultOverviewRectID,
		Ring: []projection.LatLng{
			{Lat: 1, Lng: -1}, {Lat: 1, Lng: 1},
			{Lat: -1, Lng: 1}, {Lat: -1, Lng: -1},
		},
	})

	// Zoom 0 renders the whole world inside one tile. A 2048 px canvas
	// at z=0 covers 8 worlds — the unprojected viewport wraps.
	nsWrite(w, nsState, "target", MapState{
		Center: projection.LatLng{Lat: 0, Lng: 0},
		Zoom:   0,
	})
	nsWrite(w, nsCanvas, "target", canvasSize{W: 2048, H: 2048})

	syncOverviewRect(w, OverviewCfg{ID: "over", MapID: "target"})

	bm := readOverlays(w, "over")
	if bm.Contains(defaultOverviewRectID) {
		t.Error("overspan viewport left a rectangle in place; want removed")
	}
}

// Defaults apply when the consumer leaves styling fields zero. Locks
// the documented fill/stroke/width contract so a refactor noticing
// "empty color" cannot silently change the demo's appearance.
func TestOverview_AppliesStylingDefaults(t *testing.T) {
	w := &gui.Window{}
	nsWrite(w, nsState, "target", MapState{
		Center: projection.LatLng{Lat: 47.6, Lng: -122.3},
		Zoom:   10,
	})
	nsWrite(w, nsCanvas, "target", canvasSize{W: 800, H: 600})

	syncOverviewRect(w, OverviewCfg{ID: "over", MapID: "target"})

	o, _ := readOverlays(w, "over").Get(defaultOverviewRectID)
	poly := o.(*Polygon)
	if poly.FillColor != defaultOverviewRectFill {
		t.Errorf("fill = %v, want default", poly.FillColor)
	}
	if poly.StrokeColor != defaultOverviewRectStroke {
		t.Errorf("stroke = %v, want default", poly.StrokeColor)
	}
	if poly.StrokeWidth != defaultOverviewRectStrokeWidth {
		t.Errorf("stroke width = %v, want %v",
			poly.StrokeWidth, defaultOverviewRectStrokeWidth)
	}
}

// Consumer-supplied styling must pass through verbatim.
func TestOverview_CustomStylingPassesThrough(t *testing.T) {
	w := &gui.Window{}
	nsWrite(w, nsState, "target", MapState{
		Center: projection.LatLng{Lat: 0, Lng: 0},
		Zoom:   5,
	})
	nsWrite(w, nsCanvas, "target", canvasSize{W: 400, H: 300})

	wantFill := gui.Color{R: 10, G: 20, B: 30, A: 40}
	wantStroke := gui.Color{R: 50, G: 60, B: 70, A: 255}
	c := OverviewCfg{
		ID:              "over",
		MapID:           "target",
		RectID:          "custom-rect",
		RectFill:        wantFill,
		RectStroke:      wantStroke,
		RectStrokeWidth: 4,
	}
	syncOverviewRect(w, c)

	o, ok := readOverlays(w, "over").Get("custom-rect")
	if !ok {
		t.Fatal("custom-rect overlay not written")
	}
	poly := o.(*Polygon)
	if poly.FillColor != wantFill ||
		poly.StrokeColor != wantStroke ||
		poly.StrokeWidth != 4 {
		t.Errorf("styling not passed through: got fill=%v stroke=%v w=%v",
			poly.FillColor, poly.StrokeColor, poly.StrokeWidth)
	}
}

// Full layout pipeline must produce a layout whose Shape carries the
// overview's own ID — proof that Overview delegated to an inner Map
// rather than returning a bare empty Layout. Inner mapview.Map is a
// DrawCanvas leaf (no child views), so a Children-count check is not
// meaningful the way it is for Legend; the Shape ID is the honest
// signal that the inner widget wired through.
func TestOverview_GenerateLayoutBuildsInnerMap(t *testing.T) {
	w := &gui.Window{}
	v := Overview(OverviewCfg{
		ID:            "over",
		MapID:         "target",
		InitialCenter: projection.LatLng{Lat: 47.6, Lng: -122.3},
		InitialZoom:   6,
	})
	root := gui.GenerateViewLayout(v, w)
	if root.Shape == nil {
		t.Fatal("root Shape is nil; inner Map did not generate")
	}
	if root.Shape.ID != "over" {
		t.Errorf("shape ID = %q, want \"over\"", root.Shape.ID)
	}
}
