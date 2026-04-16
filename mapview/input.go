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
		// Hit-test once per click — nesting the loop inside an
		// OnPOISelect-nil gate would mask a Marker.OnClick set on an
		// overlay when the author elected not to set the map-level
		// selector.
		var hit Overlay
		for _, o := range readOverlays(w, id) {
			if o.HitTest(vp, upX, upY) {
				hit = o
				break
			}
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
// viewport center. Home restores the Initial* seed.
func onKeyDown(id string, src tile.Source, seed MapState) func(*gui.Layout, *gui.Event, *gui.Window) {
	return func(_ *gui.Layout, e *gui.Event, w *gui.Window) {
		s := gui.StateReadOr[string, MapState](w, nsState, id, seed)
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

// shiftCenter translates s.Center by (dx, dy) screen-pixels at the
// current zoom.
func shiftCenter(s MapState, dx, dy float64) projection.LatLng {
	p := projection.Project(s.Center, s.Zoom)
	p.X += dx
	p.Y += dy
	return projection.Unproject(p, s.Zoom).Clamp()
}
