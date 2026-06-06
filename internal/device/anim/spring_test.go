package anim

import (
	"math"
	"testing"
)

func stepN(s *Spring, dt float64, n int) {
	for i := 0; i < n; i++ {
		s.Step(dt)
	}
}

func TestSpringConverges(t *testing.T) {
	s := NewSpring(0, 18)
	s.SetTarget(100)
	stepN(&s, 1.0/60, 180) // ~3 s
	if !s.AtRest(0.5) {
		t.Fatalf("spring not at rest after 3s: pos=%.4f vel=%.4f", s.Pos, s.Vel)
	}
	if math.Abs(s.Pos-100) > 0.5 {
		t.Fatalf("pos=%.4f, want ~100", s.Pos)
	}
}

// TestSpringNoOvershoot checks the defining property of critical damping: from
// rest, the position approaches the target monotonically and never passes it.
func TestSpringNoOvershoot(t *testing.T) {
	s := NewSpring(0, 18)
	s.SetTarget(100)
	maxPos := 0.0
	for i := 0; i < 300; i++ {
		if p := s.Step(1.0 / 60); p > maxPos {
			maxPos = p
		}
	}
	if maxPos > 100+1e-9 {
		t.Fatalf("critically damped spring overshot: max=%.9f", maxPos)
	}
}

// TestSpringStableHugeDt is the reason for the closed-form integrator: an absurd
// frame time must not make the spring explode — it should just settle.
func TestSpringStableHugeDt(t *testing.T) {
	s := NewSpring(0, 18)
	s.SetTarget(100)
	s.Step(1e6)
	if math.IsNaN(s.Pos) || math.IsInf(s.Pos, 0) {
		t.Fatalf("pos blew up: %v", s.Pos)
	}
	if math.Abs(s.Pos-100) > 1e-6 || math.Abs(s.Vel) > 1e-6 {
		t.Fatalf("huge dt should settle at target: pos=%.6f vel=%.6f", s.Pos, s.Vel)
	}
}

// TestSpringRetargetContinuity verifies a mid-flight retarget preserves position
// and velocity (no visible jump) and then converges to the new target.
func TestSpringRetargetContinuity(t *testing.T) {
	s := NewSpring(0, 14)
	s.SetTarget(100)
	stepN(&s, 1.0/60, 10)
	posBefore, velBefore := s.Pos, s.Vel
	s.SetTarget(0)
	if s.Pos != posBefore || s.Vel != velBefore {
		t.Fatalf("retarget perturbed state: pos %.6f->%.6f vel %.6f->%.6f",
			posBefore, s.Pos, velBefore, s.Vel)
	}
	stepN(&s, 1.0/60, 240)
	if math.Abs(s.Pos) > 0.5 {
		t.Fatalf("did not converge to new target 0: pos=%.4f", s.Pos)
	}
}

func TestSpringStepNoOp(t *testing.T) {
	s := NewSpring(10, 18)
	s.SetTarget(50)
	s.Vel = 3
	if p := s.Step(0); p != 10 || s.Pos != 10 || s.Vel != 3 {
		t.Fatalf("Step(0) changed state: pos=%.4f vel=%.4f", s.Pos, s.Vel)
	}
}

func TestSpringZeroOmegaDrifts(t *testing.T) {
	s := Spring{Pos: 0, Vel: 10, Target: 999, Omega: 0}
	s.Step(2) // drift = vel*dt = 20, spring force disabled
	if math.Abs(s.Pos-20) > 1e-9 {
		t.Fatalf("zero-omega drift: pos=%.6f, want 20", s.Pos)
	}
}
