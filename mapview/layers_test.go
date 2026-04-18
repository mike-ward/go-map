package mapview

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/mike-ward/go-gui/gui"
	"github.com/mike-ward/go-map/projection"
	"github.com/mike-ward/go-map/tile"
)

// fakeSource is a minimal tile.Source stub that yields deterministic
// Attribution / MaxZoom values so layer tests can distinguish layers.
type fakeSource struct {
	attr string
	max  uint32
}

func (f fakeSource) Fetch(context.Context, tile.Coord) ([]byte, error) {
	return nil, tile.ErrNotFound
}
func (f fakeSource) URL(tile.Coord) string { return "" }
func (f fakeSource) Attribution() string   { return f.attr }
func (f fakeSource) MaxZoom() uint32       { return f.max }

func TestAddLayer_RejectsEmptyID(t *testing.T) {
	w := &gui.Window{}
	AddLayer(w, "m", Layer{Source: fakeSource{}, Visible: true})
	if bm := readLayers(w, "m"); bm.Len() != 0 {
		t.Errorf("empty LayerID was registered: len=%d", bm.Len())
	}
	if v := readVersion(w, "m"); v != 0 {
		t.Errorf("rejected AddLayer bumped version to %d", v)
	}
}

func TestAddLayer_ZeroOpacityDefaultsToOne(t *testing.T) {
	w := &gui.Window{}
	AddLayer(w, "m", Layer{
		LayerID: "base", Source: fakeSource{}, Kind: LayerKindBase,
		Visible: true,
	})
	got, _ := readLayers(w, "m").Get("base")
	if got.Opacity != 1 {
		t.Errorf("zero Opacity = %v, want normalized to 1", got.Opacity)
	}
}

func TestAddLayer_DemotesExistingBase(t *testing.T) {
	w := &gui.Window{}
	AddLayer(w, "m", Layer{
		LayerID: "a", Source: fakeSource{}, Kind: LayerKindBase, Visible: true,
	})
	AddLayer(w, "m", Layer{
		LayerID: "b", Source: fakeSource{}, Kind: LayerKindBase, Visible: true,
	})
	bm := readLayers(w, "m")
	a, _ := bm.Get("a")
	b, _ := bm.Get("b")
	if a.Kind != LayerKindReference {
		t.Errorf("first base not demoted: Kind=%v", a.Kind)
	}
	if b.Kind != LayerKindBase {
		t.Errorf("new base not promoted: Kind=%v", b.Kind)
	}
}

func TestSetBaseLayer_Swaps(t *testing.T) {
	w := &gui.Window{}
	AddLayer(w, "m", Layer{
		LayerID: "a", Source: fakeSource{}, Kind: LayerKindBase, Visible: true,
	})
	AddLayer(w, "m", Layer{
		LayerID: "b", Source: fakeSource{}, Kind: LayerKindReference, Visible: true,
	})
	SetBaseLayer(w, "m", "b")
	bm := readLayers(w, "m")
	a, _ := bm.Get("a")
	b, _ := bm.Get("b")
	if a.Kind != LayerKindReference {
		t.Errorf("a after swap: Kind=%v, want Reference", a.Kind)
	}
	if b.Kind != LayerKindBase {
		t.Errorf("b after swap: Kind=%v, want Base", b.Kind)
	}
}

func TestSetBaseLayer_NoOpAbsent(t *testing.T) {
	w := &gui.Window{}
	AddLayer(w, "m", Layer{
		LayerID: "a", Source: fakeSource{}, Kind: LayerKindBase, Visible: true,
	})
	before := readVersion(w, "m")
	SetBaseLayer(w, "m", "nope")
	if got := readVersion(w, "m"); got != before {
		t.Errorf("absent SetBaseLayer bumped %d → %d", before, got)
	}
}

