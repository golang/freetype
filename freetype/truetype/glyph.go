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

	font   *Font
	hinter *Hinter
	scale  int32
	// pp1x is the X co-ordinate of the first phantom point.
	pp1x int32
	// metricsSet is whether the glyph's metrics have been set yet. For a
	// compound glyph, a sub-glyph may override the outer glyph's metrics.
	metricsSet bool
	// tmp is a scratch buffer.
	tmp []Point
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

// Load loads a glyph's contours from a Font, overwriting any previously
// loaded contours for this GlyphBuf. scale is the number of 26.6 fixed point
// units in 1 em. The Hinter is optional; if non-nil, then the resulting glyph
// will be hinted by the Font's bytecode instructions.
func (g *GlyphBuf) Load(f *Font, scale int32, i Index, h *Hinter) error {
	g.B = Bounds{}
	g.Point = g.Point[:0]
	g.Unhinted = g.Unhinted[:0]
	g.InFontUnits = g.InFontUnits[:0]
	g.End = g.End[:0]
	g.font = f
	g.hinter = h
	g.scale = scale
	g.pp1x = 0
	g.metricsSet = false

	if h != nil {
		if err := h.init(f, scale); err != nil {
			return err
		}
	}
	if err := g.load(0, i, true); err != nil {
		return err
	}
	if g.pp1x != 0 {
		for i := range g.Point {
			g.Point[i].X -= g.pp1x
		}
		// TODO: also adjust g.B?
	}
	return nil
}

func (g *GlyphBuf) load(recursion int32, i Index, useMyMetrics bool) (err error) {
	// The recursion limit here is arbitrary, but defends against malformed glyphs.
	if recursion >= 32 {
		return UnsupportedError("excessive compound glyph recursion")
	}
	// Find the relevant slice of g.font.glyf.
	var g0, g1 uint32
	if g.font.locaOffsetFormat == locaOffsetFormatShort {
		g0 = 2 * uint32(u16(g.font.loca, 2*int(i)))
		g1 = 2 * uint32(u16(g.font.loca, 2*int(i)+2))
	} else {
		g0 = u32(g.font.loca, 4*int(i))
		g1 = u32(g.font.loca, 4*int(i)+4)
	}
	if g0 == g1 {
		return nil
	}
	glyf := g.font.glyf[g0:g1]
	// Decode the contour end indices.
	ne := int(int16(u16(glyf, 0)))
	b := Bounds{
		XMin: int32(int16(u16(glyf, 2))),
		YMin: int32(int16(u16(glyf, 4))),
		XMax: int32(int16(u16(glyf, 6))),
		YMax: int32(int16(u16(glyf, 8))),
	}
	uhm, pp1x := g.font.unscaledHMetric(i), int32(0)
	if ne < 0 {
		if ne != -1 {
			// http://developer.apple.com/fonts/TTRefMan/RM06/Chap6glyf.html says that
			// "the values -2, -3, and so forth, are reserved for future use."
			return UnsupportedError("negative number of contours")
		}
		pp1x = g.font.scale(g.scale * (b.XMin - uhm.LeftSideBearing))
		if err := g.loadCompound(recursion, b, uhm, i, glyf, useMyMetrics); err != nil {
			return err
		}
	} else {
		np0, ne0 := len(g.Point), len(g.End)
		program := g.loadSimple(glyf, ne)
		g.addPhantomsAndScale(b, uhm, i, np0, true)
		pp1x = g.Point[len(g.Point)-4].X
		if g.hinter != nil {
			if len(program) != 0 {
				err := g.hinter.run(
					program,
					g.Point[np0:],
					g.Unhinted[np0:],
					g.InFontUnits[np0:],
					g.End[ne0:],
				)
				if err != nil {
					return err
				}
			}
			// Drop the four phantom points.
			g.InFontUnits = g.InFontUnits[:len(g.InFontUnits)-4]
			g.Unhinted = g.Unhinted[:len(g.Unhinted)-4]
		}
		g.Point = g.Point[:len(g.Point)-4]
		if np0 != 0 {
			// The hinting program expects the []End values to be indexed relative
			// to the inner glyph, not the outer glyph, so we delay adding np0 until
			// after the hinting program (if any) has run.
			for i := ne0; i < len(g.End); i++ {
				g.End[i] += np0
			}
		}
	}
	if useMyMetrics && !g.metricsSet {
		g.metricsSet = true
		g.B.XMin = g.font.scale(g.scale * b.XMin)
		g.B.YMin = g.font.scale(g.scale * b.YMin)
		g.B.XMax = g.font.scale(g.scale * b.XMax)
		g.B.YMax = g.font.scale(g.scale * b.YMax)
		g.pp1x = pp1x
	}
	return nil
}

