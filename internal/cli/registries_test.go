package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dircue/pkg/profile"
)

func TestRegistriesCLIExplicitAndAdditive(t *testing.T) {
	root := t.TempDir()
	for name, data := range map[string]string{
		"main.go":      "package main\n",
		".npmrc":       "registry=https://user:NPM_PASSWORD@npm.example.test/PATH_SECRET?token=QUERY_SECRET#FRAGMENT_SECRET\n//npm.example.test/:_authToken=AUTH_SECRET\n",
		"NuGet.Config": `<configuration><packageSources><add key="internal" value="https://user:NUGET_PASSWORD@nuget.example.test/feed/index.json" /></packageSources><packageSourceCredentials><internal><add key="ClearTextPassword" value="CREDENTIAL_SECRET" /></internal></packageSourceCredentials></configuration>`,
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) profile.Report {
		t.Helper()
		var out, stderr bytes.Buffer
		args = append(args, "--source", "directory", "--json", root)
		if err := Execute(context.Background(), args, &out, &stderr); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		assertRegistrySecretsAbsent(t, out.String()+stderr.String())
		var r profile.Report
		if err := json.Unmarshal(out.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	baseline := run("analyze", "all")
	if baseline.Registries != nil || baseline.SchemaVersion != profile.SchemaVersion {
		t.Fatal("configuration files enabled registry inventory implicitly")
	}
	separate := run("analyze", "registries")
	if separate.Registries == nil || separate.Registries.Status != "complete" || separate.Registries.Coverage.CandidateFiles != 2 || separate.Registries.Coverage.RetainedDeclarations != 2 {
		t.Fatalf("unexpected declarations: %+v", separate.Registries)
	}
	if separate.SchemaVersion != profile.EnhancedSchemaVersion || len(separate.Languages) != 0 || separate.Projects != nil || separate.Metrics != nil || separate.Structure != nil || separate.Rules != nil {
		t.Fatal("registry inventory enabled unrelated content analysis")
	}
	origins := map[string]bool{}
	for _, cfg := range separate.Registries.Configurations {
		for _, d := range cfg.Declarations {
			if d.Endpoint != nil {
				origins[d.Endpoint.Origin] = true
			}
		}
	}
	if len(origins) != 2 || !origins["https://npm.example.test"] || !origins["https://nuget.example.test"] {
		t.Fatalf("unexpected origins: %v", origins)
	}
	combined := run("analyze", "all", "--registries")
	if !bytes.Equal(mustJSON(t, separate.Registries), mustJSON(t, combined.Registries)) {
		t.Fatal("combined modules changed registry declarations")
	}
	combined.Registries = nil
	combined.SchemaVersion = baseline.SchemaVersion
	if !bytes.Equal(mustJSON(t, combined), mustJSON(t, baseline)) {
		t.Fatal("registry opt-in changed existing all output")
	}
	discovery := run("analyze", "discovery")
	withDiscovery := run("analyze", "registries", "--discovery")
	if !bytes.Equal(mustJSON(t, discovery.Discovery), mustJSON(t, withDiscovery.Discovery)) {
		t.Fatal("registry reads changed metadata discovery")
	}
	withDiscovery.Discovery = nil
	if !bytes.Equal(mustJSON(t, withDiscovery), mustJSON(t, separate)) {
		t.Fatal("discovery changed registry output or enabled content classifiers")
	}
	limited := run("analyze", "registries", "--max-file-bytes", "1")
	if limited.Registries.Status != "partial" || limited.Registries.Coverage.ReadFiles != 0 {
		t.Fatal("file size limit was not reflected without reading payloads")
	}
	skipped := run("analyze", "registries", "--tree-size", "1")
	if skipped.Registries.Status != "skipped" || skipped.Registries.Coverage.EnumerationComplete {
		t.Fatal("tree limit cannot claim complete enumeration")
	}
	var out, stderr bytes.Buffer
	if err := Execute(context.Background(), []string{"analyze", "registries", "--discovery", "--source", "directory", root}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	assertRegistrySecretsAbsent(t, out.String()+stderr.String())
	for _, want := range []string{"Registry declarations;", "Metadata discovery;", "https://npm.example.test", "no effective feed set"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("text missing %q: %s", want, out.String())
		}
	}
}

func assertRegistrySecretsAbsent(t *testing.T, output string) {
	t.Helper()
	for _, secret := range []string{"NPM_PASSWORD", "NUGET_PASSWORD", "PATH_SECRET", "QUERY_SECRET", "FRAGMENT_SECRET", "AUTH_SECRET", "CREDENTIAL_SECRET", "user:"} {
		if strings.Contains(output, secret) {
			t.Fatalf("registry output exposed %q", secret)
		}
	}
}

func TestRegistriesCLIFlagScope(t *testing.T) {
	for _, args := range [][]string{{"--registries"}, {"analyze", "languages", "--registries"}, {"analyze", "discovery", "--registries"}, {"analyze", "rules", "--registries"}, {"analyze", "registries", "--metrics"}, {"analyze", "registries", "--rules-file", "anything"}} {
		var out, stderr bytes.Buffer
		if err := Execute(context.Background(), args, &out, &stderr); err == nil || out.Len() != 0 {
			t.Fatalf("%v: error=%v stdout=%q", args, err, out.String())
		}
	}
}
