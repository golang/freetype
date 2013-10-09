// Copyright 2012 The Freetype-Go Authors. All rights reserved.
// Use of this source code is governed by your choice of either the
// FreeType License or the GNU General Public License version 2 (or
// any later version), both of which can be found in the LICENSE file.

package truetype

// This file implements a Truetype bytecode interpreter.
// The opcodes are described at https://developer.apple.com/fonts/TTRefMan/RM05/Chap5.html

import (
	"errors"
)

const (
	twilightZone = 0
	glyphZone    = 1
	numZone      = 2
)

type pointType uint32

const (
	current      pointType = 0
	unhinted     pointType = 1
	inFontUnits  pointType = 2
	numPointType           = 3
)

// callStackEntry is a bytecode call stack entry.
type callStackEntry struct {
	program   []byte
	pc        int
	loopCount int32
}

// Hinter implements bytecode hinting. Pass a Hinter to GlyphBuf.Load to hint
// the resulting glyph. A Hinter can be re-used to hint a series of glyphs from
// a Font.
type Hinter struct {
	stack, store []int32

	// functions is a map from function number to bytecode.
	functions map[int32][]byte

	// font and scale are the font and scale last used for this Hinter.
	// Changing the font will require running the new font's fpgm bytecode.
	// Changing either will require running the font's prep bytecode.
	font  *Font
	scale int32

	// gs and defaultGS are the current and default graphics state. The
	// default graphics state is the global default graphics state after
	// the font's fpgm and prep programs have been run.
	gs, defaultGS graphicsState

	// points and ends are the twilight zone's points, glyph's points
	// and glyph's contour boundaries.
	points [numZone][numPointType][]Point
	ends   []int
}

// graphicsState is described at https://developer.apple.com/fonts/TTRefMan/RM04/Chap4.html
type graphicsState struct {
	// Projection vector, freedom vector and dual projection vector.
	pv, fv, dv [2]f2dot14
	// Reference points and zone pointers.
	rp, zp [3]int32
	// Control Value / Single Width Cut-In.
	controlValueCutIn, singleWidthCutIn, singleWidth f26dot6
	// Delta base / shift.
	deltaBase, deltaShift int32
	// Minimum distance.
	minDist f26dot6
	// Loop count.
	loop int32
	// Rounding policy.
	roundPeriod, roundPhase, roundThreshold f26dot6
	// Auto-flip.
	autoFlip bool
}

var globalDefaultGS = graphicsState{
	pv:                [2]f2dot14{0x4000, 0}, // Unit vector along the X axis.
	fv:                [2]f2dot14{0x4000, 0},
	dv:                [2]f2dot14{0x4000, 0},
	zp:                [3]int32{1, 1, 1},
	controlValueCutIn: (17 << 6) / 16, // 17/16 as an f26dot6.
	deltaBase:         9,
	deltaShift:        3,
	minDist:           1 << 6, // 1 as an f26dot6.
	loop:              1,
	roundPeriod:       1 << 6, // 1 as an f26dot6.
	roundThreshold:    1 << 5, // 1/2 as an f26dot6.
	autoFlip:          true,
}

func resetTwilightPoints(f *Font, p []Point) []Point {
	if n := int(f.maxTwilightPoints) + 4; n <= cap(p) {
		p = p[:n]
		for i := range p {
			p[i] = Point{}
		}
	} else {
		p = make([]Point, n)
	}
	return p
}

func (h *Hinter) init(f *Font, scale int32) error {
	h.points[twilightZone][0] = resetTwilightPoints(f, h.points[twilightZone][0])
	h.points[twilightZone][1] = resetTwilightPoints(f, h.points[twilightZone][1])
	h.points[twilightZone][2] = resetTwilightPoints(f, h.points[twilightZone][2])

	rescale := h.scale != scale
	if h.font != f {
		h.font, rescale = f, true
		if h.functions == nil {
			h.functions = make(map[int32][]byte)
		} else {
			for k := range h.functions {
				delete(h.functions, k)
			}
		}

		if x := int(f.maxStackElements); x > len(h.stack) {
			x += 255
			x &^= 255
			h.stack = make([]int32, x)
		}
		if x := int(f.maxStorage); x > len(h.store) {
			x += 15
			x &^= 15
			h.store = make([]int32, x)
		}
		if len(f.fpgm) != 0 {
			if err := h.run(f.fpgm, nil, nil, nil, nil); err != nil {
				return err
			}
		}
	}

	if rescale {
		h.scale = scale

		h.defaultGS = globalDefaultGS

		if len(f.prep) != 0 {
			if err := h.run(f.prep, nil, nil, nil, nil); err != nil {
				return err
			}
			h.defaultGS = h.gs
			// The MS rasterizer doesn't allow the following graphics state
			// variables to be modified by the CVT program.
			h.defaultGS.pv = globalDefaultGS.pv
			h.defaultGS.fv = globalDefaultGS.fv
			h.defaultGS.dv = globalDefaultGS.dv
			h.defaultGS.rp = globalDefaultGS.rp
			h.defaultGS.zp = globalDefaultGS.zp
			h.defaultGS.loop = globalDefaultGS.loop
		}
	}
	return nil
}