// loadOffset is the initial offset for loadSimple and loadCompound. The first
// 10 bytes are the number of contours and the bounding box.
const loadOffset = 10

func (g *GlyphBuf) loadSimple(glyf []byte, ne int) (program []byte) {
	offset := loadOffset
	for i := 0; i < ne; i++ {
		g.End = append(g.End, 1+int(u16(glyf, offset)))
		offset += 2
	}

	// Note the TrueType hinting instructions.
	instrLen := int(u16(glyf, offset))
	offset += 2
	program = glyf[offset : offset+instrLen]
	offset += instrLen

	np0 := len(g.Point)
	np1 := np0 + int(g.End[len(g.End)-1])

	// Decode the flags.
	for i := np0; i < np1; {
		c := uint32(glyf[offset])
		offset++
		g.Point = append(g.Point, Point{Flags: c})
		i++
		if c&flagRepeat != 0 {
			count := glyf[offset]
			offset++
			for ; count > 0; count-- {
				g.Point = append(g.Point, Point{Flags: c})
				i++
			}
		}
	}

	// Decode the co-ordinates.
	var x int16
	for i := np0; i < np1; i++ {
		f := g.Point[i].Flags
		if f&flagXShortVector != 0 {
			dx := int16(glyf[offset])
			offset++
			if f&flagPositiveXShortVector == 0 {
				x -= dx
			} else {
				x += dx
			}
		} else if f&flagThisXIsSame == 0 {
			x += int16(u16(glyf, offset))
			offset += 2
		}
		g.Point[i].X = int32(x)
	}
	var y int16
	for i := np0; i < np1; i++ {
		f := g.Point[i].Flags
		if f&flagYShortVector != 0 {
			dy := int16(glyf[offset])
			offset++
			if f&flagPositiveYShortVector == 0 {
				y -= dy
			} else {
				y += dy
			}
		} else if f&flagThisYIsSame == 0 {
			y += int16(u16(glyf, offset))
			offset += 2
		}
		g.Point[i].Y = int32(y)
	}

	return program
}