func TestSetBaseLayer_NoOpAlreadyBase(t *testing.T) {
	w := &gui.Window{}
	AddLayer(w, "m", Layer{
		LayerID: "a", Source: fakeSource{}, Kind: LayerKindBase, Visible: true,
	})
	before := readVersion(w, "m")
	SetBaseLayer(w, "m", "a")
	if got := readVersion(w, "m"); got != before {
		t.Errorf("already-base SetBaseLayer bumped %d → %d", before, got)
	}
}

func TestRemoveLayer_NoOpAbsent(t *testing.T) {
	w := &gui.Window{}
	before := readVersion(w, "m")
	RemoveLayer(w, "m", "nope")
	if got := readVersion(w, "m"); got != before {
		t.Errorf("absent RemoveLayer bumped %d → %d", before, got)
	}
}

func TestSetLayerVisible_NoOpWhenEqual(t *testing.T) {
	w := &gui.Window{}
	AddLayer(w, "m", Layer{
		LayerID: "a", Source: fakeSource{}, Visible: true,
	})
	before := readVersion(w, "m")
	SetLayerVisible(w, "m", "a", true)
	if got := readVersion(w, "m"); got != before {
		t.Errorf("same-value Visible bumped %d → %d", before, got)
	}
}

func TestSetLayerOpacity_Clamps(t *testing.T) {
	w := &gui.Window{}
	AddLayer(w, "m", Layer{
		LayerID: "a", Source: fakeSource{}, Visible: true,
	})
	cases := []struct {
		in, want float32
	}{
		{-0.5, 0},
		{0, 0},
		{0.5, 0.5},
		{1, 1},
		{2, 1},
		{float32(math.NaN()), 1},
	}
	for _, c := range cases {
		SetLayerOpacity(w, "m", "a", c.in)
		got, _ := readLayers(w, "m").Get("a")
		if got.Opacity != c.want {
			t.Errorf("in=%v → Opacity=%v, want %v", c.in, got.Opacity, c.want)
		}
	}
}

func TestOrderedLayers_BaseFirstThenReferences(t *testing.T) {
	w := &gui.Window{}
	AddLayer(w, "m", Layer{
		LayerID: "r1", Source: fakeSource{}, Kind: LayerKindReference, Visible: true,
	})
	AddLayer(w, "m", Layer{
		LayerID: "base", Source: fakeSource{}, Kind: LayerKindBase, Visible: true,
	})
	AddLayer(w, "m", Layer{
		LayerID: "r2", Source: fakeSource{}, Kind: LayerKindReference, Visible: true,
	})
	got := orderedLayers(w, "m")
	if len(got) != 3 {
		t.Fatalf("got %d layers, want 3", len(got))
	}
	if got[0].LayerID != "base" {
		t.Errorf("order[0]=%q, want base", got[0].LayerID)
	}
	if got[1].LayerID != "r1" || got[2].LayerID != "r2" {
		t.Errorf("reference order=%q,%q, want r1,r2",
			got[1].LayerID, got[2].LayerID)
	}
}

func TestOrderedLayers_SkipsHidden(t *testing.T) {
	w := &gui.Window{}
	AddLayer(w, "m", Layer{
		LayerID: "base", Source: fakeSource{}, Kind: LayerKindBase, Visible: true,
	})
	AddLayer(w, "m", Layer{
		LayerID: "hidden", Source: fakeSource{}, Kind: LayerKindReference, Visible: false,
	})
	got := orderedLayers(w, "m")
	if len(got) != 1 || got[0].LayerID != "base" {
		t.Errorf("ordered=%v, want [base] only", got)
	}
}

func TestOrderedLayers_SkipsZeroOpacity(t *testing.T) {
	w := &gui.Window{}
	AddLayer(w, "m", Layer{
		LayerID: "base", Source: fakeSource{}, Kind: LayerKindBase, Visible: true,
	})
	AddLayer(w, "m", Layer{
		LayerID: "faded", Source: fakeSource{}, Kind: LayerKindReference, Visible: true,
	})
	SetLayerOpacity(w, "m", "faded", 0)
	got := orderedLayers(w, "m")
	if len(got) != 1 || got[0].LayerID != "base" {
		t.Errorf("ordered=%v, want [base] only", got)
	}
}

