package mapview

import (
	"math"
	"testing"
	"time"

	"github.com/mike-ward/go-gui/gui"
	"github.com/mike-ward/go-map/projection"
)

// First call to sampleKineticVelocity seeds LastX/Y/T and leaves the
// EMA at zero — no prior sample means no velocity yet. Guards against
// a refactor that initializes velocity from a dangling previous call.
func TestSampleKineticVelocity_FirstCallSeedsOnly(t *testing.T) {
	p := panState{}
	now := time.Unix(0, 1_000_000_000)
	sampleKineticVelocity(&p, 100, 200, now)
	if p.VelX != 0 || p.VelY != 0 {
		t.Errorf("first call velocity = (%v,%v), want (0,0)", p.VelX, p.VelY)
	}
	if p.LastX != 100 || p.LastY != 200 || !p.LastT.Equal(now) {
		t.Errorf("first call did not seed Last*: %+v", p)
	}
}

// With two samples 10 ms apart the EMA must reflect the world-px
// velocity derived from the screen delta — screen-right movement
// yields negative VelX (center follows leftward per the pan sign
// convention).
func TestSampleKineticVelocity_TwoSamplesProduceEMA(t *testing.T) {
	p := panState{}
	t0 := time.Unix(0, 1_000_000_000)
	sampleKineticVelocity(&p, 0, 0, t0)
	t1 := t0.Add(10 * time.Millisecond)
	sampleKineticVelocity(&p, 100, 0, t1) // screen moved +100 px in 10 ms

	// Instant VelX = -(0-100)/0.01 = +10000 wait — sign flip:
	// ivx = (p.LastX - mx)/dt = (0 - 100)/0.01 = -10000. Center
	// moves left (negative world-x) at 10000 px/sec.
	// EMA with alpha=0.5 from zero: 0.5 * -10000 + 0.5 * 0 = -5000.
	wantVx := -5000.0
	if diff := math.Abs(p.VelX - wantVx); diff > 1e-6 {
		t.Errorf("VelX = %v, want %v (diff %v)", p.VelX, wantVx, diff)
	}
	if p.VelY != 0 {
		t.Errorf("VelY = %v, want 0 (no y movement)", p.VelY)
	}
}

// A pre-threshold drag (Moved=false) must not fling — the press was
// a click.
func TestSpawnKineticPan_RejectsClickLikeDrag(t *testing.T) {
	w := &gui.Window{}
	p := panState{
		Moved: false,
		VelX:  -1000,
		VelY:  0,
		LastT: time.Now(),
	}
	if spawnKineticPan(w, "m", p, time.Now()) {
		t.Error("click-like drag launched a fling; want false")
	}
}

// A drag whose last sample is older than kineticStaleWindow must not
// fling — user paused mid-drag before release.
func TestSpawnKineticPan_RejectsStaleLastSample(t *testing.T) {
	w := &gui.Window{}
	p := panState{
		Moved: true,
		VelX:  -1000,
		LastT: time.Now(),
	}
	releasedAt := p.LastT.Add(kineticStaleWindow + time.Millisecond)
	if spawnKineticPan(w, "m", p, releasedAt) {
		t.Error("stale drag launched a fling; want false")
	}
}

// Below kineticStartSpeed, a drag is deliberate and must not fling.
func TestSpawnKineticPan_RejectsLowSpeed(t *testing.T) {
	w := &gui.Window{}
	p := panState{
		Moved: true,
		VelX:  -(kineticStartSpeed - 1), // just below threshold
		LastT: time.Now(),
	}
	if spawnKineticPan(w, "m", p, time.Now()) {
		t.Error("low-speed drag launched a fling; want false")
	}
}

// A fresh, fast drag must launch a fling and register it under the
// per-map animation id so cancellation and replacement stay keyed.
func TestSpawnKineticPan_LaunchesOnGoodDrag(t *testing.T) {
	w := &gui.Window{}
	p := panState{
		Moved:     true,
		VelX:      -500,
		VelY:      300,
		StartZoom: 10,
		LastT:     time.Now(),
	}
	if !spawnKineticPan(w, "m", p, p.LastT) {
		t.Fatal("good drag did not launch a fling")
	}
	if !w.HasAnimation(kineticAnimationID("m")) {
		t.Error("animation not registered under the kinetic ID")
	}
}

