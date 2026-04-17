package mapview

import (
	"math"
	"strings"
	"testing"

	"github.com/mike-ward/go-gui/gui"
	"github.com/mike-ward/go-map/projection"
)

func TestNextFocusID(t *testing.T) {
	cases := []struct {
		name    string
		ids     []string
		current string
		step    int
		want    string
	}{
		{"empty", nil, "a", 1, ""},
		{"wrap_forward", []string{"a", "b", "c"}, "c", 1, "a"},
		{"wrap_back", []string{"a", "b", "c"}, "a", -1, "c"},
		{"advance_forward", []string{"a", "b", "c"}, "b", 1, "c"},
		{"advance_back", []string{"a", "b", "c"}, "b", -1, "a"},
		{"missing_current_forward", []string{"a", "b"}, "zz", 1, "a"},
		{"missing_current_back", []string{"a", "b"}, "zz", -1, "b"},
		{"single_element", []string{"only"}, "only", 1, "only"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := nextFocusID(c.ids, c.current, c.step); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}

// TestFocusableMarkerIDs_FiltersNonMarkers: only *Marker overlays
// participate in keyboard focus; Polyline/Polygon/Circle registrations
// must not leak into the Tab cycle.
func TestFocusableMarkerIDs_FiltersNonMarkers(t *testing.T) {
	bm := gui.NewBoundedMap[string, Overlay](16)
	bm.Set("poly", &Polygon{PolyID: "poly", Ring: []projection.LatLng{{}, {}, {}}})
	bm.Set("m1", &Marker{MarkerID: "m1"})
	bm.Set("line", &Polyline{LineID: "line", Points: []projection.LatLng{{}, {}}})
	bm.Set("m2", &Marker{MarkerID: "m2"})
	bm.Set("circle", &Circle{CircleID: "circle", RadiusMeters: 10})

	got := focusableMarkerIDs(bm)
	want := []string{"m1", "m2"}
	if len(got) != len(want) {
		t.Fatalf("len got %d want %d: %v", len(got), len(want), got)
	}
	for i, id := range want {
		if got[i] != id {
			t.Fatalf("index %d: got %q want %q", i, got[i], id)
		}
	}
}

// TestFocusableMarkerIDs_NilBM returns nil, not a panic.
func TestFocusableMarkerIDs_NilBM(t *testing.T) {
	if got := focusableMarkerIDs(nil); got != nil {
		t.Fatalf("got %v want nil", got)
	}
}

// TestFocusedMarker_StaleID: a FocusedOverlayID that no longer matches
// a registered marker resolves to nil so callers can treat stale focus
// as "no focus" without a panic.
func TestFocusedMarker_StaleID(t *testing.T) {
	bm := gui.NewBoundedMap[string, Overlay](16)
	bm.Set("m1", &Marker{MarkerID: "m1"})
	if m := focusedMarker(bm, MapState{FocusedOverlayID: "gone"}); m != nil {
		t.Fatalf("want nil for stale id, got %+v", m)
	}
	if m := focusedMarker(bm, MapState{FocusedOverlayID: "m1"}); m == nil {
		t.Fatalf("want marker, got nil")
	}
}

// TestHandleFocusKey_EnterEntersMarkerMode: from viewport mode, Enter
// selects the first marker and does not open the popup.
func TestHandleFocusKey_EnterEntersMarkerMode(t *testing.T) {
	w := &gui.Window{}
	AddOverlay(w, "m", &Marker{MarkerID: "first", Title: "One"})
	AddOverlay(w, "m", &Marker{MarkerID: "second", Title: "Two"})
	s := MapState{}
	e := &gui.Event{KeyCode: gui.KeyEnter}
	if !handleFocusKey(Cfg{ID: "m"}, &s, e, w) {
		t.Fatalf("want event consumed")
	}
	if s.FocusedOverlayID != "first" {
		t.Fatalf("focused id: got %q want %q", s.FocusedOverlayID, "first")
	}
	if s.InfoOpen {
		t.Fatalf("InfoOpen should stay false on first Enter")
	}
}

// TestHandleFocusKey_EnterOpensInfo: second Enter opens the popup and
// fires OnPOISelect exactly once.
func TestHandleFocusKey_EnterOpensInfo(t *testing.T) {
	w := &gui.Window{}
	AddOverlay(w, "m", &Marker{MarkerID: "first", Title: "One"})
	var selected Overlay
	var fires int
	c := Cfg{ID: "m", OnPOISelect: func(_ *gui.Window, o Overlay) {
		selected = o
		fires++
	}}
	s := MapState{FocusedOverlayID: "first"}
	e := &gui.Event{KeyCode: gui.KeyEnter}
	if !handleFocusKey(c, &s, e, w) {
		t.Fatalf("want event consumed")
	}
	if !s.InfoOpen {
		t.Fatalf("InfoOpen should be true after Enter on focused marker")
	}
	if fires != 1 || selected == nil || selected.ID() != "first" {
		t.Fatalf("OnPOISelect fired %d times, selected %v", fires, selected)
	}
}

// TestHandleFocusKey_EnterNoMarkersDoesNotConsume: with no markers
// registered, Enter must not be swallowed so downstream handlers can
// still react.
func TestHandleFocusKey_EnterNoMarkersDoesNotConsume(t *testing.T) {
	w := &gui.Window{}
	s := MapState{}
	e := &gui.Event{KeyCode: gui.KeyEnter}
	if handleFocusKey(Cfg{ID: "m"}, &s, e, w) {
		t.Fatalf("want event not consumed when no markers")
	}
	if s.FocusedOverlayID != "" {
		t.Fatalf("no focus change expected, got %q", s.FocusedOverlayID)
	}
}

// TestHandleFocusKey_TabCycles: with the popup closed, Tab walks
// forward through markers, Shift-Tab walks back, both wrap.
func TestHandleFocusKey_TabCycles(t *testing.T) {
	w := &gui.Window{}
	for _, id := range []string{"a", "b", "c"} {
		AddOverlay(w, "m", &Marker{MarkerID: id, Title: id})
	}
	s := MapState{FocusedOverlayID: "a"}

	cases := []struct {
		name string
		ev   *gui.Event
		from string
		want string
	}{
		{"tab_forward", &gui.Event{KeyCode: gui.KeyTab}, "a", "b"},
		{"tab_wrap", &gui.Event{KeyCode: gui.KeyTab}, "c", "a"},
		{"shift_tab_back", &gui.Event{KeyCode: gui.KeyTab, Modifiers: gui.ModShift}, "b", "a"},
		{"shift_tab_wrap", &gui.Event{KeyCode: gui.KeyTab, Modifiers: gui.ModShift}, "a", "c"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s.FocusedOverlayID = c.from
			s.InfoOpen = false
			if !handleFocusKey(Cfg{ID: "m"}, &s, c.ev, w) {
				t.Fatalf("want consumed")
			}
			if s.FocusedOverlayID != c.want {
				t.Fatalf("got %q want %q", s.FocusedOverlayID, c.want)
			}
			if s.InfoOpen {
				t.Fatalf("marker-cycling Tab must leave InfoOpen unchanged")
			}
		})
	}
}

// TestHandleFocusKey_TabViewportPassthrough: Tab from viewport mode
// must not be consumed so system focus can advance off the map.
func TestHandleFocusKey_TabViewportPassthrough(t *testing.T) {
	w := &gui.Window{}
	AddOverlay(w, "m", &Marker{MarkerID: "a"})
	s := MapState{}
	e := &gui.Event{KeyCode: gui.KeyTab}
	if handleFocusKey(Cfg{ID: "m"}, &s, e, w) {
		t.Fatalf("viewport-mode Tab must not be consumed")
	}
	if s.FocusedOverlayID != "" {
		t.Fatalf("focused id must remain empty, got %q", s.FocusedOverlayID)
	}
}

// TestHandleFocusKey_EscapeClosesInfoThenExits: first Escape closes
// the popup, second exits marker mode, third is passed through.
func TestHandleFocusKey_EscapeClosesInfoThenExits(t *testing.T) {
	w := &gui.Window{}
	AddOverlay(w, "m", &Marker{MarkerID: "a", Title: "A"})
	s := MapState{FocusedOverlayID: "a", InfoOpen: true}
	e := &gui.Event{KeyCode: gui.KeyEscape}

	if !handleFocusKey(Cfg{ID: "m"}, &s, e, w) {
		t.Fatalf("first Escape must be consumed")
	}
	if s.InfoOpen || s.FocusedOverlayID != "a" {
		t.Fatalf("after first esc: InfoOpen=%v focus=%q", s.InfoOpen, s.FocusedOverlayID)
	}

	if !handleFocusKey(Cfg{ID: "m"}, &s, e, w) {
		t.Fatalf("second Escape must be consumed")
	}
	if s.FocusedOverlayID != "" {
		t.Fatalf("second esc should clear focus, got %q", s.FocusedOverlayID)
	}

	if handleFocusKey(Cfg{ID: "m"}, &s, e, w) {
		t.Fatalf("third Escape must not be consumed (no focus state)")
	}
}

// TestHandleFocusKey_EnterStaleFocusResets: overlay removed while
// focused -> Enter drops the stale ID without a panic, consuming the
// event so the user sees an immediate state change.
func TestHandleFocusKey_EnterStaleFocusResets(t *testing.T) {
	w := &gui.Window{}
	s := MapState{FocusedOverlayID: "ghost"}
	e := &gui.Event{KeyCode: gui.KeyEnter}
	if !handleFocusKey(Cfg{ID: "m"}, &s, e, w) {
		t.Fatalf("stale-focus Enter must be consumed")
	}
	if s.FocusedOverlayID != "" || s.InfoOpen {
		t.Fatalf("stale focus must reset; got focus=%q info=%v",
			s.FocusedOverlayID, s.InfoOpen)
	}
}

// TestHandleFocusKey_EnterSuppressesInfoForTitlelessMarker: a marker
// with no Title announces selection but must not open a blank popup.
func TestHandleFocusKey_EnterSuppressesInfoForTitlelessMarker(t *testing.T) {
	w := &gui.Window{}
	AddOverlay(w, "m", &Marker{MarkerID: "a", Label: "decorative"})
	s := MapState{FocusedOverlayID: "a"}
	e := &gui.Event{KeyCode: gui.KeyEnter}
	if !handleFocusKey(Cfg{ID: "m"}, &s, e, w) {
		t.Fatalf("want consumed")
	}
	if s.InfoOpen {
		t.Fatalf("titleless marker must not open InfoWindow")
	}
}

// TestMarkerA11YText_Fallbacks: verifies the Title → Label → ID
// precedence and the Title+Body join when both are present. Ensures
// decorative markers still announce something and a blank marker
// degrades to its stable ID.
func TestMarkerA11YText_Fallbacks(t *testing.T) {
	cases := []struct {
		name string
		m    Marker
		want string
	}{
		{"title_and_body", Marker{MarkerID: "a", Title: "T", Body: "B"}, "T, B"},
		{"title_only", Marker{MarkerID: "a", Title: "T"}, "T"},
		{"label_only", Marker{MarkerID: "a", Label: "L"}, "L"},
		{"id_fallback", Marker{MarkerID: "a"}, "a"},
		{"title_beats_label", Marker{MarkerID: "a", Title: "T", Label: "L"}, "T"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := c.m
			if got := markerA11YText(&m); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}

// TestMarkerA11YText_TruncatesLongFields: a pathological Title or
// Body (or Label/ID fallback) must be capped so the A11Y string never
// carries the full pathological payload downstream each frame.
func TestMarkerA11YText_TruncatesLongFields(t *testing.T) {
	huge := strings.Repeat("x", maxInfoBodyBytes*4)
	m := &Marker{MarkerID: "a", Title: huge, Body: huge}
	got := markerA11YText(m)
	// Upper bound: truncated title + ", " + truncated body + two ellipsis
	// characters ("…" = 3 bytes each). Add a generous headroom.
	maxLen := maxInfoTitleBytes + maxInfoBodyBytes + 16
	if len(got) > maxLen {
		t.Fatalf("len=%d exceeds cap=%d", len(got), maxLen)
	}
	if !strings.Contains(got, "…") {
		t.Fatalf("truncated output must contain ellipsis: %q", got)
	}

	// Label fallback path truncates too.
	mLabel := &Marker{MarkerID: "a", Label: huge}
	if got := markerA11YText(mLabel); len(got) > maxInfoBodyBytes+4 {
		t.Fatalf("label fallback len=%d exceeds cap", len(got))
	}

	// MarkerID fallback path truncates too.
	mID := &Marker{MarkerID: huge}
	if got := markerA11YText(mID); len(got) > maxInfoBodyBytes+4 {
		t.Fatalf("id fallback len=%d exceeds cap", len(got))
	}
}

// TestInfoRectHit_Boundaries: Valid=false never hits; coords exactly
// at the top-left corner hit (half-open convention includes origin);
// coords exactly at the bottom-right corner miss (half-open excludes
// far edge); NaN coords never hit (safe-by-default for float compares).
// Tests only the body rect here (no close, no actions) — close/action
// priority and multi-rect dispatch live in TestInfoRectHit_Priority.
func TestInfoRectHit_Boundaries(t *testing.T) {
	r := infoRectState{X: 10, Y: 20, W: 100, H: 40, Valid: true}
	cases := []struct {
		name   string
		px, py float32
		want   infoHitKind
	}{
		{"inside", 50, 30, infoHitBody},
		{"top_left_corner_inclusive", 10, 20, infoHitBody},
		{"bottom_right_corner_exclusive", 110, 60, infoHitMiss},
		{"right_edge_excluded", 110, 30, infoHitMiss},
		{"bottom_edge_excluded", 50, 60, infoHitMiss},
		{"left_of_rect", 9, 30, infoHitMiss},
		{"above_rect", 50, 19, infoHitMiss},
		{"nan_x", float32(math.NaN()), 30, infoHitMiss},
		{"nan_y", 50, float32(math.NaN()), infoHitMiss},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := r.hit(c.px, c.py); got.Kind != c.want {
				t.Fatalf("got %v want %v", got.Kind, c.want)
			}
		})
	}
	if (infoRectState{}).hit(0, 0).Kind != infoHitMiss {
		t.Fatal("Valid=false must never hit")
	}
	if (infoRectState{X: 10, Y: 10, W: 10, H: 10}).hit(15, 15).Kind != infoHitMiss {
		t.Fatal("Valid=false (with non-zero geometry) must never hit")
	}
}

// TestTruncateUTF8: hardens text-rendering inputs. Short strings pass
// through; oversize inputs truncate at a rune boundary; multibyte
// runes never split; negative and zero limits are tolerated.
func TestTruncateUTF8(t *testing.T) {
	cases := []struct {
		name, in, want string
		limit          int
	}{
		{"short_unchanged", "hi", "hi", 10},
		{"at_limit_unchanged", "hello", "hello", 5},
		{"ascii_truncates", "hello world", "hello…", 5},
		{"multibyte_boundary", "héllo", "h…", 2},
		{"negative_limit", "abc", "…", -1},
		{"zero_limit", "abc", "…", 0},
		{"empty_string", "", "", 10},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := truncateUTF8(c.in, c.limit); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}

// TestIsFiniteF32: guards the drawFocus NaN/Inf check against regressions.
func TestIsFiniteF32(t *testing.T) {
	cases := []struct {
		name string
		v    float32
		want bool
	}{
		{"zero", 0, true},
		{"finite_positive", 123.5, true},
		{"finite_negative", -1e6, true},
		{"nan", float32(math.NaN()), false},
		{"pos_inf", float32(math.Inf(1)), false},
		{"neg_inf", float32(math.Inf(-1)), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isFiniteF32(c.v); got != c.want {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

// TestStateForA11Y_IncludesFocusedMarker: the default A11Y description
// must prepend marker info once focused so screen-reader announcement
// order matches user intent.
func TestStateForA11Y_IncludesFocusedMarker(t *testing.T) {
	s := MapState{
		Center:           projection.LatLng{Lat: 45.52, Lng: -122.67},
		Zoom:             12,
		FocusedOverlayID: "pdx",
		InfoOpen:         true,
	}
	m := &Marker{MarkerID: "pdx", Title: "Portland", Body: "Home"}
	got := stateForA11Y(s, m)
	for _, want := range []string{"Portland", "Home", "Info window open", "zoom level 12"} {
		if !strings.Contains(got, want) {
			t.Fatalf("a11y text missing %q: %q", want, got)
		}
	}
}

// TestStateForA11Y_PopupFocusAnnouncement: focused sub-element text
// (action label or "Close button") must appear in the announcement so
// Tab presses produce audible state changes for screen-reader users.
func TestStateForA11Y_PopupFocusAnnouncement(t *testing.T) {
	m := &Marker{
		MarkerID: "pdx",
		Title:    "Portland",
		Actions: []InfoWindowAction{
			{Label: "Zoom in"},
			{Label: "Reset"},
		},
	}
	base := MapState{FocusedOverlayID: "pdx", InfoOpen: true}

	cases := []struct {
		name    string
		idx     int8
		want    string
		notWant string
	}{
		{"action_0", 0, "Action focused: Zoom in", "Close button focused"},
		{"action_1", 1, "Action focused: Reset", "Close button focused"},
		{"close_slot", 2, "Close button focused", "Action focused"},
		{"stale_out_of_range", 9, "Info window open", "Action focused"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := base
			s.InfoFocusIndex = c.idx
			got := stateForA11Y(s, m)
			if !strings.Contains(got, c.want) {
				t.Fatalf("missing %q: %q", c.want, got)
			}
			if c.notWant != "" && strings.Contains(got, c.notWant) {
				t.Fatalf("unexpected %q in: %q", c.notWant, got)
			}
		})
	}
}

// TestCycleInfoFocus: wrap-around across [0, actionCount] where the
// trailing slot is the close button. Stale indices drifting past the
// cap snap back into range so the next step lands somewhere sensible.
func TestCycleInfoFocus(t *testing.T) {
	cases := []struct {
		name        string
		current     int8
		actionCount int
		step        int
		want        int8
	}{
		{"forward_first_action", 0, 2, 1, 1},  // action_0 -> action_1
		{"forward_to_close", 1, 2, 1, 2},      // action_1 -> close
		{"forward_wrap", 2, 2, 1, 0},          // close -> action_0
		{"back_wrap", 0, 2, -1, 2},            // action_0 -> close
		{"back_one", 2, 2, -1, 1},             // close -> action_1
		{"no_actions_forward", 0, 0, 1, 0},    // close only, wrap to self
		{"no_actions_back", 0, 0, -1, 0},      // close only, wrap to self
		{"stale_high_forward", 99, 2, 1, 1},   // clamp to 0, then +1
		{"stale_negative_back", -5, 2, -1, 2}, // clamp to 0, then -1 wraps
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cycleInfoFocus(c.current, c.actionCount, c.step); got != c.want {
				t.Fatalf("got %d want %d", got, c.want)
			}
		})
	}
}

// TestCycleInfoFocus_ClampsOversizedActionCount: an author-supplied
// Actions slice longer than MaxInfoActions must not drive the wrap
// math past int8 range (silent truncation) or land on slots that
// won't render. Cap semantics match the draw loop.
func TestCycleInfoFocus_ClampsOversizedActionCount(t *testing.T) {
	// 200 > MaxInfoActions. After clamp n == MaxInfoActions+1 == 5.
	// From close (slot == MaxInfoActions) + forward wraps to action_0.
	got := cycleInfoFocus(int8(MaxInfoActions), 200, 1)
	if got != 0 {
		t.Fatalf("forward wrap: got %d want 0", got)
	}
	// From action_0 back wraps to the close slot (MaxInfoActions).
	got = cycleInfoFocus(0, 200, -1)
	if got != int8(MaxInfoActions) {
		t.Fatalf("back wrap: got %d want %d", got, MaxInfoActions)
	}
	// Negative actionCount collapses to close-only (n=1, wraps in place).
	if got := cycleInfoFocus(0, -3, 1); got != 0 {
		t.Fatalf("negative count: got %d want 0", got)
	}
}

// TestHandleFocusKey_TabTrapsInPopup: Tab with InfoOpen must cycle the
// popup sub-element focus and NOT advance to the next marker or close
// the dialog. This is the core slice-4b contract.
func TestHandleFocusKey_TabTrapsInPopup(t *testing.T) {
	w := &gui.Window{}
	AddOverlay(w, "m", &Marker{
		MarkerID: "a", Title: "A",
		Actions: []InfoWindowAction{{Label: "Do"}, {Label: "Undo"}},
	})
	AddOverlay(w, "m", &Marker{MarkerID: "b", Title: "B"})
	s := MapState{FocusedOverlayID: "a", InfoOpen: true, InfoFocusIndex: 0}
	e := &gui.Event{KeyCode: gui.KeyTab}

	for i, want := range []int8{1, 2, 0, 1} {
		if !handleFocusKey(Cfg{ID: "m"}, &s, e, w) {
			t.Fatalf("step %d: want consumed", i)
		}
		if s.InfoFocusIndex != want {
			t.Fatalf("step %d: got idx %d want %d", i, s.InfoFocusIndex, want)
		}
		if !s.InfoOpen {
			t.Fatalf("step %d: Tab must not close popup", i)
		}
		if s.FocusedOverlayID != "a" {
			t.Fatalf("step %d: marker focus must not move, got %q",
				i, s.FocusedOverlayID)
		}
	}
}

// TestHandleFocusKey_ShiftTabTrapsInPopup: reverse direction through
// the popup focus cycle.
func TestHandleFocusKey_ShiftTabTrapsInPopup(t *testing.T) {
	w := &gui.Window{}
	AddOverlay(w, "m", &Marker{
		MarkerID: "a", Title: "A",
		Actions: []InfoWindowAction{{Label: "Do"}},
	})
	s := MapState{FocusedOverlayID: "a", InfoOpen: true, InfoFocusIndex: 0}
	e := &gui.Event{KeyCode: gui.KeyTab, Modifiers: gui.ModShift}

	// 2 focusables (1 action + close). 0 -> 1 (close) -> 0 (action).
	for i, want := range []int8{1, 0, 1} {
		if !handleFocusKey(Cfg{ID: "m"}, &s, e, w) {
			t.Fatalf("step %d: want consumed", i)
		}
		if s.InfoFocusIndex != want {
			t.Fatalf("step %d: got %d want %d", i, s.InfoFocusIndex, want)
		}
	}
}

// TestHandleFocusKey_TabStaleMarkerDropsPopup: a stale popup (marker
// removed while dialog was open) closes cleanly on Tab instead of
// panic-diving through a nil Marker.
func TestHandleFocusKey_TabStaleMarkerDropsPopup(t *testing.T) {
	w := &gui.Window{}
	s := MapState{FocusedOverlayID: "ghost", InfoOpen: true, InfoFocusIndex: 1}
	e := &gui.Event{KeyCode: gui.KeyTab}
	if !handleFocusKey(Cfg{ID: "m"}, &s, e, w) {
		t.Fatalf("want consumed")
	}
	if s.InfoOpen || s.InfoFocusIndex != 0 {
		t.Fatalf("stale popup must reset; got InfoOpen=%v idx=%d",
			s.InfoOpen, s.InfoFocusIndex)
	}
}

// TestHandleFocusKey_EnterActivatesAction: Enter on an action-focused
// popup fires the callback and closes the dialog. Dispatch order
// mirrors handlePopupClick — state flips before the callback so a
// Snapshot read inside the callback sees InfoOpen=false.
func TestHandleFocusKey_EnterActivatesAction(t *testing.T) {
	w := &gui.Window{}
	var fired int
	var snapOpen bool
	AddOverlay(w, "m", &Marker{
		MarkerID: "a", Title: "A",
		Actions: []InfoWindowAction{
			{Label: "First", OnClick: func(win *gui.Window) {
				fired++
				if sn, ok := Snapshot(win, "m"); ok {
					snapOpen = sn.InfoOpen
				}
			}},
			{Label: "Second"},
		},
	})
	nsWrite(w, nsState, "m", MapState{FocusedOverlayID: "a", InfoOpen: true})
	s := MapState{FocusedOverlayID: "a", InfoOpen: true, InfoFocusIndex: 0}
	e := &gui.Event{KeyCode: gui.KeyEnter}

	if !handleFocusKey(Cfg{ID: "m"}, &s, e, w) {
		t.Fatalf("want consumed")
	}

	if fired != 1 {
		t.Fatalf("callback fired %d times, want 1", fired)
	}
	if s.InfoOpen {
		t.Fatalf("popup must close before callback returns")
	}
	if s.InfoFocusIndex != 0 {
		t.Fatalf("focus index must reset, got %d", s.InfoFocusIndex)
	}
	if snapOpen {
		t.Fatalf("Snapshot inside callback saw InfoOpen=true")
	}
}

// TestHandleFocusKey_EnterActionPreservesCallbackMutations: Actions
// typically call map mutators (SetZoom, PanTo) from OnClick.
// handleFocusKey owns its state writes and persists the dismissal
// *before* the callback fires so the callback's own SetZoom isn't
// clobbered by a post-return overwrite.
func TestHandleFocusKey_EnterActionPreservesCallbackMutations(t *testing.T) {
	w := &gui.Window{}
	id := "m"
	AddOverlay(w, id, &Marker{
		MarkerID: "a", Title: "A",
		Actions: []InfoWindowAction{
			{Label: "ZoomIn", OnClick: func(win *gui.Window) {
				SetZoom(win, id, 15)
			}},
		},
	})
	nsWrite(w, nsState, id, MapState{
		FocusedOverlayID: "a",
		InfoOpen:         true,
		Zoom:             10,
	})
	s := nsRead[MapState](w, nsState, id)
	if !handleFocusKey(Cfg{ID: id}, &s, &gui.Event{KeyCode: gui.KeyEnter}, w) {
		t.Fatalf("want consumed")
	}
	got, _ := Snapshot(w, id)
	if got.Zoom != 15 {
		t.Fatalf("SetZoom inside action callback was lost: zoom=%d want 15", got.Zoom)
	}
	if got.InfoOpen {
		t.Fatalf("popup must be closed after action fires")
	}
}

// TestHandleFocusKey_EnterOpensPopupPreservesOnPOISelectMutations:
// OnPOISelect callbacks mutating map state via PanTo/SetZoom must
// survive handleFocusKey's popup-open write.
func TestHandleFocusKey_EnterOpensPopupPreservesOnPOISelectMutations(t *testing.T) {
	w := &gui.Window{}
	id := "m"
	AddOverlay(w, id, &Marker{MarkerID: "a", Title: "A"})
	nsWrite(w, nsState, id, MapState{FocusedOverlayID: "a", Zoom: 10})

	c := Cfg{ID: id, OnPOISelect: func(win *gui.Window, _ Overlay) {
		SetZoom(win, id, 18)
	}}
	s := nsRead[MapState](w, nsState, id)
	if !handleFocusKey(c, &s, &gui.Event{KeyCode: gui.KeyEnter}, w) {
		t.Fatalf("want consumed")
	}
	got, _ := Snapshot(w, id)
	if got.Zoom != 18 {
		t.Fatalf("OnPOISelect SetZoom lost: zoom=%d want 18", got.Zoom)
	}
	if !got.InfoOpen {
		t.Fatalf("popup must stay open after OnPOISelect fires")
	}
}

// TestHandleFocusKey_EnterOnCloseSlot: Enter on the close slot closes
// the dialog without invoking any action callback.
func TestHandleFocusKey_EnterOnCloseSlot(t *testing.T) {
	w := &gui.Window{}
	var fired int
	AddOverlay(w, "m", &Marker{
		MarkerID: "a", Title: "A",
		Actions: []InfoWindowAction{
			{Label: "X", OnClick: func(*gui.Window) { fired++ }},
		},
	})
	s := MapState{FocusedOverlayID: "a", InfoOpen: true, InfoFocusIndex: 1}
	e := &gui.Event{KeyCode: gui.KeyEnter}
	if !handleFocusKey(Cfg{ID: "m"}, &s, e, w) {
		t.Fatalf("want consumed")
	}
	if fired != 0 {
		t.Fatalf("close slot must not fire action callback")
	}
	if s.InfoOpen {
		t.Fatalf("popup must close")
	}
}

// TestHandleFocusKey_EnterOnStaleActionIndex: the Actions slice shrank
// (or was replaced) after the index was stored. Enter must neither
// panic nor invoke a callback out of range; it closes the popup cleanly
// so the user gets a consistent state to restart from.
func TestHandleFocusKey_EnterOnStaleActionIndex(t *testing.T) {
	w := &gui.Window{}
	var fired int
	AddOverlay(w, "m", &Marker{
		MarkerID: "a", Title: "A",
		Actions: []InfoWindowAction{
			{Label: "Only", OnClick: func(*gui.Window) { fired++ }},
		},
	})
	// Index 5 is past every slot — a classic "stored when Actions had
	// more entries" scenario.
	s := MapState{FocusedOverlayID: "a", InfoOpen: true, InfoFocusIndex: 5}
	e := &gui.Event{KeyCode: gui.KeyEnter}
	if !handleFocusKey(Cfg{ID: "m"}, &s, e, w) {
		t.Fatalf("want consumed")
	}
	if fired != 0 {
		t.Fatalf("stale index must not invoke callback")
	}
	if s.InfoOpen {
		t.Fatalf("popup must close")
	}
}

// TestHandleFocusKey_EnterInPopupStaleMarkerDropsPopup: Enter inside a
// popup whose owning marker was removed while the dialog was open must
// reset to a consistent closed state instead of dereferencing a nil
// Marker. Mirror of TestHandleFocusKey_TabStaleMarkerDropsPopup for
// the Enter dispatch path.
func TestHandleFocusKey_EnterInPopupStaleMarkerDropsPopup(t *testing.T) {
	w := &gui.Window{}
	s := MapState{FocusedOverlayID: "ghost", InfoOpen: true, InfoFocusIndex: 1}
	e := &gui.Event{KeyCode: gui.KeyEnter}
	if !handleFocusKey(Cfg{ID: "m"}, &s, e, w) {
		t.Fatalf("want consumed")
	}
	if s.InfoOpen || s.InfoFocusIndex != 0 {
		t.Fatalf("stale popup must reset; got InfoOpen=%v idx=%d",
			s.InfoOpen, s.InfoFocusIndex)
	}
}

// TestHandleFocusKey_EnterTitlelessMarkerFiresOnPOISelect: Enter on a
// decorative focused marker (no Title) with OnPOISelect fires the
// callback without opening a popup; the callback's map-state mutation
// survives the return.
func TestHandleFocusKey_EnterTitlelessMarkerFiresOnPOISelect(t *testing.T) {
	w := &gui.Window{}
	id := "m"
	AddOverlay(w, id, &Marker{MarkerID: "a", Label: "decorative"})
	nsWrite(w, nsState, id, MapState{FocusedOverlayID: "a", Zoom: 10})
	var fires int
	c := Cfg{ID: id, OnPOISelect: func(win *gui.Window, _ Overlay) {
		fires++
		SetZoom(win, id, 18)
	}}
	s := nsRead[MapState](w, nsState, id)
	if !handleFocusKey(c, &s, &gui.Event{KeyCode: gui.KeyEnter}, w) {
		t.Fatalf("want consumed")
	}
	if fires != 1 {
		t.Fatalf("OnPOISelect fired %d times, want 1", fires)
	}
	if s.InfoOpen {
		t.Fatalf("titleless marker must not open popup")
	}
	got, _ := Snapshot(w, id)
	if got.Zoom != 18 {
		t.Fatalf("OnPOISelect SetZoom lost: zoom=%d want 18", got.Zoom)
	}
}

// TestHandleFocusKey_EnterOpensPopupResetsFocusIndex: opening the popup
// via Enter on a focused marker must seed InfoFocusIndex=0 so Tab lands
// on the first action, regardless of any prior stale index.
func TestHandleFocusKey_EnterOpensPopupResetsFocusIndex(t *testing.T) {
	w := &gui.Window{}
	AddOverlay(w, "m", &Marker{MarkerID: "a", Title: "A"})
	s := MapState{FocusedOverlayID: "a", InfoFocusIndex: 7}
	e := &gui.Event{KeyCode: gui.KeyEnter}
	if !handleFocusKey(Cfg{ID: "m"}, &s, e, w) {
		t.Fatalf("want consumed")
	}
	if !s.InfoOpen {
		t.Fatalf("popup must open")
	}
	if s.InfoFocusIndex != 0 {
		t.Fatalf("InfoFocusIndex must reset to 0, got %d", s.InfoFocusIndex)
	}
}

// TestHandleFocusKey_EscapeResetsInfoFocusIndex: Escape while the popup
// is open must clear both InfoOpen and InfoFocusIndex.
func TestHandleFocusKey_EscapeResetsInfoFocusIndex(t *testing.T) {
	w := &gui.Window{}
	AddOverlay(w, "m", &Marker{MarkerID: "a", Title: "A"})
	s := MapState{FocusedOverlayID: "a", InfoOpen: true, InfoFocusIndex: 2}
	e := &gui.Event{KeyCode: gui.KeyEscape}
	if !handleFocusKey(Cfg{ID: "m"}, &s, e, w) {
		t.Fatalf("want consumed")
	}
	if s.InfoOpen || s.InfoFocusIndex != 0 {
		t.Fatalf("esc must reset; got InfoOpen=%v idx=%d",
			s.InfoOpen, s.InfoFocusIndex)
	}
}
