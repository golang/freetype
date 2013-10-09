// Copyright 2010 The Freetype-Go Authors. All rights reserved.
// Use of this source code is governed by your choice of either the
// FreeType License or the GNU General Public License version 2 (or
// any later version), both of which can be found in the LICENSE file.

package truetype

// A Point is a co-ordinate pair plus whether it is ``on'' a contour or an
// ``off'' control point.
type Point struct {
	X, Y int32
	// The Flags' LSB means whether or not this Point is ``on'' the contour.
	// Other bits are reserved for internal use.
	Flags uint32
}

// A GlyphBuf holds a glyph's contours. A GlyphBuf can be re-used to load a
// series of glyphs from a Font.
type GlyphBuf struct {
	// B is the glyph's bounding box.
	B Bounds
	// Point contains all Points from all contours of the glyph. If a
	// Hinter was used to load a glyph then Unhinted contains those
	// Points before they were hinted, and InFontUnits contains those
	// Points before they were hinted and scaled.
	Point, Unhinted, InFontUnits []Point
	// End is the point indexes of the end point of each countour. The
	// length of End is the number of contours in the glyph. The i'th
	// contour consists of points Point[End[i-1]:End[i]], where End[-1]
	// is interpreted to mean zero.
	End []int
}

// Flags for decoding a glyph's contours. These flags are documented at
// http://developer.apple.com/fonts/TTRefMan/RM06/Chap6glyf.html.
const (
	flagOnCurve = 1 << iota
	flagXShortVector
	flagYShortVector
	flagRepeat
	flagPositiveXShortVector
	flagPositiveYShortVector

	// The remaining flags are for internal use.
	flagTouchedX
	flagTouchedY
)

// The same flag bits (0x10 and 0x20) are overloaded to have two meanings,
// dependent on the value of the flag{X,Y}ShortVector bits.
const (
	flagThisXIsSame = flagPositiveXShortVector
	flagThisYIsSame = flagPositiveYShortVector
)

// decodeFlags decodes a glyph's run-length encoded flags,
// and returns the remaining data.
func (g *GlyphBuf) decodeFlags(d []byte, offset int, np0, np int) (offset1 int) {
	for i := np0; i < np; {
		c := uint32(d[offset])
		offset++
		g.Point[i].Flags = c
		i++
		if c&flagRepeat != 0 {
			count := d[offset]
			offset++
			for ; count > 0; count-- {
				g.Point[i].Flags = c
				i++
			}
		}
	}
	return offset
}

// decodeCoords decodes a glyph's delta encoded co-ordinates.
func (g *GlyphBuf) decodeCoords(d []byte, offset int, np0, np int) int {
	var x int16
	for i := np0; i < np; i++ {
		f := g.Point[i].Flags
		if f&flagXShortVector != 0 {
			dx := int16(d[offset])
			offset++
			if f&flagPositiveXShortVector == 0 {
				x -= dx
			} else {
				x += dx
			}
		} else if f&flagThisXIsSame == 0 {
			x += int16(u16(d, offset))
			offset += 2
		}
		g.Point[i].X = int32(x)
	}
	var y int16
	for i := np0; i < np; i++ {
		f := g.Point[i].Flags
		if f&flagYShortVector != 0 {
			dy := int16(d[offset])
			offset++
			if f&flagPositiveYShortVector == 0 {
				y -= dy
			} else {
				y += dy
			}
		} else if f&flagThisYIsSame == 0 {
			y += int16(u16(d, offset))
			offset += 2
		}
		g.Point[i].Y = int32(y)
	}
	return offset
}

// Load loads a glyph's contours from a Font, overwriting any previously
// loaded contours for this GlyphBuf. scale is the number of 26.6 fixed point
// units in 1 em. The Hinter is optional; if non-nil, then the resulting glyph
// will be hinted by the Font's bytecode instructions.
func (g *GlyphBuf) Load(f *Font, scale int32, i Index, h *Hinter) error {
	// Reset the GlyphBuf.
	g.B = Bounds{}
	g.Point = g.Point[:0]
	g.Unhinted = g.Unhinted[:0]
	g.InFontUnits = g.InFontUnits[:0]
	g.End = g.End[:0]
	if h != nil {
		if err := h.init(f, scale); err != nil {
			return err
		}
	}
	if _, err := g.load(f, scale, i, h, 0, 0, false, 0); err != nil {
		return err
	}
	g.B.XMin = f.scale(scale * g.B.XMin)
	g.B.YMin = f.scale(scale * g.B.YMin)
	g.B.XMax = f.scale(scale * g.B.XMax)
	g.B.YMax = f.scale(scale * g.B.YMax)
	return nil
}

