package mapview

import (
	"hash/fnv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mike-ward/go-gui/gui"
)

// GalleryEntry is one base-map choice rendered as a card. LayerID
// must match a Layer already registered via AddLayer (or seeded
// through Cfg.InitialLayers); entries whose LayerID is absent from
// the registry are silently skipped, matching Legend's skip-empty-Name
// policy.
//
// ThumbnailURL is forwarded to gui.Image for render + on-disk caching.
// An empty URL falls back to a colored letter tile drawn entirely
// from standard containers — no network dependency, no placeholder
// flash on first paint.
type GalleryEntry struct {
	LayerID      string
	Label        string
	ThumbnailURL string
}

// GalleryCfg configures a Gallery widget. MapID is the Cfg.ID of the
// Map whose base layer the gallery controls; clicking a card calls
// SetBaseLayer(w, MapID, LayerID). The gallery re-reads the layer
// registry every frame so base-layer switches made elsewhere surface
// on the next paint without extra wiring.
//
// ThumbSize is the square edge of each thumbnail in pixels; zero
// coerces to defaultGalleryThumbSize so Cfg{MapID, Entries} works as
// a zero-config call. Cards wrap via the outer Row so Gallery slots
// into sidebars and toolbars without consumer layout math.
//
// Unlike Legend, entries are explicit. Base-map switchers display a
// curated set — the consumer owns which layers belong in the gallery
// and which thumbnail represents each. Entries whose LayerID is not
// registered on the map are skipped silently.
//
// Entries may point at any registered layer regardless of Kind;
// clicking always routes through SetBaseLayer, which promotes a
// Reference layer to Base (demoting the prior base). If a consumer
// wants to restrict the gallery to current-Base candidates only, they
// curate Entries accordingly — the widget does not filter.
type GalleryCfg struct {
	// Identity
	ID    string
	MapID string

	// Content
	Title   string
	Entries []GalleryEntry

	// Sizing
	Sizing    gui.Sizing
	Width     float32
	Height    float32
	MinWidth  float32
	MaxWidth  float32
	MinHeight float32
	MaxHeight float32
	ThumbSize float32

	// IDFocusBase, when > 0, allocates sequential focus IDs to cards
	// starting from this value in Entries order. 0 leaves cards non-
	// focusable. Consumers managing a global focus order reserve a
	// contiguous range and pass the first entry here.
	IDFocusBase uint32

	// Layout
	Padding gui.Opt[gui.Padding]
	Spacing gui.Opt[float32]

	// Appearance
	Color gui.Color

	// Accessibility
	A11YLabel string
}

// defaultGalleryThumbSize is the square edge used when ThumbSize is
// zero. 80 px matches Esri's basemap gallery at normal density and
// leaves room for a two-word caption below each tile without blowing
// out a 240-px sidebar.
const defaultGalleryThumbSize float32 = 80

// galleryBorderWidth is the stroke width of the selection ring drawn
// around the active base card. 2 px reads as a deliberate selection
// rather than an accidental divider.
const galleryBorderWidth float32 = 2

// galleryView is the custom View returned by Gallery. Same shape as
// legendView: nil Content + GenerateLayout that re-reads state each
// frame. The handoff to gui.GenerateViewLayout recurses into the
// Column children so cards (and their Image / Text subtrees) layout
// correctly — returning buildGallery's bare Column.GenerateLayout
// would drop children for the same reason it did in Legend.
type galleryView struct {
	cfg GalleryCfg
}

func (*galleryView) Content() []gui.View { return nil }

func (gv *galleryView) GenerateLayout(w *gui.Window) gui.Layout {
	return gui.GenerateViewLayout(buildGallery(w, gv.cfg), w)
}

// Gallery returns a View rendering a base-layer switcher keyed to
// Cfg.MapID. Panics if MapID is empty — a gallery with no target is
// always a bug.
func Gallery(cfg GalleryCfg) gui.View {
	if cfg.MapID == "" {
		panic("mapview: Gallery Cfg.MapID is required")
	}
	return &galleryView{cfg: cfg}
}

