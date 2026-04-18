package mapview

import (
	"math"
	"time"

	"github.com/mike-ward/go-gui/gui"
	"github.com/mike-ward/go-map/projection"
)

// Kinetic pan constants. Tuned against trackpad flicks at 60 fps;
// revisit if deeper input samples (e.g. high-rate touch) land.
const (
	// kineticDecayTau is the time constant of velocity decay.
	// v(t) = v0 * exp(-t / tau), so v falls to ~37% in tau and
	// ~5% in 3*tau (~900ms at 300ms tau). Balances "long glide"
	// vs "stops feeling unresponsive to the next input".
	kineticDecayTau = 0.3

	// kineticStopSpeed is the world-px/sec floor: below this the
	// animation stops. Low enough that the final pixels of a glide
	// are imperceptible; high enough to avoid a multi-frame trailing
	// tick on every fling.
	kineticStopSpeed = 20.0

	// kineticStartSpeed is the minimum release velocity (world-px/sec)
	// that launches a fling. Short, deliberate drags (e.g. a 5 px
	// nudge to center a POI) should not fling.
	kineticStartSpeed = 80.0

	// kineticStaleWindow: if more than this elapsed since the last
	// mouse sample before release, reject the velocity as stale (user
	// paused mid-drag, then released). Prevents an "old velocity"
	// fling from a tracked-but-not-moving drag.
	kineticStaleWindow = 50 * time.Millisecond

	// kineticSampleEMAAlpha weights the newest mouse delta against the
	// running EMA. 0.5 gives roughly the last 2 samples ~75% weight at
	// 16 ms sampling — responsive to direction changes without
	// amplifying jitter on a single noisy sample.
	kineticSampleEMAAlpha = 0.5
)

// kineticAnimationID returns the per-map animation id used by
// w.AnimationAdd / AnimationRemove. A stable per-map id lets any
// state-mutating code path cancel an in-flight fling without a shared
// handle — the ID is computable from Cfg.ID alone.
func kineticAnimationID(mapID string) string {
	return "mapview.kinetic." + mapID
}

// kineticFling is a custom gui.Animation (go-gui v0.12.5+) that
// slides the map's center under an exponentially-decaying velocity
// sampled from the drag-release cursor motion. Stops itself by
// returning false from Update once speed drops below
// kineticStopSpeed.
//
// Center shift is computed at the map's *current* zoom so a mid-
// fling zoom does not produce a visible speedup — vx/vy are sampled
// in world-px at startZoom, and the scale factor re-maps them to
// the current zoom's world-pixel grid. (User-driven zoom cancels
// the fling outright via cancelKineticPan; the scaling is defence-
// in-depth for any future mid-fling zoom path.)
type kineticFling struct {
	mapID     string
	animID    string
	startZoom float64
	vx, vy    float64
	stopped   bool
}

// ID implements gui.Animation.
func (k *kineticFling) ID() string { return k.animID }

// RefreshKind requests a full layout each tick so the map redraws at
// the new center. Render-only would skip the OnDraw closure and
// leave tiles at the pre-fling viewport.
func (k *kineticFling) RefreshKind() gui.AnimationRefreshKind {
	return gui.AnimationRefreshLayout
}

// IsStopped implements gui.Animation.
func (k *kineticFling) IsStopped() bool { return k.stopped }

// SetStart implements gui.Animation. Not used — the animation loop
// passes dt to Update directly, so start time is unnecessary.
func (k *kineticFling) SetStart(_ time.Time) {}

// Update decays velocity, shifts center, and returns false once
// speed falls below kineticStopSpeed — the animation loop retires
// stopped animations automatically.
func (k *kineticFling) Update(
	w *gui.Window, dt float32, _ *gui.AnimationCommands,
) bool {
	if k.stopped {
		return false
	}
	if dt <= 0 || !isFinite(float64(dt)) {
		return true
	}
	decay := math.Exp(-float64(dt) / kineticDecayTau)
	k.vx *= decay
	k.vy *= decay
	if math.Hypot(k.vx, k.vy) < kineticStopSpeed {
		k.stopped = true
		return false
	}
	s := nsRead[MapState](w, nsState, k.mapID)
	scale := math.Exp2(s.Zoom - k.startZoom)
	p := projection.ProjectF(s.Center, s.Zoom)
	p.X += k.vx * float64(dt) * scale
	p.Y += k.vy * float64(dt) * scale
	s.Center = projection.UnprojectF(p, s.Zoom).Clamp()
	nsWrite(w, nsState, k.mapID, s)
	return true
}

// sampleKineticVelocity updates p's world-pixel velocity EMA from a
// single MouseMove sample. World-px is the drag code's natural unit:
// panDragMove computes center shift as `start + (screenDelta)` at
// StartZoom, so one screen pixel equals one world pixel at that zoom.
// Signs are flipped from the screen delta because center moves
// opposite the cursor ("content follows cursor" feel).
//
// Split from panDragMove so the EMA math is unit-testable without
// standing up a fake Event dispatch.
func sampleKineticVelocity(p *panState, mx, my float32, now time.Time) {
	if !p.LastT.IsZero() {
		dt := now.Sub(p.LastT).Seconds()
		if dt > 0 {
			// World-px velocity = -(screen delta) / dt. Negative because
			// a screen-right mouse move pans center to the left.
			ivx := float64(p.LastX-mx) / dt
			ivy := float64(p.LastY-my) / dt
			p.VelX = kineticSampleEMAAlpha*ivx +
				(1-kineticSampleEMAAlpha)*p.VelX
			p.VelY = kineticSampleEMAAlpha*ivy +
				(1-kineticSampleEMAAlpha)*p.VelY
		}
	}
	p.LastX = mx
	p.LastY = my
	p.LastT = now
}

// spawnKineticPan starts a fling from the given panState's sampled
// velocity, unless (a) the drag never crossed the threshold, (b) the
// last sample is staler than kineticStaleWindow (user paused mid-
// drag), or (c) speed is below kineticStartSpeed (drag was
// deliberate, not flung). Returns true when a fling was launched.
//
// Takes the current wall-clock time as an argument so tests control
// staleness without mocking time.Now globally.
func spawnKineticPan(
	w *gui.Window, mapID string, p panState, now time.Time,
) bool {
	if !p.Moved {
		return false
	}
	if p.LastT.IsZero() || now.Sub(p.LastT) > kineticStaleWindow {
		return false
	}
	if math.Hypot(p.VelX, p.VelY) < kineticStartSpeed {
		return false
	}
	w.AnimationAdd(&kineticFling{
		mapID:     mapID,
		animID:    kineticAnimationID(mapID),
		startZoom: p.StartZoom,
		vx:        p.VelX,
		vy:        p.VelY,
	})
	return true
}

// cancelKineticPan stops any in-flight fling for the given map.
// Idempotent — safe to call from every state-mutating code path
// (new drag, keyboard pan, wheel zoom, SetView) without first
// checking whether a fling is active.
func cancelKineticPan(w *gui.Window, mapID string) {
	w.AnimationRemove(kineticAnimationID(mapID))
}
