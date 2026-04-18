package mapview

import (
	"strings"
	"testing"

	"github.com/mike-ward/go-gui/gui"
)

// TestGallery_PanicsOnEmptyMapID mirrors Legend's required-field
// contract. A gallery with no target is always a bug.
func TestGallery_PanicsOnEmptyMapID(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Gallery did not panic on empty MapID")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "MapID") {
			t.Errorf("panic = %v, want message mentioning MapID", r)
		}
	}()
	Gallery(GalleryCfg{})
}

// TestGallery_EmptyEntries returns a container with just the wrapping
// Row child; no cards are rendered. Guards a zero-entries configuration
// from crashing the layout pass.
func TestGallery_EmptyEntries(t *testing.T) {
	w := &gui.Window{}
	v := buildGallery(w, GalleryCfg{MapID: "m"})
	// Content: no Title + the Row wrapper = 1 child. The Row's Content
	// must be empty.
	kids := v.Content()
	if len(kids) != 1 {
		t.Fatalf("root children = %d, want 1 (Row wrapper)", len(kids))
	}
	if cards := kids[0].Content(); len(cards) != 0 {
		t.Errorf("empty Entries produced %d cards, want 0", len(cards))
	}
}

// TestGallery_TitlePrependsOneChild confirms Title adds exactly one
// leading child above the cards Row. Refactors that swap the Title
// for a framed header without updating layout would otherwise pass.
func TestGallery_TitlePrependsOneChild(t *testing.T) {
	w := &gui.Window{}
	AddLayer(w, "m", Layer{
		LayerID: "osm", Source: fakeSource{}, Kind: LayerKindBase,
		Visible: true,
	})
	withTitle := buildGallery(w, GalleryCfg{
		MapID:   "m",
		Title:   "Basemaps",
		Entries: []GalleryEntry{{LayerID: "osm", Label: "OSM"}},
	})
	without := buildGallery(w, GalleryCfg{
		MapID:   "m",
		Entries: []GalleryEntry{{LayerID: "osm", Label: "OSM"}},
	})
	if len(withTitle.Content()) != len(without.Content())+1 {
		t.Errorf("title children = %d, no-title = %d, want +1",
			len(withTitle.Content()), len(without.Content()))
	}
}

// TestGallery_UnknownLayerIDSkipped confirms entries pointing at an
// unregistered layer are silently dropped. Matches Legend's policy of
// ignoring unnamed layers — both widgets re-read registry state each
// frame so a consumer mutation elsewhere surfaces on the next paint
// without panic.
func TestGallery_UnknownLayerIDSkipped(t *testing.T) {
	w := &gui.Window{}
	AddLayer(w, "m", Layer{
		LayerID: "osm", Source: fakeSource{}, Kind: LayerKindBase,
		Visible: true,
	})
	v := buildGallery(w, GalleryCfg{
		MapID: "m",
		Entries: []GalleryEntry{
			{LayerID: "osm", Label: "OSM"},
			{LayerID: "nope", Label: "Missing"},
			{LayerID: "", Label: "Blank"},
		},
	})
	row := v.Content()[0]
	if got := len(row.Content()); got != 1 {
		t.Errorf("cards = %d, want 1 (only osm registered)", got)
	}
}

// TestGallery_RendersCardPerRegisteredEntry locks the one-card-per-
// entry contract for registered layers.
func TestGallery_RendersCardPerRegisteredEntry(t *testing.T) {
	w := &gui.Window{}
	AddLayer(w, "m", Layer{
		LayerID: "osm", Source: fakeSource{}, Kind: LayerKindBase,
		Visible: true,
	})
	AddLayer(w, "m", Layer{
		LayerID: "sat", Source: fakeSource{}, Kind: LayerKindReference,
		Visible: true,
	})
	v := buildGallery(w, GalleryCfg{
		MapID: "m",
		Entries: []GalleryEntry{
			{LayerID: "osm", Label: "OSM"},
			{LayerID: "sat", Label: "Satellite"},
		},
	})
	row := v.Content()[0]
	if got := len(row.Content()); got != 2 {
		t.Errorf("cards = %d, want 2", got)
	}
}

