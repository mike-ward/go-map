package mapview

import (
	"math"

	"github.com/mike-ward/go-gui/gui"
	"github.com/mike-ward/go-map/projection"
)

// dragThresholdPx is the pixel distance separating a click from a pan.
const dragThresholdPx float32 = 4

// onMouseDown handles mouse-down on the canvas. HUD buttons get a hit
// test first so the recenter button does not trigger a drag-pan;
// otherwise the press is recorded and MouseLock takes over the
// subsequent mouse-move / mouse-up events. The press becomes a pan
// only once the cursor leaves the drag-threshold radius; shorter
// presses collapse into a click at mouse-up time.
func onMouseDown(c Cfg, seed MapState) func(*gui.Layout, *gui.Event, *gui.Window) {
	id := c.ID
	return func(l *gui.Layout, e *gui.Event, w *gui.Window) {
		// Popup-rect hit-test runs first so a click on the InfoWindow
		// body neither starts a drag-pan nor falls through to an
		// overlay beneath the popup. No-op when no popup is drawn.
		if handlePopupClick(w, id, e) {
			return
		}
		if homeButtonHit(l.Shape.Width, e.MouseX, e.MouseY) {
			nsWrite(w, nsState, id, seed)
			e.IsHandled = true
			return
		}
		s := nsRead[MapState](w, nsState, id)
		// OnClick delivers widget-local coords; MouseLock callbacks
		// deliver absolute coords. Storing both, plus canvas size,
		// lets panDragEnd resolve the release LatLng without a second
		// event dispatch.
		nsWrite(w, nsPan, id, panState{
			Active:    true,
			StartX:    e.MouseX + l.Shape.X,
			StartY:    e.MouseY + l.Shape.Y,
			LocalX:    e.MouseX,
			LocalY:    e.MouseY,
			StartCtr:  s.Center,
			StartZoom: s.Zoom,
			CanvasW:   l.Shape.Width,
			CanvasH:   l.Shape.Height,
		})
		w.MouseLock(gui.MouseLockCfg{
			MouseMove: panDragMove(id),
			MouseUp:   panDragEnd(c),
		})
		e.IsHandled = true
	}
}

// handlePopupClick consumes a mouse-down that landed on the InfoWindow
// popup. Close-button and action-button hits dispatch on press
// (matching the Home button) so no drag-tracking is started; the
// state write to close the popup fires *before* any action callback so
// an OnClick that reads Snapshot sees InfoOpen=false. Returns true when
// the event was consumed (body hits, close, action); false means no
// popup is drawn or the press was outside the popup rect — caller
// continues with its normal handling.
func handlePopupClick(w *gui.Window, id string, e *gui.Event) bool {
	rect := nsRead[infoRectState](w, nsInfoRect, id)
	h := rect.hit(e.MouseX, e.MouseY)
	switch h.Kind {
	case infoHitMiss:
		return false
	case infoHitBody:
		e.IsHandled = true
		return true
	case infoHitClose:
		closeInfoPopup(w, id)
		e.IsHandled = true
		return true
	case infoHitAction:
		dispatchInfoAction(w, id, markerByID(w, id, rect.MarkerID), h.Index)
		e.IsHandled = true
		return true
	}
	return false
}

// dispatchInfoAction closes the popup and, when idx is in range, fires
// the action callback. Invariant shared with the keyboard Enter path:
// registry state is persisted with InfoOpen=false *before* the callback
// runs so any Snapshot read inside the callback sees the dismissed
// state. Bounds-guarded against both MaxInfoActions and the live
// Actions length so a stale index from a shrunken Actions slice cannot
// OOB.
func dispatchInfoAction(w *gui.Window, id string, m *Marker, idx int) {
	closeInfoPopup(w, id)
	if m == nil || idx < 0 || idx >= MaxInfoActions || idx >= len(m.Actions) {
		return
	}
	if cb := m.Actions[idx].OnClick; cb != nil {
		cb(w)
	}
}

