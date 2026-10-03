package registries

import "testing"

func TestMavenXMLDeclarationRegexIsReadyForMavenOnlyInput(t *testing.T) {
	configuration, err := Parse("build/settings.xml", []byte(`<?xml version="1.0" encoding="UTF-8"?><settings xmlns="http://maven.apache.org/SETTINGS/1.2.0"></settings>`))
	if err != nil {
		t.Fatal(err)
	}
	if configuration.Ecosystem != "maven" || configuration.SyntaxStatus != "complete" || configuration.Status != "complete" {
		t.Fatalf("minimal Maven settings did not parse completely: %+v", configuration)
	}
}
