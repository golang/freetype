// Copyright 2012 The Freetype-Go Authors. All rights reserved.
// Use of this source code is governed by your choice of either the
// FreeType License or the GNU General Public License version 2 (or
// any later version), both of which can be found in the LICENSE file.

package truetype

import (
	"reflect"
	"strings"
	"testing"
)

func TestBytecode(t *testing.T) {
	testCases := []struct {
		desc   string
		prog   []byte
		want   []int32
		errStr string
	}{
		{
			"underflow",
			[]byte{
				opDUP,
			},
			nil,
			"underflow",
		},
		{
			"stack ops",
			[]byte{
				opPUSHB010, // [10, 20, 30]
				10,
				20,
				30,
				opCLEAR,    // []
				opPUSHB010, // [40, 50, 60]
				40,
				50,
				60,
				opSWAP,     // [40, 60, 50]
				opDUP,      // [40, 60, 50, 50]
				opDUP,      // [40, 60, 50, 50, 50]
				opPOP,      // [40, 60, 50, 50]
				opDEPTH,    // [40, 60, 50, 50, 4]
				opCINDEX,   // [40, 60, 50, 50, 40]
				opPUSHB000, // [40, 60, 50, 50, 40, 4]
				4,
				opMINDEX, // [40, 50, 50, 40, 60]
			},
			[]int32{40, 50, 50, 40, 60},
			"",
		},
		{
			"push ops",
			[]byte{
				opPUSHB000, // [255]
				255,
				opPUSHW001, // [255, -2, 253]
				255,
				254,
				0,
				253,
				opNPUSHB, // [1, -2, 253, 1, 2]
				2,
				1,
				2,
				opNPUSHW, // [1, -2, 253, 1, 2, 0x0405, 0x0607, 0x0809]
				3,
				4,
				5,
				6,
				7,
				8,
				9,
			},
			[]int32{255, -2, 253, 1, 2, 0x0405, 0x0607, 0x0809},
			"",
		},
		{
			"comparison ops",
			[]byte{
				opPUSHB001, // [10, 20]
				10,
				20,
				opLT,       // [1]
				opPUSHB001, // [1, 10, 20]
				10,
				20,
				opLTEQ,     // [1, 1]
				opPUSHB001, // [1, 1, 10, 20]
				10,
				20,
				opGT,       // [1, 1, 0]
				opPUSHB001, // [1, 1, 0, 10, 20]
				10,
				20,
				opGTEQ, // [1, 1, 0, 0]
				opEQ,   // [1, 1, 1]
				opNEQ,  // [1, 0]
			},
			[]int32{1, 0},
			"",
		},
		{
			"logical ops",
			[]byte{
				opPUSHB010, // [0, 10, 20]
				0,
				10,
				20,
				opAND, // [0, 1]
				opOR,  // [1]
				opNOT, // [0]
			},
			[]int32{0},
			"",
		},
		{
			"arithmetic ops",
			// Calculate abs((-(1 - (2*3)))/2 + 1/64).
			// The answer is 5/2 + 1/64 in ideal numbers, or 161 in 26.6 fixed point math.
			[]byte{
				opPUSHB010, // [64, 128, 192]
				1 << 6,
				2 << 6,
				3 << 6,
				opMUL,      // [64, 384]
				opSUB,      // [-320]
				opNEG,      // [320]
				opPUSHB000, // [320, 128]
				2 << 6,
				opDIV,      // [160]
				opPUSHB000, // [160, 1]
				1,
				opADD, // [161]
				opABS, // [161]
			},
			[]int32{161},
			"",
		},
		{
			"floor, ceiling",
			[]byte{
				opPUSHB000, // [96]
				96,
				opFLOOR,    // [64]
				opPUSHB000, // [64, 96]
				96,
				opCEILING, // [64, 128]
			},
			[]int32{64, 128},
			"",
		},
	}

	for _, tc := range testCases {
		h := &hinter{}
		err, errStr := h.run(tc.prog), ""
		if err != nil {
			errStr = err.Error()
		}
		if tc.errStr != "" {
			if errStr == "" {
				t.Errorf("%s: got no error, want %q", tc.desc, tc.errStr)
			} else if !strings.Contains(errStr, tc.errStr) {
				t.Errorf("%s: got error %q, want one containing %q", tc.desc, errStr, tc.errStr)
			}
			continue
		}
		if errStr != "" {
			t.Errorf("%s: got error %q, want none", tc.desc, errStr)
			continue
		}
		got := h.stack[:len(tc.want)]
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("got %v, want %v", got, tc.want)
			continue
		}
	}
}
