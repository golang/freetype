// The text2svg command converts a text string to a stroked SVG path
// in a given TrueType v1 font.
package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"

	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// flags
var (
	textFlag = flag.String("text", "Hamburger", "the text to print")
	fontFlag = flag.String("font", "/Library/Fonts/Georgia Italic.ttf",
		"file name of the TrueType v1 font to use")
	scaleFlag = flag.Int("scale", 100, "scale in points")
)

func main() {
	flag.Parse()

	log.SetPrefix("text2svg: ")
	log.SetFlags(0)

	ttfdata, err := ioutil.ReadFile(*fontFlag)
	if err != nil {
		log.Fatalf("loading font: %v", err)
	}

	f, err := truetype.Parse(ttfdata)
	if err != nil {
		log.Fatalf("parsing font: %v", err)
	}

	fmt.Printf("<svg xmlns='http://www.w3.org/2000/svg' "+
		"style='fill: grey' width='%d' height='%d'>\n",
		1000, 1000)

	scale := fixed.I(*scaleFlag)

	dy = scale // set the baseline one line below the origin

	var prevIndex truetype.Index
	for i, r := range *textFlag {
		index := f.Index(r)

		// Load the contours for a glyph.
		var gbuf truetype.GlyphBuf
		if err := gbuf.Load(f, scale, index, font.HintingNone); err != nil {
			log.Fatalf("loading glyph: %v", err)
		}

		// Emit a single SVG <path> for all glyph contours.
		fmt.Printf("<path d='")
		prevEnd := 0
		for _, end := range gbuf.Ends {
			drawContour(gbuf.Points[prevEnd:end], drawSVG)
			prevEnd = end
		}
		fmt.Printf("'/>\n")

		// Advance the position.
		dx += gbuf.AdvanceWidth
		if i > 0 {
			dx += f.Kern(scale, prevIndex, index)
		}
		prevIndex = index
	}
	fmt.Println("</svg>")
}

func drawSVG(cmd rune, p0, p1 fixed.Point26_6) {
	switch cmd {
	case 'M': // moveto
		fmt.Printf("M%s ", p2svg(p0))
	case 'L': // lineto
		fmt.Printf("L%s ", p2svg(p0))
	case 'Q': // quadratic spline
		fmt.Printf("Q%s %s ", p2svg(p0), p2svg(p1))
	}
}

var dx, dy fixed.Int26_6

func p2svg(p fixed.Point26_6) string {
	return fmt.Sprintf("%v,%v",
		float64(dx+p.X)/64,
		float64(dy-p.Y)/64)
}

var dummy fixed.Point26_6

// drawContour calls the draw function for each moveto, lineto, or
// quadratic spline command in the specified contour.
//
// Stolen from drawContour in github.com/golang/freetype/freetype.go.
// It would be nice if that version was reusable.
func drawContour(ps []truetype.Point, draw func(cmd rune, p0, p1 fixed.Point26_6)) {
	if len(ps) == 0 {
		return
	}

	// The low bit of each point's Flags value is whether the
	// point is on the curve. Truetype fonts only have quadratic
	// Bézier curves, not cubics.  Thus, two consecutive off-curve
	// points imply an on-curve point in the middle of those two.
	//
	// See http://chanae.walon.org/pub/ttf/ttf_glyphs.htm for more details.

	// ps[0] is a truetype.Point measured in FUnits and positive Y going
	// upwards. start is the same thing measured in fixed point units and
	// positive Y going downwards, and offset by (dx, dy).
	start := fixed.Point26_6{
		X: ps[0].X,
		Y: ps[0].Y,
	}
	var others []truetype.Point
	if ps[0].Flags&1 != 0 {
		others = ps[1:]
	} else {
		last := fixed.Point26_6{
			X: ps[len(ps)-1].X,
			Y: ps[len(ps)-1].Y,
		}
		if ps[len(ps)-1].Flags&1 != 0 {
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
	draw('M', start, dummy)
	q0, on0 := start, true
	for _, p := range others {
		q := fixed.Point26_6{
			X: p.X,
			Y: p.Y,
		}
		on := p.Flags&1 != 0
		if on {
			if on0 {
				draw('L', q, dummy)
			} else {
				draw('Q', q0, q)
			}
		} else {
			if on0 {
				// No-op.
			} else {
				mid := fixed.Point26_6{
					X: (q0.X + q.X) / 2,
					Y: (q0.Y + q.Y) / 2,
				}
				draw('Q', q0, mid)
			}
		}
		q0, on0 = q, on
	}
	// Close the curve.
	if on0 {
		draw('L', start, dummy)
	} else {
		draw('Q', q0, start)
	}
}