func (h *Hinter) run(program []byte, pCurrent, pUnhinted, pInFontUnits []Point, ends []int) error {
	h.gs = h.defaultGS
	h.points[glyphZone][current] = pCurrent
	h.points[glyphZone][unhinted] = pUnhinted
	h.points[glyphZone][inFontUnits] = pInFontUnits
	h.ends = ends

	if len(program) > 50000 {
		return errors.New("truetype: hinting: too many instructions")
	}
	var (
		steps, pc, top int
		opcode         uint8

		callStack    [32]callStackEntry
		callStackTop int
	)

	for 0 <= pc && pc < len(program) {
		steps++
		if steps == 100000 {
			return errors.New("truetype: hinting: too many steps")
		}
		opcode = program[pc]
		if popCount[opcode] == q {
			return errors.New("truetype: hinting: unimplemented instruction")
		}
		if top < int(popCount[opcode]) {
			return errors.New("truetype: hinting: stack underflow")
		}
		switch opcode {

		case opSVTCA0:
			h.gs.pv = [2]f2dot14{0, 0x4000}
			h.gs.fv = [2]f2dot14{0, 0x4000}
			h.gs.dv = [2]f2dot14{0, 0x4000}

		case opSVTCA1:
			h.gs.pv = [2]f2dot14{0x4000, 0}
			h.gs.fv = [2]f2dot14{0x4000, 0}
			h.gs.dv = [2]f2dot14{0x4000, 0}

		case opSPVTCA0:
			h.gs.pv = [2]f2dot14{0, 0x4000}
			h.gs.dv = [2]f2dot14{0, 0x4000}

		case opSPVTCA1:
			h.gs.pv = [2]f2dot14{0x4000, 0}
			h.gs.dv = [2]f2dot14{0x4000, 0}

		case opSFVTCA0:
			h.gs.fv = [2]f2dot14{0, 0x4000}

		case opSFVTCA1:
			h.gs.fv = [2]f2dot14{0x4000, 0}

		case opSPVFS:
			top -= 2
			h.gs.pv[0] = f2dot14(h.stack[top+0])
			h.gs.pv[1] = f2dot14(h.stack[top+1])
			// TODO: normalize h.gs.pv ??
			// TODO: h.gs.dv = h.gs.pv ??

		case opSFVFS:
			top -= 2
			h.gs.fv[0] = f2dot14(h.stack[top+0])
			h.gs.fv[1] = f2dot14(h.stack[top+1])
			// TODO: normalize h.gs.fv ??

		case opGPV:
			if top+1 >= len(h.stack) {
				return errors.New("truetype: hinting: stack overflow")
			}
			h.stack[top+0] = int32(h.gs.pv[0])
			h.stack[top+1] = int32(h.gs.pv[1])
			top += 2

		case opGFV:
			if top+1 >= len(h.stack) {
				return errors.New("truetype: hinting: stack overflow")
			}
			h.stack[top+0] = int32(h.gs.fv[0])
			h.stack[top+1] = int32(h.gs.fv[1])
			top += 2

		case opSFVTPV:
			h.gs.fv = h.gs.pv

		case opSRP0, opSRP1, opSRP2:
			top--
			h.gs.rp[opcode-opSRP0] = h.stack[top]

		case opSZP0, opSZP1, opSZP2:
			top--
			h.gs.zp[opcode-opSZP0] = h.stack[top]

		case opSZPS:
			top--
			h.gs.zp[0] = h.stack[top]
			h.gs.zp[1] = h.stack[top]
			h.gs.zp[2] = h.stack[top]

		case opSLOOP:
			top--
			if h.stack[top] <= 0 {
				return errors.New("truetype: hinting: invalid data")
			}
			h.gs.loop = h.stack[top]

		case opRTG:
			h.gs.roundPeriod = 1 << 6
			h.gs.roundPhase = 0
			h.gs.roundThreshold = 1 << 5

		case opRTHG:
			h.gs.roundPeriod = 1 << 6
			h.gs.roundPhase = 1 << 5
			h.gs.roundThreshold = 1 << 5

		case opSMD:
			top--
			h.gs.minDist = f26dot6(h.stack[top])

		case opELSE:
			opcode = 1
			goto ifelse

		case opJMPR:
			top--
			pc += int(h.stack[top])
			continue

		case opSCVTCI:
			top--
			h.gs.controlValueCutIn = f26dot6(h.stack[top])

		case opSSWCI:
			top--
			h.gs.singleWidthCutIn = f26dot6(h.stack[top])

		case opSSW:
			top--
			h.gs.singleWidth = f26dot6(h.font.scale(h.scale * h.stack[top]))

		case opDUP:
			if top >= len(h.stack) {
				return errors.New("truetype: hinting: stack overflow")
			}
			h.stack[top] = h.stack[top-1]
			top++

		case opPOP:
			top--

		case opCLEAR:
			top = 0

		case opSWAP:
			h.stack[top-1], h.stack[top-2] = h.stack[top-2], h.stack[top-1]

		case opDEPTH:
			if top >= len(h.stack) {
				return errors.New("truetype: hinting: stack overflow")
			}
			h.stack[top] = int32(top)
			top++

		case opCINDEX, opMINDEX:
			x := int(h.stack[top-1])
			if x <= 0 || x >= top {
				return errors.New("truetype: hinting: invalid data")
			}
			h.stack[top-1] = h.stack[top-1-x]
			if opcode == opMINDEX {
				copy(h.stack[top-1-x:top-1], h.stack[top-x:top])
				top--
			}

		case opLOOPCALL, opCALL:
			if callStackTop >= len(callStack) {
				return errors.New("truetype: hinting: call stack overflow")
			}
			top--
			f, ok := h.functions[h.stack[top]]
			if !ok {
				return errors.New("truetype: hinting: undefined function")
			}
			callStack[callStackTop] = callStackEntry{program, pc, 1}
			if opcode == opLOOPCALL {
				top--
				if h.stack[top] == 0 {
					break
				}
				callStack[callStackTop].loopCount = h.stack[top]
			}
			callStackTop++
			program, pc = f, 0
			continue

		case opFDEF:
			// Save all bytecode up until the next ENDF.
			startPC := pc + 1
		fdefloop:
			for {
				pc++
				if pc >= len(program) {
					return errors.New("truetype: hinting: unbalanced FDEF")
				}
				switch program[pc] {
				case opFDEF:
					return errors.New("truetype: hinting: nested FDEF")
				case opENDF:
					top--
					h.functions[h.stack[top]] = program[startPC : pc+1]
					break fdefloop
				default:
					var ok bool
					pc, ok = skipInstructionPayload(program, pc)
					if !ok {
						return errors.New("truetype: hinting: unbalanced FDEF")
					}
				}
			}

		case opENDF:
			if callStackTop == 0 {
				return errors.New("truetype: hinting: call stack underflow")
			}
			callStackTop--
			callStack[callStackTop].loopCount--
			if callStack[callStackTop].loopCount != 0 {
				callStackTop++
				pc = 0
				continue
			}
			program, pc = callStack[callStackTop].program, callStack[callStackTop].pc

		case opMDAP0, opMDAP1:
			top--
			i := h.stack[top]
			p := h.point(0, current, i)
			if p == nil {
				return errors.New("truetype: hinting: point out of range")
			}
			distance := f26dot6(0)
			if opcode == opMDAP1 {
				distance = dotProduct(f26dot6(p.X), f26dot6(p.Y), h.gs.pv)
				// TODO: metrics compensation.
				distance = h.round(distance) - distance
			}
			h.move(p, distance)
			h.gs.rp[0] = i
			h.gs.rp[1] = i

		case opIUP0, opIUP1:
			iupY, mask := opcode == opIUP0, uint32(flagTouchedX)
			if iupY {
				mask = flagTouchedY
			}
			prevEnd := 0
			for _, end := range h.ends {
				for i := prevEnd; i < end; i++ {
					for i < end && h.points[glyphZone][current][i].Flags&mask == 0 {
						i++
					}
					if i == end {
						break
					}
					firstTouched, curTouched := i, i
					i++
					for ; i < end; i++ {
						if h.points[glyphZone][current][i].Flags&mask != 0 {
							h.iupInterp(iupY, curTouched+1, i-1, curTouched, i)
							curTouched = i
						}
					}
					if curTouched == firstTouched {
						h.iupShift(iupY, prevEnd, end, curTouched)
					} else {
						h.iupInterp(iupY, curTouched+1, end-1, curTouched, firstTouched)
						if firstTouched > 0 {
							h.iupInterp(iupY, prevEnd, firstTouched-1, curTouched, firstTouched)
						}
					}
				}
				prevEnd = end
			}

		case opIP:
			if top < int(h.gs.loop) {
				return errors.New("truetype: hinting: stack underflow")
			}
			pointType := inFontUnits
			twilight := h.gs.zp[0] == 0 || h.gs.zp[1] == 0 || h.gs.zp[2] == 0
			if twilight {
				pointType = unhinted
			}
			p := h.point(1, pointType, h.gs.rp[2])
			oldP := h.point(0, pointType, h.gs.rp[1])
			oldRange := dotProduct(f26dot6(p.X-oldP.X), f26dot6(p.Y-oldP.Y), h.gs.dv)

			p = h.point(1, current, h.gs.rp[2])
			curP := h.point(0, current, h.gs.rp[1])
			curRange := dotProduct(f26dot6(p.X-curP.X), f26dot6(p.Y-curP.Y), h.gs.pv)

			for ; h.gs.loop != 0; h.gs.loop-- {
				top--
				i := h.stack[top]
				p = h.point(2, pointType, i)
				oldDist := dotProduct(f26dot6(p.X-oldP.X), f26dot6(p.Y-oldP.Y), h.gs.dv)
				p = h.point(2, current, i)
				curDist := dotProduct(f26dot6(p.X-curP.X), f26dot6(p.Y-curP.Y), h.gs.pv)
				newDist := f26dot6(0)
				if oldDist != 0 {
					if oldRange != 0 {
						newDist = f26dot6(
							(int64(oldDist)*int64(curRange) + int64(oldRange/2)) / int64(oldRange))
					} else {
						newDist = -oldDist
					}
				}
				h.move(p, newDist-curDist)
			}
			h.gs.loop = 1

		case opALIGNRP:
			if top < int(h.gs.loop) {
				return errors.New("truetype: hinting: stack underflow")
			}
			ref := h.point(0, current, h.gs.rp[0])
			if ref == nil {
				return errors.New("truetype: hinting: point out of range")
			}
			for ; h.gs.loop != 0; h.gs.loop-- {
				top--
				p := h.point(1, current, h.stack[top])
				if p == nil {
					return errors.New("truetype: hinting: point out of range")
				}
				h.move(p, -dotProduct(f26dot6(p.X-ref.X), f26dot6(p.Y-ref.Y), h.gs.pv))
			}
			h.gs.loop = 1

		case opRTDG:
			h.gs.roundPeriod = 1 << 5
			h.gs.roundPhase = 0
			h.gs.roundThreshold = 1 << 4

		case opMIAP0, opMIAP1:
			top -= 2
			i := h.stack[top]
			distance := h.cvt(h.stack[top+1])
			if h.gs.zp[0] == 0 {
				p := h.point(0, unhinted, i)
				q := h.point(0, current, i)
				p.X = int32((int64(distance) * int64(h.gs.fv[0])) >> 14)
				p.Y = int32((int64(distance) * int64(h.gs.fv[1])) >> 14)
				*q = *p
			}
			p := h.point(0, current, i)
			oldDist := dotProduct(f26dot6(p.X), f26dot6(p.Y), h.gs.pv)
			if opcode == opMIAP1 {
				if (distance - oldDist).abs() > h.gs.controlValueCutIn {
					distance = oldDist
				}
				// TODO: metrics compensation.
				distance = h.round(distance)
			}
			h.move(p, distance-oldDist)
			h.gs.rp[0] = i
			h.gs.rp[1] = i

		case opNPUSHB:
			opcode = 0
			goto push

		case opNPUSHW:
			opcode = 0x80
			goto push

		case opWS:
			top -= 2
			i := int(h.stack[top])
			if i < 0 || len(h.store) <= i {
				return errors.New("truetype: hinting: invalid data")
			}
			h.store[i] = h.stack[top+1]

		case opRS:
			i := int(h.stack[top-1])
			if i < 0 || len(h.store) <= i {
				return errors.New("truetype: hinting: invalid data")
			}
			h.stack[top-1] = h.store[i]

		case opMPPEM, opMPS:
			if top >= len(h.stack) {
				return errors.New("truetype: hinting: stack overflow")
			}
			// For MPS, point size should be irrelevant; we return the PPEM.
			h.stack[top] = h.scale >> 6
			top++

		case opFLIPON, opFLIPOFF:
			h.gs.autoFlip = opcode == opFLIPON

		case opDEBUG:
			// No-op.

		case opLT:
			top--
			h.stack[top-1] = bool2int32(h.stack[top-1] < h.stack[top])

		case opLTEQ:
			top--
			h.stack[top-1] = bool2int32(h.stack[top-1] <= h.stack[top])

		case opGT:
			top--
			h.stack[top-1] = bool2int32(h.stack[top-1] > h.stack[top])

		case opGTEQ:
			top--
			h.stack[top-1] = bool2int32(h.stack[top-1] >= h.stack[top])

		case opEQ:
			top--
			h.stack[top-1] = bool2int32(h.stack[top-1] == h.stack[top])

		case opNEQ:
			top--
			h.stack[top-1] = bool2int32(h.stack[top-1] != h.stack[top])

		case opODD, opEVEN:
			i := h.round(f26dot6(h.stack[top-1])) >> 6
			h.stack[top-1] = int32(i&1) ^ int32(opcode-opODD)

		case opIF:
			top--
			if h.stack[top] == 0 {
				opcode = 0
				goto ifelse
			}

		case opEIF:
			// No-op.

		case opAND:
			top--
			h.stack[top-1] = bool2int32(h.stack[top-1] != 0 && h.stack[top] != 0)

		case opOR:
			top--
			h.stack[top-1] = bool2int32(h.stack[top-1]|h.stack[top] != 0)

		case opNOT:
			h.stack[top-1] = bool2int32(h.stack[top-1] == 0)

		case opSDB:
			top--
			h.gs.deltaBase = h.stack[top]

		case opSDS:
			top--
			h.gs.deltaShift = h.stack[top]

		case opADD:
			top--
			h.stack[top-1] += h.stack[top]

		case opSUB:
			top--
			h.stack[top-1] -= h.stack[top]

		case opDIV:
			top--
			if h.stack[top] == 0 {
				return errors.New("truetype: hinting: division by zero")
			}
			h.stack[top-1] = int32(f26dot6(h.stack[top-1]).div(f26dot6(h.stack[top])))

		case opMUL:
			top--
			h.stack[top-1] = int32(f26dot6(h.stack[top-1]).mul(f26dot6(h.stack[top])))

		case opABS:
			if h.stack[top-1] < 0 {
				h.stack[top-1] = -h.stack[top-1]
			}

		case opNEG:
			h.stack[top-1] = -h.stack[top-1]

		case opFLOOR:
			h.stack[top-1] &^= 63

		case opCEILING:
			h.stack[top-1] += 63
			h.stack[top-1] &^= 63

		case opROUND00, opROUND01, opROUND10, opROUND11:
			// The four flavors of opROUND are equivalent. See the comment below on
			// opNROUND for the rationale.
			h.stack[top-1] = int32(h.round(f26dot6(h.stack[top-1])))

		case opNROUND00, opNROUND01, opNROUND10, opNROUND11:
			// No-op. The spec says to add one of four "compensations for the engine
			// characteristics", to cater for things like "different dot-size printers".
			// https://developer.apple.com/fonts/TTRefMan/RM02/Chap2.html#engine_compensation
			// This code does not implement engine compensation, as we don't expect to
			// be used to output on dot-matrix printers.

		case opSROUND, opS45ROUND:
			top--
			switch (h.stack[top] >> 6) & 0x03 {
			case 0:
				h.gs.roundPeriod = 1 << 5
			case 1, 3:
				h.gs.roundPeriod = 1 << 6
			case 2:
				h.gs.roundPeriod = 1 << 7
			}
			if opcode == opS45ROUND {
				// The spec says to multiply by √2, but the C Freetype code says 1/√2.
				// We go with 1/√2.
				h.gs.roundPeriod *= 46341
				h.gs.roundPeriod /= 65536
			}
			h.gs.roundPhase = h.gs.roundPeriod * f26dot6((h.stack[top]>>4)&0x03) / 4
			if x := h.stack[top] & 0x0f; x != 0 {
				h.gs.roundThreshold = h.gs.roundPeriod * f26dot6(x-4) / 8
			} else {
				h.gs.roundThreshold = h.gs.roundPeriod - 1
			}

		case opJROT:
			top -= 2
			if h.stack[top+1] != 0 {
				pc += int(h.stack[top])
				continue
			}

		case opJROF:
			top -= 2
			if h.stack[top+1] == 0 {
				pc += int(h.stack[top])
				continue
			}

		case opROFF:
			h.gs.roundPeriod = 0
			h.gs.roundPhase = 0
			h.gs.roundThreshold = 0

		case opRUTG:
			h.gs.roundPeriod = 1 << 6
			h.gs.roundPhase = 0
			h.gs.roundThreshold = 1<<6 - 1

		case opRDTG:
			h.gs.roundPeriod = 1 << 6
			h.gs.roundPhase = 0
			h.gs.roundThreshold = 0

		case opSANGW, opAA:
			// These ops are "anachronistic" and no longer used.
			top--

		case opSCANCTRL:
			// We do not support dropout control, as we always rasterize grayscale glyphs.
			top--

		case opGETINFO:
			res := int32(0)
			if h.stack[top-1]&(1<<0) != 0 {
				// Set the engine version. We hard-code this to 35, the same as
				// the C freetype code, which says that "Version~35 corresponds
				// to MS rasterizer v.1.7 as used e.g. in Windows~98".
				res |= 35
			}
			if h.stack[top-1]&(1<<5) != 0 {
				// Set that we support grayscale.
				res |= 1 << 12
			}
			// We set no other bits, as we do not support rotated or stretched glyphs.
			h.stack[top-1] = res

		case opIDEF:
			// IDEF is for ancient versions of the bytecode interpreter, and is no longer used.
			return errors.New("truetype: hinting: unsupported IDEF instruction")

		case opROLL:
			h.stack[top-1], h.stack[top-3], h.stack[top-2] =
				h.stack[top-3], h.stack[top-2], h.stack[top-1]

		case opMAX:
			top--
			if h.stack[top-1] < h.stack[top] {
				h.stack[top-1] = h.stack[top]
			}

		case opMIN:
			top--
			if h.stack[top-1] > h.stack[top] {
				h.stack[top-1] = h.stack[top]
			}

		case opSCANTYPE:
			// We do not support dropout control, as we always rasterize grayscale glyphs.
			top--

		case opPUSHB000, opPUSHB001, opPUSHB010, opPUSHB011,
			opPUSHB100, opPUSHB101, opPUSHB110, opPUSHB111:

			opcode -= opPUSHB000 - 1
			goto push

		case opPUSHW000, opPUSHW001, opPUSHW010, opPUSHW011,
			opPUSHW100, opPUSHW101, opPUSHW110, opPUSHW111:

			opcode -= opPUSHW000 - 1
			opcode += 0x80
			goto push

		case opMDRP00000, opMDRP00001, opMDRP00010, opMDRP00011,
			opMDRP00100, opMDRP00101, opMDRP00110, opMDRP00111,
			opMDRP01000, opMDRP01001, opMDRP01010, opMDRP01011,
			opMDRP01100, opMDRP01101, opMDRP01110, opMDRP01111,
			opMDRP10000, opMDRP10001, opMDRP10010, opMDRP10011,
			opMDRP10100, opMDRP10101, opMDRP10110, opMDRP10111,
			opMDRP11000, opMDRP11001, opMDRP11010, opMDRP11011,
			opMDRP11100, opMDRP11101, opMDRP11110, opMDRP11111:

			top--
			i := h.stack[top]
			ref := h.point(0, current, h.gs.rp[0])
			p := h.point(1, current, i)
			if ref == nil || p == nil {
				return errors.New("truetype: hinting: point out of range")
			}

			oldDist := f26dot6(0)
			if h.gs.zp[0] == 0 || h.gs.zp[1] == 0 {
				p0 := h.point(1, unhinted, i)
				p1 := h.point(0, unhinted, h.gs.rp[0])
				oldDist = dotProduct(f26dot6(p0.X-p1.X), f26dot6(p0.Y-p1.Y), h.gs.dv)
			} else {
				p0 := h.point(1, inFontUnits, i)
				p1 := h.point(0, inFontUnits, h.gs.rp[0])
				oldDist = dotProduct(f26dot6(p0.X-p1.X), f26dot6(p0.Y-p1.Y), h.gs.dv)
				oldDist = f26dot6(h.font.scale(h.scale * int32(oldDist)))
			}

			// Single-width cut-in test.
			if x := (oldDist - h.gs.singleWidth).abs(); x < h.gs.singleWidthCutIn {
				if oldDist >= 0 {
					oldDist = +h.gs.singleWidth
				} else {
					oldDist = -h.gs.singleWidth
				}
			}

			// Rounding bit.
			// TODO: metrics compensation.
			distance := oldDist
			if opcode&0x04 != 0 {
				distance = h.round(oldDist)
			}

			// Minimum distance bit.
			if opcode&0x08 != 0 {
				if oldDist >= 0 {
					if distance < h.gs.minDist {
						distance = h.gs.minDist
					}
				} else {
					if distance > -h.gs.minDist {
						distance = -h.gs.minDist
					}
				}
			}

			// Set-RP0 bit.
			if opcode&0x10 != 0 {
				h.gs.rp[0] = i
			}
			h.gs.rp[1] = h.gs.rp[0]
			h.gs.rp[2] = i

			// Move the point.
			oldDist = dotProduct(f26dot6(p.X-ref.X), f26dot6(p.Y-ref.Y), h.gs.pv)
			h.move(p, distance-oldDist)

		case opMIRP00000, opMIRP00001, opMIRP00010, opMIRP00011,
			opMIRP00100, opMIRP00101, opMIRP00110, opMIRP00111,
			opMIRP01000, opMIRP01001, opMIRP01010, opMIRP01011,
			opMIRP01100, opMIRP01101, opMIRP01110, opMIRP01111,
			opMIRP10000, opMIRP10001, opMIRP10010, opMIRP10011,
			opMIRP10100, opMIRP10101, opMIRP10110, opMIRP10111,
			opMIRP11000, opMIRP11001, opMIRP11010, opMIRP11011,
			opMIRP11100, opMIRP11101, opMIRP11110, opMIRP11111:

			top -= 2
			i := h.stack[top]
			cvtDist := h.cvt(h.stack[top+1])
			if (cvtDist - h.gs.singleWidth).abs() < h.gs.singleWidthCutIn {
				if cvtDist >= 0 {
					cvtDist = +h.gs.singleWidth
				} else {
					cvtDist = -h.gs.singleWidth
				}
			}

			if h.gs.zp[1] == 0 {
				// TODO: implement once we have a .ttf file that triggers
				// this, so that we can step through C's freetype.
				return errors.New("truetype: hinting: unimplemented twilight point adjustment")
			}

			ref := h.point(0, unhinted, h.gs.rp[0])
			p := h.point(1, unhinted, i)
			if ref == nil || p == nil {
				return errors.New("truetype: hinting: point out of range")
			}
			oldDist := dotProduct(f26dot6(p.X-ref.X), f26dot6(p.Y-ref.Y), h.gs.dv)

			ref = h.point(0, current, h.gs.rp[0])
			p = h.point(1, current, i)
			if ref == nil || p == nil {
				return errors.New("truetype: hinting: point out of range")
			}
			curDist := dotProduct(f26dot6(p.X-ref.X), f26dot6(p.Y-ref.Y), h.gs.dv)

			if h.gs.autoFlip && oldDist^cvtDist < 0 {
				cvtDist = -cvtDist
			}

			// Rounding bit.
			// TODO: metrics compensation.
			distance := oldDist
			if opcode&0x04 != 0 {
				distance = h.round(oldDist)
			}

			// Minimum distance bit.
			if opcode&0x08 != 0 {
				if oldDist >= 0 {
					if distance < h.gs.minDist {
						distance = h.gs.minDist
					}
				} else {
					if distance > -h.gs.minDist {
						distance = -h.gs.minDist
					}
				}
			}

			// Set-RP0 bit.
			if opcode&0x10 != 0 {
				h.gs.rp[0] = i
			}
			h.gs.rp[1] = h.gs.rp[0]
			h.gs.rp[2] = i

			// Move the point.
			h.move(p, distance-curDist)

		default:
			return errors.New("truetype: hinting: unrecognized instruction")
		}
		pc++
		continue

	ifelse:
		// Skip past bytecode until the next ELSE (if opcode == 0) or the
		// next EIF (for all opcodes). Opcode == 0 means that we have come
		// from an IF. Opcode == 1 means that we have come from an ELSE.
		{
		ifelseloop:
			for depth := 0; ; {
				pc++
				if pc >= len(program) {
					return errors.New("truetype: hinting: unbalanced IF or ELSE")
				}
				switch program[pc] {
				case opIF:
					depth++
				case opELSE:
					if depth == 0 && opcode == 0 {
						break ifelseloop
					}
				case opEIF:
					depth--
					if depth < 0 {
						break ifelseloop
					}
				default:
					var ok bool
					pc, ok = skipInstructionPayload(program, pc)
					if !ok {
						return errors.New("truetype: hinting: unbalanced IF or ELSE")
					}
				}
			}
			pc++
			continue
		}

	push:
		// Push n elements from the program to the stack, where n is the low 7 bits of
		// opcode. If the low 7 bits are zero, then n is the next byte from the program.
		// The high bit being 0 means that the elements are zero-extended bytes.
		// The high bit being 1 means that the elements are sign-extended words.
		{
			width := 1
			if opcode&0x80 != 0 {
				opcode &^= 0x80
				width = 2
			}
			if opcode == 0 {
				pc++
				if pc >= len(program) {
					return errors.New("truetype: hinting: insufficient data")
				}
				opcode = program[pc]
			}
			pc++
			if top+int(opcode) > len(h.stack) {
				return errors.New("truetype: hinting: stack overflow")
			}
			if pc+width*int(opcode) > len(program) {
				return errors.New("truetype: hinting: insufficient data")
			}
			for ; opcode > 0; opcode-- {
				if width == 1 {
					h.stack[top] = int32(program[pc])
				} else {
					h.stack[top] = int32(int8(program[pc]))<<8 | int32(program[pc+1])
				}
				top++
				pc += width
			}
			continue
		}
	}
	return nil
}

