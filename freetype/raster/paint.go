// Copyright 2010 The Freetype-Go Authors. All rights reserved.
// Use of this source code is governed by your choice of either the
// FreeType License or the GNU General Public License version 2,
// both of which can be found in the LICENSE file.

package raster

import (
	"image"
	"math"
)

// A Span is a horizontal segment of pixels with constant alpha. X0 is an
// inclusive bound and X1 is exclusive, the same as for slices. A fully
// opaque Span has A == 1<<32 - 1.
type Span struct {
	Y, X0, X1 int
	A         uint32
}

// A Painter knows how to paint a batch of Spans. A Span's alpha is non-zero
// until the final Span of the rasterization, which is the zero value Span.
// A rasterization may involve Painting multiple batches, but the final zero
// value Span will occur only once per rasterization, not once per Paint call.
// Paint may use all of ss as scratch space during the call.
type Painter interface {
	Paint(ss []Span)
}

// The PainterFunc type adapts an ordinary function to the Painter interface.
type PainterFunc func(ss []Span)

// Paint just delegates the call to f.
func (f PainterFunc) Paint(ss []Span) { f(ss) }

// AlphaOverPainter returns a Painter that paints onto the given Alpha image
// using the "src over dst" Porter-Duff composition operator.
func AlphaOverPainter(m *image.Alpha) Painter {
	return PainterFunc(func(ss []Span) {
		for _, s := range ss {
			a := int(s.A >> 24)
			p := m.Pixel[s.Y]
			for i := s.X0; i < s.X1; i++ {
				ai := int(p[i].A)
				ai = (ai*255 + (255-ai)*a) / 255
				p[i] = image.AlphaColor{uint8(ai)}
			}
		}
	})
}

// AlphaSrcPainter returns a Painter that paints onto the given Alpha image
// using the "src" Porter-Duff composition operator.
func AlphaSrcPainter(m *image.Alpha) Painter {
	return PainterFunc(func(ss []Span) {
		for _, s := range ss {
			color := image.AlphaColor{uint8(s.A >> 24)}
			p := m.Pixel[s.Y]
			for i := s.X0; i < s.X1; i++ {
				p[i] = color
			}
		}
	})
}

// A monochromePainter has a wrapped painter and an accumulator for merging
// adjacent opaque Spans.
type monochromePainter struct {
	p         Painter
	y, x0, x1 int
}

// Paint delegates to the wrapped Painter after quantizing each Span's alpha
// values and merging adjacent fully opaque Spans.
func (m *monochromePainter) Paint(ss []Span) {
	// We compact the ss slice, discarding any Spans whose alpha quantizes to zero.
	j := 0
	for _, s := range ss {
		if s.A >= 1<<31 {
			if m.y == s.Y && m.x1 == s.X0 {
				m.x1 = s.X1
			} else {
				ss[j] = Span{m.y, m.x0, m.x1, 1<<32 - 1}
				j++
				m.y, m.x0, m.x1 = s.Y, s.X0, s.X1
			}

		} else if s.A == 0 {
			// The final Span of a rasterization is a zero value. We flush
			// our accumulated Span and finish with a zero Span.
			ss[j] = Span{m.y, m.x0, m.x1, 1<<32 - 1}
			j++
			if j < len(ss) {
				ss[j] = Span{}
				j++
				m.p.Paint(ss[0:j])
			} else if j == len(ss) {
				m.p.Paint(ss)
				ss[0] = Span{}
				m.p.Paint(ss[0:1])
			} else {
				panic("unreachable")
			}
			// Reset the accumulator, so that this Painter can be re-used.
			m.y, m.x0, m.x1 = 0, 0, 0
			return
		}
	}
	m.p.Paint(ss[0:j])
}

// A MonochromePainter wraps another Painter, quantizing each Span's alpha to
// be either fully opaque or fully transparent.
func MonochromePainter(p Painter) Painter {
	return &monochromePainter{p: p}
}

// A gammaCorrectionPainter has a wrapped painter and a precomputed linear
// interpolation of the exponential gamma-correction curve.
type gammaCorrectionPainter struct {
	p Painter
	a [256]uint16 // Alpha values, with fully opaque == 1<<16-1.
}

// Paint delegates to the wrapped Painter after performing gamma-correction
// on each Span.
func (g *gammaCorrectionPainter) Paint(ss []Span) {
	const (
		M = 0x1010101 // 255*M == 1<<32-1
		N = 0x8080    // N = M>>9, and N < 1<<16-1
	)
	for i, _ := range ss {
		if ss[i].A == 0 || ss[i].A == 1<<32-1 {
			continue
		}
		p, q := ss[i].A/M, (ss[i].A%M)>>9
		// The resultant alpha is a linear interpolation of g.a[p] and g.a[p+1].
		a := uint32(g.a[p])*(N-q) + uint32(g.a[p+1])*q
		a = (a + N/2) / N
		// Convert the alpha from 16-bit (which is g.a's range) to 32-bit.
		a |= a << 16
		// A non-final Span can't have zero alpha.
		if a == 0 {
			a = 1
		}
		ss[i].A = a
	}
	g.p.Paint(ss)
}

// A GammaCorrectionPainter wraps another Painter, performing gamma-correction
// on the alpha values of each Span.
func GammaCorrectionPainter(p Painter, gamma float64) Painter {
	g := &gammaCorrectionPainter{p: p}
	for i := 0; i < 256; i++ {
		a := float64(i) / 0xff
		a = math.Pow(a, gamma)
		g.a[i] = uint16(0xffff * a)
	}
	return g
}
