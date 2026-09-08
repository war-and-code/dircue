//go:build !oniguruma
// +build !oniguruma

package regex

import (
	"regexp"
)

const Name = RE2

type EnryRegexp = *regexp.Regexp

func MustCompile(str string) EnryRegexp {
	return regexp.MustCompile(str)
}

// MustCompileMultiline mimics Ruby defaults for regexp, where ^$ matches begin/end of line.
// I.e. it converts Ruby regexp syntaxt to RE2 equivalent
func MustCompileMultiline(s string) EnryRegexp {
	const multilineModeFlag = "(?m)"
	return regexp.MustCompile(multilineModeFlag + s)
}

// MustCompileRuby used for expressions with syntax not supported by RE2.
// Now it's confusing as we use the result as [data/rule.Matcher] and
//
//	(*Matcher)(nil) != nil
//
// What is a better way for an expression to indicate unsupported syntax?
// e.g. add .IsValidSyntax() to both, Matcher interface and EnryRegexp implementations?
func MustCompileRuby(s string) EnryRegexp {
	// This pinned Linguist grammar uses a named subroutine to repeat a
	// version token. Inlining it and removing possessive repetition is exact:
	// digits/dots cannot consume the following semicolon or closing bracket.
	if s == rubyAdblockFilterList {
		return reAdblockFilterList
	}
	return nil
}

const rubyAdblockFilterList = `(?x)\A
\[
(?<version>
  (?:
    [Aa]d[Bb]lock
    (?:[ \t][Pp]lus)?
    |
    u[Bb]lock
    (?:[ \t][Oo]rigin)?
    |
    [Aa]d[Gg]uard
  )
  (?:[ \t] \d+(?:\.\d+)*+)?
)
(?:
  [ \t]?;[ \t]?
  \g<version>
)*+
\]`

var reAdblockFilterList = regexp.MustCompile(`\A\[(?:[Aa]d[Bb]lock(?:[ \t][Pp]lus)?|u[Bb]lock(?:[ \t][Oo]rigin)?|[Aa]d[Gg]uard)(?:[ \t]\d+(?:\.\d+)*)?(?:[ \t]?;[ \t]?(?:[Aa]d[Bb]lock(?:[ \t][Pp]lus)?|u[Bb]lock(?:[ \t][Oo]rigin)?|[Aa]d[Gg]uard)(?:[ \t]\d+(?:\.\d+)*)?)*\]`)

func QuoteMeta(s string) string {
	return regexp.QuoteMeta(s)
}
