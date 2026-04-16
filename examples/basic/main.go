// Basic go-map demo: display an interactive OSM map centered on Seattle.
package main

import (
	"github.com/mike-ward/go-gui/gui"
	"github.com/mike-ward/go-gui/gui/backend"
	"github.com/mike-ward/go-map/mapview"
	"github.com/mike-ward/go-map/projection"
	"github.com/mike-ward/go-map/tile"
)

// src is shared between the mapview Source and the window's
// ImageFetcher so tile downloads on the render path carry the same
// OSM-policy-compliant User-Agent as Fetch() would.
var src = tile.OSMWithUserAgent(
	"go-map-example/0 (https://github.com/mike-ward/go-map)",
)

func main() {
	gui.SetTheme(gui.ThemeDarkBordered)
	cfg := gui.WindowCfg{
		Title:  "go-map",
		Width:  900,
		Height: 650,
		OnInit: func(w *gui.Window) {
			w.UpdateView(view)
		},
	}
	if f, ok := src.(tile.HTTPFetcher); ok {
		cfg.ImageFetcher = f.HTTPFetcher()
	}
	w := gui.NewWindow(cfg)
	backend.Run(w)
}

func view(w *gui.Window) gui.View {
	// Root layouts need an explicit size; FillFill has no parent to
	// inherit from. Wrap in a FixedFixed Column sized to the window
	// and let the map fill inside.
	ww, wh := w.WindowSize()
	return gui.Column(gui.ContainerCfg{
		Width:   float32(ww),
		Height:  float32(wh),
		Sizing:  gui.FixedFixed,
		Padding: gui.Some(gui.Padding{}),
		Content: []gui.View{
			mapview.Map(mapview.Cfg{
				ID:            "map",
				IDFocus:       1,
				Sizing:        gui.FillFill,
				InitialCenter: projection.LatLng{Lat: 47.6062, Lng: -122.3321},
				InitialZoom:   11,
				Source:        src,
				A11YLabel:     "Seattle street map",
			}),
		},
	})
}
