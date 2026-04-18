// Reference-map demo: a detail map alongside a locator (overview)
// map. The locator draws a rectangle showing the detail map's
// visible extent; clicking the locator recenters the detail map on
// the clicked point. The locator is independently pannable so the
// user can scout context around the detail viewport.
// Demonstrates mapview.Overview — the widget that packages the
// viewport-sync + recenter-on-click logic into a single call.
package main

import (
	"github.com/mike-ward/go-gui/gui"
	"github.com/mike-ward/go-gui/gui/backend"
	"github.com/mike-ward/go-map/mapview"
	"github.com/mike-ward/go-map/projection"
	"github.com/mike-ward/go-map/tile"
)

const (
	detailID   = "ref-detail"
	overviewID = "ref-overview"
	sidebarW   = 300
	overviewH  = 240
	// Overview stays this many zoom levels wider than the detail map
	// so it reads as a locator, not a duplicate.
	overviewZoomDelta = 4
)

var src = tile.OSMWithUserAgent(
	"go-map-reference-example/0 (https://github.com/mike-ward/go-map)",
)

// Times Square — a dense, recognizable starting point.
var (
	initCenter = projection.LatLng{Lat: 40.7580, Lng: -73.9855}
	initZoom   = 13.0
)

func main() {
	gui.SetTheme(gui.ThemeDarkBordered)
	cfg := gui.WindowCfg{
		Title:  "go-map — reference",
		Width:  1200,
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

	return gui.Row(gui.ContainerCfg{
		Width:   float32(ww),
		Height:  float32(wh),
		Sizing:  gui.FixedFixed,
		Padding: gui.Some(gui.Padding{}),
		Content: []gui.View{
			mapview.Map(mapview.Cfg{
				ID:            detailID,
				IDFocus:       1,
				Sizing:        gui.FillFill,
				InitialCenter: initCenter,
				InitialZoom:   initZoom,
				Source:        src,
				A11YLabel:     "Detail map of New York City",
			}),
			sidebar(),
		},
	})
}

func sidebar() gui.View {
	return gui.Column(gui.ContainerCfg{
		Sizing:  gui.FixedFill,
		Width:   float32(sidebarW),
		Padding: gui.Some(gui.Padding{Left: 8, Right: 8, Top: 8, Bottom: 8}),
		Spacing: gui.Some[float32](8),
		Content: []gui.View{
			gui.Text(gui.TextCfg{Text: "Locator", Hero: true}),
			mapview.Overview(mapview.OverviewCfg{
				ID:            overviewID,
				MapID:         detailID,
				IDFocus:       2,
				Sizing:        gui.FillFixed,
				Height:        float32(overviewH),
				InitialCenter: initCenter,
				InitialZoom:   initZoom - overviewZoomDelta,
				Source:        src,
				A11YLabel:     "Overview locator map",
			}),
			gui.Text(gui.TextCfg{
				Mode: gui.TextModeWrap,
				Text: "Click locator to recenter the detail view. " +
					"Drag the locator to scout context; drag or zoom " +
					"the detail map to watch the rectangle follow.",
			}),
		},
	})
}
