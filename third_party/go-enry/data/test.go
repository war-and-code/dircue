package data

import "github.com/war-and-code/dircue/third_party/go-enry/regex"

// TestMatchers is hand made collection of regexp used by the function `enry.IsTest`
// to identify test files in different languages.
var TestMatchers = []regex.EnryRegexp{
	regex.MustCompile(`(^|/)tests/.*Test\.php$`),
	regex.MustCompile(`(^|/)test/.*Test(s?)\.java$`),
	regex.MustCompile(`(^|/)test(/|/.*/)Test.*\.java$`),
	regex.MustCompile(`(^|/)test/.*(Test(s?)|Spec(s?))\.scala$`),
	regex.MustCompile(`(^|/)test_.*\.py$`),
	regex.MustCompile(`(^|/).*_test\.go$`),
	regex.MustCompile(`(^|/).*_(test|spec)\.rb$`),
	regex.MustCompile(`(^|/).*Test(s?)\.cs$`),
	regex.MustCompile(`(^|/).*\.(test|spec)\.(ts|tsx|js)$`),
}
