package cli

import (
	"runtime/debug"
	"strings"
)

const defaultVersion = "1.2.0-dev"

// effectiveVersion reports the ldflags-injected release version. A binary
// built by `go install github.com/war-and-code/dircue@vX.Y.Z` carries no
// injected version, so it falls back to the module version Go recorded.
func effectiveVersion() string {
	if Version != defaultVersion {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := moduleVersion(info.Main.Version); v != "" {
			return v
		}
	}
	return Version
}

// moduleVersion converts a module version such as v1.0.0 to 1.0.0. Local
// builds report "(devel)", and pseudo-versions identify no release; both
// yield "".
func moduleVersion(v string) string {
	if !strings.HasPrefix(v, "v") || strings.Count(v, "-") >= 2 || strings.Contains(v, "+") {
		return ""
	}
	return strings.TrimPrefix(v, "v")
}
