package cli

import (
	"path/filepath"
	"runtime"
	"strings"
)

// detectName returns the display name for the given argv0 path. The result is
// "dirq" when and only when the basename, with any .exe suffix stripped, equals
// "dirq" (case-insensitive on Windows, case-sensitive on other systems). Every
// other basename—"dircue", "dircue-linux-amd64", "dirq-old", a full path,
// or an empty string—returns "dircue". Renamed binaries never print odd names.
func detectName(argv0 string) string {
	return detectNameForOS(argv0, runtime.GOOS)
}

// detectNameForOS is the portable core of detectName, parameterised by the
// target OS so tests can exercise Windows semantics on any host.
func detectNameForOS(argv0, goos string) string {
	base := filepath.Base(argv0)
	// Strip .exe case-insensitively on all platforms: a Windows binary copied
	// to Linux should still identify as "dirq" when it is named dirq.exe.
	if strings.HasSuffix(strings.ToLower(base), ".exe") {
		base = base[:len(base)-4]
	}
	if goos == "windows" {
		if strings.EqualFold(base, "dirq") {
			return "dirq"
		}
		return "dircue"
	}
	if base == "dirq" {
		return "dirq"
	}
	return "dircue"
}