func (g *GlyphBuf) loadCompound(recursion int32, b Bounds, uhm HMetric, i Index,
	glyf []byte, useMyMetrics bool) error {

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
	np0, ne0 := len(g.Point), len(g.End)
	offset := loadOffset
	for {
		flags := u16(glyf, offset)
		component := Index(u16(glyf, offset+2))
		dx, dy, transform, hasTransform := int32(0), int32(0), [4]int32{}, false
		if flags&flagArg1And2AreWords != 0 {
			dx = int32(int16(u16(glyf, offset+4)))
			dy = int32(int16(u16(glyf, offset+6)))
			offset += 8
		} else {
			dx = int32(int16(int8(glyf[offset+4])))
			dy = int32(int16(int8(glyf[offset+5])))
			offset += 6
		}
		if flags&flagArgsAreXYValues == 0 {
			return UnsupportedError("compound glyph transform vector")
		}
		if flags&(flagWeHaveAScale|flagWeHaveAnXAndYScale|flagWeHaveATwoByTwo) != 0 {
			hasTransform = true
			switch {
			case flags&flagWeHaveAScale != 0:
				transform[0] = int32(int16(u16(glyf, offset+0)))
				transform[3] = transform[0]
				offset += 2
			case flags&flagWeHaveAnXAndYScale != 0:
				transform[0] = int32(int16(u16(glyf, offset+0)))
				transform[3] = int32(int16(u16(glyf, offset+2)))
				offset += 4
			case flags&flagWeHaveATwoByTwo != 0:
				transform[0] = int32(int16(u16(glyf, offset+0)))
				transform[1] = int32(int16(u16(glyf, offset+2)))
				transform[2] = int32(int16(u16(glyf, offset+4)))
				transform[3] = int32(int16(u16(glyf, offset+6)))
				offset += 8
			}
		}
		np0 := len(g.Point)
		componentUMM := useMyMetrics && (flags&flagUseMyMetrics != 0)
		if err := g.load(recursion+1, component, componentUMM); err != nil {
			return err
		}
		if hasTransform {
			for j := np0; j < len(g.Point); j++ {
				p := &g.Point[j]
				newX := int32((int64(p.X)*int64(transform[0])+1<<13)>>14) +
					int32((int64(p.Y)*int64(transform[2])+1<<13)>>14)
				newY := int32((int64(p.X)*int64(transform[1])+1<<13)>>14) +
					int32((int64(p.Y)*int64(transform[3])+1<<13)>>14)
				p.X, p.Y = newX, newY
			}
		}
		dx = g.font.scale(g.scale * dx)
		dy = g.font.scale(g.scale * dy)
		if flags&flagRoundXYToGrid != 0 {
			dx = (dx + 32) &^ 63
			dy = (dy + 32) &^ 63
		}
		for j := np0; j < len(g.Point); j++ {
			p := &g.Point[j]
			p.X += dx
			p.Y += dy
		}
		// TODO: also adjust g.InFontUnits and g.Unhinted?
		if flags&flagMoreComponents == 0 {
			break
		}
	}

	// Hint the compound glyph.
	if g.hinter == nil || offset+2 > len(glyf) {
		return nil
	}
	instrLen := int(u16(glyf, offset))
	offset += 2
	if instrLen == 0 {
		return nil
	}
	program := glyf[offset : offset+instrLen]
	g.addPhantomsAndScale(b, uhm, i, len(g.Point), false)
	points, ends := g.Point[np0:], g.End[ne0:]
	g.Point = g.Point[:len(g.Point)-4]
	for j := range points {
		points[j].Flags &^= flagTouchedX | flagTouchedY
	}
	// Temporarily adjust the ends to be relative to this compound glyph.
	if np0 != 0 {
		for i := range ends {
			ends[i] -= np0
		}
	}
	// Hinting instructions of a composite glyph completely refer to the
	// (already) hinted subglyphs.
	g.tmp = append(g.tmp[:0], points...)
	if err := g.hinter.run(program, points, g.tmp, g.tmp, ends); err != nil {
		return err
	}
	if np0 != 0 {
		for i := range ends {
			ends[i] += np0
		}
	}
	return nil
}

func (g *GlyphBuf) addPhantomsAndScale(b Bounds, uhm HMetric, i Index, np0 int, simple bool) {
	// Add the four phantom points.
	uvm := g.font.unscaledVMetric(i, b.YMax)
	g.Point = append(g.Point,
		Point{X: b.XMin - uhm.LeftSideBearing},
		Point{X: b.XMin - uhm.LeftSideBearing + uhm.AdvanceWidth},
		Point{X: uhm.AdvanceWidth / 2, Y: b.YMax + uvm.TopSideBearing},
		Point{X: uhm.AdvanceWidth / 2, Y: b.YMax + uvm.TopSideBearing - uvm.AdvanceHeight},
	)
	// Scale the points.
	if simple && g.hinter != nil {
		g.InFontUnits = append(g.InFontUnits, g.Point[np0:]...)
	}
	for i := np0; i < len(g.Point); i++ {
		p := &g.Point[i]
		p.X = g.font.scale(g.scale * p.X)
		p.Y = g.font.scale(g.scale * p.Y)
	}
	if simple && g.hinter != nil {
		// Round the 1st phantom point to the grid, shifting all other points equally.
		pp1x := g.Point[len(g.Point)-4].X
		if dx := ((pp1x + 32) &^ 63) - pp1x; dx != 0 {
			for i := np0; i < len(g.Point); i++ {
				g.Point[i].X += dx
			}
		}
		g.Unhinted = append(g.Unhinted, g.Point[np0:]...)
	}
	// Round the 2nd and 4th phantom point to the grid.
	p := &g.Point[len(g.Point)-3]
	p.X = (p.X + 32) &^ 63
	p = &g.Point[len(g.Point)-1]
	p.Y = (p.Y + 32) &^ 63
}

// TODO: is this necessary? The zero-valued GlyphBuf is perfectly usable.

// NewGlyphBuf returns a newly allocated GlyphBuf.
func NewGlyphBuf() *GlyphBuf {
	return &GlyphBuf{
		Point: make([]Point, 0, 256),
		End:   make([]int, 0, 32),
	}
}
