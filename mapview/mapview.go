// Package mapview provides the interactive slippy-tile map widget.
//
// The widget is a go-gui View built on DrawCanvas. It owns pan/zoom
// state in the Window state registry (namespace "mapview.state",
// keyed by Cfg.ID), fetches tiles asynchronously through a
// tile.Source, and renders overlays (markers, polylines, attribution)
// via the draw context.
//
// Immediate-mode convention: the Widget factory re-runs every frame.
// Initial* fields on Cfg seed the registry on the first frame only;
// subsequent frames read the persistent state. Consumers mutate state
// through package-level helpers (PanTo, SetZoom, SetView, Snapshot).
package mapview

import (
	"github.com/mike-ward/go-gui/gui"
	"github.com/mike-ward/go-map/projection"
	"github.com/mike-ward/go-map/tile"
)

// Cfg configures a Widget. ID is required.
type Cfg struct {
	// Identity
	ID string

	// Sizing
	Sizing    gui.Sizing
	Width     float32
	Height    float32
	MinWidth  float32
	MaxWidth  float32
	MinHeight float32
	MaxHeight float32

	// Focus (tab-order index; zero means not focusable)
	IDFocus uint32

	// Initial viewport (seeds first-frame state only; ignored after)
	InitialCenter projection.LatLng
	InitialZoom   float64

	// InitialOverlays seed the overlay registry on the first frame
	// only; subsequent frames read from the registry. Authors wanting
	// to add/remove overlays after first render call AddOverlay /
	// RemoveOverlay from event callbacks.
	InitialOverlays []Overlay

	// InitialLayers seed the layer registry on the first frame only.
	// Exactly one Base layer is expected; extras are demoted at seed
	// time. Authors add / remove layers after first render via
	// AddLayer / RemoveLayer / SetBaseLayer. When both InitialLayers
	// and Source are set, InitialLayers wins.
	InitialLayers []Layer

	// Data. Source is shorthand for a single Base layer keyed
	// "base"; ignored when InitialLayers is non-empty.
	Source tile.Source

	// Appearance
	Background gui.Color

	// ScrollZoomGain scales raw wheel/trackpad ScrollY before it feeds
	// the zoom accumulator. Defaults to 1.0 — one notch-wheel click
	// advances one full zoom level, matching conventional slippy-map
	// feel. Set below 1.0 to trade wheel speed for fractional-zoom
	// precision on notch hardware (e.g. 0.25 → four clicks per full
	// zoom level, each landing on a fractional rest state). Values ≤ 0
	// or non-finite fall back to 1.0.
	ScrollZoomGain float32

	// Accessibility
	A11YLabel       string
	A11YDescription string

	// Events. Callbacks run on the UI goroutine; do not block.
	// Fired only when the relevant state actually changes; the first
	// frame seeds the comparison baseline and does not fire either
	// callback.
	OnMove       func(*gui.Window, MapState)
	OnZoomChange func(*gui.Window, float64)
	// OnClick fires on a mouse-down / mouse-up pair that did not drag
	// past dragThresholdPx. The LatLng is the projected position of
	// the up-point. If the click hits an overlay, OnPOISelect runs
	// first; OnClick still fires after (authors can discriminate via
	// the overlay callback).
	OnClick     func(*gui.Window, projection.LatLng)
	OnPOISelect func(*gui.Window, Overlay)
}

// fireDecision is the pure-function core of fireCallbacks. Returns
// the next baseline plus flags for which callbacks (if any) the
// caller should invoke. Splitting this out from the registry plumbing
// makes the delta logic unit-testable without a Window.
func fireDecision(prev lastFired, s MapState) (next lastFired, fireMove, fireZoom bool) {
	if !prev.Set {
		return lastFired{State: s, Set: true}, false, false
	}
	if prev.State == s {
		return prev, false, false
	}
	return lastFired{State: s, Set: true},
		prev.State.Center != s.Center,
		prev.State.Zoom != s.Zoom
}

// fireCallbacks invokes OnMove / OnZoomChange when the current
// snapshot differs from the last-fired snapshot. Maintains its own
// state-registry slot so callback semantics stay independent of the
// public MapState lifecycle.
func fireCallbacks(w *gui.Window, c Cfg, s MapState) {
	prev := nsRead[lastFired](w, nsLastFired, c.ID)
	next, fireMove, fireZoom := fireDecision(prev, s)
	if next != prev {
		nsWrite(w, nsLastFired, c.ID, next)
	}
	if fireMove && c.OnMove != nil {
		c.OnMove(w, s)
	}
	if fireZoom && c.OnZoomChange != nil {
		c.OnZoomChange(w, s.Zoom)
	}
}

