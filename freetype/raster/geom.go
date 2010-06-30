// Copyright 2010 The Freetype-Go Authors. All rights reserved.
// Use of this source code is governed by your choice of either the
// FreeType License or the GNU General Public License version 2,
// both of which can be found in the LICENSE file.

package raster

import (
	"fmt"
	"math"
)

// A Fix32 is a 24.8 fixed point number.
type Fix32 int32

// A Fix64 is a 48.16 fixed point number.
type Fix64 int64

// String returns a human-readable representation of a 24.8 fixed point number.
// For example, the number one-and-a-quarter becomes "1:064".
func (x Fix32) String() string {
	i, f := x/256, x%256
	if f < 0 {
		f = -f
	}
	return fmt.Sprintf("%d:%03d", int32(i), int32(f))
}

// String returns a human-readable representation of a 48.16 fixed point number.
// For example, the number one-and-a-quarter becomes "1:00064".
func (x Fix64) String() string {
	i, f := x/65536, x%65536
	if f < 0 {
		f = -f
	}
	return fmt.Sprintf("%d:%05d", int64(i), int64(f))
}

// maxAbs returns the maximum of abs(a) and abs(b).
func maxAbs(a, b Fix32) Fix32 {
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}
	if a < b {
		return b
	}
	return a
}

// A Point represents a two-dimensional point or vector, in 24.8 fixed point
// format.
type Point struct {
	X, Y Fix32
}

// Add returns the vector p + q.
func (p Point) Add(q Point) Point {
	return Point{p.X + q.X, p.Y + q.Y}
}

// Sub returns the vector p - q.
func (p Point) Sub(q Point) Point {
	return Point{p.X - q.X, p.Y - q.Y}
}

// Mul returns the vector k * p.
func (p Point) Mul(k Fix32) Point {
	return Point{p.X * k / 256, p.Y * k / 256}
}

// Neg returns the vector -p, or equivalently p rotated by 180 degrees.
func (p Point) Neg() Point {
	return Point{-p.X, -p.Y}
}

// Dot returns the dot product p·q.
func (p Point) Dot(q Point) Fix64 {
	px, py := int64(p.X), int64(p.Y)
	qx, qy := int64(q.X), int64(q.Y)
	return Fix64(px*qx + py*qy)
}

// Len returns the length of the vector p.
func (p Point) Len() Fix32 {
	// TODO(nigeltao): use fixed point math.
	x := float64(p.X)
	y := float64(p.Y)
	return Fix32(math.Sqrt(x*x + y*y))
}

// Norm returns the vector p normalized to the given length, or the zero Point
// if p is degenerate.
func (p Point) Norm(length Fix32) Point {
	d := p.Len()
	if d == 0 {
		return Point{0, 0}
	}
	s, t := int64(length), int64(d)
	x := int64(p.X) * s / t
	y := int64(p.Y) * s / t
	return Point{Fix32(x), Fix32(y)}
}

// Rot45CW returns the vector p rotated clockwise by 45 degrees.
// Note that the Y-axis grows downwards, so {1, 0}.Rot45CW is {1/√2, 1/√2}.
func (p Point) Rot45CW() Point {
	// 181/256 is approximately 1/√2, or sin(π/4).
	px, py := int64(p.X), int64(p.Y)
	qx := (+px - py) * 181 / 256
	qy := (+px + py) * 181 / 256
	return Point{Fix32(qx), Fix32(qy)}
}

// Rot90CW returns the vector p rotated clockwise by 90 degrees.
// Note that the Y-axis grows downwards, so {1, 0}.Rot90CW is {0, 1}.
func (p Point) Rot90CW() Point {
	return Point{-p.Y, p.X}
}

// Rot135CW returns the vector p rotated clockwise by 135 degrees.
// Note that the Y-axis grows downwards, so {1, 0}.Rot135CW is {-1/√2, 1/√2}.
func (p Point) Rot135CW() Point {
	// 181/256 is approximately 1/√2, or sin(π/4).
	px, py := int64(p.X), int64(p.Y)
	qx := (-px - py) * 181 / 256
	qy := (+px - py) * 181 / 256
	return Point{Fix32(qx), Fix32(qy)}
}

// Rot45CCW returns the vector p rotated counter-clockwise by 45 degrees.
// Note that the Y-axis grows downwards, so {1, 0}.Rot45CCW is {1/√2, -1/√2}.
func (p Point) Rot45CCW() Point {
	// 181/256 is approximately 1/√2, or sin(π/4).
	px, py := int64(p.X), int64(p.Y)
	qx := (+px + py) * 181 / 256
	qy := (-px + py) * 181 / 256
	return Point{Fix32(qx), Fix32(qy)}
}

