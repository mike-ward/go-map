package mapview

import (
	"math"

	"github.com/mike-ward/go-gui/gui"
	"github.com/mike-ward/go-map/projection"
	"github.com/mike-ward/go-map/tile"
)

// viewport holds the derived screen geometry for one frame: size in
// pixels, the world-pixel position of the center, and the tile range
// visible on screen. Computed once per OnDraw from MapState.
type viewport struct {
	W, H    float32 // canvas size in pixels
	Zoom    uint32
	CtrPx   projection.Point // world-pixel coords of state.Center
	MinTX   int32
	MaxTX   int32
	MinTY   int32
	MaxTY   int32
	OriginX float32 // world-pixel x of canvas top-left corner
	OriginY float32
}

func computeViewport(dc *gui.DrawContext, s MapState) viewport {
	vp := viewport{W: dc.Width, H: dc.Height, Zoom: s.Zoom}
	vp.CtrPx = projection.Project(s.Center, s.Zoom)
	vp.OriginX = float32(vp.CtrPx.X) - vp.W/2
	vp.OriginY = float32(vp.CtrPx.Y) - vp.H/2

	ts := float64(projection.TileSize)
	vp.MinTX = int32(math.Floor(float64(vp.OriginX) / ts))
	vp.MinTY = int32(math.Floor(float64(vp.OriginY) / ts))
	vp.MaxTX = int32(math.Floor(float64(vp.OriginX+vp.W) / ts))
	vp.MaxTY = int32(math.Floor(float64(vp.OriginY+vp.H) / ts))
	return vp
}

// tileScreenPos returns the top-left screen-pixel position of the
// given tile within the viewport.
func (vp viewport) tileScreenPos(tx, ty int32) (x, y float32) {
	ts := float32(projection.TileSize)
	x = float32(tx)*ts - vp.OriginX
	y = float32(ty)*ts - vp.OriginY
	return
}

// screenToLatLng converts canvas pixel coords to geographic coords
// using the viewport's zoom and origin.
func (vp viewport) screenToLatLng(sx, sy float32) projection.LatLng {
	return projection.Unproject(projection.Point{
		X: float64(vp.OriginX + sx),
		Y: float64(vp.OriginY + sy),
	}, vp.Zoom)
}

// drawTiles renders the visible tile grid. Tiles with a URL from the
// Source render as gui.DrawContext.Image; sources without a URL (or
// no Source at all) fall back to a labeled placeholder checkerboard
// so pan/zoom is still usable.
func drawTiles(dc *gui.DrawContext, vp viewport, src tile.Source) {
	maxN := int32(1) << vp.Zoom
	ts := float32(projection.TileSize)
	even := gui.Hex(0xE8E6E0)
	odd := gui.Hex(0xDCDAD3)
	border := gui.Hex(0xBDBAB3)
	labelStyle := gui.TextStyle{Size: 10, Color: gui.Hex(0x888888)}

	for ty := vp.MinTY; ty <= vp.MaxTY; ty++ {
		if ty < 0 || ty >= maxN {
			continue
		}
		for tx := vp.MinTX; tx <= vp.MaxTX; tx++ {
			wrapped := ((tx % maxN) + maxN) % maxN
			x, y := vp.tileScreenPos(tx, ty)

			var url string
			if src != nil {
				url = src.URL(tile.Coord{
					Z: vp.Zoom,
					X: uint32(wrapped),
					Y: uint32(ty),
				})
			}
			if url != "" {
				dc.Image(x, y, ts, ts, url, gui.Opt[float32]{}, even)
				continue
			}
			// Placeholder.
			c := even
			if (wrapped+ty)&1 == 1 {
				c = odd
			}
			dc.FilledRect(x, y, ts, ts, c)
			dc.Rect(x, y, ts, ts, border, 1)
			dc.Text(x+6, y+4,
				(tile.Coord{Z: vp.Zoom, X: uint32(wrapped), Y: uint32(ty)}).String(),
				labelStyle)
		}
	}
}
