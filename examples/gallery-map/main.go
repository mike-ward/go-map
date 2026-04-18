// Gallery-map demo: two base-layer candidates (OSM color + terrestris
// WMS grayscale) wired to a mapview.Gallery sidebar. Clicking a card
// promotes that layer to base and demotes the other; the gallery's
// selection ring tracks whichever is currently the base.
//
// One card carries a live thumbnail URL (the OSM z=0 tile — the whole
// world at ~256 px, cached by go-gui's Image view) so the gallery
// shows both thumbnail-driven and fallback-letter card styles side by
// side. The terrestris card omits a URL so the deterministic letter
// fallback covers it.
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
	mapID      = "gallery"
	galleryID  = "gallery-picker"
	osmLayerID = "osm-color"
	wmsLayerID = "terrestris-gray"
	sidebarW   = 260
	initZoom   = 10.0
)

// initCenter — London. Broad coverage on both providers at initZoom.
var initCenter = projection.LatLng{Lat: 51.5074, Lng: -0.1278}

func main() {
	osmSrc := tile.OSMWithUserAgent(
		"go-map gallery-demo/0 osm " +
			"(https://github.com/mike-ward/go-map)")
	waySrc, err := wms.New(wms.Cfg{
		Endpoint: "https://ows.terrestris.de/osm-gray/service",
		Layers:   []string{"OSM-WMS"},
		Attribution: "© terrestris (OSM data) / " +
			"© OpenStreetMap contributors",
		MaxZoom: 18,
		UserAgent: "go-map gallery-demo/0 terrestris " +
			"(https://github.com/mike-ward/go-map)",
	})
	if err != nil {
		panic(err)
	}

	gui.SetTheme(gui.ThemeDarkBordered)
	cfg := gui.WindowCfg{
		Title:  "go-map — gallery (base-layer picker)",
		Width:  1200,
		Height: 720,
		OnInit: func(w *gui.Window) { w.UpdateView(viewWith(osmSrc, waySrc)) },
	}
	backend.Run(gui.NewWindow(cfg))
}

// viewWith seeds both candidates on the map — OSM as the initial
// base, terrestris as a Visible:false Reference waiting to be
// promoted. The Gallery's selectGalleryLayer helper flips Visible to
// true on promotion so picking the hidden candidate renders on the
// next frame without extra wiring here.
func viewWith(osmSrc tile.Source, waySrc tile.Source) func(*gui.Window) gui.View {
	layers := []mapview.Layer{
		{
			LayerID: osmLayerID,
			Name:    "OSM (color)",
			Source:  osmSrc,
			Kind:    mapview.LayerKindBase,
			Visible: true,
		},
		{
			LayerID: wmsLayerID,
			Name:    "Terrestris (gray)",
			Source:  waySrc,
			Kind:    mapview.LayerKindReference,
			Visible: false,
		},
	}
	entries := []mapview.GalleryEntry{
		{
			LayerID:      osmLayerID,
			Label:        "OSM",
			ThumbnailURL: "https://tile.openstreetmap.org/0/0/0.png",
		},
		{
			// No ThumbnailURL — exercises the letter fallback.
			LayerID: wmsLayerID,
			Label:   "Gray",
		},
	}
	return func(w *gui.Window) gui.View {
		ww, wh := w.WindowSize()
		mapW := float32(ww) - float32(sidebarW) - gui.SpacingLarge
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
					A11YLabel:     "Gallery-driven base map of London",
				}),
				sidebar(entries),
			},
		})
	}
}

func sidebar(entries []mapview.GalleryEntry) gui.View {
	return gui.Column(gui.ContainerCfg{
		Sizing:  gui.FixedFill,
		Width:   float32(sidebarW),
		Padding: gui.Some(gui.Padding{Left: 8, Right: 8, Top: 8, Bottom: 8}),
		Spacing: gui.Some[float32](8),
		Content: []gui.View{
			mapview.Gallery(mapview.GalleryCfg{
				ID:          galleryID,
				MapID:       mapID,
				Title:       "Base map",
				Entries:     entries,
				Sizing:      gui.FillFit,
				IDFocusBase: 2,
			}),
			gui.Text(gui.TextCfg{
				Mode: gui.TextModeWrap,
				Text: "Click a card to pick that layer as the base. " +
					"The active card is ringed blue. The OSM card loads " +
					"a live z=0 thumbnail; the Gray card uses the " +
					"letter-fallback tile.",
			}),
		},
	})
}