// Rot90CCW returns the vector p rotated counter-clockwise by 90 degrees.
// Note that the Y-axis grows downwards, so {1, 0}.Rot90CCW is {0, -1}.
func (p Point) Rot90CCW() Point {
	return Point{p.Y, -p.X}
}

// Rot135CCW returns the vector p rotated counter-clockwise by 135 degrees.
// Note that the Y-axis grows downwards, so {1, 0}.Rot135CCW is {-1/√2, -1/√2}.
func (p Point) Rot135CCW() Point {
	// 181/256 is approximately 1/√2, or sin(π/4).
	px, py := int64(p.X), int64(p.Y)
	qx := (-px + py) * 181 / 256
	qy := (-px - py) * 181 / 256
	return Point{Fix32(qx), Fix32(qy)}
}

// An Adder accumulates points on a curve.
type Adder interface {
	// Start starts a new curve at the given point.
	Start(a Point)
	// Add1 adds a linear segment to the current curve.
	Add1(b Point)
	// Add2 adds a quadratic segment to the current curve.
	Add2(b, c Point)
	// Add3 adds a cubic segment to the current curve.
	Add3(b, c, d Point)
}

// A Path is a sequence of curves, and a curve is a start point followed by a
// sequence of linear, quadratic or cubic segments.
type Path []Fix32

// String returns a human-readable representation of a Path.
func (p Path) String() string {
	s := ""
	for i := 0; i < len(p); {
		if i != 0 {
			s += " "
		}
		switch p[i] {
		case 0:
			s += "S0" + fmt.Sprint([]Fix32(p[i+1:i+3]))
			i += 4
		case 1:
			s += "A1" + fmt.Sprint([]Fix32(p[i+1:i+3]))
			i += 4
		case 2:
			s += "A2" + fmt.Sprint([]Fix32(p[i+1:i+5]))
			i += 6
		case 3:
			s += "A3" + fmt.Sprint([]Fix32(p[i+1:i+7]))
			i += 8
		default:
			panic("freetype/raster: bad path")
		}
	}
	return s
}

// grow adds n elements to p.
func (p *Path) grow(n int) {
	n += len(*p)
	if n > cap(*p) {
		old := *p
		*p = make([]Fix32, n, 2*n+8)
		copy(*p, old)
		return
	}
	*p = (*p)[0:n]
}

// Clear cancels any previous calls to p.Start or p.AddXxx.
func (p *Path) Clear() {
	*p = (*p)[0:0]
}

// Start starts a new curve at the given point.
func (p *Path) Start(a Point) {
	n := len(*p)
	p.grow(4)
	(*p)[n] = 0
	(*p)[n+1] = a.X
	(*p)[n+2] = a.Y
	(*p)[n+3] = 0
}

// Add1 adds a linear segment to the current curve.
func (p *Path) Add1(b Point) {
	n := len(*p)
	p.grow(4)
	(*p)[n] = 1
	(*p)[n+1] = b.X
	(*p)[n+2] = b.Y
	(*p)[n+3] = 1
}

// Add2 adds a quadratic segment to the current curve.
func (p *Path) Add2(b, c Point) {
	n := len(*p)
	p.grow(6)
	(*p)[n] = 2
	(*p)[n+1] = b.X
	(*p)[n+2] = b.Y
	(*p)[n+3] = c.X
	(*p)[n+4] = c.Y
	(*p)[n+5] = 2
}

// Add3 adds a cubic segment to the current curve.
func (p *Path) Add3(b, c, d Point) {
	n := len(*p)
	p.grow(8)
	(*p)[n] = 3
	(*p)[n+1] = b.X
	(*p)[n+2] = b.Y
	(*p)[n+3] = c.X
	(*p)[n+4] = c.Y
	(*p)[n+5] = d.X
	(*p)[n+6] = d.Y
	(*p)[n+7] = 3
}

// AddPath adds the Path q to p.
func (p *Path) AddPath(q Path) {
	n, m := len(*p), len(q)
	p.grow(m)
	copy((*p)[n:n+m], q)
}

// TODO(nigeltao): should a Cap be a func rather than an int, so that callers
// can specify custom cap styles? Similarly for Join.

// A Cap signifies how to begin or end a stroked curve.
type Cap int

const (
	RoundCap Cap = iota
	ButtCap
	SquareCap
)

// A Join signifies how to join interior nodes of a stroked curve.
type Join int

const (
	RoundJoin Join = iota
	BevelJoin
	MiterJoin
)

