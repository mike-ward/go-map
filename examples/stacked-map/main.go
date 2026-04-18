// Stacked-map demo: OSM base layer plus a terrestris WMS reference
// layer, each with its own source-specific HTTP User-Agent via the
// per-call fetcher landed in go-gui v0.12.4. A sidebar Legend toggles
// the reference on and off so the effect is visually obvious.
//
// Because go-gui does not yet modulate image textures by alpha, a
// visible reference layer with an opaque body (terrestris OSM-gray)
// fully covers the base. That is the expected v0.3 behavior. Use the
// Legend to peek at the colored base underneath.
package main

import (
	"github.com/mike-ward/go-gui/gui"
	"github.com/mike-ward/go-gui/gui/backend"
	"github.com/mike-ward/go-map/mapview"
	"github.com/mike-ward/go-map/projection"
	"github.com/mike-ward/go-map/tile"
	"github.com/mike-ward/go-map/tile/wms"
)

const (
	mapID    = "stacked"
	legendID = "stacked-legend"
	baseID   = "osm"
	refID    = "terrestris-gray"
	sidebarW = 260
	initZoom = 10.0
)

// London — broad WMS and OSM coverage; zoom 10 fits within terrestris's
// published max. Paris / Berlin / New York work equally well.
var initCenter = projection.LatLng{Lat: 51.5074, Lng: -0.1278}

func main() {
	osmSrc := tile.OSMWithUserAgent(
		"go-map stacked-demo/0 osm " +
			"(https://github.com/mike-ward/go-map)")

	// Terrestris publishes a grayscale OSM render at
	// ows.terrestris.de/osm-gray; no auth, EPSG:3857, PNG output.
	// Demo UA identifies go-map so terrestris can rate-limit or
	// contact the author under their fair-use policy.
	refSrc, err := wms.New(wms.Cfg{
		Endpoint: "https://ows.terrestris.de/osm-gray/service",
		Layers:   []string{"OSM-WMS"},
		Attribution: "© terrestris (OSM data) / " +
			"© OpenStreetMap contributors",
		MaxZoom: 18,
		UserAgent: "go-map stacked-demo/0 terrestris " +
			"(https://github.com/mike-ward/go-map)",
	})
	if err != nil {
		panic(err)
	}

	gui.SetTheme(gui.ThemeDarkBordered)
	cfg := gui.WindowCfg{
		Title:  "go-map — stacked layers (OSM + terrestris WMS)",
		Width:  1200,
		Height: 720,
		OnInit: func(w *gui.Window) {
			w.UpdateView(viewWith(osmSrc, refSrc))
		},
	}
	// A window-level fallback fetcher is not needed here: both layers
	// implement tile.HTTPFetcher, so mapview.drawLayerTiles threads
	// each source's own fetcher into the per-call draw path.
	backend.Run(gui.NewWindow(cfg))
}

// viewWith returns a view generator closed over the two tile sources.
// Kept out of main() so the demo file reads top-to-bottom without a
// package-level source variable.
func viewWith(osmSrc tile.Source, refSrc tile.Source) func(*gui.Window) gui.View {
	layers := []mapview.Layer{
		{
			LayerID: baseID,
			Name:    "OSM (color)",
			Source:  osmSrc,
			Kind:    mapview.LayerKindBase,
			Visible: true,
		},
		{
			LayerID: refID,
			Name:    "Terrestris (gray)",
			Source:  refSrc,
			Kind:    mapview.LayerKindReference,
			Visible: true,
		},
	}
	return func(w *gui.Window) gui.View {
		ww, wh := w.WindowSize()
		mapW := float32(ww) - float32(sidebarW)
		return gui.Row(gui.ContainerCfg{
			Width:   float32(ww),
			Height:  float32(wh),
			Sizing:  gui.FixedFixed,
			Padding: gui.Some(gui.Padding{}),
			Content: []gui.View{
				mapview.Map(mapview.Cfg{
					ID:            mapID,
					IDFocus:       1,
					Sizing:        gui.FixedFill,
					Width:         mapW,
					InitialCenter: initCenter,
					InitialZoom:   initZoom,
					InitialLayers: layers,
					A11YLabel:     "Stacked-layer map of London",
				}),
				sidebar(),
			},
		})
	}
}

func sidebar() gui.View {
	return gui.Column(gui.ContainerCfg{
		Sizing:  gui.FixedFill,
		Width:   float32(sidebarW),
		Padding: gui.Some(gui.Padding{Left: 8, Right: 8, Top: 8, Bottom: 8}),
		Spacing: gui.Some[float32](8),
		Content: []gui.View{
			mapview.Legend(mapview.LegendCfg{
				ID:          legendID,
				MapID:       mapID,
				Title:       "Layers",
				Sizing:      gui.FillFit,
				IDFocusBase: 2,
			}),
			gui.Text(gui.TextCfg{
				Mode: gui.TextModeWrap,
				Text: "Click a legend row to toggle that layer. " +
					"OSM tiles carry one User-Agent; terrestris WMS " +
					"tiles carry another — both travel with their " +
					"own fetcher via go-gui v0.12.4 per-call override.",
			}),
		},
	})
}
