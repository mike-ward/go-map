package mapview

import (
	"math"
	"testing"

	"github.com/mike-ward/go-gui/gui"
	"github.com/mike-ward/go-map/projection"
)

// Map factory must reject empty Cfg.ID — the registry key is the
// only thing that ties state to a widget, so silently accepting ""
// would have multiple maps share state.
func TestMap_PanicsOnEmptyID(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Map(Cfg{}) did not panic")
		}
	}()
	_ = Map(Cfg{})
}

// InitialZoom > maxZoomF must clamp at construction so the seed
// (and therefore the Home key) lands inside the renderable range.
func TestMap_ClampsInitialZoom(t *testing.T) {
	v := Map(Cfg{ID: "x", InitialZoom: maxZoomF + 10})
	mv, ok := v.(*mapView)
	if !ok {
		t.Fatalf("Map returned %T, want *mapView", v)
	}
	if mv.cfg.InitialZoom != maxZoomF {
		t.Errorf("InitialZoom = %g, want %g", mv.cfg.InitialZoom, maxZoomF)
	}
}

// NaN / ±Inf in InitialZoom must collapse to the default seed (2),
// not propagate through the registry.
func TestMap_SanitizesInitialZoomNonFinite(t *testing.T) {
	for _, z := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		v := Map(Cfg{ID: "x", InitialZoom: z})
		mv := v.(*mapView)
		if math.IsNaN(mv.cfg.InitialZoom) || math.IsInf(mv.cfg.InitialZoom, 0) {
			t.Errorf("InitialZoom=%v not sanitized: got %v", z, mv.cfg.InitialZoom)
		}
	}
}

// NaN coordinates in InitialCenter must be neutralized at
// construction; otherwise the first frame seeds the registry with
// NaN and every subsequent computation propagates it.
func TestMap_SanitizesInitialCenterNaN(t *testing.T) {
	v := Map(Cfg{
		ID:            "x",
		InitialCenter: projection.LatLng{Lat: math.NaN(), Lng: math.NaN()},
		InitialZoom:   5,
	})
	mv := v.(*mapView)
	if math.IsNaN(mv.cfg.InitialCenter.Lat) ||
		math.IsNaN(mv.cfg.InitialCenter.Lng) {
		t.Errorf("InitialCenter still contains NaN: %+v",
			mv.cfg.InitialCenter)
	}
}

// Zero-value Cfg (other than ID) must populate sensible defaults so
// a minimal Map(Cfg{ID:"x"}) renders without further setup.
func TestMap_DefaultsAppliedOnZeroCfg(t *testing.T) {
	v := Map(Cfg{ID: "x"})
	mv := v.(*mapView)
	if mv.cfg.Sizing != gui.FillFill {
		t.Errorf("Sizing = %+v, want FillFill", mv.cfg.Sizing)
	}
	if !mv.cfg.Background.IsSet() {
		t.Error("Background not set to default")
	}
	if mv.cfg.InitialZoom == 0 {
		t.Error("InitialZoom still 0; expected default seed")
	}
	if mv.cfg.ScrollZoomGain != 1 {
		t.Errorf("ScrollZoomGain = %g, want 1 (default)",
			mv.cfg.ScrollZoomGain)
	}
}

// Invalid ScrollZoomGain (zero, negative, NaN, ±Inf) must coerce to 1
// so a stray author value cannot silently disable wheel zoom.
func TestMap_SanitizesInvalidScrollZoomGain(t *testing.T) {
	for _, g := range []float32{
		0, -0.5, float32(math.NaN()),
		float32(math.Inf(1)), float32(math.Inf(-1)),
	} {
		v := Map(Cfg{ID: "x", ScrollZoomGain: g})
		mv := v.(*mapView)
		if mv.cfg.ScrollZoomGain != 1 {
			t.Errorf("gain=%v: got %g, want 1", g, mv.cfg.ScrollZoomGain)
		}
	}
}

// Valid sub-1 gain must survive the factory so consumers can opt into
// fractional-per-notch zoom (slice 5b UX).
func TestMap_PreservesValidScrollZoomGain(t *testing.T) {
	v := Map(Cfg{ID: "x", ScrollZoomGain: 0.25})
	mv := v.(*mapView)
	if mv.cfg.ScrollZoomGain != 0.25 {
		t.Errorf("ScrollZoomGain = %g, want 0.25",
			mv.cfg.ScrollZoomGain)
	}
}
