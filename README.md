# go-map

[![Go 1.26+](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue)](LICENSE)

Interactive slippy-tile map widget for
[go-gui](https://github.com/mike-ward/go-gui). Raster tiles, pan/zoom,
vector overlays.

## Status

Pre-alpha. Public API is unstable.

## Packages

- `projection/` — Web Mercator math, `LatLng`, `Bounds`
- `tile/` — tile coordinates, `TileSource` interface, OSM adapter, LRU cache
- `mapview/` — the `Map` factory returning a `gui.View`
- `examples/basic/` — minimal runnable demo

## Quick Start

```go
package main

import (
    "github.com/mike-ward/go-gui/gui"
    "github.com/mike-ward/go-gui/gui/backend"
    "github.com/mike-ward/go-map/mapview"
    "github.com/mike-ward/go-map/projection"
    "github.com/mike-ward/go-map/tile"
)

// Share one Source between Cfg.Source and WindowCfg.ImageFetcher so
// tile downloads carry an OSM-policy-compliant User-Agent.
var src = tile.OSMWithUserAgent("my-app/1.0 (contact@example.com)")

func main() {
    cfg := gui.WindowCfg{
        Title:  "Map",
        Width:  800,
        Height: 600,
        OnInit: func(w *gui.Window) { w.UpdateView(view) },
    }
    if f, ok := src.(tile.HTTPFetcher); ok {
        cfg.ImageFetcher = f.HTTPFetcher()
    }
    backend.Run(gui.NewWindow(cfg))
}

// Root layouts need a definite size. Wrap the map in a FixedFixed
// container sized to the window, then let the map fill inside.
func view(w *gui.Window) gui.View {
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
            }),
        },
    })
}
```

## Attribution

OSM tile usage requires attribution per the
[OSM Tile Usage Policy](https://operations.osmfoundation.org/policies/tiles/).
`mapview` renders an attribution overlay by default.

## License

MIT
