// Package projection provides Web Mercator projection math and WGS84
// coordinate types for slippy-tile maps.
package projection

import "math"

// LatLng is a WGS84 coordinate in degrees. Latitudes outside the
// Web Mercator range (±85.05112878) are clamped by Clamp; longitudes
// wrap into the canonical (-180, 180] range.
type LatLng struct {
	Lat float64 // -85.05..85.05 after Clamp
	Lng float64 // -180..180   after Clamp
}

// Point is a 2D pixel position.
type Point struct {
	X, Y float64
}

// Bounds is an axis-aligned latitude/longitude box. NE must hold the
// larger latitude and (naïvely) the larger longitude — antimeridian-
// straddling boxes are not supported in this type; callers that need
// them should represent the crossing with two Bounds.
type Bounds struct {
	NE, SW LatLng
}

// Extend returns the smallest Bounds containing both b and p. Callers
// must seed b with a valid point (use BoundsOf); extending a zero
// Bounds would silently stretch the box down to (0, 0) on the next
// point, dropping the real extent for polylines whose first vertex is
// exactly (0, 0).
func (b Bounds) Extend(p LatLng) Bounds {
	p = p.Clamp()
	if p.Lat > b.NE.Lat {
		b.NE.Lat = p.Lat
	}
	if p.Lat < b.SW.Lat {
		b.SW.Lat = p.Lat
	}
	if p.Lng > b.NE.Lng {
		b.NE.Lng = p.Lng
	}
	if p.Lng < b.SW.Lng {
		b.SW.Lng = p.Lng
	}
	return b
}

// BoundsOf returns the smallest Bounds containing every supplied
// point. Zero-length input returns a zero Bounds (callers that care
// should check IsZero).
func BoundsOf(points ...LatLng) Bounds {
	if len(points) == 0 {
		return Bounds{}
	}
	p0 := points[0].Clamp()
	b := Bounds{NE: p0, SW: p0}
	for _, p := range points[1:] {
		b = b.Extend(p)
	}
	return b
}

// Center returns the midpoint of the bounds.
func (b Bounds) Center() LatLng {
	return LatLng{
		Lat: (b.NE.Lat + b.SW.Lat) / 2,
		Lng: (b.NE.Lng + b.SW.Lng) / 2,
	}
}

// IsZero reports whether b is the zero value.
func (b Bounds) IsZero() bool { return b == Bounds{} }

// TileSize is the fixed pixel edge length of a slippy tile.
const TileSize = 256

// maxMercatorLat is the max |latitude| representable in Web Mercator
// (where the projection diverges). Standard slippy value.
const maxMercatorLat = 85.05112878

// Clamp returns a LatLng bounded by Web Mercator limits. NaN and ±Inf
// inputs are replaced with 0; otherwise math operations would
// propagate them through Project/Unproject and silently corrupt all
// downstream viewport state.
func (p LatLng) Clamp() LatLng {
	lat := p.Lat
	if math.IsNaN(lat) || math.IsInf(lat, 0) {
		lat = 0
	}
	lng := p.Lng
	if math.IsNaN(lng) || math.IsInf(lng, 0) {
		lng = 0
	}
	lat = math.Max(-maxMercatorLat, math.Min(maxMercatorLat, lat))
	lng = math.Mod(lng+540, 360) - 180
	return LatLng{Lat: lat, Lng: lng}
}

// WorldSize returns the total pixel edge length of the world at zoom z.
// Equals TileSize * 2^z.
func WorldSize(z uint32) float64 {
	return float64(TileSize) * float64(uint64(1)<<z)
}

// Project converts a LatLng to world pixel coordinates at zoom z.
// Origin (0,0) is the top-left of tile (z,0,0).
func Project(p LatLng, z uint32) Point {
	p = p.Clamp()
	size := WorldSize(z)
	x := (p.Lng + 180) / 360 * size
	sinLat := math.Sin(p.Lat * math.Pi / 180)
	y := (0.5 - math.Log((1+sinLat)/(1-sinLat))/(4*math.Pi)) * size
	return Point{X: x, Y: y}
}

// Unproject converts world pixel coordinates to a LatLng at zoom z.
func Unproject(pt Point, z uint32) LatLng {
	size := WorldSize(z)
	lng := pt.X/size*360 - 180
	n := math.Pi - 2*math.Pi*pt.Y/size
	lat := 180 / math.Pi * math.Atan(math.Sinh(n))
	return LatLng{Lat: lat, Lng: lng}
}