// buildGallery assembles the gallery's Column view. Split from
// galleryView.GenerateLayout so tests can inspect the view tree
// without standing up the full widget.
func buildGallery(w *gui.Window, c GalleryCfg) gui.View {
	thumb := c.ThumbSize
	if thumb <= 0 {
		thumb = defaultGalleryThumbSize
	}
	bm := readLayers(w, c.MapID)
	currentBase, _ := baseLayer(w, c.MapID)

	content := make([]gui.View, 0, 2)
	if c.Title != "" {
		content = append(content, gui.Text(gui.TextCfg{
			Text: c.Title,
			Hero: true,
		}))
	}

	cards := make([]gui.View, 0, len(c.Entries))
	idx := uint32(0)
	for _, e := range c.Entries {
		if e.LayerID == "" || !bm.Contains(e.LayerID) {
			continue
		}
		selected := currentBase.LayerID == e.LayerID
		focusID := uint32(0)
		if c.IDFocusBase > 0 {
			focusID = c.IDFocusBase + idx
		}
		idx++
		cards = append(cards, galleryCard(e, thumb, selected, focusID, c.MapID))
	}

	// Row with Wrap so cards reflow when the container is narrower
	// than n*ThumbSize. Spacing drives both row and column gap.
	content = append(content, gui.Row(gui.ContainerCfg{
		Wrap:     true,
		Spacing:  gui.Some[float32](8),
		A11YRole: gui.AccessRoleRadioGroup,
		Content:  cards,
	}))

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

// galleryCard builds one thumbnail+caption card. Selection is
// communicated visually via a border ring and semantically via
// AccessStateSelected on the RadioButton role. Clicking the outer
// Column routes through selectGalleryLayer: the target is made
// visible and promoted to base. Visibility flip is required because
// SetBaseLayer alone only swaps Kind — a consumer that seeds a
// candidate with Visible:false would otherwise see a blank map after
// the swap.
//
// Layout math: the outer Column always reserves galleryBorderWidth of
// inset on every side. Selected cards spend that inset on a visible
// ring via SizeBorder; unselected cards spend it on equivalent
// Padding, so card edges align and the selection state reads as a
// ring appearing in place rather than card sizes jumping. An always-
// on SizeBorder with an unset ColorBorder was rejected because
// backend rendering of a zero-value Color is not defined to skip the
// draw.
func galleryCard(
	e GalleryEntry, thumb float32, selected bool, focusID uint32,
	mapID string,
) gui.View {
	state := gui.AccessStateNone
	if selected {
		state = gui.AccessStateSelected
	}
	inset := galleryBorderWidth
	cfg := gui.ContainerCfg{
		IDFocus:         focusID,
		A11YRole:        gui.AccessRoleRadioButton,
		A11YState:       state,
		A11YLabel:       galleryCardA11YLabel(e.Label, selected),
		A11YDescription: galleryCardA11YDescription(selected),
		OnClick: func(_ *gui.Layout, _ *gui.Event, w *gui.Window) {
			selectGalleryLayer(w, mapID, e.LayerID)
		},
		Content: []gui.View{
			galleryThumb(e, thumb),
			gui.Text(gui.TextCfg{Text: e.Label}),
		},
	}
	if selected {
		cfg.SizeBorder = gui.Some(inset)
		cfg.ColorBorder = gui.Hex(0x2C6FD0)
	} else {
		cfg.Padding = gui.Some(gui.Padding{
			Top: inset, Right: inset, Bottom: inset, Left: inset,
		})
	}
	return gui.Column(cfg)
}

// selectGalleryLayer is the mutator the card OnClick forwards to.
// Split from the closure so tests can drive the combined swap without
// reconstructing a layout tree.
//
// Three-step sequence:
//  1. Hide the current base. SetBaseLayer demotes it to Reference but
//     leaves Visible alone; since References render *over* Base, a
//     still-visible demoted former base would paint over the new one
//     — exactly the "pick color → map stays gray" inversion that
//     originally masked the stacking model.
//  2. Make the target visible.
//  3. Promote the target to Base.
//
// Clicking the already-selected card short-circuits step 1 so the
// current base is not flickered off and back on.
func selectGalleryLayer(w *gui.Window, mapID, layerID string) {
	if !readLayers(w, mapID).Contains(layerID) {
		return
	}
	if cur, ok := baseLayer(w, mapID); ok && cur.LayerID != layerID {
		SetLayerVisible(w, mapID, cur.LayerID, false)
	}
	SetLayerVisible(w, mapID, layerID, true)
	SetBaseLayer(w, mapID, layerID)
}

// galleryThumb returns the ThumbnailURL image when set, otherwise a
// deterministic colored letter tile. Fallback colors derive from a
// FNV-1a hash of the LayerID + Label so a given entry keeps the same
// color across renders, and adjacent entries differ predictably.
func galleryThumb(e GalleryEntry, size float32) gui.View {
	if e.ThumbnailURL != "" {
		return gui.Image(gui.ImageCfg{
			Src:    e.ThumbnailURL,
			Width:  size,
			Height: size,
		})
	}
	return gui.Column(gui.ContainerCfg{
		Width:   size,
		Height:  size,
		Color:   galleryFallbackColor(e.LayerID + e.Label),
		HAlign:  gui.HAlignCenter,
		VAlign:  gui.VAlignMiddle,
		Content: []gui.View{gui.Text(gui.TextCfg{Text: galleryLetter(e.Label)})},
	})
}

// galleryLetter returns the single-character uppercase glyph shown
// on the fallback tile. Empty Label degrades to "?" so the card is
// never blank — still clearly clickable even if the consumer forgot
// a caption.
func galleryLetter(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return "?"
	}
	r, _ := utf8.DecodeRuneInString(label)
	if r == utf8.RuneError {
		return "?"
	}
	return string(unicode.ToUpper(r))
}

// galleryFallbackColor maps the seed string to a saturated hue. The
// three byte channels of the FNV sum land in (64..=255) so every
// fallback reads as a colored chip against both light and dark card
// frames — avoids dropping to near-black or near-white that would
// wash out the letter.
func galleryFallbackColor(seed string) gui.Color {
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	sum := h.Sum32()
	r := byte(64 + (sum>>0)%192)
	g := byte(64 + (sum>>8)%192)
	b := byte(64 + (sum>>16)%192)
	return gui.Color{R: r, G: g, B: b, A: 255}
}

// galleryCardA11YLabel composes the spoken name for a card. Mirrors
// legendRowA11YLabel's "name, state" pattern so screen-reader users
// hear consistent phrasing across the two widgets.
func galleryCardA11YLabel(label string, selected bool) string {
	state := "not selected"
	if selected {
		state = "selected"
	}
	return label + ", base map, " + state
}

// galleryCardA11YDescription returns the action hint announced after
// the label. The phrasing mirrors Legend for consistency.
func galleryCardA11YDescription(selected bool) string {
	if selected {
		return "Already the active base map"
	}
	return "Press space to set as base map"
}