// closeInfoPopup flips InfoOpen off on the map state and resets the
// popup focus index so the next open lands on the first sub-element.
// No-op when the popup is already closed so a second close press on a
// race does not dirty the state map.
func closeInfoPopup(w *gui.Window, id string) {
	s := nsRead[MapState](w, nsState, id)
	if !s.InfoOpen && s.InfoFocusIndex == 0 {
		return
	}
	s.InfoOpen = false
	s.InfoFocusIndex = 0
	nsWrite(w, nsState, id, s)
}

// markerByID fetches the Marker overlay with the given overlay-ID,
// returning nil when absent or when the overlay is not a Marker. Used
// by action dispatch to re-resolve the callback at press time — the
// overlay map may have changed between the frame that rendered the
// popup and the frame that delivered the click.
func markerByID(w *gui.Window, id, markerID string) *Marker {
	if markerID == "" {
		return nil
	}
	o, ok := readOverlays(w, id).Get(markerID)
	if !ok {
		return nil
	}
	m, _ := o.(*Marker)
	return m
}

func homeButtonHit(canvasW, mx, my float32) bool {
	x, y, w, h := homeButtonRect(canvasW)
	return mx >= x && mx < x+w && my >= y && my < y+h
}

func panDragMove(id string) func(*gui.Layout, *gui.Event, *gui.Window) {
	return func(_ *gui.Layout, e *gui.Event, w *gui.Window) {
		p := nsRead[panState](w, nsPan, id)
		if !p.Active {
			return
		}
		dx := p.StartX - e.MouseX
		dy := p.StartY - e.MouseY
		if !p.Moved {
			// Swallow intra-threshold movement entirely. Prevents the
			// map from jittering when the user shakes the pointer mid-
			// click, and keeps the drag-vs-click decision crisp.
			if dx*dx+dy*dy < dragThresholdPx*dragThresholdPx {
				e.IsHandled = true
				return
			}
			p.Moved = true
			nsWrite(w, nsPan, id, p)
		}
		// Convert screen-pixel delta to world-pixel delta at the
		// drag-start zoom. Inverting the sign gives "content follows
		// cursor" feel. Fractional zoom flows through the F-variants.
		startPt := projection.ProjectF(p.StartCtr, p.StartZoom)
		newCtr := projection.UnprojectF(projection.Point{
			X: startPt.X + float64(dx),
			Y: startPt.Y + float64(dy),
		}, p.StartZoom)

		s := nsRead[MapState](w, nsState, id)
		s.Center = newCtr.Clamp()
		nsWrite(w, nsState, id, s)
		e.IsHandled = true
	}
}

