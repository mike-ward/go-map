package mapview

import (
	"github.com/mike-ward/go-gui/gui"
	"github.com/mike-ward/go-map/tile"
)

// LayerKind distinguishes exclusive base layers from stackable reference
// layers. Overlays (Marker / Polyline / Polygon / Circle) remain on the
// separate nsOverlays path — layers hold tile Sources only.
type LayerKind uint8

const (
	// LayerKindBase is the exclusive background tile source. At most one
	// layer is marked Base at any time; SetBaseLayer demotes the prior
	// base to Reference so no tile data disappears on a switch.
	LayerKindBase LayerKind = iota
	// LayerKindReference stacks over the base in insertion order.
	LayerKindReference
)

// Layer is one tile-source slot on a map. Zero-value Opacity seeds to
// 1.0 so Layer{LayerID, Source, Kind, Visible:true} renders at full
// strength without explicit Opacity. Opacity ≤ 0 is treated as hidden.
//
// Fractional opacity is not yet honored at draw time: go-gui's
// DrawContext.Image modulates the background color, not the image
// texture. The field is kept for API stability; visibility is binary
// until upstream gains texture-alpha support.
type Layer struct {
	LayerID string
	// Name is the human-readable display string consumed by the
	// Legend / layer-gallery widgets (Phase 3, not yet shipped).
	// Unused by the render path; safe to leave empty until then.
	Name    string
	Source  tile.Source
	Kind    LayerKind
	Opacity float32
	Visible bool
}

// capLayersPerMap caps stacked reference layers. 32 is well above any
// practical legend; the BoundedMap evicts the oldest on overflow.
const capLayersPerMap = 32

// readLayers returns the live layer map for id. Mutators and readers
// share the same pointer; BoundedMap.Range iterates in insertion order
// which drives deterministic render + attribution ordering.
func readLayers(w *gui.Window, id string) *gui.BoundedMap[string, Layer] {
	return readRegistryMap[Layer](w, nsLayers, id, capLayersPerMap)
}

// normalizeLayer coerces zero-value Opacity to 1.0 and clamps to [0, 1].
// Applied on every write path so draw-time code can assume sane values.
// Asymmetry with SetLayerOpacity is intentional: Layer{} literals treat
// an unset Opacity as "full strength" so authors do not have to spell
// out Opacity: 1 in every struct, while SetLayerOpacity(_, 0) keeps
// zero as a deliberate hide — the mutator is the only path to an
// invisible-via-opacity layer.
func normalizeLayer(l Layer) Layer {
	if !isFiniteF32(l.Opacity) || l.Opacity <= 0 {
		l.Opacity = 1
	} else if l.Opacity > 1 {
		l.Opacity = 1
	}
	return l
}

// orderedLayers returns the visible layers in render order: the base
// layer first (if any and visible), then references in insertion order.
// Returned slice is owned by the caller; safe to retain across frames
// since Layer is a value type. Allocates once per frame (slot 0
// reserved for the base and back-filled after the Range) so the draw
// loop sees a single contiguous slice without a merge copy.
func orderedLayers(w *gui.Window, id string) []Layer {
	bm := readLayers(w, id)
	n := bm.Len()
	if n == 0 {
		return nil
	}
	out := make([]Layer, 1, n)
	hasBase := false
	bm.Range(func(_ string, l Layer) bool {
		if !l.Visible || l.Opacity <= 0 {
			return true
		}
		if l.Kind == LayerKindBase {
			out[0] = l
			hasBase = true
			return true
		}
		out = append(out, l)
		return true
	})
	if !hasBase {
		return out[1:]
	}
	return out
}

// baseLayer returns the current base layer and true when one exists.
// Used by MaxZoom clamping and the single-source fallback in input.
func baseLayer(w *gui.Window, id string) (found Layer, hasBase bool) {
	readLayers(w, id).Range(func(_ string, l Layer) bool {
		if l.Kind == LayerKindBase {
			found, hasBase = l, true
			return false
		}
		return true
	})
	return
}

// AddLayer registers l on the map identified by id. l.LayerID must be
// non-empty; duplicates replace the prior entry in place. When l.Kind
// is LayerKindBase, any existing base is demoted to LayerKindReference
// so exactly one base is ever active.
//
// The in-Range bm.Set below relies on BoundedMap.Set updating
// m.data[key] in place (no m.order mutation) when key already exists
// — a future refactor that reorders on update would corrupt this
// iteration.
func AddLayer(w *gui.Window, id string, l Layer) {
	if l.LayerID == "" {
		return
	}
	bm := readLayers(w, id)
	if l.Kind == LayerKindBase {
		bm.Range(func(k string, existing Layer) bool {
			if existing.Kind == LayerKindBase && k != l.LayerID {
				existing.Kind = LayerKindReference
				bm.Set(k, existing)
			}
			return true
		})
	}
	bm.Set(l.LayerID, normalizeLayer(l))
	bumpVersion(w, id)
}

// RemoveLayer deletes the layer with layerID. No-op (and no version
// bump) if absent.
func RemoveLayer(w *gui.Window, id, layerID string) {
	bm := readLayers(w, id)
	if !bm.Contains(layerID) {
		return
	}
	bm.Delete(layerID)
	bumpVersion(w, id)
}

// SetBaseLayer promotes layerID to LayerKindBase. The prior base (if
// any) is demoted to LayerKindReference rather than hidden, so the
// switch is reversible without re-registering tile sources. No-op when
// layerID is absent. See AddLayer on the in-Range bm.Set dependency.
func SetBaseLayer(w *gui.Window, id, layerID string) {
	bm := readLayers(w, id)
	target, ok := bm.Get(layerID)
	if !ok {
		return
	}
	if target.Kind == LayerKindBase {
		return
	}
	bm.Range(func(k string, l Layer) bool {
		if l.Kind == LayerKindBase && k != layerID {
			l.Kind = LayerKindReference
			bm.Set(k, l)
		}
		return true
	})
	target.Kind = LayerKindBase
	bm.Set(layerID, target)
	bumpVersion(w, id)
}

// SetLayerVisible toggles the visibility of layerID. No-op when absent
// or when the current value already matches.
func SetLayerVisible(w *gui.Window, id, layerID string, visible bool) {
	bm := readLayers(w, id)
	l, ok := bm.Get(layerID)
	if !ok || l.Visible == visible {
		return
	}
	l.Visible = visible
	bm.Set(layerID, l)
	bumpVersion(w, id)
}

// SetLayerOpacity updates the opacity of layerID, clamped to [0, 1].
// Non-finite values fall back to 1.0. Note: go-gui does not yet modulate
// tile textures by opacity; this field controls only the binary
// visibility cutoff (opacity ≤ 0 hides the layer) until upstream lands
// texture-alpha support.
func SetLayerOpacity(w *gui.Window, id, layerID string, opacity float32) {
	bm := readLayers(w, id)
	l, ok := bm.Get(layerID)
	if !ok {
		return
	}
	switch {
	case !isFiniteF32(opacity):
		l.Opacity = 1
	case opacity < 0:
		l.Opacity = 0
	case opacity > 1:
		l.Opacity = 1
	default:
		l.Opacity = opacity
	}
	bm.Set(layerID, l)
	bumpVersion(w, id)
}
