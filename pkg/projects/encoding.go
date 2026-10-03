package projects

import (
	"bytes"
	"encoding/binary"
	"errors"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/war-and-code/dircue/internal/xmlencoding"
)

// decodeBOMText accepts only the encodings that can be identified without
// guessing. Callers apply their normal byte limit before decoding.
func decodeBOMText(content []byte) ([]byte, error) {
	if bytes.HasPrefix(content, []byte{0xef, 0xbb, 0xbf}) {
		return content[3:], nil
	}
	var order binary.ByteOrder
	switch {
	case bytes.HasPrefix(content, []byte{0xff, 0xfe}):
		order, content = binary.LittleEndian, content[2:]
	case bytes.HasPrefix(content, []byte{0xfe, 0xff}):
		order, content = binary.BigEndian, content[2:]
	default:
		return content, nil
	}
	if len(content)%2 != 0 {
		return nil, errors.New("truncated UTF-16 input")
	}
	units := make([]uint16, len(content)/2)
	for i := range units {
		units[i] = order.Uint16(content[i*2:])
	}
	for i := 0; i < len(units); i++ {
		if units[i] >= 0xd800 && units[i] <= 0xdbff {
			if i+1 >= len(units) || units[i+1] < 0xdc00 || units[i+1] > 0xdfff {
				return nil, errors.New("invalid UTF-16 input")
			}
			i++
		} else if units[i] >= 0xdc00 && units[i] <= 0xdfff {
			return nil, errors.New("invalid UTF-16 input")
		}
	}
	runes := utf16.Decode(units)
	out := make([]byte, 0, len(runes)*2)
	for _, r := range runes {
		out = utf8.AppendRune(out, r)
	}
	return out, nil
}

func decodeBOMXML(content []byte) ([]byte, error) {
	return xmlencoding.Decode(content)
}