// TestGallery_GenerateLayoutPropagatesChildren guards against the same
// regression Legend hit: a View that returns buildX's bare Column
// GenerateLayout drops all children because Content() is nil. Only
// exercising through gui.GenerateViewLayout catches the omission.
func TestGallery_GenerateLayoutPropagatesChildren(t *testing.T) {
	w := &gui.Window{}
	AddLayer(w, "m", Layer{
		LayerID: "osm", Source: fakeSource{}, Kind: LayerKindBase,
		Visible: true,
	})
	root := gui.GenerateViewLayout(Gallery(GalleryCfg{
		MapID: "m", Title: "Bases",
		Entries: []GalleryEntry{{LayerID: "osm", Label: "OSM"}},
	}), w)
	if len(root.Children) == 0 {
		t.Error("GenerateLayout dropped children; got empty Children")
	}
}

// TestSelectGalleryLayer_FlipsVisibleAndBase covers the mutator the
// card OnClick forwards to. Hidden candidates must become visible
// and become the base, AND the prior base must be hidden — otherwise
// it remains a Visible Reference stacked over the new base, inverting
// the gallery pick visually (the original "click color, map goes
// gray" regression).
func TestSelectGalleryLayer_FlipsVisibleAndBase(t *testing.T) {
	w := &gui.Window{}
	AddLayer(w, "m", Layer{
		LayerID: "osm", Source: fakeSource{}, Kind: LayerKindBase,
		Visible: true,
	})
	AddLayer(w, "m", Layer{
		LayerID: "gray", Source: fakeSource{}, Kind: LayerKindReference,
		Visible: false,
	})
	selectGalleryLayer(w, "m", "gray")
	got, _ := readLayers(w, "m").Get("gray")
	if got.Kind != LayerKindBase {
		t.Errorf("Kind = %v, want Base", got.Kind)
	}
	if !got.Visible {
		t.Error("Visible = false after gallery select; want true")
	}
	prev, _ := readLayers(w, "m").Get("osm")
	if prev.Kind != LayerKindReference {
		t.Errorf("prior base Kind = %v, want Reference (demoted)", prev.Kind)
	}
	if prev.Visible {
		t.Error("prior base Visible = true after swap; stacking would" +
			" invert the pick (Reference paints over new Base)")
	}
}

// TestSelectGalleryLayer_AlreadySelectedIsNoop confirms clicking the
// current base short-circuits step 1 so the base does not flicker
// off and on between frames.
func TestSelectGalleryLayer_AlreadySelectedIsNoop(t *testing.T) {
	w := &gui.Window{}
	AddLayer(w, "m", Layer{
		LayerID: "osm", Source: fakeSource{}, Kind: LayerKindBase,
		Visible: true,
	})
	before := readVersion(w, "m")
	selectGalleryLayer(w, "m", "osm")
	got, _ := readLayers(w, "m").Get("osm")
	if !got.Visible || got.Kind != LayerKindBase {
		t.Errorf("already-selected target mutated: %+v", got)
	}
	if got := readVersion(w, "m"); got != before {
		t.Errorf("already-selected click bumped version %d → %d",
			before, got)
	}
}

// TestSelectGalleryLayer_UnknownNoop guards the registry check so a
// stale click path cannot ping SetBaseLayer / SetLayerVisible with
// garbage layerIDs.
func TestSelectGalleryLayer_UnknownNoop(t *testing.T) {
	w := &gui.Window{}
	AddLayer(w, "m", Layer{
		LayerID: "osm", Source: fakeSource{}, Kind: LayerKindBase,
		Visible: true,
	})
	before := readVersion(w, "m")
	selectGalleryLayer(w, "m", "ghost")
	if got := readVersion(w, "m"); got != before {
		t.Errorf("unknown layerID bumped version %d → %d", before, got)
	}
}

// TestGalleryCardA11YLabel freezes the "Name, base map, state" string
// so a well-meaning phrasing change has to acknowledge the a11y impact.
func TestGalleryCardA11YLabel(t *testing.T) {
	cases := []struct {
		label    string
		selected bool
		want     string
	}{
		{"OSM", true, "OSM, base map, selected"},
		{"Satellite", false, "Satellite, base map, not selected"},
	}
	for _, c := range cases {
		got := galleryCardA11YLabel(c.label, c.selected)
		if got != c.want {
			t.Errorf("(%q,%v) = %q, want %q", c.label, c.selected, got, c.want)
		}
	}
}

