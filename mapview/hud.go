package mapview

import (
	"fmt"
	"math"
	"strconv"

	"github.com/mike-ward/go-gui/gui"
	"github.com/mike-ward/go-map/projection"
	"github.com/mike-ward/go-map/tile"
)

// HUD overlay sizes and colors. Kept package-local and free of
// allocation — values reused each frame.
var (
	hudBG      = gui.Color{R: 0, G: 0, B: 0, A: 140}
	hudFG      = gui.Color{R: 240, G: 240, B: 240, A: 255}
	hudStyle   = gui.TextStyle{Size: 11, Color: hudFG}
	attrStyle  = gui.TextStyle{Size: 10, Color: hudFG}
	scaleStyle = gui.TextStyle{Size: 10, Color: hudFG}
)

// HUD layout constants. Anchors and sizes are deterministic so input
// hit-tests match draw positions without sharing state.
const (
	homeBtnSize        float32 = 28
	homeBtnMargin      float32 = 4
	zoomChipHeight     float32 = 24
	scaleBarMaxPx      float32 = 110
	scaleBarRowSpacing float32 = 10
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
	w, h := chipMetrics(dc, text, attrStyle)
	x := dc.Width - w - 4
	y := dc.Height - h - 4
	drawHUDChip(dc, x, y, w, h, text, attrStyle)
}

// drawCoordReadout renders the geographic location under the mouse
// cursor plus the current zoom level, bottom-left. Falls back to the
// map center when the cursor is not over the canvas.
func drawCoordReadout(dc *gui.DrawContext, vp viewport, s MapState, h hoverState) {
	ll := s.Center
	if h.Valid {
		ll = vp.screenToLatLng(h.X, h.Y)
	}
	text := fmt.Sprintf("z%d  %7.4f°, %8.4f°", vp.Z, ll.Lat, ll.Lng)
	w, ch := chipMetrics(dc, text, hudStyle)
	drawHUDChip(dc, 4, dc.Height-ch-4, w, ch, text, hudStyle)
}

// drawZoomIndicator renders a numeric zoom level top-right. Paired
// with the attribution line but anchored top so the home button can
// stack directly below.
func drawZoomIndicator(dc *gui.DrawContext, z uint32) {
	text := "z" + strconv.FormatUint(uint64(z), 10)
	w, _ := chipMetrics(dc, text, hudStyle)
	x := dc.Width - w - homeBtnMargin
	drawHUDChip(dc, x, homeBtnMargin, w, zoomChipHeight, text, hudStyle)
}

// homeButtonRect returns the screen rect of the home button. Used by
// both the draw pass and onClick hit-test so the two never disagree.
func homeButtonRect(canvasW float32) (x, y, w, h float32) {
	w, h = homeBtnSize, homeBtnSize
	x = canvasW - w - homeBtnMargin
	y = homeBtnMargin + zoomChipHeight + homeBtnMargin
	return
}

// homeRoofTri is a reusable triangle-vertex buffer for the home
// glyph. drawHomeButton overwrites the six floats in place each
// frame so FilledPolygon never sees a heap-allocated literal.
var homeRoofTri = [6]float32{}

// drawHomeButton renders a recenter button below the zoom indicator.
// Click handling lives in input.go so the hit-test runs before the
// drag-pan path.
func drawHomeButton(dc *gui.DrawContext) {
	x, y, w, h := homeButtonRect(dc.Width)
	dc.FilledRoundedRect(x, y, w, h, 4, hudBG)
	cx, cy := x+w/2, y+h/2
	homeRoofTri = [6]float32{
		cx, cy - 8,
		cx - 9, cy - 1,
		cx + 9, cy - 1,
	}
	dc.FilledPolygon(homeRoofTri[:], hudFG)
	dc.FilledRect(cx-7, cy-2, 14, 9, hudFG)
	dc.FilledRect(cx-2, cy+2, 4, 5, hudBG)
}

// drawScaleBar renders stacked metric and imperial scale bars in the
// bottom-left corner above the coord readout. Distances scale with
// the cosine of the center latitude, matching standard slippy-map
// scale-bar convention.
func drawScaleBar(dc *gui.DrawContext, s MapState) {
	mpp := metersPerPixel(s.Center.Lat, s.Zoom)
	if mpp <= 0 || math.IsNaN(mpp) || math.IsInf(mpp, 0) {
		return
	}
	metricLabel, metricPx := metricBar(mpp, scaleBarMaxPx)
	imperialLabel, imperialPx := imperialBar(mpp, scaleBarMaxPx)
	if metricPx <= 0 && imperialPx <= 0 {
		return
	}

	// Coord readout sits at dc.Height-20; stack scale bars above it.
	const tickH float32 = 4
	bx := float32(8)
	by := dc.Height - 30 // baseline of metric bar

	// Backing chip sized to the longest (bar + label) so wide labels
	// at low zoom (e.g. "10000 km") stay legible without spillover.
	metricLW := dc.TextWidth(metricLabel, scaleStyle)
	imperialLW := dc.TextWidth(imperialLabel, scaleStyle)
	chipW := max(metricPx+metricLW, imperialPx+imperialLW) + 12
	chipH := float32(28)
	dc.FilledRect(bx-4, by-chipH+4, chipW, chipH, hudBG)

	if metricPx > 0 {
		drawScaleSegment(dc, bx, by, metricPx, tickH, metricLabel)
	}
	if imperialPx > 0 {
		drawScaleSegment(dc, bx, by+scaleBarRowSpacing, imperialPx, tickH, imperialLabel)
	}
}

