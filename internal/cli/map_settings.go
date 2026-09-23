package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"dircue/pkg/scanner"
	"github.com/spf13/cobra"
)

var mapPresetNames = []string{"balanced", "fast", "low-memory", "thorough"}

type mapSettingsFlags struct {
	Preset    string
	Overrides []string
}

type mapEffectiveSetting struct {
	Name        string `json:"name"`
	Value       string `json:"value"`
	Unit        string `json:"unit"`
	Category    string `json:"category"`
	Origin      string `json:"origin"`
	Description string `json:"description"`
}

type mapSettingsReport struct {
	SchemaVersion string                `json:"schema_version"`
	Kind          string                `json:"kind"`
	Preset        string                `json:"preset"`
	Settings      []mapEffectiveSetting `json:"settings"`
	Notes         []string              `json:"notes"`
}

type resolvedMapSettings struct {
	Workers      int
	MaxFiles     int
	MaxFileBytes int64
	Report       mapSettingsReport
}

func addMapSettingsFlags(command *cobra.Command, flags *mapSettingsFlags) {
	command.Flags().StringVar(&flags.Preset, "preset", "balanced", "Resource preset: balanced, fast, low-memory, or thorough")
	command.Flags().StringArrayVar(&flags.Overrides, "set", nil, "Override a map setting as NAME=VALUE (repeatable; see: dircue map settings --json)")
}

func resolveMapSettings(cmd *cobra.Command, opts *options, budgetFiles int, flags mapSettingsFlags) (resolvedMapSettings, error) {
	if !slices.Contains(mapPresetNames, flags.Preset) {
		return resolvedMapSettings{}, enumValueError("--preset", strings.Join(mapPresetNames, ", "), flags.Preset, mapPresetNames)
	}
	workers, maxFiles, maxFileBytes := 0, scanner.DefaultMaxTreeSize, int64(0)
	origins := map[string]string{"workers": "default", "inventory.files": "default", "content.file_bytes": "default"}
	switch flags.Preset {
	case "fast":
		workers, origins["workers"] = 16, "preset:fast"
	case "low-memory":
		workers, origins["workers"] = 2, "preset:low-memory"
	case "thorough":
		maxFiles, origins["inventory.files"] = 250000, "preset:thorough"
	}
	for _, override := range flags.Overrides {
		name, value, ok := strings.Cut(override, "=")
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		if !ok || name == "" || value == "" {
			return resolvedMapSettings{}, fmt.Errorf("--set requires NAME=VALUE; see: dircue map settings --json")
		}
		switch name {
		case "workers":
			parsed, err := parseMapSettingInt(name, value, 0, 1024)
			if err != nil {
				return resolvedMapSettings{}, err
			}
			workers = parsed
		case "inventory.files":
			parsed, err := parseMapSettingInt(name, value, 1, 10000000)
			if err != nil {
				return resolvedMapSettings{}, err
			}
			maxFiles = parsed
		case "content.file_bytes":
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil || parsed < 0 {
				return resolvedMapSettings{}, fmt.Errorf("--set %s requires an integer of zero or greater", name)
			}
			maxFileBytes = parsed
		default:
			return resolvedMapSettings{}, fmt.Errorf("unknown map setting %q; supported settings: workers, inventory.files, content.file_bytes", diagnosticValue(name))
		}
		origins[name] = "--set"
	}
	// Named flags are the most explicit spelling and therefore win over both a
	// preset and --set. This precedence is also reported in origin.
	if cmd.Flags().Changed("workers") {
		workers, origins["workers"] = opts.workers, "--workers"
	}
	if cmd.Flags().Changed("tree-size") {
		maxFiles, origins["inventory.files"] = opts.maxTreeSize, "--tree-size"
	}
	if cmd.Flags().Changed("budget-files") {
		maxFiles, origins["inventory.files"] = budgetFiles, "--budget-files"
	}
	if cmd.Flags().Changed("max-file-bytes") {
		maxFileBytes, origins["content.file_bytes"] = opts.maxFileBytes, "--max-file-bytes"
	}
	if workers < 0 || workers > 1024 {
		return resolvedMapSettings{}, fmt.Errorf("workers must be between 0 and 1024")
	}
	if maxFiles < 1 || maxFiles > 10000000 {
		return resolvedMapSettings{}, fmt.Errorf("inventory file limit must be between 1 and 10000000")
	}
	if maxFileBytes < 0 {
		return resolvedMapSettings{}, fmt.Errorf("content file byte limit must be zero or greater")
	}
	settings := []mapEffectiveSetting{
		{"workers", strconv.Itoa(workers), "workers", "performance-only", origins["workers"], "Concurrent file workers; 0 selects min(GOMAXPROCS, 16)."},
		{"inventory.files", strconv.Itoa(maxFiles), "entries", "coverage-affecting", origins["inventory.files"], "Maximum selected-source entries inventoried before partial coverage."},
		{"content.file_bytes", strconv.FormatInt(maxFileBytes, 10), "bytes", "coverage-affecting", origins["content.file_bytes"], "Skip larger files with explicit partial coverage; 0 disables this optional limit."},
	}
	return resolvedMapSettings{Workers: workers, MaxFiles: maxFiles, MaxFileBytes: maxFileBytes, Report: mapSettingsReport{
		SchemaVersion: "1.0.0", Kind: "map_settings", Preset: flags.Preset, Settings: settings,
		Notes: []string{
			"fast and low-memory currently tune worker concurrency only and must preserve map answers",
			"low-memory is a relative execution preference, not a measured RSS guarantee or hard memory ceiling",
			"GOMEMLIMIT is a cooperative Go runtime limit inherited from the process environment; it is not a hard process or native-worker limit",
			"thorough raises an inventory coverage limit and may change answers that balanced reports as partial",
		},
	}}, nil
}

func parseMapSettingInt(name, value string, minimum, maximum int) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, fmt.Errorf("--set %s requires an integer between %d and %d", name, minimum, maximum)
	}
	return parsed, nil
}

func newMapSettingsCommand(opts *options) *cobra.Command {
	var flags mapSettingsFlags
	command := &cobra.Command{
		Use:     "settings",
		Short:   "Show effective map resource settings",
		Long:    "Resolve a map preset and fine-grained overrides without scanning. Categories distinguish answer-preserving execution controls from coverage-affecting limits. No preset claims a hard memory or CPU ceiling.",
		Example: "  dircue map settings --preset low-memory\n  dircue map settings --preset fast --set workers=8 --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			for _, flag := range analysisFlagNames {
				if cmd.Flags().Changed(flag) {
					return fmt.Errorf("--%s does not apply to settings introspection; use --set NAME=VALUE", flag)
				}
			}
			resolved, err := resolveMapSettings(cmd, &options{}, scanner.DefaultMaxTreeSize, flags)
			if err != nil {
				return err
			}
			if opts.json {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(resolved.Report)
			}
			return writeMapSettings(cmd.OutOrStdout(), resolved.Report)
		},
	}
	addMapSettingsFlags(command, &flags)
	setSavedReportHelp(command)
	return command
}

func writeMapSettings(out io.Writer, report mapSettingsReport) error {
	if _, err := fmt.Fprintf(out, "Map settings (%s preset)\n", report.Preset); err != nil {
		return err
	}
	for _, setting := range report.Settings {
		if _, err := fmt.Fprintf(out, "%-20s %-10s %-18s %s\n", setting.Name, setting.Value, setting.Category, setting.Origin); err != nil {
			return err
		}
	}
	for _, note := range report.Notes {
		if _, err := fmt.Fprintln(out, "Note: "+note); err != nil {
			return err
		}
	}
	return nil
}
