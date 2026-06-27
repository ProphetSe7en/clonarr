// Package calculator back-solves Custom Format scores from a user-specified
// release ranking. lp.go is the linear-programming core: a dependency-free
// two-phase simplex (Bland's rule for guaranteed termination) that handles
// general <=, =, >= constraints and free (sign-unrestricted) variables.
//
// Problem sizes here are small (a few hundred variables/constraints at most),
// so a dense tableau is fine and keeps the build C-free per clonarr's policy.
package calculator

import "math"

// Sense is the relation of a linear constraint.
type Sense int

const (
	LE Sense = iota // Σ coef·x <= rhs
	EQ              // Σ coef·x == rhs
	GE              // Σ coef·x >= rhs
)

const lpEpsilon = 1e-9

// Problem is a linear program: minimize c·x subject to linear constraints.
// Variables are non-negative unless declared free.
type Problem struct {
	free []bool
	obj  []float64
	rows []lpRow
}

type lpRow struct {
	coef  []float64
	sense Sense
	rhs   float64
}

// NewProblem returns an empty minimization problem.
func NewProblem() *Problem { return &Problem{} }

// AddVar appends a variable and returns its index. A free variable is
// unrestricted in sign (split into a pos/neg pair internally).
func (p *Problem) AddVar(free bool) int {
	p.free = append(p.free, free)
	p.obj = append(p.obj, 0)
	return len(p.free) - 1
}

// SetObjective sets the (minimization) objective coefficient for variable v.
func (p *Problem) SetObjective(v int, c float64) { p.obj[v] = c }

// AddConstraint adds Σ coef[j]·x_j (sense) rhs.
func (p *Problem) AddConstraint(coef map[int]float64, sense Sense, rhs float64) {
	row := make([]float64, len(p.free))
	for j, c := range coef {
		row[j] = c
	}
	p.rows = append(p.rows, lpRow{coef: row, sense: sense, rhs: rhs})
}

// Solution is the result of Solve.
type Solution struct {
	Feasible bool
	X        []float64 // value per original problem variable
	Obj      float64
}

// Solve runs a two-phase simplex and returns the optimal solution, or
// Feasible=false when the constraints cannot all be satisfied.
func (p *Problem) Solve() Solution {
	n := len(p.free)

	// Map each problem variable to standard-form (non-negative) columns.
	// A free variable becomes x = posCol - negCol.
	posCol := make([]int, n)
	negCol := make([]int, n)
	ncols := 0
	for j := 0; j < n; j++ {
		posCol[j] = ncols
		ncols++
		if p.free[j] {
			negCol[j] = ncols
			ncols++
		} else {
			negCol[j] = -1
		}
	}

	m := len(p.rows)

	// Build structural rows over the standard columns, normalising rhs >= 0.
	structRows := make([][]float64, m)
	rhs := make([]float64, m)
	senses := make([]Sense, m)
	for i, r := range p.rows {
		a := make([]float64, ncols)
		for j := 0; j < n; j++ {
			// coef rows are sized when the constraint was added, so a row
			// added before later variables is shorter than n; missing
			// trailing entries are implicitly zero.
			if j >= len(r.coef) {
				break
			}
			c := r.coef[j]
			if c == 0 {
				continue
			}
			a[posCol[j]] += c
			if negCol[j] >= 0 {
				a[negCol[j]] -= c
			}
		}
		b := r.rhs
		s := r.sense
		if b < 0 {
			for k := range a {
				a[k] = -a[k]
			}
			b = -b
			switch s {
			case LE:
				s = GE
			case GE:
				s = LE
			}
		}
		structRows[i] = a
		rhs[i] = b
		senses[i] = s
	}

	// Width = structural columns + one slack per LE, surplus+artificial per
	// GE, artificial per EQ.
	width := ncols
	for i := 0; i < m; i++ {
		switch senses[i] {
		case LE:
			width++
		case GE:
			width += 2
		case EQ:
			width++
		}
	}

	A := make([][]float64, m)
	basis := make([]int, m)
	isArtificial := make([]bool, width)
	cur := ncols
	for i := 0; i < m; i++ {
		row := make([]float64, width)
		copy(row, structRows[i])
		switch senses[i] {
		case LE:
			row[cur] = 1 // slack, basic
			basis[i] = cur
			cur++
		case GE:
			row[cur] = -1 // surplus
			cur++
			row[cur] = 1 // artificial, basic
			isArtificial[cur] = true
			basis[i] = cur
			cur++
		case EQ:
			row[cur] = 1 // artificial, basic
			isArtificial[cur] = true
			basis[i] = cur
			cur++
		}
		A[i] = row
	}

	// Phase I: minimise the sum of artificials to find a feasible basis.
	hasArt := false
	for k := 0; k < width; k++ {
		if isArtificial[k] {
			hasArt = true
			break
		}
	}
	noneForbidden := make([]bool, width)
	if hasArt {
		phase1 := make([]float64, width)
		for k := 0; k < width; k++ {
			if isArtificial[k] {
				phase1[k] = 1
			}
		}
		simplexTableau(A, rhs, basis, phase1, noneForbidden)
		infeasObj := 0.0
		for i := 0; i < m; i++ {
			if isArtificial[basis[i]] {
				infeasObj += rhs[i]
			}
		}
		if infeasObj > 1e-7 {
			return Solution{Feasible: false}
		}
		driveOutArtificials(A, rhs, basis, isArtificial)
	}

	// Phase II: minimise the real objective. Artificials are forbidden from
	// re-entering the basis (any left basic sit at value 0).
	cost := make([]float64, width)
	for j := 0; j < n; j++ {
		cost[posCol[j]] = p.obj[j]
		if negCol[j] >= 0 {
			cost[negCol[j]] = -p.obj[j]
		}
	}
	simplexTableau(A, rhs, basis, cost, isArtificial)

	// Extract the standard solution, then fold free-variable pairs back.
	xstd := make([]float64, width)
	for i := 0; i < m; i++ {
		xstd[basis[i]] = rhs[i]
	}
	x := make([]float64, n)
	obj := 0.0
	for j := 0; j < n; j++ {
		v := xstd[posCol[j]]
		if negCol[j] >= 0 {
			v -= xstd[negCol[j]]
		}
		x[j] = v
		obj += p.obj[j] * v
	}
	return Solution{Feasible: true, X: x, Obj: obj}
}

