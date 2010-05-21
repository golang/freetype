// Copyright 2010 The Freetype-Go Authors. All rights reserved.
// Use of this source code is governed by your choice of either the
// FreeType License or the GNU General Public License version 2,
// both of which can be found in the LICENSE file.

package raster

import (
	"exp/draw"
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

// A Painter knows how to paint a batch of Spans. Rasterization may involve
// Painting multiple batches, and done will be true for the final batch.
// The Spans' Y values are monotonically increasing during a rasterization.
// Paint may use all of ss as scratch space during the call.
type Painter interface {
	Paint(ss []Span, done bool)
}

// The PainterFunc type adapts an ordinary function to the Painter interface.
type PainterFunc func(ss []Span, done bool)

// Paint just delegates the call to f.
func (f PainterFunc) Paint(ss []Span, done bool) { f(ss, done) }

// An AlphaPainter is a Painter that paints Spans onto an image.Alpha.
type AlphaPainter struct {
	// The image to compose onto.
	Image *image.Alpha
	// The Porter-Duff composition operator.
	Op draw.Op
	// An offset (in pixels) to the painted spans.
	Dx, Dy int
}

// Paint satisfies the Painter interface by painting ss onto an image.Alpha.
func (r *AlphaPainter) Paint(ss []Span, done bool) {
	for _, s := range ss {
		y := r.Dy + s.Y
		if y < 0 {
			continue
		}
		if y >= len(r.Image.Pixel) {
			return
		}
		p := r.Image.Pixel[y]
		x0, x1 := r.Dx+s.X0, r.Dx+s.X1
		if x0 < 0 {
			x0 = 0
		}
		if x1 > len(p) {
			x1 = len(p)
		}
		if r.Op == draw.Over {
			a := int(s.A >> 24)
			for x := x0; x < x1; x++ {
				ax := int(p[x].A)
				ax = (ax*255 + (255-ax)*a) / 255
				p[x] = image.AlphaColor{uint8(ax)}
			}
		} else {
			color := image.AlphaColor{uint8(s.A >> 24)}
			for x := x0; x < x1; x++ {
				p[x] = color
			}
		}
	}
}

// NewAlphaPainter creates a new AlphaPainter for the given image.
func NewAlphaPainter(m *image.Alpha) *AlphaPainter {
	return &AlphaPainter{Image: m}
}

type RGBAPainter struct {
	// The image to compose onto.
	Image *image.RGBA
	// The Porter-Duff composition operator.
	Op draw.Op
	// An offset (in pixels) to the painted spans.
	Dx, Dy int
	// The 16-bit color to paint the spans.
	cr, cg, cb, ca uint32
}

// Paint satisfies the Painter interface by painting ss onto an image.RGBA.
func (r *RGBAPainter) Paint(ss []Span, done bool) {
	for _, s := range ss {
		y := r.Dy + s.Y
		if y < 0 {
			continue
		}
		if y >= len(r.Image.Pixel) {
			return
		}
		p := r.Image.Pixel[y]
		x0, x1 := r.Dx+s.X0, r.Dx+s.X1
		if x0 < 0 {
			x0 = 0
		}
		if x1 > len(p) {
			x1 = len(p)
		}
		for x := x0; x < x1; x++ {
			// This code is duplicated from drawGlyphOver in $GOROOT/src/pkg/exp/draw/draw.go.
			// TODO(nigeltao): Factor out common code into a utility function, once the compiler
			// can inline such function calls.
			ma := s.A >> 16
			const M = 1<<16 - 1
			if r.Op == draw.Over {
				rgba := p[x]
				dr := uint32(rgba.R)
				dg := uint32(rgba.G)
				db := uint32(rgba.B)
				da := uint32(rgba.A)
				a := M - (r.ca * ma / M)
				a *= 0x101
				dr = (dr*a + r.cr*ma) / M
				dg = (dg*a + r.cg*ma) / M
				db = (db*a + r.cb*ma) / M
				da = (da*a + r.ca*ma) / M
				p[x] = image.RGBAColor{uint8(dr >> 8), uint8(dg >> 8), uint8(db >> 8), uint8(da >> 8)}
			} else {
				dr := r.cr * ma / M
				dg := r.cg * ma / M
				db := r.cb * ma / M
				da := r.ca * ma / M
				p[x] = image.RGBAColor{uint8(dr >> 8), uint8(dg >> 8), uint8(db >> 8), uint8(da >> 8)}
			}
		}
	}
}

// SetColor sets the color to paint the spans.
func (r *RGBAPainter) SetColor(c image.Color) {
	r.cr, r.cg, r.cb, r.ca = c.RGBA()
	r.cr >>= 16
	r.cg >>= 16
	r.cb >>= 16
	r.ca >>= 16
}

// NewRGBAPainter creates a new RGBAPainter for the given image.
func NewRGBAPainter(m *image.RGBA) *RGBAPainter {
	return &RGBAPainter{Image: m}
}

// A MonochromePainter wraps another Painter, quantizing each Span's alpha to
// be either fully opaque or fully transparent.
type MonochromePainter struct {
	Painter   Painter
	y, x0, x1 int
}

// Paint delegates to the wrapped Painter after quantizing each Span's alpha
// value and merging adjacent fully opaque Spans.
func (m *MonochromePainter) Paint(ss []Span, done bool) {
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
		}
	}
	if done {
		// Flush the accumulated Span.
		finalSpan := Span{m.y, m.x0, m.x1, 1<<32 - 1}
		if j < len(ss) {
			ss[j] = finalSpan
			j++
			m.Painter.Paint(ss[0:j], true)
		} else if j == len(ss) {
			m.Painter.Paint(ss, false)
			if cap(ss) > 0 {
				ss = ss[0:1]
			} else {
				ss = make([]Span, 1)
			}
			ss[0] = finalSpan
			m.Painter.Paint(ss, true)
		} else {
			panic("unreachable")
		}
		// Reset the accumulator, so that this Painter can be re-used.
		m.y, m.x0, m.x1 = 0, 0, 0
	} else {
		m.Painter.Paint(ss[0:j], false)
	}
}

