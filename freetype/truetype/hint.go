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

type hinter struct {
	stack [800]int32
	// TODO: add more state, as per https://developer.apple.com/fonts/TTRefMan/RM04/Chap4.html
}

func (h *hinter) run(program []byte) error {
	var (
		steps, pc, top int
		opcode         uint8
	)
	for int(pc) < len(program) {
		steps++
		if steps == 100000 {
			return errors.New("truetype: hinting: too many instructions")
		}
		opcode = program[pc]
		if popCount[opcode] == q {
			return errors.New("truetype: hinting: unimplemented instruction")
		}
		if top < int(popCount[opcode]) {
			return errors.New("truetype: hinting: stack underflow")
		}
		switch opcode {

		case opDUP:
			if int(top) >= len(h.stack) {
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
			if int(top) >= len(h.stack) {
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

		case opNPUSHB:
			opcode = 0
			goto push

		case opNPUSHW:
			opcode = 0x80
			goto push

		case opLT:
			h.stack[top-2] = bool2int32(h.stack[top-2] < h.stack[top-1])
			top--

		case opLTEQ:
			h.stack[top-2] = bool2int32(h.stack[top-2] <= h.stack[top-1])
			top--

		case opGT:
			h.stack[top-2] = bool2int32(h.stack[top-2] > h.stack[top-1])
			top--

		case opGTEQ:
			h.stack[top-2] = bool2int32(h.stack[top-2] >= h.stack[top-1])
			top--

		case opEQ:
			h.stack[top-2] = bool2int32(h.stack[top-2] == h.stack[top-1])
			top--

		case opNEQ:
			h.stack[top-2] = bool2int32(h.stack[top-2] != h.stack[top-1])
			top--

		case opAND:
			h.stack[top-2] = bool2int32(h.stack[top-2] != 0 && h.stack[top-1] != 0)
			top--

		case opOR:
			h.stack[top-2] = bool2int32(h.stack[top-2]|h.stack[top-1] != 0)
			top--

		case opNOT:
			h.stack[top-1] = bool2int32(h.stack[top-1] == 0)

		case opADD:
			h.stack[top-2] += h.stack[top-1]
			top--

		case opSUB:
			h.stack[top-2] -= h.stack[top-1]
			top--

		case opDIV:
			if h.stack[top-1] == 0 {
				return errors.New("truetype: hinting: division by zero")
			}
			h.stack[top-2] = int32((int64(h.stack[top-2]) << 6) / int64(h.stack[top-1]))
			top--

		case opMUL:
			h.stack[top-2] = int32((int64(h.stack[top-2]) * int64(h.stack[top-1])) >> 6)
			top--

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

		case opPUSHB000, opPUSHB001, opPUSHB010, opPUSHB011, opPUSHB100, opPUSHB101, opPUSHB110, opPUSHB111:
			opcode -= opPUSHB000 - 1
			goto push

		case opPUSHW000, opPUSHW001, opPUSHW010, opPUSHW011, opPUSHW100, opPUSHW101, opPUSHW110, opPUSHW111:
			opcode -= opPUSHW000 - 1
			opcode += 0x80
			goto push

		default:
			return errors.New("truetype: hinting: unrecognized instruction")
		}
		pc++
		continue

	push:
		// push n elements from the program to the stack, where n is the low 7 bits of
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
				if int(pc) >= len(program) {
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

func bool2int32(b bool) int32 {
	if b {
		return 1
	}
	return 0
}