// cvt returns the scaled value from the font's Control Value Table.
func (h *Hinter) cvt(i int32) f26dot6 {
	i *= 2
	if i < 0 || len(h.font.cvt) < int(i) {
		return 0
	}
	cv := uint16(h.font.cvt[i])<<8 | uint16(h.font.cvt[i+1])
	return f26dot6(h.font.scale(h.scale * int32(int16(cv))))
}

func (h *Hinter) point(zonePointer uint32, pt pointType, i int32) *Point {
	points := h.points[h.gs.zp[zonePointer]][pt]
	if i < 0 || len(points) <= int(i) {
		return nil
	}
	return &points[i]
}

func (h *Hinter) move(p *Point, distance f26dot6) {
	if h.gs.fv[0] == 0 {
		p.Y += int32(distance)
		p.Flags |= flagTouchedY
		return
	}
	if h.gs.fv[1] == 0 {
		p.X += int32(distance)
		p.Flags |= flagTouchedX
		return
	}
	fvx := int64(h.gs.fv[0])
	fvy := int64(h.gs.fv[1])
	pvx := int64(h.gs.pv[0])
	pvy := int64(h.gs.pv[1])
	fvDotPv := (fvx*pvx + fvy*pvy) >> 14
	p.X += int32(int64(distance) * fvx / fvDotPv)
	p.Y += int32(int64(distance) * fvy / fvDotPv)
	p.Flags |= flagTouchedX | flagTouchedY
}

