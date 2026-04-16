// Basic go-map demo: display an interactive OSM map centered on Seattle.
package main

import (
	"github.com/mike-ward/go-gui/gui"
	"github.com/mike-ward/go-gui/gui/backend"
	"github.com/mike-ward/go-map/mapview"
	"github.com/mike-ward/go-map/projection"
	"github.com/mike-ward/go-map/tile"
)

func main() {
	gui.SetTheme(gui.ThemeDarkBordered)
	w := gui.NewWindow(gui.WindowCfg{
		Title:  "go-map",
		Width:  900,
		Height: 650,
		OnInit: func(w *gui.Window) {
			w.UpdateView(view)
		},
	})
	backend.Run(w)
}

func view(_ *gui.Window) gui.View {
	return mapview.Widget(mapview.Cfg{
		ID:     "map",
		Sizing: gui.FillFill,
		Center: projection.LatLng{Lat: 47.6062, Lng: -122.3321},
		Zoom:   11,
		Source: tile.OSM(),
	})
}
