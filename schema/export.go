package schema

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// These resources are exported only on request. Importing this package does
// not parse or compile them, inspect a directory, or contact a schema server.
//
//go:embed capabilities.schema.json
var capabilitiesJSON []byte

//go:embed comparison.schema.json
var comparisonJSON []byte

//go:embed findings.schema.json
var findingsJSON []byte

//go:embed languages.schema.json
var languagesJSON []byte

//go:embed planning.schema.json
var planningJSON []byte

//go:embed map.schema.json
var mapJSON []byte

//go:embed cli-capabilities.schema.json
var cliCapabilitiesJSON []byte

//go:embed guide.schema.json
var guideJSON []byte

//go:embed forest.schema.json
var forestJSON []byte

//go:embed stats.schema.json
var statsJSON []byte

const resourceBase = "https://dircue.invalid/schema/"

// The explicit registry is also the export allowlist. In particular, a name
// supplied by a caller is never interpreted as a filename or a URL.
var exportResources = map[string][]byte{
	"availability":     availabilityJSON,
	"capabilities":     capabilitiesJSON,
	"cli-capabilities": cliCapabilitiesJSON,
	"comparison":       comparisonJSON,
	"declarations":     declarationsJSON,
	"environments":     environmentsJSON,
	"explanation":      explanationJSON,
	"findings":         findingsJSON,
	"focus":            focusJSON,
	"forest":           forestJSON,
	"formats":          formatsJSON,
	"hotspots":         hotspotsJSON,
	"guide":            guideJSON,
	"languages":        languagesJSON,
	"map":              mapJSON,
	"planning":         planningJSON,
	"profile":          profileJSON,
	"stats":            statsJSON,
}

// Names returns the sorted canonical names accepted by Export. Some schemas
// describe a component inside a profile rather than a whole command response.
// The languages schema covers directory statistics, not the distinct legacy
// single-file response; no single-file schema is currently supplied.
func Names() []string {
	names := make([]string, 0, len(exportResources))
	for name := range exportResources {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// Export returns a standalone Draft 2020-12 compound schema document for an
// exact canonical name from Names. It embeds the transitive resource closure
// and gives each resource an absolute $id, preserving relative references and
// local fragments without rewriting validation rules. Those IDs identify
// bundled resources; they are not URLs that need to be fetched.
//
// Export does not validate caller reports. Every returned byte slice is owned
// by the caller. No filesystem or network resource is ever loaded.
func Export(name string) ([]byte, error) {
	if _, ok := exportResources[name]; !ok {
		return nil, fmt.Errorf("unknown bundled schema %q", name)
	}
	resources := make(map[string]map[string]any)
	var include func(string) error
	include = func(name string) error {
		if _, ok := resources[name]; ok {
			return nil
		}
		raw, ok := exportResources[name]
		if !ok {
			return fmt.Errorf("schema resource is not bundled")
		}
		var document map[string]any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&document); err != nil {
			return fmt.Errorf("invalid bundled schema %s: %w", name, err)
		}
		identity := resourceBase + name + ".schema.json"
		document["$id"] = identity
		resources[name] = document // Mark before following references; allow cycles.
		base, err := url.Parse(identity)
		if err != nil {
			return err
		}
		dependencies, err := schemaDependencies(document, base)
		if err != nil {
			return fmt.Errorf("bundled schema %s: %w", name, err)
		}
		for _, dependency := range dependencies {
			if err := include(dependency); err != nil {
				return err
			}
		}
		return nil
	}
	if err := include(name); err != nil {
		return nil, err
	}
	root := resources[name]
	if len(resources) > 1 {
		defs, ok := root["$defs"].(map[string]any)
		if !ok {
			defs = make(map[string]any)
			root["$defs"] = defs
		}
		const bundleKey = "_dircue_bundled_resources"
		if _, exists := defs[bundleKey]; exists {
			return nil, fmt.Errorf("bundled schema uses reserved definition %s", bundleKey)
		}
		bundled := make(map[string]any, len(resources)-1)
		for key, resource := range resources {
			if key != name {
				bundled[key] = resource
			}
		}
		// Each child retains its own $id, so #/$defs/... inside a child
		// resolves within that child, not against the profile's definitions.
		defs[bundleKey] = map[string]any{"$defs": bundled}
	}
	encoded, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func schemaDependencies(document any, base *url.URL) ([]string, error) {
	dependencies := make(map[string]bool)
	var visit func(any, *url.URL) error
	visit = func(value any, scope *url.URL) error {
		switch value := value.(type) {
		case map[string]any:
			if id, ok := value["$id"].(string); ok {
				parsed, err := url.Parse(id)
				if err != nil {
					return fmt.Errorf("invalid resource identity: %w", err)
				}
				scope = scope.ResolveReference(parsed)
			}
			for _, keyword := range []string{"$ref", "$dynamicRef"} {
				if ref, ok := value[keyword].(string); ok {
					parsed, err := url.Parse(ref)
					if err != nil {
						return fmt.Errorf("invalid schema reference: %w", err)
					}
					resolved := *scope.ResolveReference(parsed)
					resolved.Fragment, resolved.RawFragment = "", ""
					identity := resolved.String()
					name := strings.TrimSuffix(strings.TrimPrefix(identity, resourceBase), ".schema.json")
					if _, ok := exportResources[name]; !ok || identity != resourceBase+name+".schema.json" {
						return fmt.Errorf("schema reference is not bundled")
					}
					dependencies[name] = true
				}
			}
			for _, child := range value {
				if err := visit(child, scope); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range value {
				if err := visit(child, scope); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(document, base); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(dependencies))
	for name := range dependencies {
		names = append(names, name)
	}
	slices.Sort(names)
	return names, nil
}
