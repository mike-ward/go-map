package mapview

import "github.com/mike-ward/go-gui/gui"

// LegendCfg configures a Legend widget. MapID is the Cfg.ID of the
// Map whose layers the legend should list; the legend re-reads the
// layer registry every frame, so AddLayer / RemoveLayer / rename
// changes surface without any extra wiring.
//
// Layers with an empty Name are skipped. That keeps the legend out of
// the Cfg.Source shorthand path (which seeds a base layer with Name
// empty by design) — consumers opt in by setting Name explicitly on
// each layer they want listed.
//
// Each rendered row is a gui.Toggle bound to the layer's Visible
// flag. Clicking the toggle flips Visible via SetLayerVisible, which
// bumps the map's draw-cache version and redraws. Opacity is not
// exposed here; the toggle tracks the boolean visibility flag only.
type LegendCfg struct {
	// Identity
	ID    string
	MapID string

	// Optional header rendered above the rows. Empty to omit.
	Title string

	// Sizing
	Sizing    gui.Sizing
	Width     float32
	Height    float32
	MinWidth  float32
	MaxWidth  float32
	MinHeight float32
	MaxHeight float32

	// IDFocusBase, when > 0, allocates sequential focus IDs to rows
	// starting from this value in layer-insertion order. 0 leaves
	// rows non-focusable. Consumers managing a global focus order
	// reserve a contiguous range and pass the first entry here.
	IDFocusBase uint32

	// Layout
	Padding gui.Opt[gui.Padding]
	Spacing gui.Opt[float32]

	// Appearance
	Color gui.Color

	// Accessibility
	A11YLabel string
}

// legendView is the custom View returned by Legend. Each frame
// GenerateLayout re-reads the layer registry so legend contents stay
// in sync with AddLayer / RemoveLayer / SetLayerVisible without any
// extra coordination.
type legendView struct {
	cfg LegendCfg
}

func (*legendView) Content() []gui.View { return nil }

// GenerateLayout rebuilds the row list from the current layer
// registry, then hands the inner Column to gui.GenerateViewLayout so
// the recursion picks up Title + Toggle children. Returning
// buildLegend's bare Column.GenerateLayout dropped every child because
// Content() is nil — only the empty Column shape survived to the
// layout pass.
func (lv *legendView) GenerateLayout(w *gui.Window) gui.Layout {
	return gui.GenerateViewLayout(buildLegend(w, lv.cfg), w)
}

// Legend returns a View listing every named layer of the map
// identified by Cfg.MapID. Panics if MapID is empty — a legend with
// no target is always a bug.
func Legend(cfg LegendCfg) gui.View {
	if cfg.MapID == "" {
		panic("mapview: Legend Cfg.MapID is required")
	}
	return &legendView{cfg: cfg}
}

// buildLegend assembles the legend's Column view from the current
// layer registry. Split from legendView.GenerateLayout so tests can
// inspect the view tree without standing up the full widget.
func buildLegend(w *gui.Window, c LegendCfg) gui.View {
	bm := readLayers(w, c.MapID)
	content := make([]gui.View, 0, bm.Len()+1)
	if c.Title != "" {
		content = append(content, gui.Text(gui.TextCfg{
			Text: c.Title,
			Hero: true,
		}))
	}
	row := uint32(0)
	bm.Range(func(layerID string, l Layer) bool {
		if l.Name == "" {
			return true
		}
		focusID := uint32(0)
		if c.IDFocusBase > 0 {
			focusID = c.IDFocusBase + row
		}
		row++
		content = append(content, gui.Toggle(gui.ToggleCfg{
			Label:           l.Name,
			Selected:        l.Visible,
			IDFocus:         focusID,
			A11YLabel:       legendRowA11YLabel(l),
			A11YDescription: legendRowA11YDescription(l),
			OnClick: func(_ *gui.Layout, _ *gui.Event, ww *gui.Window) {
				toggleLayerVisible(ww, c.MapID, layerID)
			},
		}))
		return true
	})
	return gui.Column(gui.ContainerCfg{
		ID:        c.ID,
		Sizing:    c.Sizing,
		Width:     c.Width,
		Height:    c.Height,
		MinWidth:  c.MinWidth,
		MaxWidth:  c.MaxWidth,
		MinHeight: c.MinHeight,
		MaxHeight: c.MaxHeight,
		Padding:   c.Padding,
		Spacing:   c.Spacing,
		Color:     c.Color,
		A11YLabel: c.A11YLabel,
		Content:   content,
	})
}

// toggleLayerVisible flips Visible on the named layer. No-op when
// absent so a stale row in a hot click path cannot force per-frame
// OnDraw re-runs. Split from the Toggle OnClick closure so tests
// drive the mutation without having to reconstruct a layout tree.
func toggleLayerVisible(w *gui.Window, mapID, layerID string) {
	l, ok := readLayers(w, mapID).Get(layerID)
	if !ok {
		return
	}
	SetLayerVisible(w, mapID, layerID, !l.Visible)
}

// legendRowA11YLabel composes a row's accessible name from the
// layer's Name plus its visibility state. Screen readers announce
// the full label on focus so a keyboard user hears "Transit,
// visible" rather than a bare "Transit".
func legendRowA11YLabel(l Layer) string {
	state := "hidden"
	if l.Visible {
		state = "visible"
	}
	return l.Name + ", " + state
}

// legendRowA11YDescription returns the action hint announced after
// the label so users learn the toggle gesture before activating.
func legendRowA11YDescription(l Layer) string {
	if l.Visible {
		return "Press space to hide layer"
	}
	return "Press space to show layer"
}