func (h *Hinter) iupInterp(interpY bool, p1, p2, ref1, ref2 int) {
	if p1 > p2 {
		return
	}
	if ref1 >= len(h.points[glyphZone][current]) ||
		ref2 >= len(h.points[glyphZone][current]) {
		return
	}

	var ifu1, ifu2 int32
	if interpY {
		ifu1 = h.points[glyphZone][inFontUnits][ref1].Y
		ifu2 = h.points[glyphZone][inFontUnits][ref2].Y
	} else {
		ifu1 = h.points[glyphZone][inFontUnits][ref1].X
		ifu2 = h.points[glyphZone][inFontUnits][ref2].X
	}
	if ifu1 > ifu2 {
		ifu1, ifu2 = ifu2, ifu1
		ref1, ref2 = ref2, ref1
	}

	var unh1, unh2, delta1, delta2 int32
	if interpY {
		unh1 = h.points[glyphZone][unhinted][ref1].Y
		unh2 = h.points[glyphZone][unhinted][ref2].Y
		delta1 = h.points[glyphZone][current][ref1].Y - unh1
		delta2 = h.points[glyphZone][current][ref2].Y - unh2
	} else {
		unh1 = h.points[glyphZone][unhinted][ref1].X
		unh2 = h.points[glyphZone][unhinted][ref2].X
		delta1 = h.points[glyphZone][current][ref1].X - unh1
		delta2 = h.points[glyphZone][current][ref2].X - unh2
	}

	var xy, ifuXY int32
	if ifu1 == ifu2 {
		for i := p1; i <= p2; i++ {
			if interpY {
				xy = h.points[glyphZone][unhinted][i].Y
			} else {
				xy = h.points[glyphZone][unhinted][i].X
			}

			if xy <= unh1 {
				xy += delta1
			} else {
				xy += delta2
			}

			if interpY {
				h.points[glyphZone][current][i].Y = xy
			} else {
				h.points[glyphZone][current][i].X = xy
			}
		}
		return
	}

	scale, scaleOK := int64(0), false
	for i := p1; i <= p2; i++ {
		if interpY {
			xy = h.points[glyphZone][unhinted][i].Y
			ifuXY = h.points[glyphZone][inFontUnits][i].Y
		} else {
			xy = h.points[glyphZone][unhinted][i].X
			ifuXY = h.points[glyphZone][inFontUnits][i].X
		}

		if xy <= unh1 {
			xy += delta1
		} else if xy >= unh2 {
			xy += delta2
		} else {
			if !scaleOK {
				scaleOK = true
				numer := int64(unh2+delta2-unh1-delta1) * 0x10000
				denom := int64(ifu2 - ifu1)
				if numer >= 0 {
					numer += denom / 2
				} else {
					numer -= denom / 2
				}
				scale = numer / denom
			}
			numer := int64(ifuXY-ifu1) * scale
			if numer >= 0 {
				numer += 0x8000
			} else {
				numer -= 0x8000
			}
			xy = unh1 + delta1 + int32(numer/0x10000)
		}

		if interpY {
			h.points[glyphZone][current][i].Y = xy
		} else {
			h.points[glyphZone][current][i].X = xy
		}
	}
}

