package cli

import (
	"strconv"

	"github.com/war-and-code/dircue/pkg/environments"
	"github.com/war-and-code/dircue/pkg/formats"
	"github.com/war-and-code/dircue/pkg/intentmap"
	"github.com/war-and-code/dircue/pkg/projects"
	"github.com/war-and-code/dircue/pkg/registries"
	"github.com/war-and-code/dircue/pkg/scanner"
	"github.com/war-and-code/dircue/pkg/structure"
)

// These entries expose safety and implementation bounds without making them
// tunable or enabling any of the optional analysis modules they describe.
func fixedMapSettings() []mapEffectiveSetting {
	var settings []mapEffectiveSetting
	add := func(name string, value int64, unit, description string) {
		v := strconv.FormatInt(value, 10)
		settings = append(settings, mapEffectiveSetting{Name: name, Value: v, Unit: unit, Category: "fixed-safety-bound", Origin: "fixed", Description: description, Minimum: v, Maximum: v})
	}
	lanes, single, concurrent := scanner.GitStorageBounds()
	add("git.object_lanes", int64(lanes), "lanes", "Maximum concurrent Git object-storage lanes; fixed to bound aggregate reader overhead.")
	add("git.retained_readers_single_lane", int64(single), "readers", "Retained pack readers with one storage lane; delta resolution may open additional transient readers.")
	add("git.retained_readers_per_concurrent_lane", int64(concurrent), "readers", "Retained pack readers per lane when concurrent; excludes transient delta readers.")
	add("formats.files", formats.MaxFiles, "files", "Maximum retained format-inspection files; lexical admission and omissions are disclosed in format coverage.")
	add("formats.file_bytes", formats.MaxFileBytes, "bytes", "Maximum inspected prefix per format candidate; formats do not expand archives or execute content.")
	add("formats.input_bytes", formats.MaxInputBytes, "bytes", "Total format-inspection input bound; limits content work independently of the inventory size.")
	add("formats.output_bytes", formats.MaxOutputBytes, "bytes", "Serialized format report safety bound.")
	add("formats.parser_depth", formats.MaxDepth, "levels", "Structured format parser depth bound.")
	add("formats.parser_tokens", formats.MaxTokens, "tokens", "Structured format parser token bound.")
	add("manifests.file_bytes", projects.MaxManifestBytes, "bytes", "Maximum complete manifest read for project declarations; larger inputs are reported as omitted.")
	add("registries.file_bytes", registries.MaxFileBytes, "bytes", "Maximum registry configuration read when registry analysis is requested; map does not enable it automatically.")
	add("environments.inventory_paths", environments.DefaultMaxInventoryPaths, "paths", "Maximum paths for explicitly requested environment analysis; not an extra map scan.")
	add("environments.file_bytes", environments.DefaultMaxGlobalJSONBytes, "bytes", "Maximum global.json input in environment analysis.")
	add("environments.input_bytes", environments.DefaultMaxInputBytes, "bytes", "Total input bound for explicitly requested environment analysis.")
	add("environments.requirements", environments.DefaultMaxRequirements, "requirements", "Retained requirement bound for environment analysis.")
	add("environments.contexts", environments.DefaultMaxContexts, "contexts", "Retained context bound for environment analysis.")
	add("environments.output_bytes", environments.DefaultMaxOutputBytes, "bytes", "Serialized environment report bound.")
	add("structure.report_functions", int64(scanner.FunctionReportLimit()), "functions", "Maximum retained functions across an explicitly requested structural report; the optional worker is not enabled by map.")
	add("structure.file_functions", structure.FunctionLimit, "functions", "Maximum retained function records per file for explicitly requested structural analysis.")
	add("intent.import_tokens_per_file", intentmap.DefaultMaxLexicalTokensPerFile, "tokens", "Maximum retained lexical tokens per Java, Kotlin, C#, Visual Basic, JavaScript, or TypeScript import candidate; reaching this bound makes import coverage partial.")
	return settings
}