func TestComposeAttribution_DedupeAndOrder(t *testing.T) {
	layers := []Layer{
		{LayerID: "a", Source: fakeSource{attr: "OSM"}, Kind: LayerKindBase, Visible: true, Opacity: 1},
		{LayerID: "b", Source: fakeSource{attr: "OSM"}, Kind: LayerKindReference, Visible: true, Opacity: 1},
		{LayerID: "c", Source: fakeSource{attr: "Transit"}, Kind: LayerKindReference, Visible: true, Opacity: 1},
	}
	if got := composeAttribution(layers); got != "OSM | Transit" {
		t.Errorf("composeAttribution = %q, want %q", got, "OSM | Transit")
	}
}

func TestComposeAttribution_SkipsEmptyAndNil(t *testing.T) {
	layers := []Layer{
		{LayerID: "nil", Source: nil, Visible: true, Opacity: 1},
		{LayerID: "empty", Source: fakeSource{attr: ""}, Visible: true, Opacity: 1},
		{LayerID: "ok", Source: fakeSource{attr: "X"}, Visible: true, Opacity: 1},
	}
	if got := composeAttribution(layers); got != "X" {
		t.Errorf("composeAttribution = %q, want %q", got, "X")
	}
}

func TestBaseMaxZoom_UsesBaseLayer(t *testing.T) {
	w := &gui.Window{}
	AddLayer(w, "m", Layer{
		LayerID: "base", Source: fakeSource{max: 18}, Kind: LayerKindBase, Visible: true,
	})
	AddLayer(w, "m", Layer{
		LayerID: "ref", Source: fakeSource{max: 22}, Kind: LayerKindReference, Visible: true,
	})
	if got := baseMaxZoom(w, "m"); got != 18 {
		t.Errorf("baseMaxZoom=%d, want 18 (from base, ignoring ref)", got)
	}
}

func TestBaseMaxZoom_NoBaseYieldsMaxZoom(t *testing.T) {
	w := &gui.Window{}
	AddLayer(w, "m", Layer{
		LayerID: "ref", Source: fakeSource{max: 10}, Kind: LayerKindReference, Visible: true,
	})
	if got := baseMaxZoom(w, "m"); got != maxZoom {
		t.Errorf("baseMaxZoom with no base=%d, want %d", got, maxZoom)
	}
}

// seedInitialLayers: Cfg.Source shorthand seeds one Base layer keyed
// "base" with Name empty (Name is for Legend/gallery — distinct from
// Attribution). Every demo uses this shorthand path.
func TestSeedInitialLayers_SourceShorthand(t *testing.T) {
	w := &gui.Window{}
	c := Cfg{ID: "m", Source: fakeSource{attr: "©OSM", max: 19}}
	seedInitialLayers(w, c)
	bm := readLayers(w, "m")
	l, ok := bm.Get("base")
	if !ok {
		t.Fatal("shorthand did not seed a base layer")
	}
	if l.Kind != LayerKindBase {
		t.Errorf("shorthand Kind=%v, want Base", l.Kind)
	}
	if !l.Visible || l.Opacity != 1 {
		t.Errorf("shorthand Visible=%v Opacity=%v, want true,1",
			l.Visible, l.Opacity)
	}
	if l.Name != "" {
		t.Errorf("shorthand Name=%q, want empty (attribution ≠ display name)",
			l.Name)
	}
}