// TestGalleryCardA11YDescription freezes the paired hint.
func TestGalleryCardA11YDescription(t *testing.T) {
	if got := galleryCardA11YDescription(false); got !=
		"Press space to set as base map" {
		t.Errorf("unselected hint = %q", got)
	}
	if got := galleryCardA11YDescription(true); got !=
		"Already the active base map" {
		t.Errorf("selected hint = %q", got)
	}
}

// TestGalleryLetter covers the three branches: uppercase, unicode
// first rune, and the "?" fallback for empty / whitespace-only labels.
func TestGalleryLetter(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Satellite", "S"},
		{"osm", "O"},
		{"  Terrain", "T"},
		{"Éire", "É"},
		{"", "?"},
		{"   ", "?"},
	}
	for _, c := range cases {
		if got := galleryLetter(c.in); got != c.want {
			t.Errorf("galleryLetter(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestGalleryFallbackColor_Deterministic locks seed → color so a
// palette change is a deliberate act. Also confirms the 64-floor so
// no channel bottoms out near black.
func TestGalleryFallbackColor_Deterministic(t *testing.T) {
	a := galleryFallbackColor("osm")
	b := galleryFallbackColor("osm")
	if a != b {
		t.Errorf("same seed → different colors: %+v vs %+v", a, b)
	}
	if a.R < 64 || a.G < 64 || a.B < 64 {
		t.Errorf("channel below 64-floor: %+v", a)
	}
	if c := galleryFallbackColor("sat"); c == a {
		t.Errorf("distinct seeds produced identical color: %+v", a)
	}
}

// TestGallery_ReferenceEntryPromotes confirms Entries may include a
// Reference-kind layer; clicking it promotes to Base via SetBaseLayer
// and demotes the prior base. Documents the no-filter policy — any
// registered layer may live in the gallery.
func TestGallery_ReferenceEntryPromotes(t *testing.T) {
	w := &gui.Window{}
	AddLayer(w, "m", Layer{
		LayerID: "osm", Source: fakeSource{}, Kind: LayerKindBase,
		Visible: true,
	})
	AddLayer(w, "m", Layer{
		LayerID: "topo", Source: fakeSource{}, Kind: LayerKindReference,
		Visible: true,
	})
	SetBaseLayer(w, "m", "topo")
	prev, _ := readLayers(w, "m").Get("osm")
	if prev.Kind != LayerKindReference {
		t.Errorf("prior base Kind = %v, want Reference (demoted)", prev.Kind)
	}
	next, _ := readLayers(w, "m").Get("topo")
	if next.Kind != LayerKindBase {
		t.Errorf("new base Kind = %v, want Base", next.Kind)
	}
}

// TestGallery_ZeroThumbSizeCoerces confirms ThumbSize:0 falls back to
// the package default. Verified indirectly — stand up a card for an
// entry with no ThumbnailURL so the fallback Column surfaces the size
// in its Width / Height fields, then reach in via the layout pass.
func TestGallery_ZeroThumbSizeCoerces(t *testing.T) {
	w := &gui.Window{}
	AddLayer(w, "m", Layer{
		LayerID: "osm", Source: fakeSource{}, Kind: LayerKindBase,
		Visible: true,
	})
	v := buildGallery(w, GalleryCfg{
		MapID:   "m",
		Entries: []GalleryEntry{{LayerID: "osm", Label: "OSM"}},
	})
	card := v.Content()[0].Content()[0]
	thumb := card.Content()[0]
	lo := thumb.GenerateLayout(w)
	if lo.Shape == nil {
		t.Fatal("fallback thumb produced nil Shape")
	}
	// Expect the default size (80) — any refactor that sets thumb to
	// zero would render a degenerate tile.
	if lo.Shape.Width != defaultGalleryThumbSize {
		t.Errorf("fallback thumb Width = %v, want %v",
			lo.Shape.Width, defaultGalleryThumbSize)
	}
}
