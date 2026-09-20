package jsonschema

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestLazyEntrypointsFreshProcesses(t *testing.T) {
	for _, entry := range []string{"idle", "new-compiler", "add-resource", "compile", "must-compile", "compile-string", "must-compile-string", "compiler-compile", "compiler-must-compile", "concurrent", "format-override"} {
		t.Run(entry, func(t *testing.T) {
			command := exec.Command(os.Args[0], "-test.run=^TestLazyProcessChild$", "-test.timeout=20s")
			command.Env = append(os.Environ(), "DIRCUE_JSONSCHEMA_CHILD="+entry)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("fresh process failed: %v\n%s", err, output)
			}
		})
	}
}

func TestLazyProcessChild(t *testing.T) {
	entry := os.Getenv("DIRCUE_JSONSCHEMA_CHILD")
	if entry == "" {
		t.Skip("fresh-process helper")
	}
	drafts := []*Draft{Draft4, Draft6, Draft7, Draft2019, Draft2020}
	for _, draft := range drafts {
		if draft.meta != nil || draft.subschemas == nil {
			t.Fatal("metaschema initialized eagerly, or cheap metadata missing")
		}
	}
	if entry == "idle" {
		return
	}
	c := NewCompiler()
	if entry == "new-compiler" {
		if Draft2020.meta != nil {
			t.Fatal("constructor initialized metaschemas")
		}
		return
	}
	const document = `{"type":"integer","minimum":2}`
	const location = "https://dircue.invalid/lazy-test"
	if err := c.AddResource(location, strings.NewReader(document)); err != nil {
		t.Fatal(err)
	}
	if entry == "add-resource" {
		if Draft2020.meta != nil {
			t.Fatal("resource registration initialized metaschemas")
		}
		return
	}
	var compiled *Schema
	var err error
	switch entry {
	case "compile", "must-compile":
		file := filepath.Join(t.TempDir(), "schema.json")
		if err := os.WriteFile(file, []byte(document), 0600); err != nil {
			t.Fatal(err)
		}
		if entry == "compile" {
			compiled, err = Compile(file)
		} else {
			compiled = MustCompile(file)
		}
	case "compile-string":
		compiled, err = CompileString(location, document)
	case "must-compile-string":
		compiled = MustCompileString(location, document)
	case "compiler-compile":
		compiled, err = c.Compile(location)
	case "compiler-must-compile":
		compiled = c.MustCompile(location)
	case "concurrent":
		var wg sync.WaitGroup
		errors := make(chan error, 80)
		for i := 0; i < 80; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				s, e := CompileString(location, fmt.Sprintf(`{"$schema":%q,"type":"integer","minimum":2}`, drafts[i%len(drafts)].URL()))
				if e == nil && (s.Validate(2) != nil || s.Validate(1) == nil) {
					e = fmt.Errorf("invalid validation result")
				}
				errors <- e
			}(i)
		}
		wg.Wait()
		close(errors)
		for e := range errors {
			if e != nil {
				t.Fatal(e)
			}
		}
		compiled, err = c.Compile(location)
	case "format-override":
		calls := 0
		Formats["uri"] = func(interface{}) bool {
			calls++
			_, e := CompileString(location+"/callback", `{"type":"string"}`)
			return e == nil
		}
		compiled, err = c.Compile(location)
		if calls != 0 {
			t.Fatal("bootstrap consulted overridden format callback")
		}
		custom, e := CompileString(location+"/custom", `{"$schema":"http://json-schema.org/draft-07/schema","format":"uri"}`)
		if e != nil || custom.Validate("custom") != nil || calls != 1 {
			t.Fatal("normal custom callback behavior changed")
		}
	default:
		t.Fatal("unknown entrypoint")
	}
	if err != nil {
		t.Fatal(err)
	}
	if compiled.Validate(2) != nil || compiled.Validate(1) == nil {
		t.Fatal("validation behavior changed")
	}
	for _, draft := range drafts {
		if draft.meta == nil {
			t.Fatal("draft was not initialized")
		}
		if _, err := CompileString(location+"/invalid", fmt.Sprintf(`{"$schema":%q,"type":42}`, draft.URL())); err == nil {
			t.Fatal("invalid schema accepted")
		}
	}
}
