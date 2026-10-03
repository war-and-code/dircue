// Package xmlencoding decodes the bounded XML encodings accepted by dircue's
// passive manifest readers. It does not resolve external entities or fetch data.
package xmlencoding

import (
	"bytes"
	"encoding/binary"
	"errors"
	"regexp"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

var declaration = regexp.MustCompile(`(?i)^\s*<\?xml\s+[^?]{0,512}?encoding\s*=\s*["']([^"']+)["']`)

// Decode returns UTF-8 XML for UTF-8, BOM-marked UTF-16, ISO-8859-1 (including
// the common XML-name alias ISO8859-1) and US-ASCII inputs. Single-byte
// declarations are transcoded exactly and the declaration is rewritten so
// encoding/xml sees the resulting UTF-8 bytes.
func Decode(content []byte) ([]byte, error) {
	utf16BOM := bytes.HasPrefix(content, []byte{0xff, 0xfe}) || bytes.HasPrefix(content, []byte{0xfe, 0xff})
	utf8BOM := bytes.HasPrefix(content, []byte{0xef, 0xbb, 0xbf})
	decoded, err := decodeTextBOM(content)
	if err != nil {
		return nil, err
	}
	match := declaration.FindSubmatchIndex(decoded)
	if match == nil {
		return decoded, nil
	}
	encoding := strings.ToLower(strings.TrimSpace(string(decoded[match[2]:match[3]])))
	if (encoding == "utf-16") != utf16BOM {
		return nil, errors.New("XML declaration and byte-order mark disagree")
	}
	if utf8BOM && encoding != "utf-8" {
		return nil, errors.New("XML declaration and byte-order mark disagree")
	}
	if encoding == "utf-8" || encoding == "utf-16" {
		return decoded, nil
	}
	latin1 := encoding == "iso-8859-1" || encoding == "iso8859-1" || encoding == "latin1"
	ascii := encoding == "us-ascii" || encoding == "ascii"
	if utf16BOM || (!latin1 && !ascii) {
		return nil, errors.New("unsupported XML encoding")
	}
	converted := make([]byte, 0, len(decoded)+16)
	for _, value := range decoded {
		if ascii && value >= 0x80 {
			return nil, errors.New("non-ASCII byte in US-ASCII XML")
		}
		converted = utf8.AppendRune(converted, rune(value))
	}
	match = declaration.FindSubmatchIndex(converted)
	if match != nil {
		converted = append(append(append([]byte(nil), converted[:match[2]]...), "UTF-8"...), converted[match[3]:]...)
	}
	return converted, nil
}

func decodeTextBOM(content []byte) ([]byte, error) {
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
