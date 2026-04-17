package mapview

import (
	"math"

	"github.com/mike-ward/go-gui/gui"
	"github.com/mike-ward/go-map/projection"
)

// Projector maps geographic coordinates to screen-pixel coordinates
// within the current frame's viewport. Overlays receive a Projector on
// Draw and HitTest so they never see the internal viewport struct —
// keeps the Overlay contract stable across rendering changes.
type Projector interface {
	// LatLngToScreen returns the canvas-pixel coordinates of p.
	LatLngToScreen(p projection.LatLng) (x, y float32)
	// MetersToPixels converts a ground-distance at the given latitude
	// into screen pixels for the current zoom. Used by Circle overlays
	// so radii expressed in meters scale correctly with zoom.
	MetersToPixels(lat, meters float64) float32
	// Zoom reports the active integer zoom level. Overlays may use it
	// for level-of-detail culling; cheap to ignore.
	Zoom() uint32
}

// Overlay is the common contract for authorable map features. Drawn
// after tiles and before the HUD. Each overlay must return a stable
// non-empty ID that is unique within a single map; the ID is the
// state-registry key used by AddOverlay / RemoveOverlay.
type Overlay interface {
	ID() string
	Bounds() projection.Bounds
	Draw(dc *gui.DrawContext, pr Projector)
	HitTest(pr Projector, sx, sy float32) bool
}

// markerHitRadiusPx is the hit-test tolerance for Marker overlays.
// Chosen to comfortably exceed the drawn pin glyph so the click target
// is forgiving on high-DPI displays without creeping into neighbor
// markers at standard density.
const markerHitRadiusPx float32 = 12

// maxOverlayPoints bounds the vertex count Draw/HitTest will process
// per overlay. A pathological value — author bug, malicious import —
// would otherwise allocate 8 MiB per frame at the cap (float32 × 2 ×
// 1e6) or loop unboundedly. Overlays above the cap silently skip
// rendering.
const maxOverlayPoints = 1_000_000

// sanitizeStroke clamps a pen-width to a finite positive value. NaN /
// ±Inf / non-positive values fall back to fallback.
func sanitizeStroke(w, fallback float32) float32 {
	f := float64(w)
	if math.IsNaN(f) || math.IsInf(f, 0) || w <= 0 {
		return fallback
	}
	// Cap oversized widths so a bogus author value cannot drive the
	// renderer to allocate absurd tessellation buffers.
	if w > 1024 {
		return 1024
	}
	return w
}

// finitePositive returns true when v is a finite value > 0.
func finitePositive(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v > 0
}

// colorOr returns c when the author set it, fallback otherwise.
// Centralises the default-color dance repeated across every overlay's
// Draw method.
func colorOr(c, fallback gui.Color) gui.Color {
	if c.IsSet() {
		return c
	}
	return fallback
}

// Marker is a point overlay rendered as a pin glyph. Label is required
// for accessibility; OnClick is optional and, if set, causes marker
// clicks to fire through the map's OnPOISelect callback first and then
// the marker's own OnClick.
//
// Title and Body feed the InfoWindow popup opened by Enter on a
// keyboard-focused marker (or by a future programmatic hook). Empty
// Title suppresses the popup entirely; the focus ring still draws so
// decorative markers remain reachable via Tab.
type Marker struct {
	MarkerID string
	Pos      projection.LatLng
	Label    string
	Title    string
	Body     string
	Color    gui.Color
	OnClick  func(*gui.Window)
}

// ID returns the marker's stable key.
func (m *Marker) ID() string { return m.MarkerID }

// Bounds returns a degenerate box at the marker position. Keeps the
// culling path uniform even though a marker is a single point.
func (m *Marker) Bounds() projection.Bounds {
	p := m.Pos.Clamp()
	return projection.Bounds{NE: p, SW: p}
}

// Draw renders the marker as a filled circle with a contrasting
// outline.
func (m *Marker) Draw(dc *gui.DrawContext, pr Projector) {
	x, y := pr.LatLngToScreen(m.Pos)
	dc.FilledCircle(x, y, 6, colorOr(m.Color, gui.Hex(0xD0342C)))
	dc.Circle(x, y, 6, gui.Hex(0xFFFFFF), 2)
}