// Map returns a map View. Cfg.ID must be non-empty; it is the key
// for all per-map state in the Window registry.
//
// InitialZoom is clamped to maxZoom so a stray Cfg value cannot
// permanently park the seed (and therefore the Home key) outside
// the renderable range. InitialCenter is run through Clamp so NaN /
// ±Inf coordinates can never reach the viewport math.
func Map(cfg Cfg) gui.View {
	if cfg.ID == "" {
		panic("mapview: Cfg.ID is required")
	}
	if cfg.Sizing == (gui.Sizing{}) {
		cfg.Sizing = gui.FillFill
	}
	if !cfg.Background.IsSet() {
		cfg.Background = gui.Hex(0xE8E6E0)
	}
	cfg.InitialZoom = clampZoom(cfg.InitialZoom)
	if cfg.InitialZoom == 0 {
		cfg.InitialZoom = 2
	}
	if !isFiniteF32(cfg.ScrollZoomGain) || cfg.ScrollZoomGain <= 0 {
		cfg.ScrollZoomGain = 1
	}
	cfg.InitialCenter = cfg.InitialCenter.Clamp()
	for _, o := range cfg.InitialOverlays {
		if o == nil || o.ID() == "" {
			panic("mapview: InitialOverlays entry missing non-empty ID")
		}
	}
	for _, l := range cfg.InitialLayers {
		if l.LayerID == "" {
			panic("mapview: InitialLayers entry missing non-empty LayerID")
		}
	}
	return &mapView{cfg: cfg}
}

// mapView is the custom View implementation. It re-reads persistent
// state from the Window registry each frame (GenerateLayout runs
// once per frame) and captures the snapshot into the DrawCanvas
// OnDraw closure. Version is driven by nsVersion (bumped on every
// MapState / hover / overlay mutation) so OnDraw re-executes only on
// state change — stable frames replay the cached tessellation.
type mapView struct {
	cfg Cfg
}

func (*mapView) Content() []gui.View { return nil }

func (mv *mapView) GenerateLayout(w *gui.Window) gui.Layout {
	c := mv.cfg
	seed := MapState{
		Center: c.InitialCenter.Clamp(),
		Zoom:   c.InitialZoom,
	}
	s := readState(w, c.ID, seed)
	seedOnce(w, c)

	// Fire delta-driven callbacks before the draw closure captures
	// state. Skip the first frame so consumers do not see a synthetic
	// "change" matching the seed they already supplied.
	fireCallbacks(w, c, s)

	// Re-read after fireCallbacks. An OnMove / OnZoomChange callback
	// may call PanTo / SetZoom mid-frame, which bumps the DrawCanvas
	// version. If the closure keeps the pre-callback s, OnDraw would
	// render the stale state but stash it under the new version key —
	// the next frame's cache hit then replays stale geometry. Capturing
	// the post-callback state keeps tessellation coherent with the
	// version DrawCanvasCfg reports below.
	s = readState(w, c.ID, seed)

	// Capture state by value into the OnDraw closure. Reads happen
	// here (on the UI goroutine) so the draw pass never touches the
	// registry. The overlay map is shared by reference — mutations
	// happen through event callbacks that run between frames, never
	// during OnDraw, so no snapshot copy is required.
	layers := orderedLayers(w, c.ID)
	hover := nsRead[hoverState](w, nsHover, c.ID)
	overlays := readOverlays(w, c.ID)
	// Resolve the focused marker once per frame; drawFocus and
	// stateForA11Y both need it and the BoundedMap Get is cheap but
	// non-zero — no reason to pay for it twice.
	focused := focusedMarker(overlays, s)
	onDraw := func(dc *gui.DrawContext) {
		// Stash canvas dims for the Overview widget to read next
		// frame. nsCanvas is NOT in the invalidatesRender whitelist —
		// a write from inside OnDraw bumping version would feedback-
		// loop the cache, mirroring the nsInfoRect rule.
		nsWrite(w, nsCanvas, c.ID, canvasSize{W: dc.Width, H: dc.Height})
		vp := computeViewport(dc.Width, dc.Height, s)
		drawTiles(dc, vp, layers)
		drawOverlays(dc, vp, overlays)
		drawScaleBar(dc, s)
		drawCoordReadout(dc, vp, s, hover)
		drawZoomIndicator(dc, s.Zoom)
		drawHomeButton(dc)
		drawAttribution(dc, layers)
		// Focus ring + InfoWindow paint last so the popup sits above
		// the HUD chrome a screen-reader user cannot otherwise navigate
		// past. drawFocus stashes the rendered popup rect in the state
		// registry for onMouseDown to consume click-through.
		drawFocus(w, c.ID, dc, vp, focused, s)
	}

	a11y := c.A11YDescription
	if a11y == "" {
		a11y = debouncedA11Y(w, c.ID, stateForA11Y(s, focused))
	}

	inner := gui.DrawCanvas(gui.DrawCanvasCfg{
		ID:              c.ID,
		A11YLabel:       c.A11YLabel,
		A11YDescription: a11y,
		Version:         readVersion(w, c.ID),
		Sizing:          c.Sizing,
		Width:           c.Width,
		Height:          c.Height,
		MinWidth:        c.MinWidth,
		MaxWidth:        c.MaxWidth,
		MinHeight:       c.MinHeight,
		MaxHeight:       c.MaxHeight,
		IDFocus:         c.IDFocus,
		Color:           c.Background,
		Clip:            true,
		OnDraw:          onDraw,
		OnClick:         onMouseDown(c, seed),
		OnMouseScroll:   onMouseScroll(c.ID, c.ScrollZoomGain),
		OnMouseMove:     onMouseMove(c.ID),
		OnMouseLeave:    onMouseLeave(c.ID),
		OnKeyDown:       onKeyDown(c, seed),
	})
	return inner.GenerateLayout(w)
}