// seedInitialLayers: explicit InitialLayers overrides Cfg.Source and
// demotes extra Base entries to Reference (mutual exclusion at seed).
func TestSeedInitialLayers_ExplicitWinsAndDemotes(t *testing.T) {
	w := &gui.Window{}
	c := Cfg{
		ID:     "m",
		Source: fakeSource{attr: "IGNORED"},
		InitialLayers: []Layer{
			{LayerID: "a", Source: fakeSource{attr: "A"}, Kind: LayerKindBase, Visible: true},
			{LayerID: "b", Source: fakeSource{attr: "B"}, Kind: LayerKindBase, Visible: true},
		},
	}
	seedInitialLayers(w, c)
	bm := readLayers(w, "m")
	if _, ok := bm.Get("base"); ok {
		t.Error("shorthand base leaked when InitialLayers set")
	}
	a, _ := bm.Get("a")
	b, _ := bm.Get("b")
	if a.Kind != LayerKindBase {
		t.Errorf("first entry Kind=%v, want Base", a.Kind)
	}
	if b.Kind != LayerKindReference {
		t.Errorf("second Base entry not demoted: Kind=%v", b.Kind)
	}
}

// Every layer mutator must bump so the DrawCanvas cache invalidates.
func TestLayerMutators_BumpVersion(t *testing.T) {
	cases := []struct {
		name string
		fn   func(w *gui.Window)
	}{
		{"AddLayer", func(w *gui.Window) {
			AddLayer(w, "m", Layer{LayerID: "a", Source: fakeSource{}, Visible: true})
		}},
		{"RemoveLayer", func(w *gui.Window) {
			AddLayer(w, "m", Layer{LayerID: "a", Source: fakeSource{}, Visible: true})
			RemoveLayer(w, "m", "a")
		}},
		{"SetBaseLayer", func(w *gui.Window) {
			AddLayer(w, "m", Layer{LayerID: "a", Source: fakeSource{}, Kind: LayerKindBase, Visible: true})
			AddLayer(w, "m", Layer{LayerID: "b", Source: fakeSource{}, Kind: LayerKindReference, Visible: true})
			SetBaseLayer(w, "m", "b")
		}},
		{"SetLayerVisible", func(w *gui.Window) {
			AddLayer(w, "m", Layer{LayerID: "a", Source: fakeSource{}, Visible: true})
			SetLayerVisible(w, "m", "a", false)
		}},
		{"SetLayerOpacity", func(w *gui.Window) {
			AddLayer(w, "m", Layer{LayerID: "a", Source: fakeSource{}, Visible: true})
			SetLayerOpacity(w, "m", "a", 0.5)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := &gui.Window{}
			c.fn(w)
			if v := readVersion(w, "m"); v == 0 {
				t.Errorf("%s did not bump version", c.name)
			}
		})
	}
}

// Map() must panic when InitialLayers contains an entry with an empty
// LayerID — mirrors the InitialOverlays contract. Catches the factory
// losing its validation loop in a refactor; at runtime AddLayer
// silently drops empty IDs, so the factory is the only place the
// panic-on-bad-config contract lives.
func TestMap_PanicsOnInitialLayerEmptyID(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Map did not panic on empty InitialLayers.LayerID")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "InitialLayers") {
			t.Errorf("panic = %v, want message mentioning InitialLayers", r)
		}
	}()
	Map(Cfg{
		ID: "m",
		InitialLayers: []Layer{
			{LayerID: "", Source: fakeSource{}, Visible: true},
		},
	})
}

// composeAttribution must truncate per-string to maxAttributionBytes
// so a pathological Source returning megabyte-scale credits cannot
// drive per-frame TextWidth / HUD layout unbounded. Locks the
// hardening cap against a future refactor that drops truncateUTF8.
func TestComposeAttribution_TruncatesOversized(t *testing.T) {
	huge := strings.Repeat("a", maxAttributionBytes*10)
	layers := []Layer{{
		LayerID: "a", Source: fakeSource{attr: huge},
		Kind: LayerKindBase, Visible: true, Opacity: 1,
	}}
	got := composeAttribution(layers)
	// truncateUTF8 appends "…" (3 bytes in UTF-8) past the cap.
	if len(got) > maxAttributionBytes+len("…") {
		t.Errorf("len(got) = %d, want <= %d",
			len(got), maxAttributionBytes+len("…"))
	}
}

