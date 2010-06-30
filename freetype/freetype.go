// Copyright 2010 The Freetype-Go Authors. All rights reserved.
// Use of this source code is governed by your choice of either the
// FreeType License or the GNU General Public License version 2,
// both of which can be found in the LICENSE file.

// The freetype package provides a convenient API to draw text onto an image.
// Use the freetype/raster and freetype/truetype packages for lower level
// control over rasterization and TrueType parsing.
package freetype

import (
	"freetype-go.googlecode.com/hg/freetype/raster"
	"freetype-go.googlecode.com/hg/freetype/truetype"
	"os"
)

// ParseFont just calls the Parse function from the freetype/truetype package.
// It is provided here so that code that imports this package doesn't need
// to also include the freetype/truetype package.
func ParseFont(b []byte) (*truetype.Font, os.Error) {
	return truetype.Parse(b)
}

// Pt converts from a co-ordinate pair measured in pixels to a raster.Point
// co-ordinate pair measured in raster.Fix32 units.
func Pt(x, y int) raster.Point {
	return raster.Point{raster.Fix32(x << 8), raster.Fix32(y << 8)}
}

// A Context holds the state for drawing text in a given font and size.
type Context struct {
	r        *raster.Rasterizer
	font     *truetype.Font
	glyphBuf *truetype.GlyphBuf
	fontSize float
	dpi      int
	upe      int
	// A TrueType's glyph's nodes can have negative co-ordinates, but the
	// rasterizer clips anything left of x=0 or above y=0. xmin and ymin
	// are the pixel offsets, based on the font's FUnit metrics, that let
	// a negative co-ordinate in TrueType space be non-negative in
	// rasterizer space. xmin and ymin are typically <= 0.
	xmin, ymin int
	// scale is a multiplication factor to convert 256 FUnits (which is truetype's
	// native unit) to 24.8 fixed point units (which is the rasterizer's native unit).
	// At the default values of 72 DPI and 2048 units-per-em, one em of a 12 point
	// font is 12 pixels, which is 3072 fixed point units, and scale is
	// (pointSize * resolution * 256 * 256) / (unitsPerEm * 72), or
	// (12 * 72 * 256 * 256) / (2048 * 72),
	// which equals 384 fixed point units per 256 FUnits.
	// To check this, 1 em * 2048 FUnits per em * 384 fixed point units per 256 FUnits
	// equals 3072 fixed point units.
	scale int
}

// FUnitToFix32 converts the given number of FUnits into fixed point units,
// rounding to nearest.
func (c *Context) FUnitToFix32(x int) raster.Fix32 {
	return raster.Fix32((x*c.scale + 128) >> 8)
}

// FUnitToPixelRD converts the given number of FUnits into pixel units,
// rounding down.
func (c *Context) FUnitToPixelRD(x int) int {
	return x * c.scale >> 16
}

// FUnitToPixelRU converts the given number of FUnits into pixel units,
// rounding up.
func (c *Context) FUnitToPixelRU(x int) int {
	return (x*c.scale + 0xffff) >> 16
}

// PointToFix32 converts the given number of points (as in ``a 12 point font'')
// into fixed point units.
func (c *Context) PointToFix32(x float) raster.Fix32 {
	return raster.Fix32(x * float(c.dpi) * (256.0 / 72.0))
}

// drawContour draws the given closed contour with the given offset.
func (c *Context) drawContour(ps []truetype.Point, dx, dy raster.Fix32) {
	if len(ps) == 0 {
		return
	}
	// ps[0] is a truetype.Point measured in FUnits and positive Y going upwards.
	// start is the same thing measured in fixed point units and positive Y
	// going downwards, and offset by (dx, dy)
	start := raster.Point{
		dx + c.FUnitToFix32(int(ps[0].X)),
		dy + c.FUnitToFix32(c.upe-int(ps[0].Y)),
	}
	c.r.Start(start)
	q0, on0 := start, true
	for _, p := range ps[1:] {
		q := raster.Point{
			dx + c.FUnitToFix32(int(p.X)),
			dy + c.FUnitToFix32(c.upe-int(p.Y)),
		}
		on := p.Flags&0x01 != 0
		if on {
			if on0 {
				c.r.Add1(q)
			} else {
				c.r.Add2(q0, q)
			}
		} else {
			if on0 {
				// No-op.
			} else {
				mid := raster.Point{
					(q0.X + q.X) / 2,
					(q0.Y + q.Y) / 2,
				}
				c.r.Add2(q0, mid)
			}
		}
		q0, on0 = q, on
	}
	// Close the curve.
	if on0 {
		c.r.Add1(start)
	} else {
		c.r.Add2(q0, start)
	}
}