func (h *Hinter) iupShift(interpY bool, p1, p2, p int) {
	var delta int32
	if interpY {
		delta = h.points[glyphZone][current][p].Y - h.points[glyphZone][unhinted][p].Y
	} else {
		delta = h.points[glyphZone][current][p].X - h.points[glyphZone][unhinted][p].X
	}
	if delta == 0 {
		return
	}
	for i := p1; i < p2; i++ {
		if i == p {
			continue
		}
		if interpY {
			h.points[glyphZone][current][i].Y += delta
		} else {
			h.points[glyphZone][current][i].X += delta
		}
	}
}

// skipInstructionPayload increments pc by the extra data that follows a
// variable length PUSHB or PUSHW instruction.
func skipInstructionPayload(program []byte, pc int) (newPC int, ok bool) {
	switch program[pc] {
	case opNPUSHB:
		pc++
		if pc >= len(program) {
			return 0, false
		}
		pc += int(program[pc])
	case opNPUSHW:
		pc++
		if pc >= len(program) {
			return 0, false
		}
		pc += 2 * int(program[pc])
	case opPUSHB000, opPUSHB001, opPUSHB010, opPUSHB011,
		opPUSHB100, opPUSHB101, opPUSHB110, opPUSHB111:
		pc += int(program[pc] - (opPUSHB000 - 1))
	case opPUSHW000, opPUSHW001, opPUSHW010, opPUSHW011,
		opPUSHW100, opPUSHW101, opPUSHW110, opPUSHW111:
		pc += 2 * int(program[pc]-(opPUSHW000-1))
	}
	return pc, true
}

