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

// safeCLIError escapes terminal controls in the final user-facing error while
// preserving the original error for errors.Is/errors.As callers. Ordinary
// messages are returned as the original error, byte-for-byte and by identity.
func safeCLIError(err error) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	safe := terminalValue(message)
	if safe == message {
		return err
	}
	return &diagnosticError{message: safe, cause: err}
}

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
// transposition. Names of at least six characters additionally accept a second
// edit as long as one candidate is uniquely closer than every other. It never
// guesses among tied candidates or executes a correction.
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
	if found != "" {
		return found
	}
	// Widen to a second edit only for longer names, and only when a single
	// candidate is strictly closer than every other. This keeps `-h`, `--v`,
	// and other short tokens from mapping across a meaning boundary.
	if len(input) < 6 {
		return ""
	}
	best, bestDistance := "", -1
	tied := false
	for _, candidate := range candidates {
		if len(candidate) < 6 {
			continue
		}
		distance := boundedEditDistance(input, candidate, 2)
		if distance < 0 || distance > 2 {
			continue
		}
		if bestDistance < 0 || distance < bestDistance {
			best, bestDistance, tied = candidate, distance, false
		} else if distance == bestDistance {
			tied = true
		}
	}
	if tied || bestDistance < 0 {
		return ""
	}
	return best
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

// boundedEditDistance returns the Levenshtein distance between a and b, capped
// at limit; distances larger than the cap return the cap value plus one.
// Inputs are treated as byte sequences, matching oneEdit's byte-oriented
// definition; canonical ASCII flag names never contain multibyte runes.
func boundedEditDistance(a, b string, limit int) int {
	if abs(len(a)-len(b)) > limit {
		return limit + 1
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := 0; j <= len(b); j++ {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		rowBest := curr[0]
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(min(prev[j]+1, curr[j-1]+1), prev[j-1]+cost)
			if curr[j] < rowBest {
				rowBest = curr[j]
			}
		}
		if rowBest > limit {
			return limit + 1
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// savedReportOpenErrorMessage returns the shared "cannot open saved report"
// diagnostic used by plan, compare, and explain. The role names which side of
// the invocation the caller referred to; the hint tells the caller how to
// produce a valid input.
func savedReportOpenErrorMessage(role string) string {
	return fmt.Sprintf("cannot open %s; supply a readable regular aggregate JSON report, for example one saved by: dircue analyze discovery --json /checkout", role)
}

// enumValueError composes the "must be one of ..." rejection for finite-choice
// flags and appends a nearest-choice suggestion when one is uniquely close.
// The suggestion is textual and never re-executes the command with a guess.
// Values are truncated and terminal controls are escaped before display.
func enumValueError(flag, wantList, value string, allowed []string) error {
	message := fmt.Sprintf("%s must be %s", flag, wantList)
	if suggestion := nearbyName(value, allowed); suggestion != "" && suggestion != value {
		message += "; did you mean " + suggestion + "?"
	}
	return fmt.Errorf("%s", message)
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
	return slices.Contains([]string{"capabilities", "plan", "compare", "locate", "route", "settings"}, cmd.Name()) && slices.Contains(analysisFlagNames, name)
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
	if !isTypoCandidate(token) {
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
	if suggestion := nearbySubcommandPath(cmd, token); suggestion != "" {
		return fmt.Errorf("%w; if you intended the command, use: %s --help", err, suggestion)
	}
	return err
}

// isTypoCandidate reports whether the token is safe to compare against
// subcommand names. It excludes empty tokens, flag-like tokens, and tokens
// carrying path separators or terminal controls that we escape but must not
// send through the fuzzy matcher.
func isTypoCandidate(token string) bool {
	if len(token) == 0 || len(token) > 64 || strings.HasPrefix(token, "-") || strings.ContainsAny(token, "/\\") {
		return false
	}
	return terminalValue(token) == token
}

// nearbySubcommandPath returns the command path of a nearby non-hidden direct
// child, or the empty string when no unique candidate is close.
// Hyphen-prefixed tokens such as `-h` or `--v` are excluded upstream by
// isTypoCandidate so nearbyName never sees them; the single-edit gate keeps
// generic short tokens from mapping to unrelated subcommand names.
func nearbySubcommandPath(cmd *cobra.Command, token string) string {
	var names []string
	byName := make(map[string]*cobra.Command)
	for _, child := range cmd.Commands() {
		if child.Hidden {
			continue
		}
		names = append(names, child.Name())
		byName[child.Name()] = child
	}
	name := nearbyName(token, names)
	if name == "" {
		return ""
	}
	return byName[name].CommandPath()
}

// subcommandTypoSuffix returns a full command-path hint when the token is a
// plausible mistyped direct subcommand, and the empty string
// otherwise. The hint never re-executes the command with a guess.
func subcommandTypoSuffix(cmd *cobra.Command, token string) string {
	if !isTypoCandidate(token) {
		return ""
	}
	if _, err := os.Lstat(token); err == nil {
		return ""
	}
	suggestion := nearbySubcommandPath(cmd, token)
	if suggestion == "" {
		return ""
	}
	return "; did you mean `" + suggestion + "`?"
}