// panDragEnd finalises a press-and-release. A drag that never crossed
// the threshold becomes a click: OnPOISelect fires first for the
// top-most overlay under the release point, then Marker.OnClick, then
// Cfg.OnClick. The release point comes from the mouse-up event (in
// absolute window coords) converted back into widget-local coords via
// panState.StartX/Y; within the drag threshold this agrees with the
// press point to within a few pixels.
func panDragEnd(c Cfg) func(*gui.Layout, *gui.Event, *gui.Window) {
	id := c.ID
	return func(_ *gui.Layout, e *gui.Event, w *gui.Window) {
		p := nsRead[panState](w, nsPan, id)
		wasClick := p.Active && !p.Moved
		p.Active = false
		p.Moved = false
		nsWrite(w, nsPan, id, p)
		w.MouseUnlock()
		if !wasClick {
			return
		}
		// Up-event coords are absolute (MouseLock delivery convention);
		// shift into widget-local space using the down-event offset.
		upX := e.MouseX - (p.StartX - p.LocalX)
		upY := e.MouseY - (p.StartY - p.LocalY)
		s := nsRead[MapState](w, nsState, id)
		vp := computeViewport(p.CanvasW, p.CanvasH, s)
		// Hit-test once per click. Walking Range forward and keeping
		// the last match makes the topmost (last-drawn) overlay win
		// without needing a reverse iterator — BoundedMap only exposes
		// forward order. Hoist the hit-test above the OnPOISelect-nil
		// gate so a Marker.OnClick still fires when the author elected
		// not to set the map-level selector.
		var hit Overlay
		readOverlays(w, id).Range(func(_ string, o Overlay) bool {
			if o.HitTest(vp, upX, upY) {
				hit = o
			}
			return true
		})
		// A marker hit is also a focus event: the clicked marker
		// becomes keyboard-focused and its InfoWindow opens (when
		// Title is set). Mirrors the Enter-on-focused-marker path so
		// click and keyboard converge on the same popup state. A
		// click into empty space (no overlay under the release) with
		// a popup open dismisses the popup — matches the "click
		// outside" gesture the plan lists alongside Escape and the
		// close button. Skip the write when nothing changed so a
		// second click on the already-focused marker, or an idle
		// click over empty water, never thrashes the state map.
		if m, ok := hit.(*Marker); ok {
			wantOpen := s.InfoOpen
			if m.Title != "" {
				wantOpen = true
			}
			// Reset sub-element focus when switching marker or
			// opening a fresh popup; preserve it on a re-click of
			// the already-open marker.
			wantIdx := s.InfoFocusIndex
			if wantOpen && (s.FocusedOverlayID != m.MarkerID || !s.InfoOpen) {
				wantIdx = 0
			}
			if s.FocusedOverlayID != m.MarkerID ||
				s.InfoOpen != wantOpen ||
				s.InfoFocusIndex != wantIdx {
				s.FocusedOverlayID = m.MarkerID
				s.InfoOpen = wantOpen
				s.InfoFocusIndex = wantIdx
				nsWrite(w, nsState, id, s)
			}
		} else if hit == nil && s.InfoOpen {
			s.InfoOpen = false
			s.InfoFocusIndex = 0
			nsWrite(w, nsState, id, s)
		}
		if hit != nil && c.OnPOISelect != nil {
			c.OnPOISelect(w, hit)
		}
		if m, ok := hit.(*Marker); ok && m.OnClick != nil {
			m.OnClick(w)
		}
		if c.OnClick != nil {
			c.OnClick(w, vp.screenToLatLng(upX, upY))
		}
	}
}

// onMouseScroll handles wheel zoom. Positive ScrollY zooms in;
// negative zooms out. The accumulator fires sub-ticks of
// scrollZoomStep so trackpad pixel-scroll produces smooth fractional
// zoom; a notch wheel at the default gain (1.0) lands on integer
// rest states because four sub-ticks sum to one full level. gain
// (from Cfg.ScrollZoomGain) scales ScrollY before it hits the
// accumulator, so gain < 1 yields fractional zoom even on notch
// hardware. Zoom pivots toward the cursor so the LatLng under the
// cursor stays fixed.
func onMouseScroll(id string, gain float32) func(*gui.Layout, *gui.Event, *gui.Window) {
	return func(l *gui.Layout, e *gui.Event, w *gui.Window) {
		if e.ScrollY == 0 {
			return
		}
		// Reject NaN/±Inf scroll deltas before they pollute the
		// accumulator — once stuck, every future event would yield
		// NaN+x = NaN and the wheel would silently stop zooming.
		if f := float64(e.ScrollY); math.IsNaN(f) || math.IsInf(f, 0) {
			return
		}
		acc := nsRead[float32](w, nsScroll, id) + e.ScrollY*gain
		delta, acc := scrollSteps(acc)
		nsWrite(w, nsScroll, id, acc)
		if delta == 0 {
			e.IsHandled = true
			return
		}

		s := nsRead[MapState](w, nsState, id)
		newZoom := clampZoom(s.Zoom + float64(delta))
		if srcMax := float64(baseMaxZoom(w, id)); newZoom > srcMax {
			newZoom = srcMax
		}
		if newZoom == s.Zoom {
			e.IsHandled = true
			return
		}
		newCtr := zoomToward(
			s, newZoom,
			e.MouseX, e.MouseY,
			l.Shape.Width, l.Shape.Height,
		)

		s.Center = newCtr
		s.Zoom = newZoom
		nsWrite(w, nsState, id, s)
		e.IsHandled = true
	}
}

