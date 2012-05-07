// Copyright 2010 The Freetype-Go Authors. All rights reserved.
// Use of this source code is governed by your choice of either the
// FreeType License or the GNU General Public License version 2 (or
// any later version), both of which can be found in the LICENSE file.

// The freetype package provides a convenient API to draw text onto an image.
// Use the freetype/raster and freetype/truetype packages for lower level
// control over rasterization and TrueType parsing.
package freetype

import (
	"errors"
	"image"
	"image/draw"

	"code.google.com/p/freetype-go/freetype/raster"
	"code.google.com/p/freetype-go/freetype/truetype"
)

// These constants determine the size of the glyph cache. The cache is keyed
// primarily by the glyph index modulo nGlyphs, and secondarily by sub-pixel
// position for the mask image. Sub-pixel positions are quantized to
// nXFractions possible values in both the x and y directions.
const (
	nGlyphs     = 256
	nXFractions = 4
	nYFractions = 1
)

// An entry in the glyph cache is keyed explicitly by the glyph index and
// implicitly by the quantized x and y fractional offset. It maps to a mask
// image and an offset.
type cacheEntry struct {
	valid  bool
	glyph  truetype.Index
	mask   *image.Alpha
	offset image.Point
}

// ParseFont just calls the Parse function from the freetype/truetype package.
// It is provided here so that code that imports this package doesn't need
// to also include the freetype/truetype package.
func ParseFont(b []byte) (*truetype.Font, error) {
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
	// clip is the clip rectangle for drawing.
	clip image.Rectangle
	// dst and src are the destination and source images for drawing.
	dst draw.Image
	src image.Image
	// fontSize, dpi and upe are used to calculate scale.
	// scale is a multiplication factor to convert 256 FUnits (which is truetype's
	// native unit) to 24.8 fixed point units (which is the rasterizer's native unit).
	// At the default values of 72 DPI and 2048 units-per-em, one em of a 12 point
	// font is 12 pixels, which is 3072 fixed point units, and scale is
	// (pointSize * resolution * 256 * 256) / (unitsPerEm * 72), or
	// (12 * 72 * 256 * 256) / (2048 * 72),
	// which equals 384 fixed point units per 256 FUnits.
	// To check this, 1 em * 2048 FUnits per em * 384 fixed point units per 256 FUnits
	// equals 3072 fixed point units.
	fontSize float64
	dpi      int
	upe      int
	scale    int
	// cache is the glyph cache.
	cache [nGlyphs * nXFractions * nYFractions]cacheEntry
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
func (c *Context) PointToFix32(x float64) raster.Fix32 {
	return raster.Fix32(x * float64(c.dpi) * (256.0 / 72.0))
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
		dy + c.FUnitToFix32(-int(ps[0].Y)),
	}
	c.r.Start(start)
	q0, on0 := start, true
	for _, p := range ps[1:] {
		q := raster.Point{
			dx + c.FUnitToFix32(int(p.X)),
			dy + c.FUnitToFix32(-int(p.Y)),
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

// rasterize returns the glyph mask and integer-pixel offset to render the
// given glyph at the given sub-pixel offsets.
// The 24.8 fixed point arguments fx and fy must be in the range [0, 1).
func (c *Context) rasterize(glyph truetype.Index, fx, fy raster.Fix32) (*image.Alpha, image.Point, error) {
	if err := c.glyphBuf.Load(c.font, glyph); err != nil {
		return nil, image.ZP, err
	}
	// Calculate the integer-pixel bounds for the glyph.
	xmin := int(fx+c.FUnitToFix32(+int(c.glyphBuf.B.XMin))) >> 8
	ymin := int(fy+c.FUnitToFix32(-int(c.glyphBuf.B.YMax))) >> 8
	xmax := int(fx+c.FUnitToFix32(+int(c.glyphBuf.B.XMax))+0xff) >> 8
	ymax := int(fy+c.FUnitToFix32(-int(c.glyphBuf.B.YMin))+0xff) >> 8
	if xmin > xmax || ymin > ymax {
		return nil, image.ZP, errors.New("freetype: negative sized glyph")
	}
	// A TrueType's glyph's nodes can have negative co-ordinates, but the
	// rasterizer clips anything left of x=0 or above y=0. xmin and ymin
	// are the pixel offsets, based on the font's FUnit metrics, that let
	// a negative co-ordinate in TrueType space be non-negative in
	// rasterizer space. xmin and ymin are typically <= 0.
	fx += raster.Fix32(-xmin << 8)
	fy += raster.Fix32(-ymin << 8)
	// Rasterize the glyph's vectors.
	c.r.Clear()
	e0 := 0
	for _, e1 := range c.glyphBuf.End {
		c.drawContour(c.glyphBuf.Point[e0:e1], fx, fy)
		e0 = e1
	}
	a := image.NewAlpha(image.Rect(0, 0, xmax-xmin, ymax-ymin))
	c.r.Rasterize(raster.NewAlphaSrcPainter(a))
	return a, image.Point{xmin, ymin}, nil
}

// glyph returns the glyph mask and integer-pixel offset to render the given
// glyph at the given sub-pixel point. It is a cache for the rasterize method.
// Unlike rasterize, p's co-ordinates do not have to be in the range [0, 1).
func (c *Context) glyph(glyph truetype.Index, p raster.Point) (*image.Alpha, image.Point, error) {
	// Split p.X and p.Y into their integer and fractional parts.
	ix, fx := int(p.X>>8), p.X&0xff
	iy, fy := int(p.Y>>8), p.Y&0xff
	// Calculate the index t into the cache array.
	tg := int(glyph) % nGlyphs
	tx := int(fx) / (256 / nXFractions)
	ty := int(fy) / (256 / nYFractions)
	t := ((tg*nXFractions)+tx)*nYFractions + ty
	// Check for a cache hit.
	if c.cache[t].valid && c.cache[t].glyph == glyph {
		return c.cache[t].mask, c.cache[t].offset.Add(image.Point{ix, iy}), nil
	}
	// Rasterize the glyph and put the result into the cache.
	mask, offset, err := c.rasterize(glyph, fx, fy)
	if err != nil {
		return nil, image.ZP, err
	}
	c.cache[t] = cacheEntry{true, glyph, mask, offset}
	return mask, offset.Add(image.Point{ix, iy}), nil
}

// DrawString draws s at p and returns p advanced by the text extent. The text
// is placed so that the left edge of the em square of the first character of s
// and the baseline intersect at p. The majority of the affected pixels will be
// above and to the right of the point, but some may be below or to the left.
// For example, drawing a string that starts with a 'J' in an italic font may
// affect pixels below and left of the point.
// p is a raster.Point and can therefore represent sub-pixel positions.
func (c *Context) DrawString(s string, p raster.Point) (raster.Point, error) {
	if c.font == nil {
		return raster.Point{}, errors.New("freetype: DrawText called with a nil font")
	}
	prev, hasPrev := truetype.Index(0), false
	for _, rune := range s {
		index := c.font.Index(rune)
		if hasPrev {
			p.X += c.FUnitToFix32(int(c.font.Kerning(prev, index)))
		}
		mask, offset, err := c.glyph(index, p)
		if err != nil {
			return raster.Point{}, err
		}
		p.X += c.FUnitToFix32(int(c.font.HMetric(index).AdvanceWidth))
		glyphRect := mask.Bounds().Add(offset)
		dr := c.clip.Intersect(glyphRect)
		if !dr.Empty() {
			mp := image.Point{0, dr.Min.Y - glyphRect.Min.Y}
			draw.DrawMask(c.dst, dr, c.src, image.ZP, mask, mp, draw.Over)
		}
		prev, hasPrev = index, true
	}
	return p, nil
}

// recalc recalculates scale and bounds values from the font size, screen
// resolution and font metrics, and invalidates the glyph cache.
func (c *Context) recalc() {
	c.scale = int((c.fontSize * float64(c.dpi) * 256 * 256) / (float64(c.upe) * 72))
	if c.font == nil {
		c.r.SetBounds(0, 0)
	} else {
		// Set the rasterizer's bounds to be big enough to handle the largest glyph.
		b := c.font.Bounds()
		xmin := c.FUnitToPixelRD(+int(b.XMin))
		ymin := c.FUnitToPixelRD(-int(b.YMax))
		xmax := c.FUnitToPixelRU(+int(b.XMax))
		ymax := c.FUnitToPixelRU(-int(b.YMin))
		c.r.SetBounds(xmax-xmin, ymax-ymin)
	}
	for i := range c.cache {
		c.cache[i] = cacheEntry{}
	}
}

// SetDPI sets the screen resolution in dots per inch.
func (c *Context) SetDPI(dpi int) {
	if c.dpi == dpi {
		return
	}
	c.dpi = dpi
	c.recalc()
}

// SetFont sets the font used to draw text.
func (c *Context) SetFont(font *truetype.Font) {
	if c.font == font {
		return
	}
	c.font = font
	c.upe = font.UnitsPerEm()
	if c.upe <= 0 {
		c.upe = 1
	}
	c.recalc()
}

// SetFontSize sets the font size in points (as in ``a 12 point font'').
func (c *Context) SetFontSize(fontSize float64) {
	if c.fontSize == fontSize {
		return
	}
	c.fontSize = fontSize
	c.recalc()
}

// SetDst sets the destination image for draw operations.
func (c *Context) SetDst(dst draw.Image) {
	c.dst = dst
}

// SetSrc sets the source image for draw operations. This is typically an
// image.Uniform.
func (c *Context) SetSrc(src image.Image) {
	c.src = src
}

// SetClip sets the clip rectangle for drawing.
func (c *Context) SetClip(clip image.Rectangle) {
	c.clip = clip
}

// TODO(nigeltao): implement Context.SetGamma.

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
