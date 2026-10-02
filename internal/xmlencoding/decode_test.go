package xmlencoding

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestDecodeSupportedXMLCharsetsAndRejectsContradictoryBOM(t *testing.T) {
	latin1, err := Decode([]byte(`<?xml version="1.0" encoding="ISO-8859-1"?><x>caf` + "\xe9" + `</x>`))
	if err != nil || !strings.Contains(string(latin1), "café") || !strings.Contains(string(latin1), `encoding="UTF-8"`) {
		t.Fatalf("Latin-1 was not exactly transcoded: %q err=%v", latin1, err)
	}
	latin1Alias, err := Decode([]byte(`<?xml version="1.0" encoding="ISO8859-1"?><x>caf` + "\xe9" + `</x>`))
	if err != nil || !strings.Contains(string(latin1Alias), "café") || !strings.Contains(string(latin1Alias), `encoding="UTF-8"`) {
		t.Fatalf("valid ISO8859-1 alias was not exactly transcoded: %q err=%v", latin1Alias, err)
	}
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		text := `<?xml version="1.0" encoding="utf-16"?><x>ok</x>`
		units := []uint16{}
		for _, r := range text {
			units = append(units, uint16(r))
		}
		body := make([]byte, len(units)*2)
		for i, unit := range units {
			order.PutUint16(body[i*2:], unit)
		}
		bom := []byte{0xff, 0xfe}
		if order == binary.BigEndian {
			bom = []byte{0xfe, 0xff}
		}
		decoded, err := Decode(append(bom, body...))
		if err != nil || !strings.Contains(string(decoded), "<x>ok</x>") {
			t.Fatalf("UTF-16 decode failed: %q err=%v", decoded, err)
		}
	}
	for _, body := range [][]byte{
		append([]byte{0xef, 0xbb, 0xbf}, []byte(`<?xml version="1.0" encoding="ISO-8859-1"?><x/>`)...),
		[]byte(`<?xml version="1.0" encoding="unsupported"?><x/>`),
		[]byte(`<?xml version="1.0" encoding="ISO_8859-1"?><x/>`),
		[]byte(`<?xml version="1.0" encoding="US-ASCII"?><x>caf` + "\xe9" + `</x>`),
	} {
		if got, err := Decode(body); err == nil {
			t.Errorf("contradictory or unsupported XML decoded as %q", got)
		}
	}
	utf8bom := append([]byte{0xef, 0xbb, 0xbf}, []byte(`<?xml version="1.0" encoding="UTF-8"?><x/>`)...)
	if got, err := Decode(utf8bom); err != nil || !strings.Contains(string(got), `<x/>`) {
		t.Fatalf("valid UTF-8 BOM rejected: %q err=%v", got, err)
	}
}
