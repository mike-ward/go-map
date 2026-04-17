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

// Close-button and action-row layout constants. Close button sits in
// the title row's top-right; action buttons lay out left-to-right in
// a single row below body, centred against popup width.
const (
	infoCloseSize     float32 = 16
	infoCloseGap      float32 = 6 // gap between title end and close box
	infoActionGapY    float32 = 6 // gap between body and action row
	infoActionPadX    float32 = 8
	infoActionPadY    float32 = 3
	infoActionSpacing float32 = 6
)

// infoCloseGlyph is the "×" glyph drawn centred in the close-button
// box. Named so the symbol isn't scattered as a raw literal through
// measurement and draw calls.
const infoCloseGlyph = "×"

// maxInfoTitleBytes / maxInfoBodyBytes cap the UTF-8 byte length of
// text rendered in the popup so a pathological Marker value (bug or
// untrusted import) cannot drive the text measurer or layout math
// into pathological territory. Exceeding strings truncate at a rune
// boundary with a trailing ellipsis.
const (
	maxInfoTitleBytes  = 256
	maxInfoBodyBytes   = 1024
	maxInfoActionBytes = 32
)

// infoTitleStyle / infoBodyStyle share the HUD foreground so the
// popup reads as part of the map chrome. Title sits at 12 px, body
// at 11 px to match the coord readout.
var (
	infoTitleStyle  = gui.TextStyle{Size: 12, Color: hudFG}
	infoBodyStyle   = gui.TextStyle{Size: 11, Color: hudFG}
	infoCloseStyle  = gui.TextStyle{Size: 14, Color: hudFG}
	infoActionStyle = gui.TextStyle{Size: 11, Color: hudFG}
	// infoActionBG is semi-transparent on top of hudBG so the action
	// chip reads as a raised pill without fighting the popup body.
	infoActionBG = gui.Color{R: 255, G: 255, B: 255, A: 36}
)

// infoActionRect records the screen-space rect of one rendered action
// button. Zero-value rect never matches a hit (W or H == 0).
type infoActionRect struct {
	X, Y, W, H float32
}

// infoRectState records the last-rendered popup geometry so
// onMouseDown can consume and dispatch clicks without re-running the
// layout pass. The fixed-size Actions array keeps infoRectState a
// comparable struct (slices aren't comparable) — drawFocus relies on
// equality to skip per-frame state-map writes. MarkerID records which
// overlay owned the popup so action dispatch can look the callback
// back up in the registry at press time. Valid=false means no popup
// is currently drawn.
type infoRectState struct {
	X, Y, W, H                     float32
	CloseX, CloseY, CloseW, CloseH float32
	Actions                        [MaxInfoActions]infoActionRect
	ActionCount                    int
	MarkerID                       string
	Valid                          bool
}

// infoHitKind enumerates popup hit regions. Miss means the point was
// outside every rect.
type infoHitKind int

const (
	infoHitMiss infoHitKind = iota
	infoHitBody
	infoHitClose
	infoHitAction
)

// infoHitResult is the lookup outcome. Index is valid only when
// Kind == infoHitAction.
type infoHitResult struct {
	Kind  infoHitKind
	Index int
}

// hit classifies (px, py) against the stored popup rects in priority
// order: close button first (nested inside body), then each action,
// then the body fill. Invalid rects never match.
func (r infoRectState) hit(px, py float32) infoHitResult {
	if !r.Valid {
		return infoHitResult{Kind: infoHitMiss}
	}
	if r.CloseW > 0 && r.CloseH > 0 &&
		px >= r.CloseX && px < r.CloseX+r.CloseW &&
		py >= r.CloseY && py < r.CloseY+r.CloseH {
		return infoHitResult{Kind: infoHitClose}
	}
	// Clamp the loop bound to the fixed-array capacity so a corrupt
	// state-registry entry with ActionCount > MaxInfoActions (bug or
	// stale read during struct evolution) cannot index past r.Actions.
	n := r.ActionCount
	if n > MaxInfoActions {
		n = MaxInfoActions
	}
	for i := 0; i < n; i++ {
		a := r.Actions[i]
		if a.W <= 0 || a.H <= 0 {
			continue
		}
		if px >= a.X && px < a.X+a.W && py >= a.Y && py < a.Y+a.H {
			return infoHitResult{Kind: infoHitAction, Index: i}
		}
	}
	if px >= r.X && px < r.X+r.W && py >= r.Y && py < r.Y+r.H {
		return infoHitResult{Kind: infoHitBody}
	}
	return infoHitResult{Kind: infoHitMiss}
}