// seedOnce seeds Cfg.InitialOverlays and Cfg.InitialLayers on the
// first frame only. The nsSeeded flag fires unconditionally so an
// immediate-mode consumer that populates seeds on a later frame
// cannot trigger a delayed reseed.
func seedOnce(w *gui.Window, c Cfg) {
	if nsRead[bool](w, nsSeeded, c.ID) {
		return
	}
	if len(c.InitialOverlays) > 0 {
		bm := readOverlays(w, c.ID)
		for _, o := range c.InitialOverlays {
			bm.Set(o.ID(), o)
		}
	}
	seedInitialLayers(w, c)
	nsWrite(w, nsSeeded, c.ID, true)
}

// seedInitialLayers writes Cfg.InitialLayers (or the Cfg.Source
// shorthand) into the layer registry. Extra Base entries past the
// first are demoted to Reference so exactly one Base is ever active.
func seedInitialLayers(w *gui.Window, c Cfg) {
	bm := readLayers(w, c.ID)
	if len(c.InitialLayers) > 0 {
		sawBase := false
		for _, l := range c.InitialLayers {
			if l.Kind == LayerKindBase {
				if sawBase {
					l.Kind = LayerKindReference
				} else {
					sawBase = true
				}
			}
			bm.Set(l.LayerID, normalizeLayer(l))
		}
		return
	}
	if c.Source != nil {
		bm.Set("base", normalizeLayer(Layer{
			LayerID: "base",
			Source:  c.Source,
			Kind:    LayerKindBase,
			Visible: true,
		}))
	}
}

// drawOverlays renders each overlay whose projected bounding box
// intersects the canvas. BoundedMap.Range walks insertion order so
// the overlay added last draws on top; the hit-test in panDragEnd
// keeps the final match to agree with that ordering. See
// overlayVisible for the antimeridian-straddle handling.
func drawOverlays(dc *gui.DrawContext, vp viewport, overlays *gui.BoundedMap[string, Overlay]) {
	worldPx := projection.WorldSizeF(vp.Z)
	minX := float64(vp.OriginX)
	maxX := float64(vp.OriginX + vp.W)
	minY := float64(vp.OriginY)
	maxY := float64(vp.OriginY + vp.H)
	overlays.Range(func(_ string, o Overlay) bool {
		if overlayVisible(o, vp.Z, worldPx, minX, maxX, minY, maxY) {
			o.Draw(dc, vp)
		}
		return true
	})
}

// overlayVisible is the culling predicate for drawOverlays. Pure
// function — no DrawContext, no state registry — so the antimeridian
// logic can be unit-tested directly. Accepts fractional zoom.
func overlayVisible(o Overlay, z float64, worldPx, minX, maxX, minY, maxY float64) bool {
	b := o.Bounds()
	ne := projection.ProjectF(b.NE, z)
	sw := projection.ProjectF(b.SW, z)
	oMinX, oMaxX := sw.X, ne.X
	oMinY, oMaxY := ne.Y, sw.Y
	if oMaxY < minY || oMinY > maxY {
		return false
	}
	// Viewport X can exceed [0, worldPx) when the user has panned
	// across the antimeridian. Accept a match at any integer world
	// shift in {-1, 0, +1}; tiles.go does the same for tile pulls.
	for _, shift := range [3]float64{0, worldPx, -worldPx} {
		if oMaxX+shift >= minX && oMinX+shift <= maxX {
			return true
		}
	}
	return false
}
