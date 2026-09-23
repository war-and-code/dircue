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
	Preset      string
	Overrides   []string
	CPULimit    int
	MemoryLimit string
}

type mapEffectiveSetting struct {
	Name        string `json:"name"`
	Value       string `json:"value"`
	Unit        string `json:"unit"`
	Category    string `json:"category"`
	Origin      string `json:"origin"`
	Description string `json:"description"`
	Minimum     string `json:"minimum,omitempty"`
	Maximum     string `json:"maximum,omitempty"`
}

type mapSettingsReport struct {
	SchemaVersion string                `json:"schema_version"`
	Kind          string                `json:"kind"`
	Preset        string                `json:"preset"`
	Settings      []mapEffectiveSetting `json:"settings"`
	Notes         []string              `json:"notes"`
}

type resolvedMapSettings struct {
	Workers       int
	MaxFiles      int
	MaxFileBytes  int64
	GitCacheBytes int64
	CPULimit      int
	MemoryLimit   int64
	Report        mapSettingsReport
}

func addMapSettingsFlags(command *cobra.Command, flags *mapSettingsFlags) {
	command.Flags().StringVar(&flags.Preset, "preset", "balanced", "Resource preset: balanced, fast, low-memory, or thorough")
	command.Flags().StringArrayVar(&flags.Overrides, "set", nil, "Override a map setting as NAME=VALUE (repeatable; see: dircue map settings --json)")
	command.Flags().IntVar(&flags.CPULimit, "cpu-limit", 0, "Cooperative Go scheduler limit in logical CPUs; 0 inherits the process setting")
	command.Flags().StringVar(&flags.MemoryLimit, "memory-limit", "0", "Cooperative Go memory limit in bytes or KiB/MiB/GiB; 0 inherits the process setting")
}

