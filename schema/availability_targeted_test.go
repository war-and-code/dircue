package schema_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"dircue/pkg/availability"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

func availabilitySchemaValue(t *testing.T) map[string]any {
	t.Helper()
	pointer := "version https://git-lfs.github.com/spec/v1\noid sha256:" + strings.Repeat("a", 64) + "\nsize 123\n"
	c := availability.New(availability.Options{Source: availability.Source{Mode: "directory", Consistency: "live_directory", CheckoutMetadata: "confined_local_metadata"}})
	c.MarkCheckoutMetadataInspected()
	c.AddFile(availability.File{Path: "assets/model.bin", Size: int64(len(pointer)), LFSAttribute: "tracked", Read: func(context.Context, int64) ([]byte, int64, error) { return []byte(pointer), int64(len(pointer)), nil }})
	c.AddGitlink(availability.Gitlink{Path: "vendor/lib", Commit: strings.Repeat("b", 40)})
	c.AddSubmoduleDeclaration(availability.SubmoduleDeclaration{Path: "vendor/lib", Evidence: ".gitmodules"})
	c.AddSparseIndication(availability.SparseIndication{Kind: "sparse_checkout_enabled", Evidence: ".git/config", Supported: true})
	c.AddMissingReference(availability.MissingReference{Project: "app/app.csproj", Kind: "project-reference", Target: "assets/model.bin", Evidence: "app/app.csproj"})
	report, err := c.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func cloneAvailabilityValue(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var clone map[string]any
	if err := json.Unmarshal(data, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}

func compileAvailabilitySchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	s, err := jsonschema.Compile("availability.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAvailabilitySchemaAcceptsBoundedReport(t *testing.T) {
	s := compileAvailabilitySchema(t)
	base := availabilitySchemaValue(t)
	if err := s.Validate(base); err != nil {
		t.Fatal(err)
	}
	git := cloneAvailabilityValue(t, base)
	git["source"] = map[string]any{"mode": "git", "tree": strings.Repeat("c", 40), "consistency": "selected_git_tree", "checkout_metadata": "not_inspected_for_git_tree"}
	git["sparse"] = []any{}
	git["counts"].(map[string]any)["sparse_indications"] = 0.0
	coverage := git["coverage"].(map[string]any)
	coverage["checkout_metadata_inspected"], coverage["checkout_metadata_complete"] = false, false
	if err := s.Validate(git); err != nil {
		t.Fatalf("valid Git source rejected: %v", err)
	}
}

func TestAvailabilitySchemaRejectsInvalidSourceBoundsAndCoverage(t *testing.T) {
	s := compileAvailabilitySchema(t)
	base := availabilitySchemaValue(t)
	tests := map[string]func(map[string]any){
		"pointer bound": func(v map[string]any) { v["bounds"].(map[string]any)["pointer_bytes"] = 2048.0 },
		"content cap": func(v map[string]any) {
			v["bounds"].(map[string]any)["content_bytes"] = float64(availability.DefaultContentBytes + 1)
		},
		"negative source file cap": func(v map[string]any) { v["bounds"].(map[string]any)["max_file_bytes"] = -1.0 },
		"directory tree":           func(v map[string]any) { v["source"].(map[string]any)["tree"] = strings.Repeat("a", 40) },
		"directory checkout policy": func(v map[string]any) {
			v["source"].(map[string]any)["checkout_metadata"] = "not_inspected_for_git_tree"
		},
		"directory metadata uninspected": func(v map[string]any) { v["coverage"].(map[string]any)["checkout_metadata_inspected"] = false },
		"complete with omission":         func(v map[string]any) { v["omissions"].(map[string]any)["made_up"] = 1.0 },
		"partial without omission":       func(v map[string]any) { v["status"] = "partial" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			value := cloneAvailabilityValue(t, base)
			mutate(value)
			if s.Validate(value) == nil {
				t.Fatal("invalid availability report accepted")
			}
		})
	}
}

func TestAvailabilitySchemaSeparatesPointerMetadataFromPointerLikeText(t *testing.T) {
	s := compileAvailabilitySchema(t)
	base := availabilitySchemaValue(t)
	tests := map[string]func(map[string]any){
		"valid missing oid":         func(v map[string]any) { delete(v["lfs"].([]any)[0].(map[string]any), "oid_digest") },
		"valid incomplete metadata": func(v map[string]any) { v["lfs"].([]any)[0].(map[string]any)["metadata_complete"] = false },
		"valid exceeds cutoff":      func(v map[string]any) { v["lfs"].([]any)[0].(map[string]any)["source_file_bytes"] = 1024.0 },
		"pointer-like carries oid": func(v map[string]any) {
			p := v["lfs"].([]any)[0].(map[string]any)
			p["kind"] = "pointer_like"
			p["reason"] = "invalid_oid"
			p["metadata_complete"] = false
			p["canonical"] = false
			p["declared_object_bytes"] = 0.0
		},
		"pointer-like canonical": func(v map[string]any) {
			p := v["lfs"].([]any)[0].(map[string]any)
			p["kind"] = "pointer_like"
			p["reason"] = "invalid_oid"
			p["metadata_complete"] = false
			p["canonical"] = true
			p["declared_object_bytes"] = 0.0
			delete(p, "version")
			delete(p, "oid_algorithm")
			delete(p, "oid_digest")
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			value := cloneAvailabilityValue(t, base)
			mutate(value)
			if s.Validate(value) == nil {
				t.Fatal("invalid LFS state accepted")
			}
		})
	}
	validLike := cloneAvailabilityValue(t, base)
	p := validLike["lfs"].([]any)[0].(map[string]any)
	p["kind"], p["reason"], p["metadata_complete"], p["canonical"], p["declared_object_bytes"] = "pointer_like", "invalid_oid", false, false, 0.0
	delete(p, "version")
	delete(p, "oid_algorithm")
	delete(p, "oid_digest")
	if err := s.Validate(validLike); err != nil {
		t.Fatalf("valid pointer-like state rejected: %v", err)
	}
}

func TestAvailabilitySchemaRejectsContradictoryRelationships(t *testing.T) {
	s := compileAvailabilitySchema(t)
	base := availabilitySchemaValue(t)
	tests := map[string]func(map[string]any){
		"gitlink object id":             func(v map[string]any) { v["gitlinks"].([]any)[0].(map[string]any)["commit"] = "not-an-object" },
		"established without boundary":  func(v map[string]any) { delete(v["references"].([]any)[0].(map[string]any), "boundary_path") },
		"unqualified with boundary":     func(v map[string]any) { v["references"].([]any)[0].(map[string]any)["qualification"] = "unqualified" },
		"sparse directory without path": func(v map[string]any) { s := v["sparse"].([]any)[0].(map[string]any); s["kind"] = "sparse_directory" },
		"unsupported marked supported": func(v map[string]any) {
			s := v["sparse"].([]any)[0].(map[string]any)
			s["kind"] = "unsupported_index_file"
		},
		"submodule evidence leak": func(v map[string]any) { v["submodules"].([]any)[0].(map[string]any)["evidence"] = "private-name" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			value := cloneAvailabilityValue(t, base)
			mutate(value)
			if s.Validate(value) == nil {
				t.Fatal("contradictory relationship accepted")
			}
		})
	}
}
