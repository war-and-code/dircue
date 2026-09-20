package jsontext

import "testing"

func TestUnicodeEscapes(t *testing.T) {
	for _, s := range []string{`{"x":"\ud800"}`, `{"x":"\udfff"}`, `{"x":"\ud800x"}`, `{"x":"\ud800\u0041"}`, string([]byte{'"', 0xff, '"'})} {
		if ValidUnicode([]byte(s)) {
			t.Errorf("accepted %q", s)
		}
	}
	for _, s := range []string{`{"x":"\ud83d\ude80"}`, `{"x":"\\ud800"}`, `{"x":"\"quoted\""}`, `{"x":"plain 🚀"}`} {
		if !ValidUnicode([]byte(s)) {
			t.Errorf("rejected %q", s)
		}
	}
}
