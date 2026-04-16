// Package mapview provides the interactive slippy-tile map widget.
//
// The widget is a go-gui View built on DrawCanvas. It owns pan/zoom
// state in the Window state registry (namespace "mapview.state",
// keyed by Cfg.ID), fetches tiles asynchronously through a
// tile.Source, and renders overlays (markers, polylines, attribution)
// via the draw context.
//
// Immediate-mode convention: the Widget factory re-runs every frame.
// Initial* fields on Cfg seed the registry on the first frame only;
// subsequent frames read the persistent state. Consumers mutate state
// through package-level helpers (PanTo, SetZoom, SetView, Snapshot).
package mapview

import (
	"github.com/mike-ward/go-gui/gui"
	"github.com/mike-ward/go-map/projection"
	"github.com/mike-ward/go-map/tile"
)

// Cfg configures a Widget. ID is required.
type Cfg struct {
	// Identity
	ID string

	// Sizing
	Sizing    gui.Sizing
	Width     float32
	Height    float32
	MinWidth  float32
	MaxWidth  float32
	MinHeight float32
	MaxHeight float32

	// Focus (tab-order index; zero means not focusable)
	IDFocus uint32

	// Initial viewport (seeds first-frame state only; ignored after)
	InitialCenter projection.LatLng
	InitialZoom   uint32

	// Data
	Source tile.Source

	// Appearance
	Background gui.Color

	// Accessibility
	A11YLabel       string
	A11YDescription string

	// Events. Callbacks run on the UI goroutine; do not block.
	OnViewportChange func(*gui.Window, MapState)
}

// Map returns a map View. Cfg.ID must be non-empty; it is the key
// for all per-map state in the Window registry.
func Map(cfg Cfg) gui.View {
	if cfg.ID == "" {
		panic("mapview: Cfg.ID is required")
	}
	if cfg.Sizing == (gui.Sizing{}) {
		cfg.Sizing = gui.FillFill
	}
	if !cfg.Background.IsSet() {
		cfg.Background = gui.Hex(0xE8E6E0)
	}
	if cfg.InitialZoom == 0 {
		cfg.InitialZoom = 2
	}
	return &mapView{cfg: cfg}
}

// mapView is the custom View implementation. It re-reads persistent
// state from the Window registry each frame (GenerateLayout runs
// once per frame) and captures the snapshot into the DrawCanvas
// OnDraw closure. Version bumps per frame to defeat the DrawCanvas
// cache while pan/zoom are state-driven rather than version-driven.
type mapView struct {
	cfg Cfg
}

func (*mapView) Content() []gui.View { return nil }

func (mv *mapView) GenerateLayout(w *gui.Window) gui.Layout {
	c := mv.cfg
	seed := MapState{
		Center: c.InitialCenter.Clamp(),
		Zoom:   c.InitialZoom,
	}
	s := readState(w, c.ID, seed)

	if c.OnViewportChange != nil {
		c.OnViewportChange(w, s)
	}

	// Capture state by value into the OnDraw closure. Reads happen
	// here (on the UI goroutine) so the draw pass never touches the
	// registry — keeping OnDraw allocation-free.
	src := c.Source
	hover := readHover(w, c.ID)
	onDraw := func(dc *gui.DrawContext) {
		vp := computeViewport(dc, s)
		drawTiles(dc, vp, src)
		drawCoordReadout(dc, vp, s, hover)
		drawZoomIndicator(dc, s.Zoom)
		drawAttribution(dc, src)
	}

	a11y := c.A11YDescription
	if a11y == "" {
		a11y = stateForA11Y(s)
	}

	inner := gui.DrawCanvas(gui.DrawCanvasCfg{
		ID:              c.ID,
		A11YLabel:       c.A11YLabel,
		A11YDescription: a11y,
		Version:         w.FrameCount(),
		Sizing:          c.Sizing,
		Width:           c.Width,
		Height:          c.Height,
		MinWidth:        c.MinWidth,
		MaxWidth:        c.MaxWidth,
		MinHeight:       c.MinHeight,
		MaxHeight:       c.MaxHeight,
		IDFocus:         c.IDFocus,
		Color:           c.Background,
		Clip:            true,
		OnDraw:          onDraw,
		OnClick:         onClick(c.ID, c.Source),
		OnMouseScroll:   onMouseScroll(c.ID, c.Source),
		OnMouseMove:     onMouseMove(c.ID),
		OnMouseLeave:    onMouseLeave(c.ID),
		OnKeyDown:       onKeyDown(c.ID, c.Source, seed),
	})
	return inner.GenerateLayout(w)
}
