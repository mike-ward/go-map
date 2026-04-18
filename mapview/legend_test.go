package mapview

import (
	"strings"
	"testing"

	"github.com/mike-ward/go-gui/gui"
)

// TestLegend_PanicsOnEmptyMapID locks the required-field contract.
// A legend with no target is always a bug; the factory is the single
// place the check lives.
func TestLegend_PanicsOnEmptyMapID(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Legend did not panic on empty MapID")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "MapID") {
			t.Errorf("panic = %v, want message mentioning MapID", r)
		}
	}()
	Legend(LegendCfg{})
}

// TestLegend_EmptyRegistry returns a container with no rows (and no
// title) so layouts survive before any layer is registered. Guards
// against a nil-map crash in buildLegend's Range.
func TestLegend_EmptyRegistry(t *testing.T) {
	w := &gui.Window{}
	v := buildLegend(w, LegendCfg{MapID: "m"})
	if got := len(v.Content()); got != 0 {
		t.Errorf("empty registry rows = %d, want 0", got)
	}
}

// TestLegend_TitlePrependsOneRow confirms Title adds exactly one
// leading child. Skipping this would let a refactor silently drop
// the header while tests still pass on row counts.
func TestLegend_TitlePrependsOneRow(t *testing.T) {
	w := &gui.Window{}
	AddLayer(w, "m", Layer{
		LayerID: "a", Name: "A", Source: fakeSource{}, Visible: true,
	})
	withTitle := buildLegend(w, LegendCfg{MapID: "m", Title: "Layers"})
	without := buildLegend(w, LegendCfg{MapID: "m"})
	if len(withTitle.Content()) != len(without.Content())+1 {
		t.Errorf("title rows = %d, no-title rows = %d, want +1",
			len(withTitle.Content()), len(without.Content()))
	}
}

// TestLegend_SkipsEmptyName confirms the Source-shorthand base layer
// (Name empty by design) does not leak a "base" row into every demo
// that uses the shorthand. Consumers opt in by setting Name.
func TestLegend_SkipsEmptyName(t *testing.T) {
	w := &gui.Window{}
	AddLayer(w, "m", Layer{
		LayerID: "base", Source: fakeSource{}, Kind: LayerKindBase,
		Visible: true,
	})
	AddLayer(w, "m", Layer{
		LayerID: "transit", Name: "Transit", Source: fakeSource{},
		Kind: LayerKindReference, Visible: true,
	})
	v := buildLegend(w, LegendCfg{MapID: "m"})
	if got := len(v.Content()); got != 1 {
		t.Errorf("rows = %d, want 1 (transit only; base has empty Name)",
			got)
	}
}

// TestLegend_RowPerNamedLayer renders a row for every layer with a
// non-empty Name, whether Visible or not — hidden layers still need
// an unchecked row so users can turn them back on.
func TestLegend_RowPerNamedLayer(t *testing.T) {
	w := &gui.Window{}
	AddLayer(w, "m", Layer{
		LayerID: "a", Name: "A", Source: fakeSource{}, Visible: true,
	})
	AddLayer(w, "m", Layer{
		LayerID: "b", Name: "B", Source: fakeSource{}, Visible: false,
	})
	v := buildLegend(w, LegendCfg{MapID: "m"})
	if got := len(v.Content()); got != 2 {
		t.Errorf("rows = %d, want 2 (hidden layers still render)", got)
	}
}

// TestLegend_InsertionOrderPreserved locks that Range delivers layers
// in insertion order so the legend reads top-to-bottom in the order
// the consumer registered them — a refactor that sorted by LayerID
// or Name would silently reorder existing UIs.
func TestLegend_InsertionOrderPreserved(t *testing.T) {
	w := &gui.Window{}
	// Register in reverse alphabetical order so a stray sort would
	// flip the output.
	AddLayer(w, "m", Layer{LayerID: "c", Name: "Zeta", Source: fakeSource{}, Visible: true})
	AddLayer(w, "m", Layer{LayerID: "b", Name: "Mid", Source: fakeSource{}, Visible: true})
	AddLayer(w, "m", Layer{LayerID: "a", Name: "Alpha", Source: fakeSource{}, Visible: true})
	v := buildLegend(w, LegendCfg{MapID: "m"})
	rows := v.Content()
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	// Toggle wraps Label inside a Row whose second child is the Text
	// carrying the label string. Dig rather than trust alphabetics.
	want := []string{"Zeta", "Mid", "Alpha"}
	for i, r := range rows {
		got := firstText(r)
		if got != want[i] {
			t.Errorf("row[%d] label = %q, want %q", i, got, want[i])
		}
	}
}

