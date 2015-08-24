// Copyright 2015 The Freetype-Go Authors. All rights reserved.
// Use of this source code is governed by your choice of either the
// FreeType License or the GNU General Public License version 2 (or
// any later version), both of which can be found in the LICENSE file.

package truetype

import (
	"image"

	"github.com/golang/freetype/raster"
	"golang.org/x/exp/shiny/font"
	"golang.org/x/image/math/fixed"
)

// Options are optional arguments to NewFace.
type Options struct {
	// Size is the font size in points, as in "a 10 point font size".
	//
	// A zero value means to use a 12 point font size.
	Size float64

	// DPI is the dots-per-inch resolution.
	//
	// A zero value means to use 72 DPI.
	DPI float64

	// Hinting is how to quantize the glyph nodes.
	//
	// A zero value means to use no hinting.
	Hinting font.Hinting
}

func (o *Options) size() float64 {
	if o.Size > 0 {
		return o.Size
	}
	return 12
}

func (o *Options) dpi() float64 {
	if o.DPI > 0 {
		return o.DPI
	}
	return 72
}

func (o *Options) hinting() font.Hinting {
	switch o.Hinting {
	case font.HintingVertical, font.HintingFull:
		// TODO: support vertical hinting.
		return font.HintingFull
	}
	return font.HintingNone
}

// NewFace returns a new font.Face for the given Font.
func NewFace(f *Font, opts Options) font.Face {
	a := &face{
		f:       f,
		hinting: opts.hinting(),
		scale:   fixed.Int26_6(0.5 + (opts.size() * opts.dpi() * 64 / 72)),
	}

	// Set the rasterizer's bounds to be big enough to handle the largest glyph.
	b := f.Bounds(a.scale)
	xmin := +int(b.XMin) >> 6
	ymin := -int(b.YMax) >> 6
	xmax := +int(b.XMax+63) >> 6
	ymax := -int(b.YMin-63) >> 6
	a.maxw = xmax - xmin
	a.maxh = ymax - ymin
	a.mask = image.NewAlpha(image.Rect(0, 0, a.maxw, a.maxh))
	a.r.SetBounds(a.maxw, a.maxh)
	a.p = raster.NewAlphaSrcPainter(a.mask)

	return a
}

type face struct {
	f        *Font
	hinting  font.Hinting
	scale    fixed.Int26_6
	mask     *image.Alpha
	r        raster.Rasterizer
	p        raster.Painter
	maxw     int
	maxh     int
	glyphBuf GlyphBuf

	// TODO: clip rectangle?
}

// Close satisfies the font.Face interface.
func (a *face) Close() error { return nil }

// Kern satisfies the font.Face interface.
func (a *face) Kern(r0, r1 rune) fixed.Int26_6 {
	i0 := a.f.Index(r0)
	i1 := a.f.Index(r1)
	kern := a.f.Kern(a.scale, i0, i1)
	if a.hinting != font.HintingNone {
		kern = (kern + 32) &^ 63
	}
	return kern
}

// Glyph satisfies the font.Face interface.
func (a *face) Glyph(dot fixed.Point26_6, r rune) (
	newDot fixed.Point26_6, dr image.Rectangle, mask image.Image, maskp image.Point, ok bool) {

	// Split p.X and p.Y into their integer and fractional parts.
	ix, fx := int(dot.X>>6), dot.X&0x3f
	iy, fy := int(dot.Y>>6), dot.Y&0x3f

	advanceWidth, offset, gw, gh, ok := a.rasterize(a.f.Index(r), fx, fy)
	if !ok {
		return fixed.Point26_6{}, image.Rectangle{}, nil, image.Point{}, false
	}
	newDot = fixed.Point26_6{
		X: dot.X + advanceWidth,
		Y: dot.Y,
	}
	dr.Min = image.Point{
		X: ix + offset.X,
		Y: iy + offset.Y,
	}
	dr.Max = image.Point{
		X: dr.Min.X + gw,
		Y: dr.Min.Y + gh,
	}
	return newDot, dr, a.mask, image.Point{}, true
}

func (a *face) GlyphBounds(r rune) (bounds fixed.Rectangle26_6, advance fixed.Int26_6, ok bool) {
	if err := a.glyphBuf.Load(a.f, a.scale, a.f.Index(r), a.hinting); err != nil {
		return fixed.Rectangle26_6{}, 0, false
	}
	xmin := +a.glyphBuf.B.XMin
	ymin := -a.glyphBuf.B.YMax
	xmax := +a.glyphBuf.B.XMax
	ymax := -a.glyphBuf.B.YMin
	if xmin > xmax || ymin > ymax {
		return fixed.Rectangle26_6{}, 0, false
	}
	return fixed.Rectangle26_6{
		Min: fixed.Point26_6{
			X: xmin,
			Y: ymin,
		},
		Max: fixed.Point26_6{
			X: xmax,
			Y: ymax,
		},
	}, a.glyphBuf.AdvanceWidth, true
}

