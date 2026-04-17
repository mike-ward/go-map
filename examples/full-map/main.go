// Full-map demo: map fills the window with a quick-jump toolbar
// above. Demonstrates the full-window mapplication pattern plus
// programmatic SetView from outside the widget.
package main

import (
	"github.com/mike-ward/go-gui/gui"
	"github.com/mike-ward/go-gui/gui/backend"
	"github.com/mike-ward/go-map/mapview"
	"github.com/mike-ward/go-map/projection"
	"github.com/mike-ward/go-map/tile"
)

const mapID = "full-map"

var src = tile.OSMWithUserAgent(
	"go-map-fullmap-example/0 (https://github.com/mike-ward/go-map)",
)

type city struct {
	Name   string
	Center projection.LatLng
	Zoom   uint32
}

var cities = []city{
	{"Seattle", projection.LatLng{Lat: 47.6062, Lng: -122.3321}, 11},
	{"London", projection.LatLng{Lat: 51.5074, Lng: -0.1278}, 11},
	{"Tokyo", projection.LatLng{Lat: 35.6762, Lng: 139.6503}, 11},
	{"Sydney", projection.LatLng{Lat: -33.8688, Lng: 151.2093}, 11},
}

func main() {
	gui.SetTheme(gui.ThemeDarkBordered)
	cfg := gui.WindowCfg{
		Title:  "go-map — full",
		Width:  1024,
		Height: 720,
		OnInit: func(w *gui.Window) { w.UpdateView(view) },
	}
	if f, ok := src.(tile.HTTPFetcher); ok {
		cfg.ImageFetcher = f.HTTPFetcher()
	}
	backend.Run(gui.NewWindow(cfg))
}

func view(w *gui.Window) gui.View {
	ww, wh := w.WindowSize()
	return gui.Column(gui.ContainerCfg{
		Width:   float32(ww),
		Height:  float32(wh),
		Sizing:  gui.FixedFixed,
		Padding: gui.Some(gui.Padding{}),
		Content: []gui.View{
			toolbar(),
			mapview.Map(mapview.Cfg{
				ID:            mapID,
				IDFocus:       1,
				Sizing:        gui.FillFill,
				InitialCenter: cities[0].Center,
				InitialZoom:   cities[0].Zoom,
				Source:        src,
				A11YLabel:     "Interactive world map",
				InitialOverlays: []mapview.Overlay{
					&mapview.Marker{
						MarkerID: "seattle",
						Pos:      cities[0].Center,
						Label:    "Seattle",
						Title:    "Seattle, WA",
						Body:     "Pacific Northwest coffee capital.",
						Actions: []mapview.InfoWindowAction{
							{Label: "Zoom in", OnClick: func(w *gui.Window) {
								mapview.SetView(w, mapID, cities[0].Center, 14)
							}},
							{Label: "Reset", OnClick: func(w *gui.Window) {
								mapview.SetView(w, mapID, cities[0].Center, cities[0].Zoom)
							}},
						},
					},
					&mapview.Marker{
						MarkerID: "london",
						Pos:      cities[1].Center,
						Label:    "London",
						Title:    "London, UK",
						Body:     "Capital of the United Kingdom.",
					},
					&mapview.Marker{
						MarkerID: "tokyo",
						Pos:      cities[2].Center,
						Label:    "Tokyo",
						Title:    "Tokyo, Japan",
						Body:     "Largest metropolitan area on Earth.",
					},
					&mapview.Marker{
						MarkerID: "sydney",
						Pos:      cities[3].Center,
						Label:    "Sydney",
						Title:    "Sydney, Australia",
						Body:     "Harbor city on the Tasman Sea.",
					},
				},
				OnPOISelect: func(w *gui.Window, o mapview.Overlay) {
					if m, ok := o.(*mapview.Marker); ok {
						mapview.PanTo(w, mapID, m.Pos)
					}
				},
			}),
		},
	})
}

func toolbar() gui.View {
	buttons := make([]gui.View, 0, len(cities))
	for _, c := range cities {
		c := c
		buttons = append(buttons, gui.Button(gui.ButtonCfg{
			Padding: gui.Some(gui.Padding{Left: 10, Right: 10, Top: 4, Bottom: 4}),
			Content: []gui.View{gui.Text(gui.TextCfg{Text: c.Name})},
			OnClick: func(_ *gui.Layout, _ *gui.Event, w *gui.Window) {
				mapview.SetView(w, mapID, c.Center, c.Zoom)
			},
		}))
	}
	return gui.Row(gui.ContainerCfg{
		Sizing:  gui.FillFixed,
		Height:  36,
		Padding: gui.Some(gui.Padding{Left: 6, Right: 6, Top: 4, Bottom: 4}),
		Spacing: gui.Some[float32](6),
		Content: buttons,
	})
}
