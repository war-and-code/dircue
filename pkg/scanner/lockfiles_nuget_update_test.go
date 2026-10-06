package scanner

import (
	"context"
	"testing"

	"github.com/war-and-code/dircue/pkg/lockfiles"
)

func TestNuGetUpdateOnlyOwnershipAndLockfileEvidence(t *testing.T) {
	tests := []struct {
		name       string
		files      map[string]string
		wantState  string
		wantChecks int
		wantCheck  string
	}{
		{
			name: "update only has no direct package declaration",
			files: map[string]string{
				"App.csproj": `<Project><ItemGroup Condition="'$(TargetFramework)' == 'net8.0'"><PackageReference Update="Imported.Package" Version="2.0" /></ItemGroup></Project>`,
			},
			wantState: "not_applicable",
		},
		{
			name: "shared props can add an updated package",
			files: map[string]string{
				"App.csproj":            `<Project Sdk="Microsoft.NET.Sdk"><ItemGroup><PackageReference Update="FromShared" Version="2.0" /></ItemGroup></Project>`,
				"Directory.Build.props": `<Project><ItemGroup><PackageReference Include="FromShared" Version="1.0" /></ItemGroup></Project>`,
			},
			wantState: "indeterminate",
		},
		{
			name: "include plus update retains direct-presence check",
			files: map[string]string{
				"App.csproj":         `<Project><ItemGroup><PackageReference Include="Declared" Version="1.0" /><PackageReference Update="Declared" Version="2.0" /></ItemGroup></Project>`,
				"packages.lock.json": `{"version":1,"dependencies":{"net8.0":{"Declared":{"type":"Direct"}}}}`,
			},
			wantState:  "observed",
			wantChecks: 1,
			wantCheck:  "match",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := fixtures(t, tt.files)
			report, err := Scan(context.Background(), root, Options{Source: "directory", Lockfiles: true, DeclarationsOnly: true})
			if err != nil {
				t.Fatalf("Scan: %v", err)
			}
			ctx := requireLockfilesContext(t, report, "App.csproj")
			if ctx.AssociationState != tt.wantState || len(ctx.Checks) != tt.wantChecks {
				t.Fatalf("state/check count: got %s/%d, want %s/%d: %+v", ctx.AssociationState, len(ctx.Checks), tt.wantState, tt.wantChecks, ctx)
			}
			if tt.wantCheck != "" && (ctx.Checks[0].Status != tt.wantCheck || ctx.Checks[0].Name != "nuget-observed-direct-package-presence") {
				t.Fatalf("named direct-presence check changed: %+v", ctx.Checks[0])
			}
			if err := lockfiles.ValidateReport(report.Lockfiles); err != nil {
				t.Fatalf("invalid lockfile report: %v", err)
			}
		})
	}
}

func TestNuGetLiteralCustomLockPathUsesSelectedSnapshotWithArbitraryFilename(t *testing.T) {
	root := fixtures(t, map[string]string{
		"src/App/App.csproj":        `<Project><PropertyGroup><TargetFramework>net10.0</TargetFramework><NuGetLockFilePath>locks/custom.data</NuGetLockFilePath></PropertyGroup><ItemGroup><PackageReference Include="A" Version="1.0" /></ItemGroup></Project>`,
		"src/App/locks/custom.data": `{"version":1,"dependencies":{"net10.0":{"A":{"type":"Direct"}}}}`,
	})
	report, err := Scan(context.Background(), root, Options{Source: "directory", Lockfiles: true, DeclarationsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	got := requireLockfilesContext(t, report, "src/App/App.csproj")
	if got.AssociationState != "observed" || got.LockfilePath != "src/App/locks/custom.data" || len(got.Checks) != 1 || got.Checks[0].Status != "match" {
		t.Fatalf("literal custom lock path was not associated from selected snapshot: %+v", got)
	}
	if got.NuGetEvidence == nil || got.NuGetEvidence.PresenceState != "observed" || got.NuGetEvidence.OwnershipState != "observed" || got.NuGetEvidence.CandidateCount != 1 || got.NuGetEvidence.CandidatePaths[0] != "src/App/locks/custom.data" {
		t.Fatalf("custom candidate presence was not retained: %+v", got.NuGetEvidence)
	}
}

func TestNuGetStaticLiteralImportChainCanBeClassifiedWithoutEvaluation(t *testing.T) {
	root := fixtures(t, map[string]string{
		"App.csproj":            `<Project><Import Project="Shared/One.props" /><ItemGroup><PackageReference Include="A" /></ItemGroup></Project>`,
		"Shared/One.props":      `<Project><Import Project="Versions.props" /></Project>`,
		"Shared/Versions.props": `<Project><PropertyGroup><ManagePackageVersionsCentrally>true</ManagePackageVersionsCentrally></PropertyGroup><ItemGroup><PackageVersion Include="A" Version="1.0" /></ItemGroup></Project>`,
		"packages.lock.json":    `{"version":1,"dependencies":{"net10.0":{"A":{"type":"Direct"}}}}`,
	})
	report, err := Scan(context.Background(), root, Options{Source: "directory", Lockfiles: true, DeclarationsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	got := requireLockfilesContext(t, report, "App.csproj")
	if got.AssociationState != "observed" || len(got.Checks) != 1 || got.Checks[0].Status != "match" {
		t.Fatalf("static literal import chain did not preserve the named direct-ID check: %+v", got)
	}
}
