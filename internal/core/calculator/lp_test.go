package calculator

import (
	"math"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// min x+y s.t. x+y >= 2, x,y >= 0  ->  obj 2.
func TestLP_SimpleGE(t *testing.T) {
	p := NewProblem()
	x := p.AddVar(false)
	y := p.AddVar(false)
	p.SetObjective(x, 1)
	p.SetObjective(y, 1)
	p.AddConstraint(map[int]float64{x: 1, y: 1}, GE, 2)
	s := p.Solve()
	if !s.Feasible {
		t.Fatal("expected feasible")
	}
	if !approx(s.Obj, 2) {
		t.Fatalf("obj = %v, want 2", s.Obj)
	}
}

// min 2x+3y s.t. x+y >= 10, x,y >= 0  ->  x=10,y=0, obj 20.
func TestLP_WeightedObjective(t *testing.T) {
	p := NewProblem()
	x := p.AddVar(false)
	y := p.AddVar(false)
	p.SetObjective(x, 2)
	p.SetObjective(y, 3)
	p.AddConstraint(map[int]float64{x: 1, y: 1}, GE, 10)
	s := p.Solve()
	if !s.Feasible || !approx(s.Obj, 20) {
		t.Fatalf("feasible=%v obj=%v, want obj 20", s.Feasible, s.Obj)
	}
	if !approx(s.X[x], 10) || !approx(s.X[y], 0) {
		t.Fatalf("x=%v y=%v, want 10,0", s.X[x], s.X[y])
	}
}

// Equality: min x s.t. x == 5  ->  x=5.
func TestLP_Equality(t *testing.T) {
	p := NewProblem()
	x := p.AddVar(false)
	p.SetObjective(x, 1)
	p.AddConstraint(map[int]float64{x: 1}, EQ, 5)
	s := p.Solve()
	if !s.Feasible || !approx(s.X[x], 5) {
		t.Fatalf("feasible=%v x=%v, want 5", s.Feasible, s.X[x])
	}
}

// Free variable taking a negative value: min s s.t. s >= -3 (s free) -> s=-3.
func TestLP_FreeVarNegative(t *testing.T) {
	p := NewProblem()
	s := p.AddVar(true)
	p.SetObjective(s, 1)
	p.AddConstraint(map[int]float64{s: 1}, GE, -3)
	sol := p.Solve()
	if !sol.Feasible || !approx(sol.X[s], -3) {
		t.Fatalf("feasible=%v s=%v, want -3", sol.Feasible, sol.X[s])
	}
}

// Absolute-value objective pattern: minimise |s| with s >= 4.
// t >= s, t >= -s, min t  ->  s=4, t=4.
func TestLP_AbsValuePattern(t *testing.T) {
	p := NewProblem()
	s := p.AddVar(true)
	tv := p.AddVar(false)
	p.SetObjective(tv, 1)
	p.AddConstraint(map[int]float64{tv: 1, s: -1}, GE, 0) // t >= s
	p.AddConstraint(map[int]float64{tv: 1, s: 1}, GE, 0)  // t >= -s
	p.AddConstraint(map[int]float64{s: 1}, GE, 4)         // s >= 4
	sol := p.Solve()
	if !sol.Feasible || !approx(sol.X[tv], 4) || !approx(sol.X[s], 4) {
		t.Fatalf("feasible=%v t=%v s=%v, want t=4 s=4", sol.Feasible, sol.X[tv], sol.X[s])
	}
}

// |s| minimised with no lower pressure should land at 0.
func TestLP_AbsValueZero(t *testing.T) {
	p := NewProblem()
	s := p.AddVar(true)
	tv := p.AddVar(false)
	p.SetObjective(tv, 1)
	p.AddConstraint(map[int]float64{tv: 1, s: -1}, GE, 0)
	p.AddConstraint(map[int]float64{tv: 1, s: 1}, GE, 0)
	p.AddConstraint(map[int]float64{s: 1}, GE, -100) // loose
	p.AddConstraint(map[int]float64{s: 1}, LE, 100)
	sol := p.Solve()
	if !sol.Feasible || !approx(sol.X[tv], 0) {
		t.Fatalf("feasible=%v t=%v, want 0", sol.Feasible, sol.X[tv])
	}
}

// Infeasible: x >= 5 AND x <= 2.
func TestLP_Infeasible(t *testing.T) {
	p := NewProblem()
	x := p.AddVar(false)
	p.AddConstraint(map[int]float64{x: 1}, GE, 5)
	p.AddConstraint(map[int]float64{x: 1}, LE, 2)
	s := p.Solve()
	if s.Feasible {
		t.Fatalf("expected infeasible, got x=%v", s.X[x])
	}
}

// LE with a binding upper bound: max-like via min -x. min -x s.t. x <= 7 -> x=7.
func TestLP_LEBinding(t *testing.T) {
	p := NewProblem()
	x := p.AddVar(false)
	p.SetObjective(x, -1)
	p.AddConstraint(map[int]float64{x: 1}, LE, 7)
	s := p.Solve()
	if !s.Feasible || !approx(s.X[x], 7) {
		t.Fatalf("feasible=%v x=%v, want 7", s.Feasible, s.X[x])
	}
}
