package mapview

import (
	"github.com/mike-ward/go-gui/gui"
	"github.com/mike-ward/go-map/projection"
	"github.com/mike-ward/go-map/tile"
)

// OverviewCfg configures a locator-style inset map that mirrors the
// viewport of another map identified by MapID.
//
// On every frame the widget reads the target's latest Snapshot and
// CanvasSize, projects its four viewport corners, and writes the
// result as a Polygon overlay on this overview. Clicking the overview
// recenters the target via PanTo. The overview is independently
// pannable / zoomable so the user can scout context around the
// target's viewport — matching the established reference-map demo.
//
// Antimeridian-straddling or >=360° viewports are not representable
// as a single 4-corner ring; the rectangle is removed in that frame
// rather than painted as a nonsensical band.
type OverviewCfg struct {
	// Identity. ID is the overview's own map id (required for state
	// registry + overlay scope). MapID names the target map whose
	// viewport this overview mirrors (required).
	ID    string
	MapID string

	// Sizing mirrors mapview.Cfg. A locator is typically fixed in at
	// least one axis so it does not fight the primary map for space.
	Sizing    gui.Sizing
	Width     float32
	Height    float32
	MinWidth  float32
	MaxWidth  float32
	MinHeight float32
	MaxHeight float32

	IDFocus uint32

	// Initial viewport. Consumers typically pass the target's own
	// InitialCenter and (InitialZoom - 4) so the overview opens
	// wider than the target.
	InitialCenter projection.LatLng
	InitialZoom   float64

	// Source is the tile source for the overview's own tiles. Usually
	// the same as the target's Source; a different basemap (e.g. a
	// grayscale variant) is allowed so the locator reads as distinct
	// from the primary.
	Source tile.Source

	A11YLabel string

	// Viewport-rectangle styling. Zero RectID selects a default id
	// ("mapview.overview.viewport"); zero colors select a semi-
	// transparent yellow fill + solid yellow stroke + 2 px width.
	RectID          string
	RectFill        gui.Color
	RectStroke      gui.Color
	RectStrokeWidth float32
}

// Default styling for the viewport rectangle. Exported via the Cfg
// fields; duplicated here so the defaults are a single source of
// truth and tests can assert against them.
var (
	defaultOverviewRectFill   = gui.Color{R: 255, G: 212, B: 0, A: 48}
	defaultOverviewRectStroke = gui.Hex(0xFFD400)
)

const (
	defaultOverviewRectID          = "mapview.overview.viewport"
	defaultOverviewRectStrokeWidth = float32(2)
)

// overviewView is the custom View returned by Overview. Kept as a
// wrapper (rather than a Map(...) call at the factory site) so the
// viewport-rectangle sync runs each frame before the inner Map's
// GenerateLayout reads the overlay registry.
type overviewView struct {
	cfg OverviewCfg
}

// Overview returns a View that renders a locator map mirroring the
// target map's viewport. Panics if MapID is empty — an overview with
// no target is always a bug; the factory is the single place the
// check lives.
func Overview(cfg OverviewCfg) gui.View {
	if cfg.MapID == "" {
		panic("mapview: Overview Cfg.MapID is required")
	}
	if cfg.ID == "" {
		panic("mapview: Overview Cfg.ID is required")
	}
	return &overviewView{cfg: cfg}
}

func (*overviewView) Content() []gui.View { return nil }

// GenerateLayout syncs the viewport rectangle from the target's
// latest snapshot, then delegates the actual layout to an inner
// mapview.Map. Using gui.GenerateViewLayout on the inner Map picks up
// its (single-layer) child structure — mirrors the Legend fix.
func (ov *overviewView) GenerateLayout(w *gui.Window) gui.Layout {
	syncOverviewRect(w, ov.cfg)
	target := ov.cfg.MapID
	inner := Map(Cfg{
		ID:            ov.cfg.ID,
		Sizing:        ov.cfg.Sizing,
		Width:         ov.cfg.Width,
		Height:        ov.cfg.Height,
		MinWidth:      ov.cfg.MinWidth,
		MaxWidth:      ov.cfg.MaxWidth,
		MinHeight:     ov.cfg.MinHeight,
		MaxHeight:     ov.cfg.MaxHeight,
		IDFocus:       ov.cfg.IDFocus,
		InitialCenter: ov.cfg.InitialCenter,
		InitialZoom:   ov.cfg.InitialZoom,
		Source:        ov.cfg.Source,
		A11YLabel:     ov.cfg.A11YLabel,
		OnClick: func(ww *gui.Window, ll projection.LatLng) {
			PanTo(ww, target, ll)
		},
	})
	return gui.GenerateViewLayout(inner, w)
}

// syncOverviewRect writes the viewport-rectangle overlay onto the
// overview from the target's latest MapState + canvas size. Removes
// the overlay (rather than painting nonsense) when the target has
// not rendered, its canvas is degenerate, or the viewport straddles
// the antimeridian / spans the whole world.
//
// Split from GenerateLayout so tests drive the rectangle logic without
// standing up a full layout tree.
func syncOverviewRect(w *gui.Window, c OverviewCfg) {
	s, ok := Snapshot(w, c.MapID)
	if !ok {
		return
	}
	tw, th, ok := CanvasSize(w, c.MapID)
	if !ok {
		return
	}
	ctr := projection.ProjectF(s.Center, s.Zoom)
	hw := float64(tw) / 2
	hh := float64(th) / 2
	ne := projection.UnprojectF(
		projection.Point{X: ctr.X + hw, Y: ctr.Y - hh}, s.Zoom)
	sw := projection.UnprojectF(
		projection.Point{X: ctr.X - hw, Y: ctr.Y + hh}, s.Zoom)

	// A single 4-corner ring cannot represent an antimeridian-
	// wrapped or larger-than-world viewport honestly. Drop the
	// rectangle this frame (and clear any stale one) rather than
	// paint a nonsensical band across the locator.
	lngSpan := ne.Lng - sw.Lng
	if lngSpan <= 0 || lngSpan >= 360 {
		RemoveOverlay(w, c.ID, overviewRectID(c))
		return
	}

	AddOverlay(w, c.ID, &Polygon{
		PolyID: overviewRectID(c),
		Ring: []projection.LatLng{
			{Lat: ne.Lat, Lng: sw.Lng},
			{Lat: ne.Lat, Lng: ne.Lng},
			{Lat: sw.Lat, Lng: ne.Lng},
			{Lat: sw.Lat, Lng: sw.Lng},
		},
		FillColor:   overviewFill(c),
		StrokeColor: overviewStroke(c),
		StrokeWidth: overviewStrokeWidth(c),
	})
}

func overviewRectID(c OverviewCfg) string {
	if c.RectID != "" {
		return c.RectID
	}
	return defaultOverviewRectID
}

func overviewFill(c OverviewCfg) gui.Color {
	if c.RectFill != (gui.Color{}) {
		return c.RectFill
	}
	return defaultOverviewRectFill
}

func overviewStroke(c OverviewCfg) gui.Color {
	if c.RectStroke != (gui.Color{}) {
		return c.RectStroke
	}
	return defaultOverviewRectStroke
}

func overviewStrokeWidth(c OverviewCfg) float32 {
	if c.RectStrokeWidth > 0 {
		return c.RectStrokeWidth
	}
	return defaultOverviewRectStrokeWidth
}