func (a *face) GlyphAdvance(r rune) (advance fixed.Int26_6, ok bool) {
	if err := a.glyphBuf.Load(a.f, a.scale, a.f.Index(r), a.hinting); err != nil {
		return 0, false
	}
	return a.glyphBuf.AdvanceWidth, true
}

// rasterize returns the advance width, integer-pixel offset to render at, and
// the width and height of the given glyph at the given sub-pixel offsets.
//
// The 26.6 fixed point arguments fx and fy must be in the range [0, 1).
func (a *face) rasterize(index Index, fx, fy fixed.Int26_6) (
	advanceWidth fixed.Int26_6, offset image.Point, gw int, gh int, ok bool) {

	if err := a.glyphBuf.Load(a.f, a.scale, index, a.hinting); err != nil {
		return 0, image.Point{}, 0, 0, false
	}
	// Calculate the integer-pixel bounds for the glyph.
	xmin := int(fx+a.glyphBuf.B.XMin) >> 6
	ymin := int(fy-a.glyphBuf.B.YMax) >> 6
	xmax := int(fx+a.glyphBuf.B.XMax+0x3f) >> 6
	ymax := int(fy-a.glyphBuf.B.YMin+0x3f) >> 6
	if xmin > xmax || ymin > ymax {
		return 0, image.Point{}, 0, 0, false
	}
	// A TrueType's glyph's nodes can have negative co-ordinates, but the
	// rasterizer clips anything left of x=0 or above y=0. xmin and ymin are
	// the pixel offsets, based on the font's FUnit metrics, that let a
	// negative co-ordinate in TrueType space be non-negative in rasterizer
	// space. xmin and ymin are typically <= 0.
	fx -= fixed.Int26_6(xmin << 6)
	fy -= fixed.Int26_6(ymin << 6)
	// Rasterize the glyph's vectors.
	a.r.Clear()
	clear(a.mask.Pix)
	e0 := 0
	for _, e1 := range a.glyphBuf.End {
		a.drawContour(a.glyphBuf.Point[e0:e1], fx, fy)
		e0 = e1
	}
	a.r.Rasterize(a.p)
	return a.glyphBuf.AdvanceWidth, image.Point{xmin, ymin}, xmax - xmin, ymax - ymin, true
}

func clear(pix []byte) {
	for i := range pix {
		pix[i] = 0
	}
}

// drawContour draws the given closed contour with the given offset.
func (a *face) drawContour(ps []Point, dx, dy fixed.Int26_6) {
	if len(ps) == 0 {
		return
	}

	// The low bit of each point's Flags value is whether the point is on the
	// curve. Truetype fonts only have quadratic Bézier curves, not cubics.
	// Thus, two consecutive off-curve points imply an on-curve point in the
	// middle of those two.
	//
	// See http://chanae.walon.org/pub/ttf/ttf_glyphs.htm for more details.

	// ps[0] is a truetype.Point measured in FUnits and positive Y going
	// upwards. start is the same thing measured in fixed point units and
	// positive Y going downwards, and offset by (dx, dy).
	start := fixed.Point26_6{
		X: dx + ps[0].X,
		Y: dy - ps[0].Y,
	}
	var others []Point
	if ps[0].Flags&0x01 != 0 {
		others = ps[1:]
	} else {
		last := fixed.Point26_6{
			X: dx + ps[len(ps)-1].X,
			Y: dy - ps[len(ps)-1].Y,
		}
		if ps[len(ps)-1].Flags&0x01 != 0 {
			start = last
			others = ps[:len(ps)-1]
		} else {
			start = fixed.Point26_6{
				X: (start.X + last.X) / 2,
				Y: (start.Y + last.Y) / 2,
			}
			others = ps
		}
	}
	a.r.Start(start)
	q0, on0 := start, true
	for _, p := range others {
		q := fixed.Point26_6{
			X: dx + p.X,
			Y: dy - p.Y,
		}
		on := p.Flags&0x01 != 0
		if on {
			if on0 {
				a.r.Add1(q)
			} else {
				a.r.Add2(q0, q)
			}
		} else {
			if on0 {
				// No-op.
			} else {
				mid := fixed.Point26_6{
					X: (q0.X + q.X) / 2,
					Y: (q0.Y + q.Y) / 2,
				}
				a.r.Add2(q0, mid)
			}
		}
		q0, on0 = q, on
	}
	// Close the curve.
	if on0 {
		a.r.Add1(start)
	} else {
		a.r.Add2(q0, start)
	}
}
