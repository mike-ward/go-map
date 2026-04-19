package mapview

import (
	"github.com/mike-ward/go-gui/gui"
)

// focusableMarkerIDs returns the IDs of overlays that participate in
// keyboard focus cycling. Only *Marker overlays qualify; polylines,
// polygons, and circles are skipped. Order mirrors BoundedMap.Range
// insertion order so Tab/Shift-Tab walk the same sequence as Draw and
// hit-test. Returns nil when no markers are registered.
func focusableMarkerIDs(bm *gui.BoundedMap[string, Overlay]) []string {
	if bm == nil {
		return nil
	}
	// Pre-size to total overlay count so the append loop never grows.
	// Over-allocates when the map holds non-Marker overlays; trades a
	// little transient memory for zero reallocation on the keypress
	// path. Trim only matters if the caller retains the slice, which
	// no current caller does.
	ids := make([]string, 0, bm.Len())
	bm.Range(func(k string, o Overlay) bool {
		if _, ok := o.(*Marker); ok {
			ids = append(ids, k)
		}
		return true
	})
	return ids
}

// nextFocusID picks the successor to current in ids when stepping by
// step (+1 forward, -1 back). Wraps at both ends. Returns "" when ids
// is empty; returns ids[0] when current is absent (first-time focus).
// Pure function so cycling semantics stay unit-testable without a
// Window or BoundedMap.
func nextFocusID(ids []string, current string, step int) string {
	n := len(ids)
	if n == 0 {
		return ""
	}
	idx := -1
	for i, id := range ids {
		if id == current {
			idx = i
			break
		}
	}
	if idx < 0 {
		if step >= 0 {
			return ids[0]
		}
		return ids[n-1]
	}
	next := (idx + step) % n
	if next < 0 {
		next += n
	}
	return ids[next]
}

// focusedMarker returns the currently focused Marker, or nil when no
// Marker matches s.FocusedOverlayID. A stale ID (marker removed while
// focused) silently resolves to nil; callers should treat that as
// "no focus" and clear the state slot on the next write.
func focusedMarker(bm *gui.BoundedMap[string, Overlay], s MapState) *Marker {
	if s.FocusedOverlayID == "" || bm == nil {
		return nil
	}
	o, ok := bm.Get(s.FocusedOverlayID)
	if !ok {
		return nil
	}
	m, ok := o.(*Marker)
	if !ok {
		return nil
	}
	return m
}
