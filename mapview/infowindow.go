package mapview

import (
	"math"

	"github.com/mike-ward/go-gui/gui"
)

// focusRingRadius is the screen-pixel radius of the outline drawn
// around the keyboard-focused marker. Sized to sit just outside the
// 6 px marker disc plus its 2 px white rim so the ring reads as
// distinct from the marker body at standard DPI.
const focusRingRadius float32 = 11

// focusRingWidth is the stroke width of the focus ring.
const focusRingWidth float32 = 2

// infoPopup layout constants. Kept package-local and sized so the
// popup stays legible at the default 11 px body style without
// crowding adjacent markers.
const (
	infoPadX       float32 = 8
	infoPadY       float32 = 6
	infoGap        float32 = 2 // vertical gap between title and body
	infoMaxWidth   float32 = 280
	infoAnchorGap  float32 = 14 // pixels between marker and popup edge
	infoMarginEdge float32 = 4  // min distance from canvas edges
)

// maxInfoTitleBytes / maxInfoBodyBytes cap the UTF-8 byte length of
// text rendered in the popup so a pathological Marker value (bug or
// untrusted import) cannot drive the text measurer or layout math
// into pathological territory. Exceeding strings truncate at a rune
// boundary with a trailing ellipsis.
const (
	maxInfoTitleBytes = 256
	maxInfoBodyBytes  = 1024
)

// infoTitleStyle / infoBodyStyle share the HUD foreground so the
// popup reads as part of the map chrome. Title sits at 12 px, body
// at 11 px to match the coord readout.
var (
	infoTitleStyle = gui.TextStyle{Size: 12, Color: hudFG}
	infoBodyStyle  = gui.TextStyle{Size: 11, Color: hudFG}
)

// infoRectState records the last-rendered popup rect so onMouseDown
// can consume clicks that land inside the popup; without this the
// click would pass through to the map beneath and start a drag-pan or
// select an overlay under the popup body. Valid=false means no popup
// is currently drawn.
type infoRectState struct {
	X, Y, W, H float32
	Valid      bool
}

// hit reports whether (px, py) lies inside the stored popup rect.
// Invalid rects never hit.
func (r infoRectState) hit(px, py float32) bool {
	if !r.Valid {
		return false
	}
	return px >= r.X && px < r.X+r.W && py >= r.Y && py < r.Y+r.H
}

// drawFocus renders the focus ring around the keyboard-focused marker
// and, when s.InfoOpen, the InfoWindow popup anchored to it. The
// popup rect is stashed in the state registry so input handlers can
// consume clicks that land on it. Callers resolve the focused marker
// once per frame (shared with stateForA11Y) and pass it in; nil
// means viewport mode. The rect write is guarded against equality so
// a static popup doesn't bump the state map on every frame.
func drawFocus(w *gui.Window, id string, dc *gui.DrawContext, vp viewport, m *Marker, s MapState) {
	if m == nil {
		clearInfoRect(w, id)
		return
	}
	mx, my := vp.LatLngToScreen(m.Pos)
	// Skip when the projection produced non-finite screen coords (bad
	// author Pos slipped past projection.Clamp, NaN zoom) — drawing
	// primitives would propagate NaN geometry into the tessellation
	// buffer and corrupt every subsequent triangle in the batch.
	if !isFiniteF32(mx) || !isFiniteF32(my) {
		clearInfoRect(w, id)
		return
	}
	dc.Circle(mx, my, focusRingRadius, gui.Hex(0xFFD400), focusRingWidth)
	if !s.InfoOpen || m.Title == "" {
		clearInfoRect(w, id)
		return
	}
	x, y, pw, ph := drawInfoWindow(dc, mx, my, m)
	next := infoRectState{X: x, Y: y, W: pw, H: ph, Valid: true}
	if nsRead[infoRectState](w, nsInfoRect, id) != next {
		nsWrite(w, nsInfoRect, id, next)
	}
}