func resolveMapSettings(cmd *cobra.Command, opts *options, budgetFiles int, flags mapSettingsFlags) (resolvedMapSettings, error) {
	if !slices.Contains(mapPresetNames, flags.Preset) {
		return resolvedMapSettings{}, enumValueError("--preset", strings.Join(mapPresetNames, ", "), flags.Preset, mapPresetNames)
	}
	workers, maxFiles, maxFileBytes := 0, scanner.DefaultMaxTreeSize, int64(0)
	gitCacheBytes := int64(96 << 20)
	cpuLimit, memoryLimit := 0, int64(0)
	origins := map[string]string{"workers": "default", "inventory.files": "default", "content.file_bytes": "default", "git.object_cache_bytes": "default", "runtime.cpu": "inherited", "runtime.memory_bytes": "inherited"}
	switch flags.Preset {
	case "fast":
		workers, origins["workers"] = 16, "preset:fast"
		gitCacheBytes, origins["git.object_cache_bytes"] = 128<<20, "preset:fast"
	case "low-memory":
		workers, origins["workers"] = 2, "preset:low-memory"
		gitCacheBytes, origins["git.object_cache_bytes"] = 8<<20, "preset:low-memory"
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
		case "git.object_cache_bytes":
			parsed, err := parseMemoryLimit(value)
			if err != nil || parsed == 0 || parsed > 2<<30 {
				return resolvedMapSettings{}, fmt.Errorf("--set %s requires 1MiB to 2GiB", name)
			}
			gitCacheBytes = parsed
		case "runtime.cpu":
			parsed, err := parseMapSettingInt(name, value, 0, 1024)
			if err != nil {
				return resolvedMapSettings{}, err
			}
			cpuLimit = parsed
		case "runtime.memory_bytes":
			parsed, err := parseMemoryLimit(value)
			if err != nil {
				return resolvedMapSettings{}, fmt.Errorf("--set %s: %w", name, err)
			}
			memoryLimit = parsed
		default:
			return resolvedMapSettings{}, fmt.Errorf("unknown map setting %q; supported settings: workers, inventory.files, content.file_bytes, git.object_cache_bytes, runtime.cpu, runtime.memory_bytes", diagnosticValue(name))
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
	if cmd.Flags().Changed("cpu-limit") {
		cpuLimit, origins["runtime.cpu"] = flags.CPULimit, "--cpu-limit"
	}
	if cmd.Flags().Changed("memory-limit") {
		parsed, err := parseMemoryLimit(flags.MemoryLimit)
		if err != nil {
			return resolvedMapSettings{}, fmt.Errorf("--memory-limit: %w", err)
		}
		memoryLimit, origins["runtime.memory_bytes"] = parsed, "--memory-limit"
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
	if cpuLimit < 0 || cpuLimit > 1024 {
		return resolvedMapSettings{}, fmt.Errorf("CPU limit must be between 0 and 1024")
	}
	settings := []mapEffectiveSetting{
		{Name: "workers", Value: strconv.Itoa(workers), Unit: "workers", Category: "performance-only", Origin: origins["workers"], Description: "Concurrent file workers; 0 selects min(GOMAXPROCS, 16).", Minimum: "0", Maximum: "1024"},
		{Name: "git.object_cache_bytes", Value: strconv.FormatInt(gitCacheBytes, 10), Unit: "bytes", Category: "performance-only", Origin: origins["git.object_cache_bytes"], Description: "Retained decoded Git object cache; lower values may save memory and cost extra object reads.", Minimum: "1048576", Maximum: "2147483648"},
		{Name: "inventory.files", Value: strconv.Itoa(maxFiles), Unit: "entries", Category: "coverage-affecting", Origin: origins["inventory.files"], Description: "Maximum selected-source entries inventoried before partial coverage.", Minimum: "1", Maximum: "10000000"},
		{Name: "content.file_bytes", Value: strconv.FormatInt(maxFileBytes, 10), Unit: "bytes", Category: "coverage-affecting", Origin: origins["content.file_bytes"], Description: "Skip larger files with explicit partial coverage; 0 disables this optional limit.", Minimum: "0"},
		{Name: "runtime.cpu", Value: strconv.Itoa(cpuLimit), Unit: "logical CPUs", Category: "performance-only", Origin: origins["runtime.cpu"], Description: "Cooperative GOMAXPROCS setting scoped to this command; 0 inherits the process setting.", Minimum: "0", Maximum: "1024"},
		{Name: "runtime.memory_bytes", Value: strconv.FormatInt(memoryLimit, 10), Unit: "bytes", Category: "performance-only", Origin: origins["runtime.memory_bytes"], Description: "Cooperative Go-managed memory target scoped to this command; 0 inherits the process setting and nonzero values must be at least 1048576.", Minimum: "0", Maximum: "9223372036854775807"},
	}
	return resolvedMapSettings{Workers: workers, MaxFiles: maxFiles, MaxFileBytes: maxFileBytes, GitCacheBytes: gitCacheBytes, CPULimit: cpuLimit, MemoryLimit: memoryLimit, Report: mapSettingsReport{
		SchemaVersion: "1.0.0", Kind: "map_settings", Preset: flags.Preset, Settings: settings,
		Notes: []string{
			"fast and low-memory tune worker concurrency and retained Git object cache size; both must preserve map answers",
			"low-memory is a relative execution preference, not a measured RSS guarantee or hard memory ceiling",
			"GOMEMLIMIT is a cooperative Go runtime limit inherited from the process environment; it is not a hard process or native-worker limit",
			"--cpu-limit and --memory-limit are scoped cooperative runtime controls, not hard CPU, RSS, subprocess, or operating-system ceilings",
			"thorough raises an inventory coverage limit and may change answers that balanced reports as partial",
		},
	}}, nil
}

func parseMemoryLimit(value string) (int64, error) {
	value = strings.TrimSpace(value)
	units := []struct {
		suffix string
		factor int64
	}{{"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10}, {"B", 1}}
	factor := int64(1)
	for _, unit := range units {
		if strings.HasSuffix(value, unit.suffix) {
			value = strings.TrimSpace(strings.TrimSuffix(value, unit.suffix))
			factor = unit.factor
			break
		}
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 || parsed > (1<<63-1)/factor {
		return 0, fmt.Errorf("requires zero or a positive integer in bytes or with a KiB, MiB, or GiB suffix")
	}
	bytes := parsed * factor
	if bytes > 0 && bytes < 1<<20 {
		return 0, fmt.Errorf("requires zero or at least 1MiB (1048576 bytes)")
	}
	return bytes, nil
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
		Example: "  dircue map settings --preset low-memory\n  dircue map settings --preset fast --set workers=8 --json\n  dircue map settings --cpu-limit 2 --memory-limit 512MiB --json",
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
