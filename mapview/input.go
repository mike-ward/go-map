package mapview

import (
	"github.com/mike-ward/go-gui/gui"
	"github.com/mike-ward/go-map/projection"
	"github.com/mike-ward/go-map/tile"
)

// onClick starts a drag-pan session. OnClick fires on mouse-down for
// DrawCanvas; MouseLock takes over subsequent mouse-move and
// mouse-up events so the drag survives cursor travel outside the
// widget bounds.
func onClick(id string, src tile.Source) func(*gui.Layout, *gui.Event, *gui.Window) {
	return func(l *gui.Layout, e *gui.Event, w *gui.Window) {
		s := gui.StateReadOr[string, MapState](w, nsState, id, MapState{})
		// OnClick delivers widget-local coords; MouseLock callbacks
		// deliver absolute coords. Store absolute to keep the delta
		// math single-space across the drag.
		writePan(w, id, panState{
			Active:    true,
			StartX:    e.MouseX + l.Shape.X,
			StartY:    e.MouseY + l.Shape.Y,
			StartCtr:  s.Center,
			StartZoom: s.Zoom,
		})
		w.MouseLock(gui.MouseLockCfg{
			MouseMove: panDragMove(id),
			MouseUp:   panDragEnd(id),
		})
		e.IsHandled = true
		_ = src
	}
}

func panDragMove(id string) func(*gui.Layout, *gui.Event, *gui.Window) {
	return func(_ *gui.Layout, e *gui.Event, w *gui.Window) {
		p := readPan(w, id)
		if !p.Active {
			return
		}
		// Convert screen-pixel delta to world-pixel delta at the
		// drag-start zoom. Inverting the sign gives "content follows
		// cursor" feel.
		dx := p.StartX - e.MouseX
		dy := p.StartY - e.MouseY
		startPt := projection.Project(p.StartCtr, p.StartZoom)
		newCtr := projection.Unproject(projection.Point{
			X: startPt.X + float64(dx),
			Y: startPt.Y + float64(dy),
		}, p.StartZoom)

		sm := gui.StateMap[string, MapState](w, nsState, capMaps)
		s, _ := sm.Get(id)
		s.Center = newCtr.Clamp()
		sm.Set(id, s)
		e.IsHandled = true
	}
}

func panDragEnd(id string) func(*gui.Layout, *gui.Event, *gui.Window) {
	return func(_ *gui.Layout, _ *gui.Event, w *gui.Window) {
		p := readPan(w, id)
		p.Active = false
		writePan(w, id, p)
		w.MouseUnlock()
	}
}

// onMouseScroll handles wheel zoom. Positive scrollY zooms in;
// negative zooms out. Zoom pivots toward the cursor so the LatLng
// under the cursor stays fixed across the transition.
func onMouseScroll(id string, src tile.Source) func(*gui.Layout, *gui.Event, *gui.Window) {
	return func(l *gui.Layout, e *gui.Event, w *gui.Window) {
		if e.ScrollY == 0 {
			return
		}
		delta := int32(1)
		if e.ScrollY < 0 {
			delta = -1
		}
		s := gui.StateReadOr[string, MapState](w, nsState, id, MapState{})
		newZoom := int32(s.Zoom) + delta
		if newZoom < 0 {
			newZoom = 0
		}
		if maxFromSrc := int32(sourceMaxZoom(src)); newZoom > maxFromSrc {
			newZoom = maxFromSrc
		}
		if uint32(newZoom) == s.Zoom {
			return
		}
		// Cursor anchor: screen coords are relative to layout.Shape
		// after dispatch; mouse coords arrive widget-local.
		cx := e.MouseX
		cy := e.MouseY
		widgetW := l.Shape.Width
		widgetH := l.Shape.Height
		// Recompute center so the LatLng under the cursor is stable.
		// World-pixel scale doubles per zoom step.
		oldCtrPx := projection.Project(s.Center, s.Zoom)
		// LatLng under cursor at old zoom
		cursorPxOld := projection.Point{
			X: oldCtrPx.X + float64(cx-widgetW/2),
			Y: oldCtrPx.Y + float64(cy-widgetH/2),
		}
		cursorLL := projection.Unproject(cursorPxOld, s.Zoom)
		// Recompute new center: cursorLL's new world-px minus the
		// same screen offset.
		cursorPxNew := projection.Project(cursorLL, uint32(newZoom))
		newCtrPx := projection.Point{
			X: cursorPxNew.X - float64(cx-widgetW/2),
			Y: cursorPxNew.Y - float64(cy-widgetH/2),
		}
		newCtr := projection.Unproject(newCtrPx, uint32(newZoom)).Clamp()

		sm := gui.StateMap[string, MapState](w, nsState, capMaps)
		s.Center = newCtr
		s.Zoom = uint32(newZoom)
		sm.Set(id, s)
		e.IsHandled = true
	}
}

// onMouseMove records the hover position for the coord readout.
func onMouseMove(id string) func(*gui.Layout, *gui.Event, *gui.Window) {
	return func(_ *gui.Layout, e *gui.Event, w *gui.Window) {
		writeHover(w, id, hoverState{X: e.MouseX, Y: e.MouseY, Valid: true})
	}
}

// onMouseLeave clears the hover so the coord readout falls back to
// the map center.
func onMouseLeave(id string) func(*gui.Layout, *gui.Event, *gui.Window) {
	return func(_ *gui.Layout, _ *gui.Event, w *gui.Window) {
		writeHover(w, id, hoverState{})
	}
}

// sourceMaxZoom returns the tile source's cap or the global maxZoom
// when no source is configured.
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
			gui.StateMap[string, MapState](w, nsState, capMaps).Set(id, s)
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
