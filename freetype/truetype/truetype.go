// Copyright 2010 The Freetype-Go Authors. All rights reserved.
// Use of this source code is governed by your choice of either the
// FreeType License or the GNU General Public License version 2,
// both of which can be found in the LICENSE file.

// The truetype package provides a parser for the TTF file format. That format
// is documented at http://developer.apple.com/fonts/TTRefMan/ and
// http://www.microsoft.com/typography/otspec/
//
// All numbers (e.g. bounds, point co-ordinates, font metrics) are measured in
// FUnits. To convert from FUnits to pixels, scale by
// (pointSize * resolution) / (font.UnitsPerEm() * 72dpi)
// For example, 550 FUnits at 18pt, 72dpi and 2048upe is 4.83 pixels.
package truetype

import (
	"fmt"
	"os"
)

// An Index is a Font's index of a Unicode code point.
type Index uint16

// A Bounds holds the co-ordinate range of one or more glyphs.
// The endpoints are inclusive.
type Bounds struct {
	XMin, YMin, XMax, YMax int16
}

// An HMetric holds the horizontal metrics of a single glyph.
type HMetric struct {
	AdvanceWidth    uint16
	LeftSideBearing int16
}

// A FormatError reports that the input is not a valid TrueType font.
type FormatError string

func (e FormatError) String() string {
	return "freetype: invalid TrueType format: " + string(e)
}

// An UnsupportedError reports that the input uses a valid but unimplemented
// TrueType feature.
type UnsupportedError string

func (e UnsupportedError) String() string {
	return "freetype: unsupported TrueType feature: " + string(e)
}

// data interprets a byte slice as a stream of integer values.
type data []byte

// u32 returns the next big-endian uint32.
func (d *data) u32() uint32 {
	x := uint32((*d)[0])<<24 | uint32((*d)[1])<<16 | uint32((*d)[2])<<8 | uint32((*d)[3])
	*d = (*d)[4:]
	return x
}

// u16 returns the next big-endian uint16.
func (d *data) u16() uint16 {
	x := uint16((*d)[0])<<8 | uint16((*d)[1])
	*d = (*d)[2:]
	return x
}

// u8 returns the next uint8.
func (d *data) u8() uint8 {
	x := (*d)[0]
	*d = (*d)[1:]
	return x
}

// skip skips the next n bytes.
func (d *data) skip(n int) {
	*d = (*d)[n:]
}

// readTable returns a slice of the TTF data given by a table's directory entry.
func readTable(ttf []byte, offsetLength []byte) ([]byte, os.Error) {
	d := data(offsetLength)
	offset := int(d.u32())
	if offset < 0 || offset > 1<<24 || offset > len(ttf) {
		return nil, FormatError(fmt.Sprintf("offset too large: %d", offset))
	}
	length := int(d.u32())
	if length < 0 || length > 1<<24 || offset+length > len(ttf) {
		return nil, FormatError(fmt.Sprintf("length too large: %d", length))
	}
	return ttf[offset : offset+length], nil
}

const (
	locaOffsetFormatUnknown int = iota
	locaOffsetFormatShort
	locaOffsetFormatLong
)

// A cm holds a parsed cmap entry.
type cm struct {
	start, end, delta, offset uint16
}

// A Font represents a Truetype font.
type Font struct {
	// Tables sliced from the TTF data. The different tables are documented
	// at http://developer.apple.com/fonts/TTRefMan/RM06/Chap6.html
	cmap, glyf, head, hhea, hmtx, kern, loca, maxp []byte
	cmapIndexes                                    []byte

	// Cached values derived from the raw ttf data.
	cm                      []cm
	locaOffsetFormat        int
	nGlyph, nHMetric, nKern int
	unitsPerEm              int
	bounds                  Bounds
}