// HitTest returns true when (sx, sy) is within markerHitRadiusPx of
// the marker's screen position.
func (m *Marker) HitTest(pr Projector, sx, sy float32) bool {
	x, y := pr.LatLngToScreen(m.Pos)
	dx, dy := sx-x, sy-y
	return dx*dx+dy*dy <= markerHitRadiusPx*markerHitRadiusPx
}

// Polyline is an open path of geographic points.
type Polyline struct {
	LineID      string
	Points      []projection.LatLng
	StrokeColor gui.Color
	StrokeWidth float32
	Label       string
}

// ID returns the polyline's stable key.
func (p *Polyline) ID() string { return p.LineID }

// Bounds returns the axis-aligned box covering all vertices. Empty
// polylines return a zero Bounds; the renderer skips them anyway via
// the len(Points) < 2 guard in Draw.
func (p *Polyline) Bounds() projection.Bounds {
	return projection.BoundsOf(p.Points...)
}

// Draw renders the polyline by projecting every vertex. Allocates a
// scratch slice sized to 2×len(Points) per frame; a widget-owned
// buffer would eliminate this but requires threading scratch state
// through the Projector call path — deferred until the per-frame alloc
// shows up in a profile.
func (p *Polyline) Draw(dc *gui.DrawContext, pr Projector) {
	if len(p.Points) < 2 || len(p.Points) > maxOverlayPoints {
		return
	}
	buf := make([]float32, 0, len(p.Points)*2)
	for _, pt := range p.Points {
		x, y := pr.LatLngToScreen(pt)
		buf = append(buf, x, y)
	}
	dc.Polyline(buf, colorOr(p.StrokeColor, gui.Hex(0x2C6FD0)),
		sanitizeStroke(p.StrokeWidth, 2))
}

// HitTest returns true when (sx, sy) is within StrokeWidth+2 pixels of
// any segment. Linear scan.
func (p *Polyline) HitTest(pr Projector, sx, sy float32) bool {
	if len(p.Points) < 2 || len(p.Points) > maxOverlayPoints {
		return false
	}
	tol := sanitizeStroke(p.StrokeWidth, 2) + 2
	if tol < 4 {
		tol = 4
	}
	tol2 := tol * tol
	x0, y0 := pr.LatLngToScreen(p.Points[0])
	for i := 1; i < len(p.Points); i++ {
		x1, y1 := pr.LatLngToScreen(p.Points[i])
		if segmentDistSq(sx, sy, x0, y0, x1, y1) <= tol2 {
			return true
		}
		x0, y0 = x1, y1
	}
	return false
}

// Polygon is a filled, closed ring of geographic points. Holes are
// not supported; model interior cutouts as separate overlays.
type Polygon struct {
	PolyID      string
	Ring        []projection.LatLng
	FillColor   gui.Color
	StrokeColor gui.Color
	StrokeWidth float32
	Label       string
}

// ID returns the polygon's stable key.
func (p *Polygon) ID() string { return p.PolyID }

// Bounds returns the axis-aligned box covering all ring vertices.
func (p *Polygon) Bounds() projection.Bounds {
	return projection.BoundsOf(p.Ring...)
}

// Draw fills the ring then strokes its outline when StrokeWidth > 0.
// Rings above maxOverlayPoints are skipped; see Polyline.Draw.
func (p *Polygon) Draw(dc *gui.DrawContext, pr Projector) {
	if len(p.Ring) < 3 || len(p.Ring) > maxOverlayPoints {
		return
	}
	buf := make([]float32, 0, (len(p.Ring)+1)*2)
	for _, pt := range p.Ring {
		x, y := pr.LatLngToScreen(pt)
		buf = append(buf, x, y)
	}
	dc.FilledPolygon(buf, colorOr(p.FillColor,
		gui.Color{R: 44, G: 111, B: 208, A: 80}))
	if p.StrokeWidth > 0 {
		// Close the ring for the stroke pass; Polyline does not auto-
		// close so the author-supplied open ring would leave a gap.
		buf = append(buf, buf[0], buf[1])
		dc.Polyline(buf, colorOr(p.StrokeColor, gui.Hex(0x2C6FD0)),
			sanitizeStroke(p.StrokeWidth, 1))
	}
}