// scrollZoomStep is the accumulator threshold at which one zoom
// sub-tick fires. At 0.25 a notch wheel (|ScrollY|≈1 per event) still
// lands on integer zoom after 4 sub-ticks while a trackpad pixel-
// scroll produces smooth fractional zoom per event. Keyboard +/-
// stays at integer deltas (see onKeyDown) so discoverable rest states
// remain reachable without wheel finesse.
const scrollZoomStep float32 = 0.25

// maxScrollAccum bounds the accumulator before it is consumed. A
// runaway scroll event (or many events between consumes) cannot make
// the computed delta dwarf the zoom range — the clampZoom downstream
// binds it to [0, maxZoomF] anyway, so capping is observable only to
// abusive input.
const maxScrollAccum float32 = 64

// scrollSteps consumes zoom sub-ticks from the accumulator and
// returns the fractional delta along with the residual. NaN flushes
// to zero (it cannot drive zoom and would otherwise re-enter the
// accumulator). Magnitudes are capped to maxScrollAccum so a single
// huge ScrollY cannot yield an excessive delta. Pure function —
// Window-free, testable.
func scrollSteps(acc float32) (delta, residual float32) {
	if math.IsNaN(float64(acc)) {
		return 0, 0
	}
	if acc > maxScrollAccum {
		acc = maxScrollAccum
	} else if acc < -maxScrollAccum {
		acc = -maxScrollAccum
	}
	steps := int32(acc / scrollZoomStep)
	delta = float32(steps) * scrollZoomStep
	residual = acc - delta
	return
}

// zoomToward returns the new map center so that the LatLng under the
// cursor at (cx, cy) stays fixed across the zoom transition. widgetW
// and widgetH are the canvas dimensions at the time of the event.
// Pure function — no Window or state-registry access — so the
// invariant is unit-testable. Fractional zoom routes through the
// F-variants; callers guarantee newZoom is clamp-safe.
func zoomToward(
	s MapState, newZoom float64,
	cx, cy, widgetW, widgetH float32,
) projection.LatLng {
	oldCtrPx := projection.ProjectF(s.Center, s.Zoom)
	cursorPxOld := projection.Point{
		X: oldCtrPx.X + float64(cx-widgetW/2),
		Y: oldCtrPx.Y + float64(cy-widgetH/2),
	}
	cursorLL := projection.UnprojectF(cursorPxOld, s.Zoom)
	cursorPxNew := projection.ProjectF(cursorLL, newZoom)
	newCtrPx := projection.Point{
		X: cursorPxNew.X - float64(cx-widgetW/2),
		Y: cursorPxNew.Y - float64(cy-widgetH/2),
	}
	return projection.UnprojectF(newCtrPx, newZoom).Clamp()
}

func onMouseMove(id string) func(*gui.Layout, *gui.Event, *gui.Window) {
	return func(_ *gui.Layout, e *gui.Event, w *gui.Window) {
		nsWrite(w, nsHover, id, hoverState{X: e.MouseX, Y: e.MouseY, Valid: true})
	}
}

// onMouseLeave clears the hover so the coord readout falls back to
// the map center, matching the convention "no cursor → center".
func onMouseLeave(id string) func(*gui.Layout, *gui.Event, *gui.Window) {
	return func(_ *gui.Layout, _ *gui.Event, w *gui.Window) {
		nsWrite(w, nsHover, id, hoverState{})
	}
}

// baseMaxZoom reports the zoom clamp for wheel / keyboard input: the
// smaller of maxZoom and the base layer's MaxZoom. Reference layers
// silently clip at higher Z; only the base constrains input.
func baseMaxZoom(w *gui.Window, id string) uint32 {
	l, ok := baseLayer(w, id)
	if !ok || l.Source == nil {
		return maxZoom
	}
	if z := l.Source.MaxZoom(); z < maxZoom {
		return z
	}
	return maxZoom
}