// AddStroke adds a stroked Path.
func (p *Path) AddStroke(q Path, width Fix32, cap Cap, join Join) {
	Stroke(p, q, width, cap, join)
}

// Stroke adds the stroked Path q to p. The resultant stroked path is typically
// self-intersecting and should be rasterized with UseNonZeroWinding.
func Stroke(p Adder, q Path, width Fix32, cap Cap, join Join) {
	if len(q) == 0 {
		return
	}
	if q[0] != 0 {
		panic("freetype/raster: bad path")
	}
	i := 0
	for j := 4; j < len(q); {
		switch q[j] {
		case 0:
			stroke(p, q[i:j], width, cap, join)
			i, j = j, j+4
		case 1:
			j += 4
		case 2:
			j += 6
		case 3:
			j += 8
		}
	}
	stroke(p, q[i:len(q)], width, cap, join)
}

func addCap(p Adder, cap Cap, center, end Point) {
	switch cap {
	case RoundCap:
		// The cubic Bézier approximation to a circle involves the magic number
		// (√2 - 1) * 4/3, which is approximately 141/256.
		const k = 141
		d := end.Sub(center)
		e := d.Rot90CCW()
		side := center.Add(e)
		start := center.Sub(d)
		d, e = d.Mul(k), e.Mul(k)
		p.Add3(start.Add(e), side.Sub(d), side)
		p.Add3(side.Add(d), end.Add(e), end)
	case ButtCap:
		p.Add1(end)
	case SquareCap:
		d := end.Sub(center)
		e := d.Rot90CCW()
		side := center.Add(e)
		p.Add1(side.Sub(d))
		p.Add1(side.Add(d))
		p.Add1(end)
	}
}

func addJoin(lhs, rhs Adder, join Join, a, anorm, bnorm Point) {
	switch join {
	case RoundJoin:
		dot := anorm.Rot90CW().Dot(bnorm)
		if dot >= 0 {
			addArc(lhs, a, anorm, bnorm)
			rhs.Add1(a.Sub(bnorm))
		} else {
			lhs.Add1(a.Add(bnorm))
			addArc(rhs, a, anorm.Neg(), bnorm.Neg())
		}
	case BevelJoin:
		lhs.Add1(a.Add(bnorm))
		rhs.Add1(a.Sub(bnorm))
	case MiterJoin:
		panic("freetype/raster: miter join unimplemented")
	}
}

// addArc adds a circular arc from pivot+n0 to pivot+n1 to p. The shorter of
// the two possible arcs is taken, i.e. the one spanning <= 180 degrees.
// The two vectors n0 and n1 must be of equal length.
func addArc(p Adder, pivot, n0, n1 Point) {
	// r2 is the square of the length of n0.
	r2 := n0.Dot(n0)
	if r2 < 4096 {
		// The arc radius is so small that we collapse to a straight line.
		p.Add1(pivot.Add(n1))
		return
	}
	// We approximate the arc by 0, 1, 2 or 3 45-degree quadratic segments plus
	// a final quadratic segment from s to n1. Each 45-degree segment has control
	// points {1, 0}, {1, tan(π/8)} and {1/√2, 1/√2} suitably scaled, rotated and
	// translated. tan(π/8) is approximately 106/256.
	const t = 106
	var s Point
	// We determine which octant the angle between n0 and n1 is in via three dot products.
	// m0, m1 and m2 are n0 rotated clockwise by 45, 90 and 135 degrees.
	m0 := n0.Rot45CW()
	m1 := n0.Rot90CW()
	m2 := m0.Rot90CW()
	if m1.Dot(n1) >= 0 {
		if n0.Dot(n1) >= 0 {
			if m2.Dot(n1) <= 0 {
				// n1 is between 0 and 45 degrees clockwise of n0.
				s = n0
			} else {
				// n1 is between 45 and 90 degrees clockwise of n0.
				p.Add2(pivot.Add(n0).Add(m1.Mul(t)), pivot.Add(m0))
				s = m0
			}
		} else {
			pm1, n0t := pivot.Add(m1), n0.Mul(t)
			p.Add2(pivot.Add(n0).Add(m1.Mul(t)), pivot.Add(m0))
			p.Add2(pm1.Add(n0t), pm1)
			if m0.Dot(n1) >= 0 {
				// n1 is between 90 and 135 degrees clockwise of n0.
				s = m1
			} else {
				// n1 is between 135 and 180 degrees clockwise of n0.
				p.Add2(pm1.Sub(n0t), pivot.Add(m2))
				s = m2
			}
		}
	} else {
		if n0.Dot(n1) >= 0 {
			if m0.Dot(n1) >= 0 {
				// n1 is between 0 and 45 degrees counter-clockwise of n0.
				s = n0
			} else {
				// n1 is between 45 and 90 degrees counter-clockwise of n0.
				p.Add2(pivot.Add(n0).Sub(m1.Mul(t)), pivot.Sub(m2))
				s = m2.Neg()
			}
		} else {
			pm1, n0t := pivot.Sub(m1), n0.Mul(t)
			p.Add2(pivot.Add(n0).Sub(m1.Mul(t)), pivot.Sub(m2))
			p.Add2(pm1.Add(n0t), pm1)
			if m2.Dot(n1) <= 0 {
				// n1 is between 90 and 135 degrees counter-clockwise of n0.
				s = m1.Neg()
			} else {
				// n1 is between 135 and 180 degrees counter-clockwise of n0.
				p.Add2(pm1.Sub(n0t), pivot.Sub(m0))
				s = m0.Neg()
			}
		}
	}
	// The final quadratic segment has two endpoints s and n1 and the middle
	// control point is a multiple of s.Add(n1), i.e. it is on the angle bisector
	// of those two points. The multiple ranges between 128/256 and 150/256 as
	// the angle between s and n1 ranges between 0 and 45 degrees.
	// When the angle is 0 degrees (i.e. s and n1 are coincident) then s.Add(n1)
	// is twice s and so the middle control point of the degenerate quadratic
	// segment should be half s.Add(n1), and half = 128/256.
	// When the angle is 45 degrees then 150/256 is the ratio of the lengths of
	// the two vectors {1, tan(π/8)} and {1 + 1/√2, 1/√2}.
	// d is the normalized dot product between s and n1. Since the angle ranges
	// between 0 and 45 degrees then d ranges between 256/256 and 181/256.
	d := 256 * s.Dot(n1) / r2
	multiple := Fix32(150 - 22*(d-181)/(256-181))
	p.Add2(pivot.Add(s.Add(n1).Mul(multiple)), pivot.Add(n1))
}

