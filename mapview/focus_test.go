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

// TestHandleFocusKey_TabCycles: Tab walks forward, Shift-Tab walks
// back, both wrap, and neither opens the popup.
func TestHandleFocusKey_TabCycles(t *testing.T) {
	w := &gui.Window{}
	for _, id := range []string{"a", "b", "c"} {
		AddOverlay(w, "m", &Marker{MarkerID: id, Title: id})
	}
	s := MapState{FocusedOverlayID: "a", InfoOpen: true}

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
			s.InfoOpen = true
			if !handleFocusKey(Cfg{ID: "m"}, &s, c.ev, w) {
				t.Fatalf("want consumed")
			}
			if s.FocusedOverlayID != c.want {
				t.Fatalf("got %q want %q", s.FocusedOverlayID, c.want)
			}
			if s.InfoOpen {
				t.Fatalf("Tab must close InfoOpen")
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
func TestInfoRectHit_Boundaries(t *testing.T) {
	r := infoRectState{X: 10, Y: 20, W: 100, H: 40, Valid: true}
	cases := []struct {
		name   string
		px, py float32
		want   bool
	}{
		{"inside", 50, 30, true},
		{"top_left_corner_inclusive", 10, 20, true},
		{"bottom_right_corner_exclusive", 110, 60, false},
		{"right_edge_excluded", 110, 30, false},
		{"bottom_edge_excluded", 50, 60, false},
		{"left_of_rect", 9, 30, false},
		{"above_rect", 50, 19, false},
		{"nan_x", float32(math.NaN()), 30, false},
		{"nan_y", 50, float32(math.NaN()), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := r.hit(c.px, c.py); got != c.want {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
	if (infoRectState{}).hit(0, 0) {
		t.Fatal("Valid=false must never hit")
	}
	if (infoRectState{X: 10, Y: 10, W: 10, H: 10}).hit(15, 15) {
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