// Two Sources whose attribution strings diverge only past the cap
// must dedupe to a single joined entry once truncateUTF8 normalises
// them. Proves the truncation runs before the dedupe scan.
func TestComposeAttribution_DedupesAfterTruncation(t *testing.T) {
	prefix := strings.Repeat("b", maxAttributionBytes)
	layers := []Layer{
		{LayerID: "a", Source: fakeSource{attr: prefix + "TAIL1"},
			Kind: LayerKindBase, Visible: true, Opacity: 1},
		{LayerID: "b", Source: fakeSource{attr: prefix + "TAIL2"},
			Kind: LayerKindReference, Visible: true, Opacity: 1},
	}
	got := composeAttribution(layers)
	if strings.Contains(got, " | ") {
		t.Errorf("composeAttribution = %q, want single entry (post-truncation dedupe)", got)
	}
}

// After removing the base layer, baseLayer must report hasBase=false
// and baseMaxZoom must fall back to the global maxZoom — a dangling
// pointer to the removed source would leak a stale zoom cap and keep
// references pretending to constrain input.
func TestRemoveLayer_RemovingBaseClearsBase(t *testing.T) {
	w := &gui.Window{}
	AddLayer(w, "m", Layer{
		LayerID: "base", Source: fakeSource{max: 10},
		Kind: LayerKindBase, Visible: true,
	})
	AddLayer(w, "m", Layer{
		LayerID: "ref", Source: fakeSource{max: 15},
		Kind: LayerKindReference, Visible: true,
	})
	RemoveLayer(w, "m", "base")
	if _, ok := baseLayer(w, "m"); ok {
		t.Error("baseLayer still reports a base after RemoveLayer")
	}
	if got := baseMaxZoom(w, "m"); got != maxZoom {
		t.Errorf("baseMaxZoom = %d after base removal, want %d (global)",
			got, maxZoom)
	}
}

// drawTiles with a nil/empty layer slice must paint the placeholder
// checkerboard across the viewport so pan/zoom stays usable before
// any Source is wired. Check by the presence of placeholder Text
// entries (images don't emit Text; placeholders do).
func TestDrawTiles_NoLayersDrawsPlaceholders(t *testing.T) {
	dc := gui.NewDrawContext(400, 400, nil)
	vp := computeViewport(400, 400, MapState{
		Center: projection.LatLng{Lat: 0, Lng: 0}, Zoom: 2,
	})
	drawTiles(dc, vp, nil)
	if len(dc.Images()) != 0 {
		t.Errorf("got %d images, want 0 (no layers → no tile fetch)",
			len(dc.Images()))
	}
	if len(dc.Texts()) == 0 {
		t.Error("no placeholder text emitted; checkerboard missing")
	}
}

// A base Layer with Source=nil still needs to render the placeholder
// so the user sees *something* while a future SetBaseLayer completes
// wiring. Split base/reference paths: base keeps placeholderOK=true.
func TestDrawTiles_NilBaseSourceDrawsPlaceholder(t *testing.T) {
	dc := gui.NewDrawContext(400, 400, nil)
	vp := computeViewport(400, 400, MapState{
		Center: projection.LatLng{Lat: 0, Lng: 0}, Zoom: 2,
	})
	drawTiles(dc, vp, []Layer{{
		LayerID: "base", Source: nil, Kind: LayerKindBase,
		Visible: true, Opacity: 1,
	}})
	if len(dc.Images()) != 0 {
		t.Errorf("nil Source emitted %d images, want 0",
			len(dc.Images()))
	}
	if len(dc.Texts()) == 0 {
		t.Error("nil base Source should still render placeholder text")
	}
}