// TODO: all these extra parameters and return values for loadCompound and load
// are awkward. We should clean this up once all the tests pass, when we can
// refactor with confidence that we don't break anything.

// loadCompound loads a glyph that is composed of other glyphs.
//
// metricsOverride is whether the sub-glyph overrides the super-glyph's
// metrics. pp1x is the x co-ordinate of the 1st phantom point.
func (g *GlyphBuf) loadCompound(f *Font, scale int32, h *Hinter, glyf []byte, offset int,
	dx, dy int32, recursion int) (metricsOverride bool, pp1x int32, offset1 int, err error) {

	// Flags for decoding a compound glyph. These flags are documented at
	// http://developer.apple.com/fonts/TTRefMan/RM06/Chap6glyf.html.
	const (
		flagArg1And2AreWords = 1 << iota
		flagArgsAreXYValues
		flagRoundXYToGrid
		flagWeHaveAScale
		flagUnused
		flagMoreComponents
		flagWeHaveAnXAndYScale
		flagWeHaveATwoByTwo
		flagWeHaveInstructions
		flagUseMyMetrics
		flagOverlapCompound
	)
	for {
		flags := u16(glyf, offset)
		component := Index(u16(glyf, offset+2))
		dx1, dy1 := dx, dy
		if flags&flagArg1And2AreWords != 0 {
			dx1 += int32(int16(u16(glyf, offset+4)))
			dy1 += int32(int16(u16(glyf, offset+6)))
			offset += 8
		} else {
			dx1 += int32(int16(int8(glyf[offset+4])))
			dy1 += int32(int16(int8(glyf[offset+5])))
			offset += 6
		}
		if flags&flagArgsAreXYValues == 0 {
			return false, 0, 0, UnsupportedError("compound glyph transform vector")
		}
		if flags&(flagWeHaveAScale|flagWeHaveAnXAndYScale|flagWeHaveATwoByTwo) != 0 {
			return false, 0, 0, UnsupportedError("compound glyph scale/transform")
		}
		b := g.B
		subPP1x, err := g.load(f, scale, component, h,
			dx1, dy1, flags&flagRoundXYToGrid != 0, recursion+1)
		if err != nil {
			return false, 0, 0, err
		}
		if flags&flagUseMyMetrics != 0 {
			metricsOverride, pp1x = true, subPP1x
		} else {
			g.B = b
		}
		if flags&flagMoreComponents == 0 {
			break
		}
	}
	return metricsOverride, pp1x, offset, nil
}