// clearInfoRect marks the popup rect as absent. Skips the write when
// the slot is already empty so a map without a focused marker does
// not dirty the state map every frame.
func clearInfoRect(w *gui.Window, id string) {
	if !nsRead[infoRectState](w, nsInfoRect, id).Valid {
		return
	}
	nsWrite(w, nsInfoRect, id, infoRectState{})
}

// isFiniteF32 reports whether v is a real finite number (not NaN and
// not ±Inf). Drawing paths use this before feeding coords to the
// DrawContext so a single bad value cannot poison the triangle batch.
func isFiniteF32(v float32) bool {
	f := float64(v)
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

// drawInfoWindow paints the popup box anchored to (mx, my) — the
// marker's screen position. Returns the final rect so drawFocus can
// persist it for input hit-testing. The popup sits above the marker
// by default; when that would clip the canvas top it flips below.
// Horizontal placement centers on the marker and clamps to keep the
// popup fully on-screen.
func drawInfoWindow(dc *gui.DrawContext, mx, my float32, m *Marker) (x, y, w, h float32) {
	// Zero / non-finite canvas size means there is no sensible place
	// to anchor the popup; skip rather than emit NaN geometry.
	if !isFiniteF32(dc.Width) || !isFiniteF32(dc.Height) ||
		dc.Width <= 0 || dc.Height <= 0 {
		return
	}
	title := truncateUTF8(m.Title, maxInfoTitleBytes)
	body := m.Body
	if body == "" {
		body = m.Label
	}
	body = truncateUTF8(body, maxInfoBodyBytes)
	titleW := dc.TextWidth(title, infoTitleStyle)
	bodyW := dc.TextWidth(body, infoBodyStyle)
	contentW := titleW
	if bodyW > contentW {
		contentW = bodyW
	}
	if contentW > infoMaxWidth {
		contentW = infoMaxWidth
	}
	titleH := dc.FontHeight(infoTitleStyle)
	bodyH := dc.FontHeight(infoBodyStyle)
	w = contentW + infoPadX*2
	h = titleH + infoPadY*2
	if body != "" {
		h += bodyH + infoGap
	}

	x = mx - w/2
	y = my - infoAnchorGap - h
	// Clamp horizontally within canvas.
	if x < infoMarginEdge {
		x = infoMarginEdge
	}
	if x+w > dc.Width-infoMarginEdge {
		x = dc.Width - infoMarginEdge - w
	}
	// Right-edge clamp can push x negative when the popup is wider
	// than the canvas (narrow windows). Final left-edge pin keeps at
	// least the popup's left side on-screen.
	if x < 0 {
		x = 0
	}
	// Flip below the marker when the popup would clip the canvas top.
	if y < infoMarginEdge {
		y = my + infoAnchorGap
	}
	// Final bottom-edge clamp (handles the flipped-below case on a
	// canvas too short for either anchor).
	if y+h > dc.Height-infoMarginEdge {
		y = dc.Height - infoMarginEdge - h
	}
	if y < 0 {
		y = 0
	}

	dc.FilledRoundedRect(x, y, w, h, 4, hudBG)
	dc.Text(x+infoPadX, y+infoPadY, title, infoTitleStyle)
	if body != "" {
		dc.Text(x+infoPadX, y+infoPadY+titleH+infoGap, body, infoBodyStyle)
	}
	return
}

// truncateUTF8 returns s unchanged when len(s) <= limit; otherwise it
// trims to the largest rune boundary at or below limit and appends
// "…" so the result remains a valid UTF-8 string. limit < 0 is
// treated as 0; s shorter than one rune cannot be truncated further.
func truncateUTF8(s string, limit int) string {
	if limit < 0 {
		limit = 0
	}
	if len(s) <= limit {
		return s
	}
	end := limit
	// UTF-8 continuation bytes are 0b10xxxxxx; roll back so we never
	// split a multibyte rune.
	for end > 0 && (s[end]&0xC0) == 0x80 {
		end--
	}
	return s[:end] + "…"
}
