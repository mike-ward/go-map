package mapview

import (
	"math"

	"github.com/mike-ward/go-gui/gui"
	"github.com/mike-ward/go-map/projection"
	"github.com/mike-ward/go-map/tile"
)

// viewport holds the derived screen geometry for one frame: size in
// pixels, the world-pixel position of the center, and the tile range
// visible on screen. Computed once per OnDraw from MapState. Z is the
// fractional zoom; TileZ = floor(Z) is the integer tile level tiles
// are fetched at, and TileScale = 2^(Z-TileZ) is the per-tile draw
// scale that produces smooth intermediates between integer levels.
// The Zoom() method returns Z so viewport satisfies the overlay
// Projector interface without a wrapper type.
type viewport struct {
	W, H      float32 // canvas size in pixels
	Z         float64
	TileZ     uint32
	TileScale float64
	CtrPx     projection.Point // world-pixel coords of state.Center
	MinTX     int32
	MaxTX     int32
	MinTY     int32
	MaxTY     int32
	OriginX   float32 // world-pixel x of canvas top-left corner
	OriginY   float32
}

// computeViewport derives the screen → world mapping for the given
// canvas size and map state. Kept pure (no DrawContext) so viewport
// math is unit-testable without a running window. Tile-range math
// happens in world-pixel units at the fractional zoom; tileZ = floor(Z)
// is used only for the URL key and the scale factor.
func computeViewport(w, h float32, s MapState) viewport {
	vp := viewport{W: w, H: h, Z: s.Zoom}
	tileZ := uint32(math.Floor(s.Zoom))
	if tileZ > maxZoom {
		tileZ = maxZoom
	}
	vp.TileZ = tileZ
	vp.TileScale = math.Exp2(s.Zoom - float64(tileZ))
	vp.CtrPx = projection.ProjectF(s.Center, s.Zoom)
	vp.OriginX = float32(vp.CtrPx.X) - vp.W/2
	vp.OriginY = float32(vp.CtrPx.Y) - vp.H/2

	// Tile step in world-pixels at the fractional zoom equals TileSize
	// scaled by TileScale (i.e. tiles for tileZ cover scaledTs pixels
	// on screen). A tile-edge ≥ 0 is guaranteed by clampZoom keeping Z
	// non-negative and TileScale in [1, 2).
	scaledTs := float64(projection.TileSize) * vp.TileScale
	vp.MinTX = int32(math.Floor(float64(vp.OriginX) / scaledTs))
	vp.MinTY = int32(math.Floor(float64(vp.OriginY) / scaledTs))
	vp.MaxTX = int32(math.Floor(float64(vp.OriginX+vp.W) / scaledTs))
	vp.MaxTY = int32(math.Floor(float64(vp.OriginY+vp.H) / scaledTs))
	return vp
}

// wrapTileX maps a (possibly negative or out-of-range) tile-x index
// into [0, maxN) so viewports that straddle the antimeridian pull
// the correct tiles. maxN must be >= 1.
func wrapTileX(tx, maxN int32) uint32 {
	return uint32(((tx % maxN) + maxN) % maxN)
}

// tileScreenPos returns the top-left screen-pixel position of the
// given tile within the viewport. Tiles are drawn at TileSize scaled
// by TileScale (=2^(Z-TileZ)) so neighboring tiles meet at the
// fractional zoom. The spacing here uses the unrounded scale while
// drawTiles draws each tile at ceil(TileSize*TileScale) — the size is
// ceil'd to overlap neighbors by ≤1 px and suppress subpixel seams;
// the position stays unrounded so tiles still advance by their true
// projected extent. A refactor that "aligns" the two formulas would
// reintroduce the seams.
func (vp viewport) tileScreenPos(tx, ty int32) (x, y float32) {
	ts := float32(projection.TileSize) * float32(vp.TileScale)
	x = float32(tx)*ts - vp.OriginX
	y = float32(ty)*ts - vp.OriginY
	return
}

// screenToLatLng converts canvas pixel coords to geographic coords
// using the viewport's fractional zoom and origin.
func (vp viewport) screenToLatLng(sx, sy float32) projection.LatLng {
	return projection.UnprojectF(projection.Point{
		X: float64(vp.OriginX + sx),
		Y: float64(vp.OriginY + sy),
	}, vp.Z)
}

// LatLngToScreen projects p into canvas-pixel coords at the viewport's
// fractional zoom. Satisfies the overlay Projector interface so
// overlays can be given a viewport directly without an adapter type.
func (vp viewport) LatLngToScreen(p projection.LatLng) (x, y float32) {
	pt := projection.ProjectF(p, vp.Z)
	return float32(pt.X) - vp.OriginX, float32(pt.Y) - vp.OriginY
}

// MetersToPixels converts ground meters at the given latitude into
// pixels at the viewport zoom. Returns 0 for non-finite derivations.
func (vp viewport) MetersToPixels(lat, meters float64) float32 {
	mpp := metersPerPixel(lat, vp.Z)
	if !finitePositive(mpp) {
		return 0
	}
	return float32(meters / mpp)
}