// cancelKineticPan removes a registered animation; second call is a
// no-op (idempotent — required by every state-mutating code path
// calling it unconditionally).
func TestCancelKineticPan_Idempotent(t *testing.T) {
	w := &gui.Window{}
	p := panState{
		Moved:     true,
		VelX:      -500,
		StartZoom: 10,
		LastT:     time.Now(),
	}
	_ = spawnKineticPan(w, "m", p, p.LastT)

	cancelKineticPan(w, "m")
	if w.HasAnimation(kineticAnimationID("m")) {
		t.Error("first cancel did not remove the animation")
	}
	cancelKineticPan(w, "m") // must not panic or re-create
}

// A single Update tick must decay velocity by exp(-dt/tau) and shift
// center by the post-decay velocity * dt. Hand-computed expected
// values so a refactor can't silently change the physics.
func TestKineticFling_UpdateDecaysAndShifts(t *testing.T) {
	w := &gui.Window{}
	// Seed map state so nsRead/nsWrite can round-trip.
	nsWrite(w, nsState, "m", MapState{
		Center: projection.LatLng{Lat: 0, Lng: 0},
		Zoom:   10,
	})
	k := &kineticFling{
		mapID:     "m",
		animID:    kineticAnimationID("m"),
		startZoom: 10,
		vx:        -10000, // strong leftward fling
		vy:        0,
	}

	if !k.Update(w, 0.020, nil) {
		t.Fatal("Update returned false on active fling")
	}

	// With dt 20 ms and tau=0.3 s: decay = exp(-0.02/0.3) ≈ 0.9355.
	wantVx := -10000 * math.Exp(-0.02/kineticDecayTau)
	if diff := math.Abs(k.vx - wantVx); diff > 200 {
		t.Errorf("post-decay vx = %v, want ~%v", k.vx, wantVx)
	}
	s := nsRead[MapState](w, nsState, "m")
	// Center moved left (negative world-x) → negative longitude at
	// Lat=0. Any nonzero negative Lng passes; exact value depends on
	// the decay approximation.
	if s.Center.Lng >= 0 {
		t.Errorf("center.Lng = %v, want negative after leftward fling",
			s.Center.Lng)
	}
}

// Update must return false and flip IsStopped once speed drops below
// kineticStopSpeed — the animation loop retires stopped animations
// automatically, so IsStopped is the honest signal.
func TestKineticFling_UpdateStopsAtFloor(t *testing.T) {
	w := &gui.Window{}
	nsWrite(w, nsState, "m", MapState{Zoom: 10})
	k := &kineticFling{
		mapID:     "m",
		animID:    kineticAnimationID("m"),
		startZoom: 10,
		vx:        kineticStopSpeed + 1, // one tick below floor
	}

	// 200 ms at tau=0.3 s is about 2/3 of one time constant: decay
	// factor ≈ exp(-0.667) ≈ 0.513, so vx ends ~ 10.8 px/s — under
	// the stop threshold.
	ok := k.Update(w, 0.200, nil)

	if ok {
		t.Error("Update returned true, want false below stop speed")
	}
	if !k.IsStopped() {
		t.Error("IsStopped = false after falling below stop speed")
	}
}

// SetView (and by extension PanTo / SetZoom, which call it) must
// cancel a fling — a programmatic recenter overrides momentum.
func TestSetView_CancelsKineticPan(t *testing.T) {
	w := &gui.Window{}
	// Need a prior MapState entry so SetView does not no-op.
	nsWrite(w, nsState, "m", MapState{
		Center: projection.LatLng{Lat: 1, Lng: 1},
		Zoom:   5,
	})
	p := panState{
		Moved: true, VelX: -500, StartZoom: 5, LastT: time.Now(),
	}
	_ = spawnKineticPan(w, "m", p, p.LastT)
	if !w.HasAnimation(kineticAnimationID("m")) {
		t.Fatal("fling did not launch; test precondition failed")
	}

	SetView(w, "m", projection.LatLng{Lat: 10, Lng: 20}, 8)

	if w.HasAnimation(kineticAnimationID("m")) {
		t.Error("SetView did not cancel the in-flight fling")
	}
}
