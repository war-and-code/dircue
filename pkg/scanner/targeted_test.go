package scanner

import "testing"

func TestValidTargetPathMatchesRootRelativePOSIXPaths(t *testing.T) {
	for _, value := range []string{"src/main.go", "with:colon.txt", "space name"} {
		if !validTargetPath(value) {
			t.Errorf("valid path rejected: %q", value)
		}
	}
	for _, value := range []string{"", ".", "../escape", "a/../b", "/absolute", `back\\slash`, "line\nbreak"} {
		if validTargetPath(value) {
			t.Errorf("invalid path accepted: %q", value)
		}
	}
}

func TestValidateTargetedOptionsRejectsMixedModes(t *testing.T) {
	tests := []Options{
		{ExplainPath: "main.go", Availability: true},
		{Availability: true, AvailabilityOnly: true, Discovery: true},
		{Environments: true, ExplainPath: "main.go"},
	}
	for _, opts := range tests {
		if err := validateTargetedOptions(opts); err == nil {
			t.Errorf("mixed targeted options accepted: %+v", opts)
		}
	}
}