func (f *Font) parseCmap() os.Error {
	const (
		cmapFormat4         = 4
		languageIndependent = 0

		// A 32-bit encoding consists of a most-significant 16-bit Platform ID and a
		// least-significant 16-bit Platform Specific ID.
		unicodeEncoding   = 0x00000003 // PID = 0 (Unicode), PSID = 3 (Unicode 2.0)
		microsoftEncoding = 0x00030001 // PID = 3 (Microsoft), PSID = 1 (UCS-2)
	)

	if len(f.cmap) < 4 {
		return FormatError("cmap too short")
	}
	d := data(f.cmap[2:])
	nsubtab := int(d.u16())
	if len(f.cmap) < 8*nsubtab+4 {
		return FormatError("cmap too short")
	}
	offset, found := 0, false
	for i := 0; i < nsubtab; i++ {
		// We read the 16-bit Platform ID and 16-bit Platform Specific ID as a single uint32.
		// All values are big-endian.
		pidPsid, o := d.u32(), d.u32()
		// We prefer the Unicode cmap encoding. Failing to find that, we fall
		// back onto the Microsoft cmap encoding.
		if pidPsid == unicodeEncoding {
			offset, found = int(o), true
			break
		} else if pidPsid == microsoftEncoding {
			offset, found = int(o), true
			// We don't break out of the for loop, so that Unicode can override Microsoft.
		}
	}
	if !found {
		return UnsupportedError("cmap encoding")
	}
	if offset <= 0 || offset > len(f.cmap) {
		return FormatError("bad cmap offset")
	}

	d = data(f.cmap[offset:])
	cmapFormat := d.u16()
	if cmapFormat != cmapFormat4 {
		return UnsupportedError(fmt.Sprintf("cmap format: %d", cmapFormat))
	}
	d.skip(2)
	language := d.u16()
	if language != languageIndependent {
		return UnsupportedError(fmt.Sprintf("language: %d", language))
	}
	segCountX2 := int(d.u16())
	if segCountX2%2 == 1 {
		return FormatError(fmt.Sprintf("bad segCountX2: %d", segCountX2))
	}
	segCount := segCountX2 / 2
	d.skip(6)
	f.cm = make([]cm, segCount)
	for i := 0; i < segCount; i++ {
		f.cm[i].end = d.u16()
	}
	d.skip(2)
	for i := 0; i < segCount; i++ {
		f.cm[i].start = d.u16()
	}
	for i := 0; i < segCount; i++ {
		f.cm[i].delta = d.u16()
	}
	for i := 0; i < segCount; i++ {
		f.cm[i].offset = d.u16()
	}
	f.cmapIndexes = []byte(d)
	return nil
}

func (f *Font) parseHead() os.Error {
	if len(f.head) != 54 {
		return FormatError(fmt.Sprintf("bad head length: %d", len(f.head)))
	}
	d := data(f.head[18:])
	f.unitsPerEm = int(d.u16())
	d.skip(16)
	f.bounds.XMin = int16(d.u16())
	f.bounds.YMin = int16(d.u16())
	f.bounds.XMax = int16(d.u16())
	f.bounds.YMax = int16(d.u16())
	d.skip(6)
	switch i := d.u16(); i {
	case 0:
		f.locaOffsetFormat = locaOffsetFormatShort
	case 1:
		f.locaOffsetFormat = locaOffsetFormatLong
	default:
		return FormatError(fmt.Sprintf("bad indexToLocFormat: %d", i))
	}
	return nil
}

func (f *Font) parseHhea() os.Error {
	if len(f.hhea) != 36 {
		return FormatError(fmt.Sprintf("bad hhea length: %d", len(f.hhea)))
	}
	d := data(f.hhea[34:])
	f.nHMetric = int(d.u16())
	if 4*f.nHMetric+2*(f.nGlyph-f.nHMetric) != len(f.hmtx) {
		return FormatError(fmt.Sprintf("bad hmtx length: %d", len(f.hmtx)))
	}
	return nil
}

func (f *Font) parseKern() os.Error {
	// Apple's TrueType documentation (http://developer.apple.com/fonts/TTRefMan/RM06/Chap6kern.html) says:
	// "Previous versions of the 'kern' table defined both the version and nTables fields in the header
	// as UInt16 values and not UInt32 values. Use of the older format on the Mac OS is discouraged
	// (although AAT can sense an old kerning table and still make correct use of it). Microsoft
	// Windows still uses the older format for the 'kern' table and will not recognize the newer one.
	// Fonts targeted for the Mac OS only should use the new format; fonts targeted for both the Mac OS
	// and Windows should use the old format."
	// Since we expect that almost all fonts aim to be Windows-compatible, we only parse the "older" format,
	// just like the C Freetype implementation.
	if len(f.kern) == 0 {
		if f.nKern != 0 {
			return FormatError("bad kern table length")
		}
		return nil
	}
	if len(f.kern) < 18 {
		return FormatError("kern data too short")
	}
	d := data(f.kern[0:])
	version := d.u16()
	if version != 0 {
		return UnsupportedError(fmt.Sprintf("kern version: %d", version))
	}
	n := d.u16()
	if n != 1 {
		return UnsupportedError(fmt.Sprintf("kern nTables: %d", n))
	}
	d.skip(2)
	length := int(d.u16())
	coverage := d.u16()
	if coverage != 0x0001 {
		// We only support horizontal kerning.
		return UnsupportedError(fmt.Sprintf("kern coverage: 0x%04x", coverage))
	}
	f.nKern = int(d.u16())
	if 6*f.nKern != length-14 {
		return FormatError("bad kern table length")
	}
	return nil
}

