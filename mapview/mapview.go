// Package mapview provides the interactive slippy-tile map widget.
//
// The widget is a go-gui View built on DrawCanvas. It owns pan/zoom
// state, fetches tiles asynchronously through a tile.Source, and
// renders overlays (markers, polylines, attribution) via the draw
// context.
package mapview

import (
	"github.com/mike-ward/go-gui/gui"
	"github.com/mike-ward/go-map/projection"
	"github.com/mike-ward/go-map/tile"
)

// Cfg configures a Widget. Zero value is usable but centered at
// (0,0) with zoom 2 over a placeholder source.
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

	// Viewport
	Center projection.LatLng
	Zoom   uint32

	// Data
	Source tile.Source

	// Appearance
	Background gui.Color

	// Events
	OnViewportChange func(center projection.LatLng, zoom uint32)
}

// Widget returns a map View. The returned View is safe to place
// anywhere a gui.View is expected.
func Widget(cfg Cfg) gui.View {
	if cfg.Sizing == (gui.Sizing{}) {
		cfg.Sizing = gui.FillFill
	}
	if cfg.Zoom == 0 {
		cfg.Zoom = 2
	}
	if !cfg.Background.IsSet() {
		cfg.Background = gui.Hex(0xE8E6E0)
	}
	return gui.DrawCanvas(gui.DrawCanvasCfg{
		ID:        cfg.ID,
		Sizing:    cfg.Sizing,
		Width:     cfg.Width,
		Height:    cfg.Height,
		MinWidth:  cfg.MinWidth,
		MaxWidth:  cfg.MaxWidth,
		MinHeight: cfg.MinHeight,
		MaxHeight: cfg.MaxHeight,
		Color:     cfg.Background,
		OnDraw:    drawer(cfg),
	})
}

// drawer returns the DrawCanvas OnDraw callback. Rendering is a
// placeholder: background plus attribution text. Tile composition,
// pan/zoom input, and overlay drawing land in follow-up work.
func drawer(cfg Cfg) func(*gui.DrawContext) {
	attribution := ""
	if cfg.Source != nil {
		attribution = cfg.Source.Attribution()
	}
	return func(dc *gui.DrawContext) {
		if attribution == "" {
			return
		}
		// Attribution overlay — required by most tile providers.
		// Final placement will anchor to bottom-right of the canvas
		// once the widget owns its measured rect.
		dc.Text(4, 4, attribution, gui.TextStyle{})
	}
}