// HitTest returns true when (sx, sy) is inside the ring, via the
// even-odd crossing-number rule.
func (p *Polygon) HitTest(pr Projector, sx, sy float32) bool {
	if len(p.Ring) < 3 || len(p.Ring) > maxOverlayPoints {
		return false
	}
	inside := false
	xj, yj := pr.LatLngToScreen(p.Ring[len(p.Ring)-1])
	for i := range p.Ring {
		xi, yi := pr.LatLngToScreen(p.Ring[i])
		if (yi > sy) != (yj > sy) {
			xIntersect := (xj-xi)*(sy-yi)/(yj-yi) + xi
			if sx < xIntersect {
				inside = !inside
			}
		}
		xj, yj = xi, yi
	}
	return inside
}

// Circle is a ground-scaled circle: RadiusMeters stays constant in
// geographic units, so screen radius grows with zoom.
type Circle struct {
	CircleID     string
	Center       projection.LatLng
	RadiusMeters float64
	FillColor    gui.Color
	StrokeColor  gui.Color
	StrokeWidth  float32
	Label        string
}

// ID returns the circle's stable key.
func (c *Circle) ID() string { return c.CircleID }

// Bounds returns an approximate lat/lng box containing the circle.
// Accuracy is sufficient for culling; small at typical radii, grows
// near the poles but the visible world is clamped there anyway.
// Non-finite or non-positive radii yield a degenerate box at Center
// so the visibility test reliably culls the overlay.
func (c *Circle) Bounds() projection.Bounds {
	ctr := c.Center.Clamp()
	if !finitePositive(c.RadiusMeters) {
		return projection.Bounds{NE: ctr, SW: ctr}
	}
	const metersPerDegLat = 111320.0
	dLat := c.RadiusMeters / metersPerDegLat
	cosLat := math.Cos(ctr.Lat * math.Pi / 180)
	if cosLat < 1e-6 {
		cosLat = 1e-6
	}
	dLng := c.RadiusMeters / (metersPerDegLat * cosLat)
	// Cap the deltas so a huge radius cannot push the corners outside
	// the representable Mercator band; the world wraps anyway and the
	// culling layer only needs a conservative bound.
	if dLat > 90 {
		dLat = 90
	}
	if dLng > 180 {
		dLng = 180
	}
	return projection.Bounds{
		NE: projection.LatLng{Lat: ctr.Lat + dLat, Lng: ctr.Lng + dLng},
		SW: projection.LatLng{Lat: ctr.Lat - dLat, Lng: ctr.Lng - dLng},
	}
}

// Draw fills the disc and strokes the outline when StrokeWidth > 0.
// Non-finite or sub-pixel radii short-circuit before any gui call so
// the draw backend never receives NaN / ±Inf geometry.
func (c *Circle) Draw(dc *gui.DrawContext, pr Projector) {
	if !finitePositive(c.RadiusMeters) {
		return
	}
	x, y := pr.LatLngToScreen(c.Center)
	r := pr.MetersToPixels(c.Center.Lat, c.RadiusMeters)
	if !(r > 0.5) { // NaN / Inf / negative all fall through here
		return
	}
	dc.FilledCircle(x, y, r, colorOr(c.FillColor,
		gui.Color{R: 44, G: 111, B: 208, A: 60}))
	if c.StrokeWidth > 0 {
		dc.Circle(x, y, r, colorOr(c.StrokeColor, gui.Hex(0x2C6FD0)),
			sanitizeStroke(c.StrokeWidth, 1))
	}
}

// HitTest returns true when (sx, sy) is within the circle's screen
// radius. Non-finite or non-positive radii never match.
func (c *Circle) HitTest(pr Projector, sx, sy float32) bool {
	if !finitePositive(c.RadiusMeters) {
		return false
	}
	x, y := pr.LatLngToScreen(c.Center)
	r := pr.MetersToPixels(c.Center.Lat, c.RadiusMeters)
	if !(r > 0) {
		return false
	}
	dx, dy := sx-x, sy-y
	return dx*dx+dy*dy <= r*r
}

// segmentDistSq returns the squared distance from point (px,py) to the
// finite segment (ax,ay)-(bx,by). Pure function; isolates the quadratic
// so Polyline.HitTest can stay readable.
func segmentDistSq(px, py, ax, ay, bx, by float32) float32 {
	dx, dy := bx-ax, by-ay
	lenSq := dx*dx + dy*dy
	if lenSq == 0 {
		ex, ey := px-ax, py-ay
		return ex*ex + ey*ey
	}
	t := ((px-ax)*dx + (py-ay)*dy) / lenSq
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	cx, cy := ax+t*dx, ay+t*dy
	ex, ey := px-cx, py-cy
	return ex*ex + ey*ey
}