func (f *Font) parseMaxp() os.Error {
	if len(f.maxp) != 32 {
		return FormatError(fmt.Sprintf("bad maxp length: %d", len(f.maxp)))
	}
	d := data(f.maxp[4:])
	f.nGlyph = int(d.u16())
	return nil
}

// Bounds returns the union of a Font's glyphs' bounds.
func (f *Font) Bounds() Bounds {
	return f.bounds
}

// UnitsPerEm returns the number of FUnits in a Font's em-square.
func (f *Font) UnitsPerEm() int {
	return f.unitsPerEm
}

// Index returns a Font's index for the given Unicode code point.
func (f *Font) Index(codePoint int) Index {
	c := uint16(codePoint)
	n := len(f.cm)
	for i := 0; i < n; i++ {
		if f.cm[i].start <= c && c <= f.cm[i].end {
			if f.cm[i].offset == 0 {
				return Index(c + f.cm[i].delta)
			}
			offset := int(f.cm[i].offset) + 2*(i-n+int(c-f.cm[i].start))
			d := data(f.cmapIndexes[offset:])
			return Index(d.u16())
		}
	}
	return Index(0)
}

// HMetric returns the horizontal metrics for the glyph with the given index.
func (f *Font) HMetric(i Index) HMetric {
	j := int(i)
	if j >= f.nGlyph {
		return HMetric{}
	}
	if j >= f.nHMetric {
		var hm HMetric
		p := 4 * (f.nHMetric - 1)
		d := data(f.hmtx[p:])
		hm.AdvanceWidth = d.u16()
		p += 2*(j-f.nHMetric) + 4
		d = data(f.hmtx[p:])
		hm.LeftSideBearing = int16(d.u16())
		return hm
	}
	d := data(f.hmtx[4*j:])
	return HMetric{d.u16(), int16(d.u16())}
}

// Kerning returns the kerning for the given glyph pair.
func (f *Font) Kerning(i0, i1 Index) int16 {
	if f.nKern == 0 {
		return 0
	}
	g := uint32(i0)<<16 | uint32(i1)
	lo, hi := 0, f.nKern
	for lo < hi {
		i := (lo + hi) / 2
		d := data(f.kern[18+6*i:])
		ig := d.u32()
		if ig < g {
			lo = i + 1
		} else if ig > g {
			hi = i
		} else {
			return int16(d.u16())
		}
	}
	return 0
}

// Parse returns a new Font for the given TTF data.
func Parse(ttf []byte) (font *Font, err os.Error) {
	if len(ttf) < 12 {
		err = FormatError("TTF data is too short")
		return
	}
	d := data(ttf[0:])
	if d.u32() != 0x00010000 {
		err = FormatError("bad version")
		return
	}
	n := int(d.u16())
	if len(ttf) < 16*n+12 {
		err = FormatError("TTF data is too short")
		return
	}
	f := new(Font)
	// Assign the table slices.
	for i := 0; i < n; i++ {
		x := 16*i + 12
		switch string(ttf[x : x+4]) {
		case "cmap":
			f.cmap, err = readTable(ttf, ttf[x+8:x+16])
		case "glyf":
			f.glyf, err = readTable(ttf, ttf[x+8:x+16])
		case "head":
			f.head, err = readTable(ttf, ttf[x+8:x+16])
		case "hhea":
			f.hhea, err = readTable(ttf, ttf[x+8:x+16])
		case "hmtx":
			f.hmtx, err = readTable(ttf, ttf[x+8:x+16])
		case "kern":
			f.kern, err = readTable(ttf, ttf[x+8:x+16])
		case "loca":
			f.loca, err = readTable(ttf, ttf[x+8:x+16])
		case "maxp":
			f.maxp, err = readTable(ttf, ttf[x+8:x+16])
		}
		if err != nil {
			return
		}
	}
	// Parse and sanity-check the TTF data.
	if err = f.parseHead(); err != nil {
		return
	}
	if err = f.parseMaxp(); err != nil {
		return
	}
	if err = f.parseCmap(); err != nil {
		return
	}
	if err = f.parseKern(); err != nil {
		return
	}
	if err = f.parseHhea(); err != nil {
		return
	}
	font = f
	return
}

