// Copyright 2010 The Freetype-Go Authors. All rights reserved.
// Use of this source code is governed by your choice of either the
// FreeType License or the GNU General Public License version 2,
// both of which can be found in the LICENSE file.

package main

import (
	"bufio"
	"exp/draw"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"

	"freetype-go.googlecode.com/hg/freetype/raster"
)

func p(x, y int) raster.Point {
	return raster.Point{raster.Fixed(x * 256), raster.Fixed(y * 256)}
}

func clear(m *image.Alpha) {
	for y := 0; y < m.Height(); y++ {
		for x := 0; x < m.Width(); x++ {
			m.Pixel[y][x] = image.AlphaColor{0}
		}
	}
}

func main() {
	// Draw a rounded corner that is one pixel wide.
	r := raster.NewRasterizer(50, 50)
	r.Start(p(5, 5))
	r.Add1(p(5, 25))
	r.Add2(p(5, 45), p(25, 45))
	r.Add1(p(45, 45))
	r.Add1(p(45, 44))
	r.Add1(p(26, 44))
	r.Add2(p(6, 44), p(6, 24))
	r.Add1(p(6, 5))
	r.Add1(p(5, 5))

	// Rasterize that curve multiple times at different gammas.
	const (
		w = 600
		h = 200
	)
	rgba := image.NewRGBA(w, h)
	draw.Draw(rgba, draw.Rect(0, 0, w, h/2), image.Black, draw.ZP)
	draw.Draw(rgba, draw.Rect(0, h/2, w, h), image.White, draw.ZP)
	mask := image.NewAlpha(50, 50)
	painter := raster.NewAlphaPainter(mask)
	painter.Op = draw.Src
	gammas := []float{1.0 / 10.0, 1.0 / 3.0, 1.0 / 2.0, 2.0 / 3.0, 4.0 / 5.0, 1.0, 5.0 / 4.0, 3.0 / 2.0, 2.0, 3.0, 10.0}
	for i, g := range gammas {
		clear(mask)
		r.Rasterize(raster.NewGammaCorrectionPainter(painter, g))
		x, y := 50*i+25, 25
		draw.DrawMask(rgba, draw.Rect(x, y, x+50, y+50), image.White, draw.ZP, mask, draw.ZP, draw.Over)
		y += 100
		draw.DrawMask(rgba, draw.Rect(x, y, x+50, y+50), image.Black, draw.ZP, mask, draw.ZP, draw.Over)
	}

	// Save that RGBA image to disk.
	f, err := os.Open("out.png", os.O_CREAT|os.O_WRONLY, 0600)
	if err != nil {
		log.Stderr(err)
		os.Exit(1)
	}
	defer f.Close()
	b := bufio.NewWriter(f)
	err = png.Encode(b, rgba)
	if err != nil {
		log.Stderr(err)
		os.Exit(1)
	}
	err = b.Flush()
	if err != nil {
		log.Stderr(err)
		os.Exit(1)
	}
	fmt.Println("Wrote out.png OK.")
}