// Zoom reports the viewport's fractional zoom level.
func (vp viewport) Zoom() float64 { return vp.Z }

// drawTiles renders the visible tile grid for every layer in draw
// order (base first, references stacked on top). Tiles with a URL from
// the layer's Source render as gui.DrawContext.Image. The base layer
// draws a labeled placeholder checkerboard when its Source is nil or
// returns an empty URL, so pan/zoom stays usable before any source is
// wired. Reference layers never draw a placeholder — a missing URL
// means "no data here", which leaves the base showing through.
//
// Tiles are fetched at integer TileZ and drawn at TileSize × TileScale
// pixels so fractional zoom fills the canvas without seams. ceil() on
// the scaled size suppresses subpixel gaps between neighbors at the
// cost of ≤1 px overdraw on transparent tile edges.
func drawTiles(dc *gui.DrawContext, vp viewport, layers []Layer) {
	maxN := int32(1) << vp.TileZ
	ts := float32(math.Ceil(float64(projection.TileSize) * vp.TileScale))

	if len(layers) == 0 {
		drawTilePlaceholders(dc, vp, maxN, ts)
		return
	}
	for i, l := range layers {
		isBase := i == 0 && l.Kind == LayerKindBase
		drawLayerTiles(dc, vp, l.Source, maxN, ts, isBase)
	}
}

// drawLayerTiles draws one layer's visible tiles. placeholderOK gates
// the labeled checkerboard fallback — only the base layer renders it,
// so reference layers stay transparent where their source has no tile.
//
// The layer's per-source HTTP fetcher (via tile.HTTPFetcher) is pulled
// once outside the tile loop and threaded into every dc.ImageWithFetcher
// call so OSM-policy and WMS-policy User-Agents travel with their own
// tiles even when both layers stack in the same window. Sources that
// do not implement tile.HTTPFetcher (e.g. offline / data: URLs) pass
// nil and fall back to gui.WindowCfg.ImageFetcher.
func drawLayerTiles(dc *gui.DrawContext, vp viewport, src tile.Source,
	maxN int32, ts float32, placeholderOK bool) {
	var fetcher gui.ImageFetcher
	if hf, ok := src.(tile.HTTPFetcher); ok {
		fetcher = hf.HTTPFetcher()
	}
	for ty := vp.MinTY; ty <= vp.MaxTY; ty++ {
		if ty < 0 || ty >= maxN {
			continue
		}
		for tx := vp.MinTX; tx <= vp.MaxTX; tx++ {
			wrapped := wrapTileX(tx, maxN)
			x, y := vp.tileScreenPos(tx, ty)

			var url string
			if src != nil {
				url = src.URL(tile.Coord{
					Z: vp.TileZ,
					X: wrapped,
					Y: uint32(ty),
				})
			}
			if url != "" {
				// Base tiles are opaque; paint the placeholder gray
				// behind them so decode latency does not flash the
				// canvas clear color. Reference tiles are frequently
				// transparent (boundaries, labels) — a solid bg
				// there would occlude the base, so pass gui.Color{}
				// (fully transparent).
				bg := gui.Color{}
				if placeholderOK {
					bg = tilePlaceholderEven
				}
				dc.ImageWithFetcher(x, y, ts, ts, url,
					gui.Opt[float32]{}, bg, fetcher)
				continue
			}
			if placeholderOK {
				drawTilePlaceholder(dc, x, y, ts, wrapped, ty, vp.TileZ)
			}
		}
	}
}

// drawTilePlaceholders fills the whole viewport with the checkerboard
// fallback when no layers are registered.
func drawTilePlaceholders(dc *gui.DrawContext, vp viewport, maxN int32, ts float32) {
	for ty := vp.MinTY; ty <= vp.MaxTY; ty++ {
		if ty < 0 || ty >= maxN {
			continue
		}
		for tx := vp.MinTX; tx <= vp.MaxTX; tx++ {
			wrapped := wrapTileX(tx, maxN)
			x, y := vp.tileScreenPos(tx, ty)
			drawTilePlaceholder(dc, x, y, ts, wrapped, ty, vp.TileZ)
		}
	}
}

var (
	tilePlaceholderEven   = gui.Hex(0xE8E6E0)
	tilePlaceholderOdd    = gui.Hex(0xDCDAD3)
	tilePlaceholderBorder = gui.Hex(0xBDBAB3)
	tilePlaceholderLabel  = gui.TextStyle{Size: 10, Color: gui.Hex(0x888888)}
)

func drawTilePlaceholder(dc *gui.DrawContext, x, y, ts float32,
	wrapped uint32, ty int32, tz uint32) {
	c := tilePlaceholderEven
	if (int32(wrapped)+ty)&1 == 1 {
		c = tilePlaceholderOdd
	}
	dc.FilledRect(x, y, ts, ts, c)
	dc.Rect(x, y, ts, ts, tilePlaceholderBorder, 1/dc.Scale)
	dc.Text(x+6, y+4,
		(tile.Coord{Z: tz, X: wrapped, Y: uint32(ty)}).String(),
		tilePlaceholderLabel)
}
