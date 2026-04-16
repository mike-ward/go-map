// Package projection provides Web Mercator projection math and WGS84
// coordinate types for slippy-tile maps.
package projection

import "math"

// LatLng is a WGS84 coordinate in degrees.
type LatLng struct {
	Lat float64 // -90..90
	Lng float64 // -180..180
}

// Bounds is an axis-aligned rectangle in WGS84 coordinates.
type Bounds struct {
	SW LatLng
	NE LatLng
}

// Point is a 2D pixel position.
type Point struct {
	X, Y float64
}

// TileSize is the fixed pixel edge length of a slippy tile.
const TileSize = 256

// maxMercatorLat is the max |latitude| representable in Web Mercator
// (where the projection diverges). Standard slippy value.
const maxMercatorLat = 85.05112878

// Clamp returns a LatLng bounded by Web Mercator limits.
func (p LatLng) Clamp() LatLng {
	lat := math.Max(-maxMercatorLat, math.Min(maxMercatorLat, p.Lat))
	lng := math.Mod(p.Lng+540, 360) - 180
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
