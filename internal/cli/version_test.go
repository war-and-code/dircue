package cli

import "testing"

func TestModuleVersion(t *testing.T) {
	for _, test := range []struct{ in, want string }{
		{"v1.0.0", "1.0.0"},
		{"v1.2.3-rc.1", "1.2.3-rc.1"},
		{"(devel)", ""},
		{"v0.0.0-20260924010203-abcdefabcdef", ""},
		{"v1.0.1-0.20260924010203-abcdefabcdef", ""},
		{"v1.0.0+dirty", ""},
		{"", ""},
	} {
		if got := moduleVersion(test.in); got != test.want {
			t.Errorf("moduleVersion(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}
