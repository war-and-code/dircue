// Package schema validates reports against the versioned schemas shipped with
// dircue. Validation is initialized only when explicitly requested.
package schema

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

//go:embed profile.schema.json
var profileJSON []byte

//go:embed declarations.schema.json
var declarationsJSON []byte

var compileProfile = sync.OnceValues(func() (*jsonschema.Schema, error) {
	compiler := jsonschema.NewCompiler()
	// All references must resolve to bundled resources. Report validation never
	// retrieves schemas from the network or the inspected directory.
	compiler.LoadURL = func(string) (io.ReadCloser, error) {
		return nil, fmt.Errorf("schema resource is not bundled")
	}
	for name, data := range map[string][]byte{
		"profile.schema.json":      profileJSON,
		"declarations.schema.json": declarationsJSON,
	} {
		if err := compiler.AddResource("https://dircue.invalid/schema/"+name, bytes.NewReader(data)); err != nil {
			return nil, err
		}
	}
	return compiler.Compile("https://dircue.invalid/schema/profile.schema.json")
})

// ValidateProfile checks an already decoded JSON value. Callers handling
// untrusted bytes must bound and validate their JSON input before decoding.
func ValidateProfile(value any) error {
	compiled, err := compileProfile()
	if err != nil {
		return err
	}
	return compiled.Validate(value)
}