// simplexTableau optimises min cost·x s.t. A x = b, x >= 0, starting from the
// basic feasible solution given by basis (A held in B^-1 A tableau form, b >= 0,
// basis columns forming an identity). forbidden columns may not enter. Bland's
// rule guarantees termination. Returns false only if the problem is unbounded.
func simplexTableau(A [][]float64, b []float64, basis []int, cost []float64, forbidden []bool) bool {
	m := len(A)
	if m == 0 {
		return true
	}
	width := len(A[0])
	for iter := 0; iter < 200000; iter++ {
		// Entering column: first index (Bland) with negative reduced cost.
		entering := -1
		for j := 0; j < width; j++ {
			if forbidden[j] {
				continue
			}
			rj := cost[j]
			for i := 0; i < m; i++ {
				rj -= cost[basis[i]] * A[i][j]
			}
			if rj < -lpEpsilon {
				entering = j
				break
			}
		}
		if entering == -1 {
			return true // optimal
		}
		// Leaving row: min ratio b[i]/A[i][entering] over positive entries,
		// Bland tie-break on smallest basis index.
		leaving := -1
		best := math.Inf(1)
		for i := 0; i < m; i++ {
			if A[i][entering] > lpEpsilon {
				ratio := b[i] / A[i][entering]
				if ratio < best-lpEpsilon ||
					(ratio < best+lpEpsilon && (leaving == -1 || basis[i] < basis[leaving])) {
					best = ratio
					leaving = i
				}
			}
		}
		if leaving == -1 {
			return false // unbounded
		}
		pivot(A, b, basis, leaving, entering)
	}
	return true
}

// pivot performs a simplex pivot making column c the basic variable in row r.
func pivot(A [][]float64, b []float64, basis []int, r, c int) {
	m := len(A)
	p := A[r][c]
	for j := range A[r] {
		A[r][j] /= p
	}
	b[r] /= p
	for i := 0; i < m; i++ {
		if i == r {
			continue
		}
		f := A[i][c]
		if f == 0 {
			continue
		}
		for j := range A[i] {
			A[i][j] -= f * A[r][j]
		}
		b[i] -= f * b[r]
	}
	basis[r] = c
}

// driveOutArtificials pivots any artificial still in the basis (at value ~0
// after phase I) out in favour of a structural column, so phase II starts from
// a clean basis. A row with no usable structural pivot is redundant and left.
func driveOutArtificials(A [][]float64, b []float64, basis []int, isArtificial []bool) {
	m := len(A)
	if m == 0 {
		return
	}
	width := len(A[0])
	for i := 0; i < m; i++ {
		if !isArtificial[basis[i]] {
			continue
		}
		for j := 0; j < width; j++ {
			if isArtificial[j] {
				continue
			}
			if math.Abs(A[i][j]) > lpEpsilon {
				pivot(A, b, basis, i, j)
				break
			}
		}
	}
}
