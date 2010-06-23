// Copyright 2010 The Freetype-Go Authors. All rights reserved.
// Use of this source code is governed by your choice of either the
// FreeType License or the GNU General Public License version 2,
// both of which can be found in the LICENSE file.

package raster

import (
	"fmt"
	"math"
)

// A 24.8 fixed point number.
type Fixed int32

// String returns a human-readable representation of a 24.8 fixed point number.
// For example, the number one-and-a-quarter becomes "1:064".
func (x Fixed) String() string {
	i, f := x/256, x%256
	if f < 0 {
		f = -f
	}
	return fmt.Sprintf("%d:%03d", int32(i), int32(f))
}

// maxAbs returns the maximum of abs(a) and abs(b).
func maxAbs(a, b Fixed) Fixed {
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

// A two-dimensional point or vector, in 24.8 fixed point format.
type Point struct {
	X, Y Fixed
}

// Len returns the length of the vector p.
func (p Point) Len() Fixed {
	// TODO(nigeltao): use fixed point math.
	x := float64(p.X)
	y := float64(p.Y)
	return Fixed(math.Sqrt(x*x + y*y))
}

// Norm returns the vector p normalized to the given length, or the zero Point
// if p is degenerate.
func (p Point) Norm(length Fixed) Point {
	d := p.Len()
	if d == 0 {
		return Point{0, 0}
	}
	// TODO(nigeltao): should we check for overflow?
	return Point{p.X * length / d, p.Y * length / d}
}

// RotateCW returns the vector p rotated clockwise by 90 degrees.
func (p Point) RotateCW() Point {
	return Point{p.Y, -p.X}
}

// RotateCCW returns the vector p rotated counter-clockwise by 90 degrees.
func (p Point) RotateCCW() Point {
	return Point{-p.Y, p.X}
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
type Path []Fixed

// String returns a human-readable representation of a Path.
func (p Path) String() string {
	s := ""
	for i := 0; i < len(p); {
		if i != 0 {
			s += " "
		}
		switch p[i] {
		case 0:
			s += "S0" + fmt.Sprint([]Fixed(p[i+1:i+3]))
			i += 3
		case 1:
			s += "A1" + fmt.Sprint([]Fixed(p[i+1:i+3]))
			i += 3
		case 2:
			s += "A2" + fmt.Sprint([]Fixed(p[i+1:i+5]))
			i += 5
		case 3:
			s += "A3" + fmt.Sprint([]Fixed(p[i+1:i+7]))
			i += 7
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
		*p = make([]Fixed, n, 2*n+8)
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
	p.grow(3)
	(*p)[n] = 0
	(*p)[n+1] = a.X
	(*p)[n+2] = a.Y
}

// Add1 adds a linear segment to the current curve.
func (p *Path) Add1(b Point) {
	n := len(*p)
	p.grow(3)
	(*p)[n] = 1
	(*p)[n+1] = b.X
	(*p)[n+2] = b.Y
}

// Add2 adds a quadratic segment to the current curve.
func (p *Path) Add2(b, c Point) {
	n := len(*p)
	p.grow(5)
	(*p)[n] = 2
	(*p)[n+1] = b.X
	(*p)[n+2] = b.Y
	(*p)[n+3] = c.X
	(*p)[n+4] = c.Y
}

// Add3 adds a cubic segment to the current curve.
func (p *Path) Add3(b, c, d Point) {
	n := len(*p)
	p.grow(7)
	(*p)[n] = 3
	(*p)[n+1] = b.X
	(*p)[n+2] = b.Y
	(*p)[n+3] = c.X
	(*p)[n+4] = c.Y
	(*p)[n+5] = d.X
	(*p)[n+6] = d.Y
}

// AddPath adds the Path q to p.
func (p *Path) AddPath(q Path) {
	n, m := len(*p), len(q)
	p.grow(m)
	copy((*p)[n:n+m], q)
}

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
func (p *Path) AddStroke(q Path, width Fixed, cap Cap, join Join) {
	Stroke(p, q, width, cap, join)
}

// Stroke adds the stroked Path q to p.
func Stroke(p Adder, q Path, width Fixed, cap Cap, join Join) {
	if len(q) == 0 {
		return
	}
	if q[0] != 0 {
		panic("freetype/raster: bad path")
	}
	i := 0
	for j := 3; j < len(q); {
		switch q[j] {
		case 0:
			stroke(p, q[i:j], width, cap, join)
			i, j = j, j+3
		case 1:
			j += 3
		case 2:
			j += 5
		case 3:
			j += 7
		}
	}
	stroke(p, q[i:len(q)], width, cap, join)
}

// stroke adds the stroked Path q to p, where q consists of exactly one curve.
func stroke(p Adder, q Path, width Fixed, cap Cap, join Join) {
	// TODO(nigeltao): replace this placeholder stroking algorithm. It only
	// handles linear segments, and it doesn't cap or join but instead only
	// fattens each segment independently by half the width, and doesn't
	// correct for overlaps.
	a := Point{q[1], q[2]}
	for i := 3; i < len(q); {
		switch q[i] {
		case 1:
			bx, by := q[i+1], q[i+2]
			delta := Point{bx - a.X, by - a.Y}
			normal := delta.Norm(width / 2).RotateCCW()
			start := Point{a.X + normal.X, a.Y + normal.Y}
			p.Start(start)
			p.Add1(Point{bx + normal.X, by + normal.Y})
			p.Add1(Point{bx - normal.X, by - normal.Y})
			p.Add1(Point{a.X - normal.X, a.Y - normal.Y})
			p.Add1(start)
			a = Point{q[i+1], q[i+2]}
			i += 3
		case 2:
			panic("freetype/raster: stroke unimplemented for quadratic segments")
		case 3:
			panic("freetype/raster: stroke unimplemented for cubic segments")
		default:
			panic("freetype/raster: bad path")
		}
	}
}
