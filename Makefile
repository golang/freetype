# Copyright 2010 The Freetype-Go Authors. All rights reserved.
# Use of this source code is governed by your choice of either the
# FreeType License or the GNU General Public License version 2 (or
# any later version), both of which can be found in the LICENSE file.

include $(GOROOT)/src/Make.$(GOARCH)

all:	install

install:
	cd freetype/raster && make install
	cd freetype/truetype && make install
	cd freetype && make install

clean:
	cd freetype/raster && make clean
	cd freetype/truetype && make clean
	cd freetype && make clean

nuke:
	cd freetype/raster && make nuke
	cd freetype/truetype && make nuke
	cd freetype && make nuke
