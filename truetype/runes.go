package truetype

import (
	"bytes"
	"io/ioutil"

	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

func decodeUTF16(b []byte) ([]byte, error) {
	r := bytes.NewReader(b)
	enc := unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM)
	r2 := transform.NewReader(r, enc.NewDecoder())
	return ioutil.ReadAll(r2)
}