// f2dot14 is a 2.14 fixed point number.
type f2dot14 int16

// f26dot6 is a 26.6 fixed point number.
type f26dot6 int32

// abs returns abs(x) in 26.6 fixed point arithmetic.
func (x f26dot6) abs() f26dot6 {
	if x < 0 {
		return -x
	}
	return x
}

// div returns x/y in 26.6 fixed point arithmetic.
func (x f26dot6) div(y f26dot6) f26dot6 {
	return f26dot6((int64(x) << 6) / int64(y))
}

// mul returns x*y in 26.6 fixed point arithmetic.
func (x f26dot6) mul(y f26dot6) f26dot6 {
	return f26dot6(int64(x) * int64(y) >> 6)
}

func dotProduct(x, y f26dot6, q [2]f2dot14) f26dot6 {
	px := int64(x)
	py := int64(y)
	qx := int64(q[0])
	qy := int64(q[1])
	return f26dot6((px*qx + py*qy) >> 14)
}

// round rounds the given number. The rounding algorithm is described at
// https://developer.apple.com/fonts/TTRefMan/RM02/Chap2.html#rounding
func (h *Hinter) round(x f26dot6) f26dot6 {
	if h.gs.roundPeriod == 0 {
		// Rounding is off.
		return x
	}
	if x >= 0 {
		ret := (x - h.gs.roundPhase + h.gs.roundThreshold) & -h.gs.roundPeriod
		if x != 0 && ret < 0 {
			ret = 0
		}
		return ret + h.gs.roundPhase
	}
	ret := -((-x - h.gs.roundPhase + h.gs.roundThreshold) & -h.gs.roundPeriod)
	if ret > 0 {
		ret = 0
	}
	return ret - h.gs.roundPhase
}

func bool2int32(b bool) int32 {
	if b {
		return 1
	}
	return 0
}