// A Point is a co-ordinate pair plus whether it is ``on'' a contour or an
// ``off'' control point.
type Point struct {
	X, Y int16
	// The Flags' LSB means whether or not this Point is ``on'' the contour.
	// Other bits are reserved for internal use.
	Flags uint8
}

// A GlyphBuf holds a glyph's contours. A GlyphBuf can be re-used to load a
// series of glyphs from a Font.
type GlyphBuf struct {
	// The glyph's bounding box.
	B Bounds
	// Point contains all Points from all contours of the glyph.
	Point []Point
	// The length of End is the number of contours in the glyph. The i'th
	// contour consists of points Point[End[i-1]:End[i]], where End[-1]
	// is interpreted to mean zero.
	End []int
}

// decodeFlags decodes a glyph's run-length encoded flags,
// and returns the remaining data.
func (g *GlyphBuf) decodeFlags(d data) data {
	for i := 0; i < len(g.Point); {
		c := d.u8()
		g.Point[i].Flags = c
		i++
		if c&0x08 != 0 {
			count := d.u8()
			for ; count > 0; count-- {
				g.Point[i].Flags = c
				i++
			}
		}
	}
	return d
}

// decodeCoords decodes a glyph's delta encoded co-ordinates.
func (g *GlyphBuf) decodeCoords(d data) {
	var x int16
	for i := 0; i < len(g.Point); i++ {
		f := g.Point[i].Flags
		if f&0x02 != 0 {
			dx := int16(d.u8())
			if f&0x10 == 0 {
				x -= dx
			} else {
				x += dx
			}
		} else if f&0x10 == 0 {
			x += int16(d.u16())
		}
		g.Point[i].X = x
	}
	var y int16
	for i := 0; i < len(g.Point); i++ {
		f := g.Point[i].Flags
		if f&0x04 != 0 {
			dy := int16(d.u8())
			if f&0x20 == 0 {
				y -= dy
			} else {
				y += dy
			}
		} else if f&0x20 == 0 {
			y += int16(d.u16())
		}
		g.Point[i].Y = y
	}
}

// Load loads a glyph's contours from a Font, overwriting any previously
// loaded contours for this GlyphBuf.
func (g *GlyphBuf) Load(f *Font, i Index) os.Error {
	// Reset the GlyphBuf.
	g.B = Bounds{}
	g.Point = g.Point[0:0]
	g.End = g.End[0:0]
	// Find the relevant slice of f.glyf.
	var g0, g1 uint32
	if f.locaOffsetFormat == locaOffsetFormatShort {
		d := data(f.loca[2*i:])
		g0 = 2 * uint32(d.u16())
		g1 = 2 * uint32(d.u16())
	} else {
		d := data(f.loca[4*i:])
		g0 = d.u32()
		g1 = d.u32()
	}
	if g0 == g1 {
		return nil
	}
	d := data(f.glyf[g0:g1])
	// Decode the contour end indices.
	ne := int(d.u16())
	if ne == 1<<16-1 {
		return UnsupportedError("compound glyph")
	}
	g.B.XMin = int16(d.u16())
	g.B.YMin = int16(d.u16())
	g.B.XMax = int16(d.u16())
	g.B.YMax = int16(d.u16())
	if ne <= cap(g.End) {
		g.End = g.End[0:ne]
	} else {
		g.End = make([]int, ne, ne*2)
	}
	for i := 0; i < ne; i++ {
		g.End[i] = 1 + int(d.u16())
	}
	// Skip the TrueType hinting instructions.
	instrLen := int(d.u16())
	d.skip(instrLen)
	// Decode the points.
	np := int(g.End[ne-1])
	if np <= cap(g.Point) {
		g.Point = g.Point[0:np]
	} else {
		g.Point = make([]Point, np, np*2)
	}
	d = g.decodeFlags(d)
	g.decodeCoords(d)
	return nil
}

// NewGlyphBuf returns a newly allocated GlyphBuf.
func NewGlyphBuf() *GlyphBuf {
	g := new(GlyphBuf)
	g.Point = make([]Point, 0, 256)
	g.End = make([]int, 0, 32)
	return g
}