// TestLegend_GenerateLayoutPropagatesChildren guards against a
// regression where legendView.GenerateLayout returned only the inner
// Column's bare shape — go-gui's GenerateViewLayout then skipped the
// children because Content() is nil, producing an empty sidebar at
// runtime while buildLegend unit tests still passed. Exercising the
// widget through the real view pipeline is the only place this surfaces.
func TestLegend_GenerateLayoutPropagatesChildren(t *testing.T) {
	w := &gui.Window{}
	AddLayer(w, "m", Layer{
		LayerID: "a", Name: "A", Source: fakeSource{}, Visible: true,
	})
	AddLayer(w, "m", Layer{
		LayerID: "b", Name: "B", Source: fakeSource{}, Visible: true,
	})
	root := gui.GenerateViewLayout(
		Legend(LegendCfg{MapID: "m", Title: "Layers"}), w)
	// Root layout is the Column shape; its Children must include the
	// Title Hero Text + one Toggle per named layer. Three children
	// total. An empty Children slice is the regression.
	if got := len(root.Children); got != 3 {
		t.Errorf("Children = %d, want 3 (Title + 2 Toggles)", got)
	}
}

// TestToggleLayerVisible_Flips exercises the mutator used by the row
// OnClick closure. Direct — the closure wiring is a one-liner, so
// testing the helper proves the behaviour without standing up a
// layout tree.
func TestToggleLayerVisible_Flips(t *testing.T) {
	w := &gui.Window{}
	AddLayer(w, "m", Layer{
		LayerID: "a", Name: "A", Source: fakeSource{}, Visible: true,
	})
	toggleLayerVisible(w, "m", "a")
	got, _ := readLayers(w, "m").Get("a")
	if got.Visible {
		t.Error("after first toggle, Visible = true; want false")
	}
	toggleLayerVisible(w, "m", "a")
	got, _ = readLayers(w, "m").Get("a")
	if !got.Visible {
		t.Error("after second toggle, Visible = false; want true")
	}
}

// TestToggleLayerVisible_AbsentIsNoOp guards the registry lookup so a
// stale row click after RemoveLayer does not bump versions forever.
func TestToggleLayerVisible_AbsentIsNoOp(t *testing.T) {
	w := &gui.Window{}
	before := readVersion(w, "m")
	toggleLayerVisible(w, "m", "nope")
	if got := readVersion(w, "m"); got != before {
		t.Errorf("absent toggle bumped %d → %d", before, got)
	}
}

// TestLegendRowA11YLabel encodes the "Name, state" format screen
// readers rely on. Freezes the string so a well-meaning refactor
// ("let's say 'on' instead of 'visible'") has to update the test
// and thinking about locale impact at the same time.
func TestLegendRowA11YLabel(t *testing.T) {
	cases := []struct {
		l    Layer
		want string
	}{
		{Layer{Name: "Transit", Visible: true}, "Transit, visible"},
		{Layer{Name: "Boundaries", Visible: false}, "Boundaries, hidden"},
	}
	for _, c := range cases {
		if got := legendRowA11YLabel(c.l); got != c.want {
			t.Errorf("%+v → %q, want %q", c.l, got, c.want)
		}
	}
}

// firstText walks a view's Content tree and returns the first Text
// string it encounters. The Toggle factory nests the label inside a
// Row > [box-row, label-text] tree; the box-row itself contains a
// [glyph-text] — so the first Text in DFS order is the checkmark
// glyph, not the Label. Step past any single-char leaf to land on
// the caller's Label instead.
func firstText(v gui.View) string {
	for _, child := range v.Content() {
		if t := extractText(child); t != "" && !isGlyph(t) {
			return t
		}
	}
	return ""
}

// extractText returns the Text shape content from a view if it is a
// Text view, else recurses into the first Text-bearing descendant.
func extractText(v gui.View) string {
	w := &gui.Window{}
	lo := v.GenerateLayout(w)
	if lo.Shape != nil && lo.Shape.TC != nil {
		// Skip single-glyph nodes; they are the toggle's check/space.
		if s := lo.Shape.TC.Text; !isGlyph(s) {
			return s
		}
	}
	for _, child := range lo.Children {
		if child.Shape != nil && child.Shape.TC != nil {
			if s := child.Shape.TC.Text; !isGlyph(s) {
				return s
			}
		}
	}
	return ""
}

// isGlyph reports the 1-rune check/space strings the Toggle factory
// emits in its indicator cell. Callers filter these out to reach the
// real Label.
func isGlyph(s string) bool {
	return s == "✓" || s == " " || s == ""
}
