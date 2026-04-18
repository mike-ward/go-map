// Package a is a consumer-style fixture exercising the analyzer
// against every target overlay type plus a control case that must not
// report.
package a

import "github.com/mike-ward/go-map/mapview"

var emptyLabel = ""

func Bad() {
	_ = mapview.Marker{MarkerID: "m1"}  // want `Marker composite literal missing Label`
	_ = &mapview.Marker{MarkerID: "m2"} // want `Marker composite literal missing Label`
	_ = mapview.InfoWindowAction{}      // want `InfoWindowAction composite literal missing Label`

	// Empty string Label still trips the lint.
	_ = mapview.Marker{MarkerID: "m3", Label: ""} // want `Marker.Label is empty`
	// Constant-folded empty string trips too.
	_ = mapview.Marker{MarkerID: "m4", Label: "" + ""} // want `Marker.Label is empty`
}

func Good() {
	// Valid Label — no report.
	_ = mapview.Marker{MarkerID: "m1", Label: "home"}
	_ = mapview.InfoWindowAction{Label: "Zoom"}

	// Non-constant Label accepted — runtime value not decidable.
	_ = mapview.Marker{MarkerID: "m2", Label: emptyLabel}

	// Polyline / Polygon / Circle out of scope until their Label
	// reaches an a11y consumer. No report expected.
	_ = mapview.Polyline{LineID: "pl"}
	_ = mapview.Polygon{PolyID: "pg"}
	_ = mapview.Circle{CircleID: "c"}
}
