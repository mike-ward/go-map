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
	Zoom   float64
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
				// Opt into fractional-zoom-per-notch so a plain mouse
				// wheel (PreciseY=1.0 per click) advances the map by
				// 0.25 zoom levels instead of 1.0 — lets the demo
				// showcase slice 5b's sub-integer zoom without needing
				// trackpad hardware.
				ScrollZoomGain: 0.25,
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

// toolbarHeight is shared by the toolbar row and the FitBounds button
// so the canvas-height math stays in one place.
const toolbarHeight float32 = 36

func toolbar() gui.View {
	buttons := make([]gui.View, 0, len(cities)+1)
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
	// "Fit all" exercises the analytical FitBounds path from slice 5a:
	// the bounds around all four cities don't fit cleanly at any
	// integer zoom, so the map should land on a non-integer zoom
	// (HUD shows e.g. "z2.3") and tiles render at the fractional
	// scale without seams.
	buttons = append(buttons, gui.Button(gui.ButtonCfg{
		Padding: gui.Some(gui.Padding{Left: 10, Right: 10, Top: 4, Bottom: 4}),
		Content: []gui.View{gui.Text(gui.TextCfg{Text: "Fit all"})},
		OnClick: func(_ *gui.Layout, _ *gui.Event, w *gui.Window) {
			b := projection.BoundsOf(
				cities[0].Center, cities[1].Center,
				cities[2].Center, cities[3].Center,
			)
			ww, wh := w.WindowSize()
			mapview.FitBounds(w, mapID, b, 40,
				float32(ww), float32(wh)-toolbarHeight)
		},
	}))
	return gui.Row(gui.ContainerCfg{
		Sizing:  gui.FillFixed,
		Height:  toolbarHeight,
		Padding: gui.Some(gui.Padding{Left: 6, Right: 6, Top: 4, Bottom: 4}),
		Spacing: gui.Some[float32](6),
		Content: buttons,
	})
}
