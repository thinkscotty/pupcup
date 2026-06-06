package anim

import "math"

// Spring eases one scalar (a position, an angle, a scale) toward a target with
// critical damping: it settles quickly with no oscillation and is interruptible
// — retarget at any time and motion stays continuous because velocity carries
// across. It is the motion primitive behind the rotary selector's snap and the
// squash/scale envelopes.
//
// Omega (rad/s) sets stiffness: larger snaps faster. Roughly 12–25 reads as
// lively for UI; 6–10 feels gentle.
//
// Step integrates with the closed-form critical-damping solution rather than
// Euler steps, so it is unconditionally stable for any dt — a long, janky frame
// can never make it explode — and exact at every step size.
type Spring struct {
	Pos    float64
	Vel    float64
	Target float64
	Omega  float64
}

// NewSpring returns a spring at rest at pos with stiffness omega.
func NewSpring(pos, omega float64) Spring {
	return Spring{Pos: pos, Target: pos, Omega: omega}
}

// Step advances the spring by dt seconds and returns the new position. dt <= 0
// is a no-op. With Omega <= 0 the spring force is disabled and the position
// simply drifts at its current velocity.
func (s *Spring) Step(dt float64) float64 {
	if dt <= 0 {
		return s.Pos
	}
	if s.Omega <= 0 {
		s.Pos += s.Vel * dt
		return s.Pos
	}
	// Critically damped solution of x'' + 2ωx' + ω²x = 0 about the target:
	//   x(t) = (x0 + (v0 + ω·x0)·t)·e^{-ωt},  v(t) = (v0 - ω·(v0 + ω·x0)·t)·e^{-ωt}
	x := s.Pos - s.Target // displacement from rest
	e := math.Exp(-s.Omega * dt)
	b := s.Vel + s.Omega*x
	s.Pos = s.Target + (x+b*dt)*e
	s.Vel = (s.Vel - s.Omega*b*dt) * e
	return s.Pos
}

// SetTarget retargets the spring while preserving position and velocity, so an
// in-flight motion redirects smoothly.
func (s *Spring) SetTarget(t float64) { s.Target = t }

// AtRest reports whether the spring has effectively settled at its target:
// position within eps of Target and velocity below eps.
func (s *Spring) AtRest(eps float64) bool {
	return math.Abs(s.Pos-s.Target) < eps && math.Abs(s.Vel) < eps
}