// onKeyDown handles keyboard navigation when the map has focus.
//
// Arrow keys pan by a fraction of the viewport (½ tile by default,
// 1 tile with Shift, ¼ tile with Ctrl). Plus/minus zoom from the
// viewport center. Home restores the Initial* seed. Tab/Shift-Tab
// cycle keyboard focus through Marker overlays once a marker is
// focused; from the viewport (no marker focused), Tab falls through
// to system focus so the user can leave the widget. Enter enters
// marker mode (picks the first marker) or, when already focused,
// fires OnPOISelect and opens the InfoWindow. Escape closes the
// InfoWindow, then (on a second press) exits marker mode.
func onKeyDown(c Cfg, seed MapState) func(*gui.Layout, *gui.Event, *gui.Window) {
	id := c.ID
	return func(_ *gui.Layout, e *gui.Event, w *gui.Window) {
		s := gui.StateReadOr[string, MapState](w, nsState, id, seed)
		if handleFocusKey(c, &s, e, w) {
			e.IsHandled = true
			return
		}
		step := float64(projection.TileSize) / 2
		switch {
		case e.Modifiers.Has(gui.ModShift):
			step = float64(projection.TileSize)
		case e.Modifiers.Has(gui.ModCtrl):
			step = float64(projection.TileSize) / 4
		}

		handled := true
		switch e.KeyCode {
		case gui.KeyLeft:
			s.Center = shiftCenter(s, -step, 0)
		case gui.KeyRight:
			s.Center = shiftCenter(s, step, 0)
		case gui.KeyUp:
			s.Center = shiftCenter(s, 0, -step)
		case gui.KeyDown:
			s.Center = shiftCenter(s, 0, step)
		case gui.KeyEqual, gui.KeyKPAdd:
			// Integer delta — slice 5a keeps keyboard and wheel on
			// whole-number steps. clampZoom enforces the ceiling;
			// baseMaxZoom adds a tighter per-source cap when set.
			if nz := clampZoom(s.Zoom + 1); nz <= float64(baseMaxZoom(w, id)) {
				s.Zoom = nz
			}
		case gui.KeyMinus, gui.KeyKPSubtract:
			if s.Zoom > 0 {
				s.Zoom = clampZoom(s.Zoom - 1)
			}
		case gui.KeyHome:
			s = seed
		default:
			handled = false
		}
		if handled {
			nsWrite(w, nsState, id, s)
			e.IsHandled = true
		}
	}
}