func drawScaleSegment(dc *gui.DrawContext, x, y, length, tickH float32, label string) {
	dc.Line(x, y, x+length, y, hudFG, 1)
	dc.Line(x, y-tickH, x, y, hudFG, 1)
	dc.Line(x+length, y-tickH, x+length, y, hudFG, 1)
	dc.Text(x+length+4, y-7, label, scaleStyle)
}

// metersPerPixel returns the ground distance covered by one pixel at
// the given latitude and zoom. Uses the equatorial circumference
// scaled by cos(lat) — the standard Web Mercator approximation.
func metersPerPixel(lat float64, z uint32) float64 {
	const earthCircum = 40075016.686
	return earthCircum * math.Cos(lat*math.Pi/180) / projection.WorldSize(z)
}

// niceRound returns the largest "1, 2, 5 × 10^n" value at or below
// maxValue. Used to pick scale-bar lengths that read as round
// distances. Returns 0 for any non-finite or non-positive input so
// the scale-bar rendering path can short-circuit instead of drawing
// NaN-sized geometry.
func niceRound(maxValue float64) float64 {
	if math.IsNaN(maxValue) || math.IsInf(maxValue, 0) || maxValue <= 0 {
		return 0
	}
	exp := math.Floor(math.Log10(maxValue))
	pow := math.Pow(10, exp)
	if math.IsInf(pow, 0) || pow == 0 {
		return 0
	}
	switch base := maxValue / pow; {
	case base >= 5:
		return 5 * pow
	case base >= 2:
		return 2 * pow
	default:
		return pow
	}
}

func metricBar(metersPerPx float64, maxPx float32) (label string, px float32) {
	m := niceRound(metersPerPx * float64(maxPx))
	if m <= 0 {
		return "", 0
	}
	px = float32(m / metersPerPx)
	if m < 1000 {
		label = fmt.Sprintf("%g m", m)
	} else {
		label = fmt.Sprintf("%g km", m/1000)
	}
	return
}

func imperialBar(metersPerPx float64, maxPx float32) (label string, px float32) {
	const ftPerM = 3.28084
	const ftPerMi = 5280
	feetPerPx := metersPerPx * ftPerM
	maxFt := feetPerPx * float64(maxPx)
	if maxFt < ftPerMi {
		ft := niceRound(maxFt)
		if ft <= 0 {
			return "", 0
		}
		return fmt.Sprintf("%g ft", ft), float32(ft / feetPerPx)
	}
	milesPerPx := feetPerPx / ftPerMi
	mi := niceRound(milesPerPx * float64(maxPx))
	if mi <= 0 {
		return "", 0
	}
	return fmt.Sprintf("%g mi", mi), float32(mi / milesPerPx)
}

// HUD chip padding. Shared between chipMetrics and drawHUDChip so a
// pad change can never desync size measurement from text placement.
const (
	chipPadX float32 = 6
	chipPadY float32 = 3
)

func chipMetrics(dc *gui.DrawContext, text string, style gui.TextStyle) (w, h float32) {
	w = dc.TextWidth(text, style) + chipPadX*2
	h = dc.FontHeight(style) + chipPadY*2
	return
}

func drawHUDChip(dc *gui.DrawContext, x, y, w, h float32, text string, style gui.TextStyle) {
	dc.FilledRect(x, y, w, h, hudBG)
	dc.Text(x+chipPadX, y+chipPadY, text, style)
}

// hoverState holds the last mouse position over the canvas, used by
// drawCoordReadout. Kept in its own registry namespace keyed by map
// ID so multiple maps coexist.
type hoverState struct {
	X, Y  float32
	Valid bool
}

// stateForA11Y produces a concise "center + zoom" sentence for
// screen readers. Called when rebuilding the A11YDescription each
// frame. When a marker is keyboard-focused, its label (and popup
// contents when open) prepend the viewport sentence so the
// screen-reader hears focus changes first.
func stateForA11Y(s MapState, focused *Marker) string {
	base := fmt.Sprintf(
		"Map centered at %.4f degrees latitude, %.4f degrees longitude, zoom level %d.",
		s.Center.Lat, s.Center.Lng, s.Zoom,
	)
	if focused == nil {
		return base
	}
	lead := "Marker focused: " + markerA11YText(focused) + ". "
	if s.InfoOpen {
		lead = "Info window open. " + lead
	}
	return lead + base
}

// markerA11YText picks the best human-readable descriptor for m. Title
// wins when present; Label is the fallback so decorative markers still
// announce something; finally the marker ID keeps the sentence
// grammatical even when the author left everything blank. Each field
// is UTF-8-safely truncated so a pathological value cannot push the
// A11YDescription to megabyte scale each frame.
func markerA11YText(m *Marker) string {
	switch {
	case m.Title != "":
		t := truncateUTF8(m.Title, maxInfoTitleBytes)
		if m.Body != "" {
			return t + ", " + truncateUTF8(m.Body, maxInfoBodyBytes)
		}
		return t
	case m.Label != "":
		return truncateUTF8(m.Label, maxInfoBodyBytes)
	default:
		return truncateUTF8(m.MarkerID, maxInfoBodyBytes)
	}
}
