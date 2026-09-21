package cli

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// diagnosticError preserves the underlying error without printing unbounded
// parser input (which can contain terminal controls or sensitive flag values).
type diagnosticError struct {
	message string
	cause   error
}

func (e *diagnosticError) Error() string { return e.message }
func (e *diagnosticError) Unwrap() error { return e.cause }

func diagnosticValue(value string) string {
	if len(value) > 256 {
		value = value[:256]
		for !utf8.ValidString(value) && len(value) > 0 {
			value = value[:len(value)-1]
		}
		value += "..."
	}
	return terminalValue(value)
}

// nearbyName accepts one insertion, deletion, substitution, or adjacent
// transposition. It never guesses among tied candidates or executes a correction.
func nearbyName(input string, candidates []string) string {
	if len(input) == 0 || len(input) > 64 {
		return ""
	}
	found := ""
	for _, candidate := range candidates {
		if input == candidate {
			return candidate
		}
		if !oneEdit(input, candidate) {
			continue
		}
		if found != "" && candidate != found {
			return ""
		}
		found = candidate
	}
	return found
}

func oneEdit(a, b string) bool {
	if len(a) > len(b) {
		a, b = b, a
	}
	if len(b)-len(a) > 1 {
		return false
	}
	i := 0
	for i < len(a) && a[i] == b[i] {
		i++
	}
	if len(a) < len(b) {
		return a[i:] == b[i+1:]
	}
	if i == len(a) {
		return false
	}
	return a[i+1:] == b[i+1:] || (i+1 < len(a) && a[i] == b[i+1] && a[i+1] == b[i] && a[i+2:] == b[i+2:])
}

func flagErrorWithHint(cmd *cobra.Command, err error) error {
	var unknown *pflag.NotExistError
	if errors.As(err, &unknown) {
		name := unknown.GetSpecifiedName()
		if unknown.GetSpecifiedShortnames() != "" {
			return &diagnosticError{fmt.Sprintf("unknown shorthand flag: %q; see: %s --help", diagnosticValue(name), cmd.CommandPath()), err}
		}
		var names []string
		collect := func(flag *pflag.Flag) {
			if !flag.Hidden && flag.Deprecated == "" && !savedReportFlagRejected(cmd, flag.Name) {
				names = append(names, flag.Name)
			}
		}
		cmd.Flags().VisitAll(collect)
		cmd.InheritedFlags().VisitAll(collect)
		message := "unknown flag: --" + diagnosticValue(name)
		if suggestion := nearbyName(name, names); suggestion != "" {
			message += "; did you mean --" + suggestion + "?"
		}
		return &diagnosticError{message + "; see: " + cmd.CommandPath() + " --help", err}
	}
	var invalid *pflag.InvalidValueError
	if errors.As(err, &invalid) {
		flag := invalid.GetFlag()
		return &diagnosticError{fmt.Sprintf("invalid value for --%s; expected %s; see: %s --help", flag.Name, flag.Value.Type(), cmd.CommandPath()), err}
	}
	var required *pflag.ValueRequiredError
	if errors.As(err, &required) {
		flag := required.GetFlag()
		return &diagnosticError{fmt.Sprintf("--%s requires a %s value; see: %s --help", flag.Name, flag.Value.Type(), cmd.CommandPath()), err}
	}
	// Parser errors may include the complete token, including a value supplied
	// after '='. Unknown future parser error types must not expose that value.
	return &diagnosticError{"invalid flag syntax; use --name=value or --name value; see: " + cmd.CommandPath() + " --help", err}
}

var analysisFlagNames = []string{"breakdown", "strategies", "workers", "max-file-bytes", "source", "rev", "tree", "tree-size", "on-error"}

func savedReportFlagRejected(cmd *cobra.Command, name string) bool {
	return slices.Contains([]string{"capabilities", "plan", "compare"}, cmd.Name()) && slices.Contains(analysisFlagNames, name)
}

func analysisSelectionError(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		var names []string
		for _, child := range cmd.Commands() {
			names = append(names, child.Name())
		}
		if suggestion := nearbyName(args[0], names); suggestion != "" {
			return fmt.Errorf("unknown analysis %q; did you mean: dircue analyze %s --json /path/to/source", diagnosticValue(args[0]), suggestion)
		}
	}
	return fmt.Errorf("choose an analysis; for a metadata inventory use: dircue analyze discovery --json /path/to/source; for language statistics use: dircue analyze languages --json /path/to/source; list profilers with: dircue analyze --help")
}

func missingPathCommandHint(cmd *cobra.Command, args []string, err error) error {
	if err == nil || !errors.Is(err, os.ErrNotExist) || len(args) != 1 || cmd.ArgsLenAtDash() >= 0 {
		return err
	}
	token := args[0]
	if len(token) > 64 || token == "" || strings.HasPrefix(token, "-") || strings.ContainsAny(token, "/\\") || terminalValue(token) != token {
		return err
	}
	if _, pathErr := os.Lstat(token); !errors.Is(pathErr, os.ErrNotExist) {
		return err
	}
	for _, child := range cmd.Commands() {
		if child.Name() != "analyze" {
			continue
		}
		for _, mode := range child.Commands() {
			if token == mode.Name() {
				return fmt.Errorf("%w; if you intended the profiler, use: dircue analyze %s --json /path/to/source", err, mode.Name())
			}
		}
	}
	var names []string
	for _, child := range cmd.Commands() {
		names = append(names, child.Name())
	}
	if suggestion := nearbyName(token, names); suggestion != "" {
		return fmt.Errorf("%w; if you intended the command, use: dircue %s --help", err, suggestion)
	}
	return err
}
