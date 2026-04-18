// Package mapview is a stub used by a11ylint analysistest fixtures.
// Only the shape required by the analyzer — named structs at the same
// import path — is reproduced. Field sets are minimal; nothing
// depends on the Pos / Points / Ring / Center fields beyond their
// presence.
package mapview

type LatLng struct{ Lat, Lng float64 }

type Marker struct {
	MarkerID string
	Pos      LatLng
	Label    string
	Title    string
}

type Polyline struct {
	LineID string
	Points []LatLng
	Label  string
}

type Polygon struct {
	PolyID string
	Ring   []LatLng
	Label  string
}

type Circle struct {
	CircleID     string
	Center       LatLng
	RadiusMeters float64
	Label        string
}

type InfoWindowAction struct {
	Label   string
	OnClick func()
}
