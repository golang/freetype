// Copyright 2010-2017 The Freetype-Go Authors. All rights reserved.
// Use of this source code is governed by your choice of either the
// FreeType License or the GNU General Public License version 2 (or
// any later version), both of which can be found in the LICENSE file.
package main

import (
	"flag"
	"io/ioutil"

	"fmt"
	"os"

	"github.com/golang/freetype"
)

var fontfile = flag.String("font", "", "filename of font to dump")

func main() {
	flag.Parse()

	// Load the raw data from disk
	fontData, err := ioutil.ReadFile(*fontfile)
	if err != nil {
		fmt.Printf("Failed to load font from %s: %+v\n", *fontfile, err)
		os.Exit(1)
	}

	// Parse the font data
	font, err := freetype.ParseFont(fontData)
	if err != nil {
		fmt.Printf("Failed to parse font from %s: %+v\n", *fontfile, err)
		os.Exit(1)
	}

	// Dump summary info
	font.Dump()
}
