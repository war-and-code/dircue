package cli

import "github.com/spf13/cobra"

// Saved-report commands inherit the legacy scan flags for compatible parsing,
// but reject them explicitly. A local template avoids hiding shared pflag.Flag
// pointers or registering process-global template functions during Execute.
func setSavedReportHelp(command *cobra.Command) {
	command.SetUsageTemplate(`Usage:
  {{.UseLine}}
{{if .HasExample}}
Examples:
{{.Example}}
{{end}}
Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}

Global Flags:
  -j, --json   Emit JSON
`)
}
