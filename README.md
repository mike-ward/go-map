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
- `mapview/` — the `Widget` factory returning a `gui.View`
- `examples/basic/` — minimal runnable demo

## Quick Start

```go
package main

import (
    "github.com/mike-ward/go-map/mapview"
    "github.com/mike-ward/go-map/projection"
    "github.com/mike-ward/go-map/tile"
    "github.com/mike-ward/go-gui/gui"
    "github.com/mike-ward/go-gui/gui/backend"
)

func main() {
    w := gui.NewWindow(gui.WindowCfg{
        Title:  "Map",
        Width:  800,
        Height: 600,
        OnInit: func(w *gui.Window) {
            w.UpdateView(view)
        },
    })
    backend.Run(w)
}

func view(w *gui.Window) gui.View {
    return mapview.Widget(mapview.Cfg{
        ID:     "map",
        Sizing: gui.FillFill,
        Center: projection.LatLng{Lat: 47.6062, Lng: -122.3321},
        Zoom:   11,
        Source: tile.OSM(),
    })
}
```

## Attribution

OSM tile usage requires attribution per the
[OSM Tile Usage Policy](https://operations.osmfoundation.org/policies/tiles/).
`mapview` renders an attribution overlay by default.

## License

MIT