// stroke adds the stroked Path q to p, where q consists of exactly one curve.
func stroke(p Adder, q Path, width Fix32, cap Cap, join Join) {
	// Stroking is implemented by deriving two paths each width/2 apart from q.
	// The left-hand-side path is added immediately to p; the right-hand-side
	// path is accumulated in r, and once we've finished adding the LHS to p
	// we add the RHS in reverse order.
	r := Path(make([]Fix32, 0, len(q)))
	var start, anorm Point
	a := Point{q[1], q[2]}
	i := 4
	for i < len(q) {
		switch q[i] {
		case 1:
			b := Point{q[i+1], q[i+2]}
			bnorm := b.Sub(a).Norm(width / 2).Rot90CCW()
			if i == 4 {
				start = a.Add(bnorm)
				p.Start(start)
				r.Start(a.Sub(bnorm))
			} else {
				addJoin(p, &r, join, a, anorm, bnorm)
			}
			p.Add1(b.Add(bnorm))
			r.Add1(b.Sub(bnorm))
			a, anorm = b, bnorm
			i += 4
		case 2:
			panic("freetype/raster: stroke unimplemented for quadratic segments")
		case 3:
			panic("freetype/raster: stroke unimplemented for cubic segments")
		default:
			panic("freetype/raster: bad path")
		}
	}
	i = len(r) - 1
	addCap(p, cap, Point{q[len(q)-3], q[len(q)-2]}, Point{r[i-2], r[i-1]})
	// Add r reversed to p.
	// For example, if r consists of a linear segment from A to B followed by a
	// quadratic segment from B to C to D, then the values of r looks like:
	// index: 01234567890123
	// value: 0AA01BB12CCDD2
	// So, when adding r backwards to p, we want to Add2(C, B) followed by Add1(A).
loop:
	for {
		switch r[i] {
		case 0:
			break loop
		case 1:
			i -= 4
			p.Add1(Point{r[i-2], r[i-1]})
		case 2:
			i -= 6
			p.Add2(Point{r[i+2], r[i+3]}, Point{r[i-2], r[i-1]})
		case 3:
			i -= 8
			p.Add3(Point{r[i+4], r[i+5]}, Point{r[i+2], r[i+3]}, Point{r[i-2], r[i-1]})
		default:
			panic("freetype/raster: bad path")
		}
	}
	// TODO(nigeltao): if q is a closed path then we should join the first and
	// last segments instead of capping them.
	addCap(p, cap, Point{q[1], q[2]}, start)
}