// A reference Layer with Source=nil must NOT emit placeholders —
// a placeholder checkerboard from a reference would occlude the
// base layer beneath. Only the base path has placeholderOK=true.
func TestDrawTiles_NilReferenceSourceIsSilent(t *testing.T) {
	dc := gui.NewDrawContext(400, 400, nil)
	vp := computeViewport(400, 400, MapState{
		Center: projection.LatLng{Lat: 0, Lng: 0}, Zoom: 2,
	})
	baseTexts := 0
	{
		tdc := gui.NewDrawContext(400, 400, nil)
		drawTiles(tdc, vp, []Layer{{
			LayerID: "base", Source: stubSource{}, Kind: LayerKindBase,
			Visible: true, Opacity: 1,
		}})
		baseTexts = len(tdc.Texts())
	}
	drawTiles(dc, vp, []Layer{
		{LayerID: "base", Source: stubSource{}, Kind: LayerKindBase,
			Visible: true, Opacity: 1},
		{LayerID: "ref", Source: nil, Kind: LayerKindReference,
			Visible: true, Opacity: 1},
	})
	if got := len(dc.Texts()); got != baseTexts {
		t.Errorf("base+nilRef Texts = %d, want %d (same as base-only); "+
			"nil reference leaked placeholder text",
			got, baseTexts)
	}
}

// Base + reference both with URLs must both emit Image calls per tile
// cell. Locks the loop that iterates layers — a refactor that only
// renders the first layer would pass single-layer tests silently.
func TestDrawTiles_StacksBaseAndReference(t *testing.T) {
	dc := gui.NewDrawContext(400, 400, nil)
	vp := computeViewport(400, 400, MapState{
		Center: projection.LatLng{Lat: 0, Lng: 0}, Zoom: 2,
	})
	drawTiles(dc, vp, []Layer{
		{LayerID: "base", Source: stubSource{}, Kind: LayerKindBase,
			Visible: true, Opacity: 1},
		{LayerID: "ref", Source: stubSource{}, Kind: LayerKindReference,
			Visible: true, Opacity: 1},
	})
	imgs := dc.Images()
	if len(imgs) == 0 {
		t.Fatal("no images emitted for stacked layers")
	}
	if len(imgs)%2 != 0 {
		t.Errorf("image count %d not divisible by 2; base and "+
			"reference should emit one image per visible cell",
			len(imgs))
	}
}

// The review-bug regression lock: base layer passes the opaque
// placeholder gray as bgColor (hides decode-latency flashes);
// reference layer passes gui.Color{} so transparent tile pixels let
// the base show through. A regression that paints refs on opaque gray
// would silently occlude the base beneath boundary / label overlays.
func TestDrawTiles_ReferenceLayerHasTransparentBgColor(t *testing.T) {
	dc := gui.NewDrawContext(400, 400, nil)
	vp := computeViewport(400, 400, MapState{
		Center: projection.LatLng{Lat: 0, Lng: 0}, Zoom: 2,
	})
	drawTiles(dc, vp, []Layer{
		{LayerID: "base", Source: stubSource{}, Kind: LayerKindBase,
			Visible: true, Opacity: 1},
		{LayerID: "ref", Source: stubSource{}, Kind: LayerKindReference,
			Visible: true, Opacity: 1},
	})
	imgs := dc.Images()
	if len(imgs) < 2 || len(imgs)%2 != 0 {
		t.Fatalf("need even image count ≥ 2, got %d", len(imgs))
	}
	// drawTiles iterates layers in order; each layer walks tiles in
	// the same (ty, tx) grid order. So imgs[:half] are base, imgs[half:]
	// are reference.
	half := len(imgs) / 2
	var transparent gui.Color
	if imgs[0].BgColor == transparent {
		t.Errorf("base BgColor is transparent (%v); want opaque placeholder "+
			"gray to hide decode-latency flashes", imgs[0].BgColor)
	}
	if imgs[half].BgColor != transparent {
		t.Errorf("reference BgColor = %v, want transparent gui.Color{} "+
			"so base shows through transparent tile pixels",
			imgs[half].BgColor)
	}
}