// drawFocus renders the focus ring around the keyboard-focused marker
// and, when s.InfoOpen, the InfoWindow popup anchored to it. The
// popup rects are stashed in the state registry so input handlers can
// consume and dispatch clicks that land on the popup. Callers resolve
// the focused marker once per frame (shared with stateForA11Y) and
// pass it in; nil means viewport mode. The rect write is guarded
// against equality so a static popup doesn't bump the state map on
// every frame.
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
	next := drawInfoWindow(dc, mx, my, m)
	if !next.Valid {
		clearInfoRect(w, id)
		return
	}
	next.MarkerID = m.MarkerID
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
// marker's screen position. Returns the final geometry so drawFocus
// can persist it for input hit-testing. The popup sits above the
// marker by default; when that would clip the canvas top it flips
// below. Horizontal placement centers on the marker and clamps to
// keep the popup fully on-screen. Non-finite / zero canvas size short-
// circuits to a zero-Valid result so the caller clears any stale rect.
func drawInfoWindow(dc *gui.DrawContext, mx, my float32, m *Marker) infoRectState {
	if !isFiniteF32(dc.Width) || !isFiniteF32(dc.Height) ||
		dc.Width <= 0 || dc.Height <= 0 {
		return infoRectState{}
	}
	title := truncateUTF8(m.Title, maxInfoTitleBytes)
	body := m.Body
	if body == "" {
		body = m.Label
	}
	body = truncateUTF8(body, maxInfoBodyBytes)

	// Measure content widths. Title row must budget a close-button
	// column so the X never overlaps glyphs.
	titleW := dc.TextWidth(title, infoTitleStyle)
	bodyW := dc.TextWidth(body, infoBodyStyle)
	titleRowW := titleW + infoCloseGap + infoCloseSize

	// Action row: measure buttons up to cap, track individual widths so
	// the draw pass doesn't re-measure. Entries past MaxInfoActions or
	// with empty labels are skipped.
	var actionWidths [MaxInfoActions]float32
	var actionLabels [MaxInfoActions]string
	actionCount := 0
	actionRowW := float32(0)
	actionH := float32(0)
	for _, a := range m.Actions {
		if actionCount >= MaxInfoActions {
			break
		}
		label := truncateUTF8(a.Label, maxInfoActionBytes)
		if label == "" {
			continue
		}
		lw := dc.TextWidth(label, infoActionStyle) + infoActionPadX*2
		actionLabels[actionCount] = label
		actionWidths[actionCount] = lw
		if actionCount > 0 {
			actionRowW += infoActionSpacing
		}
		actionRowW += lw
		actionCount++
	}
	if actionCount > 0 {
		actionH = dc.FontHeight(infoActionStyle) + infoActionPadY*2
	}

	contentW := titleRowW
	if bodyW > contentW {
		contentW = bodyW
	}
	if actionRowW > contentW {
		contentW = actionRowW
	}
	if contentW > infoMaxWidth {
		contentW = infoMaxWidth
	}

	titleH := dc.FontHeight(infoTitleStyle)
	bodyH := dc.FontHeight(infoBodyStyle)
	w := contentW + infoPadX*2
	h := titleH + infoPadY*2
	if body != "" {
		h += bodyH + infoGap
	}
	if actionCount > 0 {
		h += actionH + infoActionGapY
	}
	// A broken font metric (NaN / ±Inf TextWidth or FontHeight) would
	// propagate into w/h here and then into every tessellation vertex
	// the popup emits — the same failure mode slice 3 guarded against
	// for focus-ring screen coords. Bail to a zero-Valid rect so the
	// caller clears any stale state instead of painting NaN geometry.
	if !isFiniteF32(w) || !isFiniteF32(h) || w <= 0 || h <= 0 {
		return infoRectState{}
	}

	x := mx - w/2
	y := my - infoAnchorGap - h
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

	// Close button: flush top-right within padX. Background matches
	// action chip so it reads as a tappable region; "×" glyph sits
	// centred inside. The close row shares the title's vertical band,
	// so centre the box on the title's vertical midline.
	closeX := x + w - infoPadX - infoCloseSize
	closeY := y + infoPadY + (titleH-infoCloseSize)/2
	if closeY < y+2 {
		closeY = y + 2
	}
	dc.FilledRoundedRect(closeX, closeY, infoCloseSize, infoCloseSize, 3, infoActionBG)
	closeGlyphW := dc.TextWidth(infoCloseGlyph, infoCloseStyle)
	closeGlyphH := dc.FontHeight(infoCloseStyle)
	dc.Text(
		closeX+(infoCloseSize-closeGlyphW)/2,
		closeY+(infoCloseSize-closeGlyphH)/2,
		infoCloseGlyph, infoCloseStyle,
	)

	// Action row: centered horizontally within the popup, anchored to
	// the bottom padding. Record each rect for hit-testing. actionH
	// already embeds FontHeight + 2*padY, so deriving glyphH from it
	// (instead of re-calling FontHeight per button) saves N measurer
	// calls per popup frame.
	var actions [MaxInfoActions]infoActionRect
	if actionCount > 0 {
		rowY := y + h - infoPadY - actionH
		rowX := x + (w-actionRowW)/2
		cx := rowX
		glyphH := actionH - infoActionPadY*2
		for i := 0; i < actionCount; i++ {
			bw := actionWidths[i]
			actions[i] = infoActionRect{X: cx, Y: rowY, W: bw, H: actionH}
			dc.FilledRoundedRect(cx, rowY, bw, actionH, 3, infoActionBG)
			dc.Text(
				cx+infoActionPadX,
				rowY+(actionH-glyphH)/2,
				actionLabels[i], infoActionStyle,
			)
			cx += bw + infoActionSpacing
		}
	}

	return infoRectState{
		X: x, Y: y, W: w, H: h,
		CloseX: closeX, CloseY: closeY,
		CloseW: infoCloseSize, CloseH: infoCloseSize,
		Actions:     actions,
		ActionCount: actionCount,
		Valid:       true,
	}
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
