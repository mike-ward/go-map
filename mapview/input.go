package mapview

import (
	"math"

	"github.com/mike-ward/go-gui/gui"
	"github.com/mike-ward/go-map/projection"
	"github.com/mike-ward/go-map/tile"
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
		m := markerByID(w, id, rect.MarkerID)
		closeInfoPopup(w, id)
		// Belt-and-suspenders bounds: hit() already clamps against
		// MaxInfoActions, but re-checking here means a future refactor
		// of either side cannot drive an OOB index on Marker.Actions.
		if m != nil && h.Index >= 0 && h.Index < MaxInfoActions &&
			h.Index < len(m.Actions) {
			if cb := m.Actions[h.Index].OnClick; cb != nil {
				cb(w)
			}
		}
		e.IsHandled = true
		return true
	}
	return false
}

// closeInfoPopup flips InfoOpen off on the map state. No-op when the
// popup is already closed so a second close press on a race does not
// dirty the state map.
func closeInfoPopup(w *gui.Window, id string) {
	s := nsRead[MapState](w, nsState, id)
	if !s.InfoOpen {
		return
	}
	s.InfoOpen = false
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
		// cursor" feel.
		startPt := projection.Project(p.StartCtr, p.StartZoom)
		newCtr := projection.Unproject(projection.Point{
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
			if s.FocusedOverlayID != m.MarkerID || s.InfoOpen != wantOpen {
				s.FocusedOverlayID = m.MarkerID
				s.InfoOpen = wantOpen
				nsWrite(w, nsState, id, s)
			}
		} else if hit == nil && s.InfoOpen {
			s.InfoOpen = false
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
// negative zooms out. Fractional scroll (trackpad pixel-scroll)
// accumulates until it crosses scrollZoomStep, then fires one zoom
// tick per crossing — keeping the integer-zoom MVP responsive to
// either notch wheels or smooth trackpads. Zoom pivots toward the
// cursor so the LatLng under the cursor stays fixed.
func onMouseScroll(id string, src tile.Source) func(*gui.Layout, *gui.Event, *gui.Window) {
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
		acc := nsRead[float32](w, nsScroll, id) + e.ScrollY
		delta, acc := scrollSteps(acc)
		nsWrite(w, nsScroll, id, acc)
		if delta == 0 {
			e.IsHandled = true
			return
		}

		s := nsRead[MapState](w, nsState, id)
		newZoom := int32(s.Zoom) + delta
		if newZoom < 0 {
			newZoom = 0
		}
		if maxFromSrc := int32(sourceMaxZoom(src)); newZoom > maxFromSrc {
			newZoom = maxFromSrc
		}
		if uint32(newZoom) == s.Zoom {
			e.IsHandled = true
			return
		}
		newCtr := zoomToward(
			s, uint32(newZoom),
			e.MouseX, e.MouseY,
			l.Shape.Width, l.Shape.Height,
		)

		s.Center = newCtr
		s.Zoom = uint32(newZoom)
		nsWrite(w, nsState, id, s)
		e.IsHandled = true
	}
}

// scrollZoomStep is the accumulator threshold at which one zoom tick
// fires. Mouse-wheel notches typically deliver |ScrollY|≥1 per event
// and zoom on contact; trackpad pixel-scroll delivers small
// increments and integrates over several events.
const scrollZoomStep float32 = 1.0

// maxScrollAccum bounds the accumulator before it is consumed. A
// runaway scroll event (or many events between consumes) cannot make
// the loop body iterate more than this many times — the user only
// gets to zoom one step per tick anyway, so capping is observable
// only to abusive input.
const maxScrollAccum float32 = 64

// scrollSteps consumes integer zoom ticks from the accumulator and
// returns the residual. NaN flushes to zero (it cannot drive zoom
// and would otherwise re-enter the accumulator). Magnitudes are
// capped to maxScrollAccum so a single huge ScrollY cannot let the
// computed delta dwarf int32. Pure function — Window-free, testable.
func scrollSteps(acc float32) (delta int32, residual float32) {
	if math.IsNaN(float64(acc)) {
		return 0, 0
	}
	if acc > maxScrollAccum {
		acc = maxScrollAccum
	} else if acc < -maxScrollAccum {
		acc = -maxScrollAccum
	}
	delta = int32(acc / scrollZoomStep)
	residual = acc - float32(delta)*scrollZoomStep
	return
}

// zoomToward returns the new map center so that the LatLng under the
// cursor at (cx, cy) stays fixed across the zoom transition. widgetW
// and widgetH are the canvas dimensions at the time of the event.
// Pure function — no Window or state-registry access — so the
// invariant is unit-testable.
func zoomToward(
	s MapState, newZoom uint32,
	cx, cy, widgetW, widgetH float32,
) projection.LatLng {
	oldCtrPx := projection.Project(s.Center, s.Zoom)
	cursorPxOld := projection.Point{
		X: oldCtrPx.X + float64(cx-widgetW/2),
		Y: oldCtrPx.Y + float64(cy-widgetH/2),
	}
	cursorLL := projection.Unproject(cursorPxOld, s.Zoom)
	cursorPxNew := projection.Project(cursorLL, newZoom)
	newCtrPx := projection.Point{
		X: cursorPxNew.X - float64(cx-widgetW/2),
		Y: cursorPxNew.Y - float64(cy-widgetH/2),
	}
	return projection.Unproject(newCtrPx, newZoom).Clamp()
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

func sourceMaxZoom(src tile.Source) uint32 {
	if src == nil {
		return maxZoom
	}
	if z := src.MaxZoom(); z < maxZoom {
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
	id, src := c.ID, c.Source
	return func(_ *gui.Layout, e *gui.Event, w *gui.Window) {
		s := gui.StateReadOr[string, MapState](w, nsState, id, seed)
		if handleFocusKey(c, &s, e, w) {
			nsWrite(w, nsState, id, s)
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
			if nz := s.Zoom + 1; nz <= sourceMaxZoom(src) {
				s.Zoom = nz
			}
		case gui.KeyMinus, gui.KeyKPSubtract:
			if s.Zoom > 0 {
				s.Zoom--
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
// focus navigation. Returns true when the event was consumed; callers
// persist the mutated MapState and flag the event handled. Pure apart
// from the OnPOISelect callback fire — reading markers through the
// overlay registry keeps the state write in onKeyDown for uniformity.
func handleFocusKey(c Cfg, s *MapState, e *gui.Event, w *gui.Window) bool {
	switch e.KeyCode {
	case gui.KeyTab:
		ids := focusableMarkerIDs(readOverlays(w, c.ID))
		if len(ids) == 0 || s.FocusedOverlayID == "" {
			// No markers, or viewport mode — let system focus advance.
			return false
		}
		step := 1
		if e.Modifiers.Has(gui.ModShift) {
			step = -1
		}
		s.FocusedOverlayID = nextFocusID(ids, s.FocusedOverlayID, step)
		s.InfoOpen = false
		return true
	case gui.KeyEnter:
		bm := readOverlays(w, c.ID)
		if s.FocusedOverlayID == "" {
			ids := focusableMarkerIDs(bm)
			if len(ids) == 0 {
				return false
			}
			s.FocusedOverlayID = ids[0]
			s.InfoOpen = false
			return true
		}
		m := focusedMarker(bm, *s)
		if m == nil {
			// Stale focus: overlay removed. Reset and let the next
			// keypress start over cleanly.
			s.FocusedOverlayID = ""
			s.InfoOpen = false
			return true
		}
		if m.Title != "" {
			s.InfoOpen = true
		}
		if c.OnPOISelect != nil {
			c.OnPOISelect(w, m)
		}
		return true
	case gui.KeyEscape:
		if s.InfoOpen {
			s.InfoOpen = false
			return true
		}
		if s.FocusedOverlayID != "" {
			s.FocusedOverlayID = ""
			return true
		}
		return false
	}
	return false
}

// shiftCenter translates s.Center by (dx, dy) screen-pixels at the
// current zoom.
func shiftCenter(s MapState, dx, dy float64) projection.LatLng {
	p := projection.Project(s.Center, s.Zoom)
	p.X += dx
	p.Y += dy
	return projection.Unproject(p, s.Zoom).Clamp()
}