// DrawText draws s at pt using p. The text is placed so that the top left of
// the em square of the first character of s is equal to pt. The majority of
// the affected pixels will be below and to the right of pt, but some may be
// above or to the left. For example, drawing a string that starts with a 'J'
// in an italic font may affect pixels to the left of pt.
// pt is a raster.Point and can therefore represent sub-pixel positions.
func (c *Context) DrawText(p raster.Painter, pt raster.Point, s string) (err os.Error) {
	if c.font == nil {
		return os.NewError("freetype: DrawText called with a nil font")
	}
	// pt.X, pt.Y, x, y, dx, dy and x0 are measured in raster.Fix32 units,
	// c.r.Dx, c.r.Dy, c.xmin and c.ymin are measured in pixels, and
	// advance is measured in FUnits.
	var x, y raster.Fix32
	advance, x0 := 0, pt.X
	dx := raster.Fix32(-c.xmin << 8)
	dy := raster.Fix32(-c.ymin << 8)
	c.r.Dy, y = c.ymin+int(pt.Y>>8), pt.Y&0xff
	y += dy
	prev, hasPrev := truetype.Index(0), false
	for _, ch := range s {
		index := c.font.Index(ch)
		// Load the next glyph (if it was different from the previous one)
		// and add any kerning adjustment.
		if hasPrev {
			advance += int(c.font.Kerning(prev, index))
			if prev != index {
				err = c.glyphBuf.Load(c.font, index)
				if err != nil {
					return
				}
			}
		} else {
			err = c.glyphBuf.Load(c.font, index)
			if err != nil {
				return
			}
		}
		// Convert the advance from FUnits to raster.Fix32 units.
		x = x0 + c.FUnitToFix32(advance)
		// Break the co-ordinate down into an integer pixel part and a
		// sub-pixel part, making sure that the latter is non-negative.
		c.r.Dx, x = c.xmin+int(x>>8), x&0xff
		x += dx
		// Draw the contours.
		c.r.Clear()
		e0 := 0
		for _, e := range c.glyphBuf.End {
			c.drawContour(c.glyphBuf.Point[e0:e], x, y)
			e0 = e
		}
		c.r.Rasterize(p)
		// Advance the cursor.
		advance += int(c.font.HMetric(index).AdvanceWidth)
		prev, hasPrev = index, true
	}
	return
}

// recalc recalculates scale and bounds values from the font size, screen
// resolution and font metrics.
func (c *Context) recalc() {
	c.scale = int((c.fontSize * float(c.dpi) * 256 * 256) / (float(c.upe) * 72))
	if c.font == nil {
		c.xmin, c.ymin = 0, 0
	} else {
		b := c.font.Bounds()
		c.xmin = c.FUnitToPixelRD(int(b.XMin))
		c.ymin = c.FUnitToPixelRD(c.upe - int(b.YMax))
		xmax := c.FUnitToPixelRU(int(b.XMax))
		ymax := c.FUnitToPixelRU(c.upe - int(b.YMin))
		c.r.SetBounds(xmax-c.xmin, ymax-c.ymin)
	}
}

// SetDPI sets the screen resolution in dots per inch.
func (c *Context) SetDPI(dpi int) {
	c.dpi = dpi
	c.recalc()
}

// SetFont sets the font used to draw text.
func (c *Context) SetFont(font *truetype.Font) {
	c.font = font
	c.upe = font.UnitsPerEm()
	if c.upe <= 0 {
		c.upe = 1
	}
	c.recalc()
}

// SetFontSize sets the font size in points (as in ``a 12 point font'').
func (c *Context) SetFontSize(fontSize float) {
	c.fontSize = fontSize
	c.recalc()
}

// NewContext creates a new Context.
func NewContext() *Context {
	return &Context{
		r:        raster.NewRasterizer(0, 0),
		glyphBuf: truetype.NewGlyphBuf(),
		fontSize: 12,
		dpi:      72,
		upe:      2048,
		scale:    (12 * 72 * 256 * 256) / (2048 * 72),
	}
}