// handleFocusKey processes Tab, Enter, and Escape for marker-mode
// focus navigation. Returns true when the event was consumed.
//
// Owns its registry writes so callback-firing branches can persist
// pre-callback state (invariant: a callback that reads Snapshot must
// see the post-dismissal map state). Pre-refactor this was split
// between handleFocusKey and the caller, forcing a (consumed, wrote)
// tuple — now every consumed branch writes exactly once internally.
//
// When an InfoWindow popup is open, Tab/Shift-Tab trap focus inside the
// popup (cycling Actions then close), and Enter activates the
// sub-element at s.InfoFocusIndex. Marker cycling and system-focus
// escape are blocked until the popup is dismissed.
func handleFocusKey(c Cfg, s *MapState, e *gui.Event, w *gui.Window) bool {
	switch e.KeyCode {
	case gui.KeyTab:
		// Popup focus-trap takes priority. Without this short-circuit
		// Tab would walk to the next marker and close the popup,
		// violating the dialog contract.
		if s.InfoOpen {
			bm := readOverlays(w, c.ID)
			m := focusedMarker(bm, *s)
			if m == nil {
				s.InfoOpen = false
				s.InfoFocusIndex = 0
				nsWrite(w, nsState, c.ID, *s)
				return true
			}
			step := 1
			if e.Modifiers.Has(gui.ModShift) {
				step = -1
			}
			s.InfoFocusIndex = cycleInfoFocus(s.InfoFocusIndex, len(m.Actions), step)
			nsWrite(w, nsState, c.ID, *s)
			return true
		}
		ids := focusableMarkerIDs(readOverlays(w, c.ID))
		if len(ids) == 0 || s.FocusedOverlayID == "" {
			return false
		}
		step := 1
		if e.Modifiers.Has(gui.ModShift) {
			step = -1
		}
		s.FocusedOverlayID = nextFocusID(ids, s.FocusedOverlayID, step)
		nsWrite(w, nsState, c.ID, *s)
		return true
	case gui.KeyEnter:
		bm := readOverlays(w, c.ID)
		if s.InfoOpen {
			m := focusedMarker(bm, *s)
			if m == nil {
				s.InfoOpen = false
				s.InfoFocusIndex = 0
				nsWrite(w, nsState, c.ID, *s)
				return true
			}
			idx := int(s.InfoFocusIndex)
			// dispatchInfoAction persists state+fires cb; the mouse
			// path uses the same helper so the invariants stay aligned.
			dispatchInfoAction(w, c.ID, m, idx)
			// Reflect the dismissal in the caller's local snapshot too
			// (tests read s after handleFocusKey returns).
			s.InfoOpen = false
			s.InfoFocusIndex = 0
			return true
		}
		if s.FocusedOverlayID == "" {
			ids := focusableMarkerIDs(bm)
			if len(ids) == 0 {
				return false
			}
			s.FocusedOverlayID = ids[0]
			s.InfoOpen = false
			s.InfoFocusIndex = 0
			nsWrite(w, nsState, c.ID, *s)
			return true
		}
		m := focusedMarker(bm, *s)
		if m == nil {
			s.FocusedOverlayID = ""
			s.InfoOpen = false
			s.InfoFocusIndex = 0
			nsWrite(w, nsState, c.ID, *s)
			return true
		}
		if m.Title != "" {
			s.InfoOpen = true
			s.InfoFocusIndex = 0
		}
		// Persist before the callback so OnPOISelect consumers that
		// mutate map state (PanTo, SetZoom) are not clobbered.
		nsWrite(w, nsState, c.ID, *s)
		if c.OnPOISelect != nil {
			c.OnPOISelect(w, m)
		}
		return true
	case gui.KeyEscape:
		if s.InfoOpen {
			s.InfoOpen = false
			s.InfoFocusIndex = 0
			nsWrite(w, nsState, c.ID, *s)
			return true
		}
		if s.FocusedOverlayID != "" {
			s.FocusedOverlayID = ""
			nsWrite(w, nsState, c.ID, *s)
			return true
		}
		return false
	}
	return false
}

// cycleInfoFocus advances a popup-focus index by step, wrapping across
// the range [0, actionCount] where the trailing slot (== actionCount)
// is the close button. Input index is clamped first so a stale value
// that drifted past the current Action count still produces a sane next
// index. actionCount is clamped to MaxInfoActions so an author-supplied
// Actions slice longer than the draw cap cannot (a) silently wrap via
// int8 truncation or (b) let Tab land on a slot that won't render.
// Negative actionCount collapses to close-only (n=1). Pure —
// Window-free, unit-testable.
func cycleInfoFocus(current int8, actionCount, step int) int8 {
	if actionCount < 0 {
		actionCount = 0
	} else if actionCount > MaxInfoActions {
		actionCount = MaxInfoActions
	}
	n := actionCount + 1 // +1 for the close-button slot
	i := int(current)
	if i < 0 || i >= n {
		i = 0
	}
	i = (i + step) % n
	if i < 0 {
		i += n
	}
	return int8(i)
}

// shiftCenter translates s.Center by (dx, dy) screen-pixels at the
// current fractional zoom.
func shiftCenter(s MapState, dx, dy float64) projection.LatLng {
	p := projection.ProjectF(s.Center, s.Zoom)
	p.X += dx
	p.Y += dy
	return projection.UnprojectF(p, s.Zoom).Clamp()
}
