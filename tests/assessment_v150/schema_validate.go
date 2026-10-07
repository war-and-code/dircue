// Offline validation helper for schemas exported by the candidate CLI.
// It uses dircue's bundled validator and deliberately has no network loader.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/war-and-code/dircue/internal/jsonschema"
)

func main() {
	if len(os.Args) != 4 {
		fail("usage: schema_validate schema.json report.json profile|assessment")
	}
	schemaBytes, err := os.ReadFile(os.Args[1])
	if err != nil {
		fail("read schema: %v", err)
	}
	var schemaDoc map[string]interface{}
	if err := json.Unmarshal(schemaBytes, &schemaDoc); err != nil {
		fail("decode schema: %v", err)
	}
	id, ok := schemaDoc["$id"].(string)
	if !ok || id == "" {
		fail("exported schema has no $id")
	}
	c := jsonschema.NewCompiler()
	c.LoadURL = func(url string) (io.ReadCloser, error) {
		return nil, fmt.Errorf("offline schema check refused external resource %q", url)
	}
	if err := c.AddResource(id, bytes.NewReader(schemaBytes)); err != nil {
		fail("add schema: %v", err)
	}
	compiled, err := c.Compile(id)
	if err != nil {
		fail("compile exported offline schema: %v", err)
	}
	reportFile, err := os.Open(os.Args[2])
	if err != nil {
		fail("open report: %v", err)
	}
	defer reportFile.Close()
	dec := json.NewDecoder(reportFile)
	dec.UseNumber()
	var report map[string]interface{}
	if err := dec.Decode(&report); err != nil {
		fail("decode report: %v", err)
	}
	var trailing interface{}
	if err := dec.Decode(&trailing); err != io.EOF {
		fail("trailing report data")
	}
	value := interface{}(report)
	if os.Args[3] == "assessment" {
		var exists bool
		value, exists = report["assessment"]
		if !exists {
			fail("report has no assessment value")
		}
	} else if os.Args[3] != "profile" {
		fail("unknown schema target %q", os.Args[3])
	}
	if err := compiled.Validate(value); err != nil {
		fail("schema validation failed: %v", err)
	}
}

func fail(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