// NewMonochromePainter creates a new MonochromePainter that wraps the given
// Painter.
func NewMonochromePainter(p Painter) *MonochromePainter {
	return &MonochromePainter{Painter: p}
}

// A GammaCorrectionPainter wraps another Painter, performing gamma-correction
// on each Span's alpha value.
type GammaCorrectionPainter struct {
	// The wrapped Painter.
	Painter Painter
	// Precomputed alpha values for linear interpolation, with fully opaque == 1<<16-1.
	a [256]uint16
	// Whether gamma correction is a no-op.
	gammaIsOne bool
}

// Paint delegates to the wrapped Painter after performing gamma-correction
// on each Span.
func (g *GammaCorrectionPainter) Paint(ss []Span, done bool) {
	if !g.gammaIsOne {
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
			ss[i].A = a
		}
	}
	g.Painter.Paint(ss, done)
}

// SetGamma sets the gamma value.
func (g *GammaCorrectionPainter) SetGamma(gamma float) {
	if gamma == 1.0 {
		g.gammaIsOne = true
		return
	}
	g.gammaIsOne = false
	gamma64 := float64(gamma)
	for i := 0; i < 256; i++ {
		a := float64(i) / 0xff
		a = math.Pow(a, gamma64)
		g.a[i] = uint16(0xffff * a)
	}
}

// NewGammaCorrectionPainter creates a new GammaCorrectionPainter that wraps
// the given Painter.
func NewGammaCorrectionPainter(p Painter, gamma float) *GammaCorrectionPainter {
	g := &GammaCorrectionPainter{Painter: p}
	g.SetGamma(gamma)
	return g
}