// load appends a glyph's contours to this GlyphBuf.
//
// pp1x is the x co-ordinate of the 1st phantom point.
func (g *GlyphBuf) load(f *Font, scale int32, i Index, h *Hinter,
	dx, dy int32, roundDxDy bool, recursion int) (pp1x int32, err error) {

	if recursion >= 4 {
		return 0, UnsupportedError("excessive compound glyph recursion")
	}
	// Find the relevant slice of f.glyf.
	var g0, g1 uint32
	if f.locaOffsetFormat == locaOffsetFormatShort {
		g0 = 2 * uint32(u16(f.loca, 2*int(i)))
		g1 = 2 * uint32(u16(f.loca, 2*int(i)+2))
	} else {
		g0 = u32(f.loca, 4*int(i))
		g1 = u32(f.loca, 4*int(i)+4)
	}
	if g0 == g1 {
		return 0, nil
	}
	glyf := f.glyf[g0:g1]
	// Decode the contour end indices.
	ne := int(int16(u16(glyf, 0)))
	b := Bounds{
		XMin: int32(int16(u16(glyf, 2))),
		YMin: int32(int16(u16(glyf, 4))),
		XMax: int32(int16(u16(glyf, 6))),
		YMax: int32(int16(u16(glyf, 8))),
	}
	offset := 10
	ne0, np0, np, metricsOverride, program := len(g.End), 0, 0, false, []byte(nil)
	if ne < 0 {
		if ne != -1 {
			// http://developer.apple.com/fonts/TTRefMan/RM06/Chap6glyf.html says that
			// "the values -2, -3, and so forth, are reserved for future use."
			return 0, UnsupportedError("negative number of contours")
		}
		var subPP1x int32
		metricsOverride, subPP1x, offset, err =
			g.loadCompound(f, scale, h, glyf, offset, dx, dy, recursion)
		if err != nil {
			return 0, err
		}
		if metricsOverride {
			pp1x = subPP1x
		}
		ne = ne0
		np0 = len(g.Point)
		np = np0
		// TODO: find the program, if present, for a compound glyph.

	} else {
		ne += ne0
		if ne <= cap(g.End) {
			g.End = g.End[:ne]
		} else {
			g.End = make([]int, ne, ne*2)
		}
		for i := ne0; i < ne; i++ {
			g.End[i] = 1 + int(u16(glyf, offset))
			offset += 2
		}
		np0 = len(g.Point)
		np = np0 + int(g.End[ne-1])

		// Note the TrueType hinting instructions.
		instrLen := int(u16(glyf, offset))
		offset += 2
		program = glyf[offset : offset+instrLen]
		offset += instrLen
	}

	// Decode the points, including room for the phantom points.
	const nPhantomPoints = 4
	if np+nPhantomPoints <= cap(g.Point) {
		g.Point = g.Point[:np+nPhantomPoints]
	} else {
		p := g.Point
		g.Point = make([]Point, np+nPhantomPoints, (np+nPhantomPoints)*2)
		copy(g.Point, p)
	}
	offset = g.decodeFlags(glyf, offset, np0, np)
	g.decodeCoords(glyf, offset, np0, np)

	// Set the four phantom points. Freetype-Go uses only the first two,
	// but the hinting bytecode may expect four.
	g.B = b
	uhm := f.unscaledHMetric(i)
	g.Point[np+0] = Point{X: b.XMin - uhm.LeftSideBearing}
	g.Point[np+1] = Point{X: b.XMin - uhm.LeftSideBearing + uhm.AdvanceWidth}
	g.Point[np+2] = Point{}
	g.Point[np+3] = Point{}

	// Delta-adjust, scale and hint.
	if h != nil {
		g.InFontUnits = append(g.InFontUnits, g.Point[np0:np+nPhantomPoints]...)
		for i := np0; i < np+nPhantomPoints; i++ {
			g.InFontUnits[i].X += dx
			g.InFontUnits[i].Y += dy
		}
	}
	scaledDx := int32(0)
	if roundDxDy {
		dx = (f.scale(scale*dx) + 32) &^ 63
		dy = (f.scale(scale*dy) + 32) &^ 63
		for i := np0; i < np+nPhantomPoints; i++ {
			g.Point[i].X = dx + f.scale(scale*g.Point[i].X)
			g.Point[i].Y = dy + f.scale(scale*g.Point[i].Y)
		}
		scaledDx = dx
	} else {
		for i := np0; i < np+nPhantomPoints; i++ {
			g.Point[i].X = f.scale(scale * (g.Point[i].X + dx))
			g.Point[i].Y = f.scale(scale * (g.Point[i].Y + dy))
		}
		scaledDx = f.scale(scale * dx)
	}
	if h != nil {
		g.Unhinted = append(g.Unhinted, g.Point[np0:np+nPhantomPoints]...)
		if program != nil {
			err := h.run(program, g.Point[np0:], g.Unhinted[np0:], g.InFontUnits[np0:], g.End[ne0:])
			if err != nil {
				return 0, err
			}
		}
		g.Unhinted = g.Unhinted[:np]
		g.InFontUnits = g.InFontUnits[:np]
	}
	if !metricsOverride {
		pp1x = g.Point[np].X - scaledDx
	}
	g.Point = g.Point[:np]
	if recursion == 0 && pp1x != 0 {
		for i := range g.Point {
			g.Point[i].X -= pp1x
		}
	}

	// The hinting program expects the []End values to be indexed relative
	// to the inner glyph, not the outer glyph, so we delay adding np0 until
	// after the hinting program (if any) has run.
	for i := ne0; i < ne; i++ {
		g.End[i] += np0
	}

	return pp1x, nil
}

// NewGlyphBuf returns a newly allocated GlyphBuf.
func NewGlyphBuf() *GlyphBuf {
	g := new(GlyphBuf)
	g.Point = make([]Point, 0, 256)
	g.End = make([]int, 0, 32)
	return g
}
