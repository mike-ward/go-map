package mapview

import (
	"fmt"

	"github.com/mike-ward/go-gui/gui"
	"github.com/mike-ward/go-map/tile"
)

// HUD overlay sizes and colors. Kept package-local and free of
// allocation — values reused each frame.
var (
	hudBG     = gui.Color{R: 0, G: 0, B: 0, A: 140}
	hudFG     = gui.Color{R: 240, G: 240, B: 240, A: 255}
	hudStyle  = gui.TextStyle{Size: 11, Color: hudFG}
	attrStyle = gui.TextStyle{Size: 10, Color: hudFG}
)

// drawAttribution renders the tile source's required credit string in
// the bottom-right corner. Required by OSM and most providers; not
// suppressible.
func drawAttribution(dc *gui.DrawContext, src tile.Source) {
	if src == nil {
		return
	}
	text := src.Attribution()
	if text == "" {
		return
	}
	tw := dc.TextWidth(text, attrStyle)
	th := dc.FontHeight(attrStyle)
	padX, padY := float32(6), float32(3)
	w := tw + padX*2
	h := th + padY*2
	x := dc.Width - w - 4
	y := dc.Height - h - 4
	dc.FilledRect(x, y, w, h, hudBG)
	dc.Text(x+padX, y+padY, text, attrStyle)
}

// drawCoordReadout renders the geographic location under the mouse
// cursor plus the current zoom level, bottom-left. Falls back to the
// map center when the cursor is not over the canvas.
func drawCoordReadout(dc *gui.DrawContext, vp viewport, s MapState, h hoverState) {
	ll := s.Center
	if h.Valid {
		ll = vp.screenToLatLng(h.X, h.Y)
	}
	text := fmt.Sprintf("z%d  %7.4f°, %8.4f°", vp.Zoom, ll.Lat, ll.Lng)
	drawHUDBadge(dc, 4, dc.Height-20, text, hudStyle)
}

// drawZoomIndicator renders a numeric zoom level top-right. Paired
// with the attribution line but anchored top so a future layer
// control can stack below.
func drawZoomIndicator(dc *gui.DrawContext, z uint32) {
	text := fmt.Sprintf("z%d", z)
	tw := dc.TextWidth(text, hudStyle)
	th := dc.FontHeight(hudStyle)
	padX, padY := float32(6), float32(3)
	w := tw + padX*2
	h := th + padY*2
	x := dc.Width - w - 4
	y := float32(4)
	dc.FilledRect(x, y, w, h, hudBG)
	dc.Text(x+padX, y+padY, text, hudStyle)
}

// drawHUDBadge draws a small rounded-corner-less HUD chip anchored at
// (x, y). Used by coord readout; kept here for a single definition
// instead of duplicating across HUD elements.
func drawHUDBadge(dc *gui.DrawContext, x, y float32, text string, style gui.TextStyle) {
	tw := dc.TextWidth(text, style)
	th := dc.FontHeight(style)
	padX, padY := float32(6), float32(3)
	w := tw + padX*2
	h := th + padY*2
	dc.FilledRect(x, y-padY, w, h, hudBG)
	dc.Text(x+padX, y, text, style)
}

// hoverState holds the last mouse position over the canvas, used by
// drawCoordReadout. Kept in its own registry namespace keyed by map
// ID so multiple maps coexist.
type hoverState struct {
	X, Y  float32
	Valid bool
}

const nsHover = "mapview.hover"

func readHover(w *gui.Window, id string) hoverState {
	return gui.StateReadOr[string, hoverState](w, nsHover, id, hoverState{})
}

func writeHover(w *gui.Window, id string, h hoverState) {
	gui.StateMap[string, hoverState](w, nsHover, capMaps).Set(id, h)
}

// stateForA11Y produces a concise "center + zoom" sentence for
// screen readers. Called when rebuilding the A11YDescription each
// frame.
func stateForA11Y(s MapState) string {
	return fmt.Sprintf(
		"Map centered at %.4f degrees latitude, %.4f degrees longitude, zoom level %d.",
		s.Center.Lat, s.Center.Lng, s.Zoom,
	)
}
