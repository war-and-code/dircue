package cli

import (
	"strconv"
	"unicode"
	"unicode/utf8"
)

// terminalValue leaves ordinary names unchanged and escapes terminal controls.
func terminalValue(value string) string {
	unsafe := !utf8.ValidString(value)
	for _, r := range value {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			unsafe = true
			break
		}
	}
	if !unsafe {
		return value
	}
	quoted := strconv.QuoteToASCII(value)
	return quoted[1 : len(quoted)-1]
}
