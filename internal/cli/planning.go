package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"slices"
	"sort"
	"strings"
	"unicode"

	"dircue/internal/atlas"
	"dircue/pkg/capabilities"
	"dircue/pkg/planning"
	"dircue/pkg/reportdiff"
	"dircue/schema"
	"github.com/spf13/cobra"
)

func newCapabilitiesCommand(opts *options) *cobra.Command {
	var cliView, guideView, accuracyView bool
	var schemaName string
	command := &cobra.Command{Use: "capabilities", Short: "Describe modules supported by saved-report planning", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := rejectPlanningAnalysisFlags(cmd); err != nil {
			return err
		}
		if err := cmd.Context().Err(); err != nil {
			return err
		}
		selected := 0
		for _, name := range []string{"cli", "guide", "schema", "accuracy"} {
			if cmd.Flags().Changed(name) {
				selected++
			}
		}
		if selected > 1 {
			return fmt.Errorf("choose exactly one capabilities view: --cli, --guide, --accuracy, or --schema NAME; omit all for planner modules")
		}
		if cmd.Flags().Changed("cli") && !cliView || cmd.Flags().Changed("guide") && !guideView || cmd.Flags().Changed("accuracy") && !accuracyView {
			return fmt.Errorf("capabilities view selectors require true; omit --cli, --guide, or --accuracy for planner modules")
		}
		if cliView {
			d := describeCLI(cmd.Root())
			if opts.json {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(d)
			}
			return writeCLIContract(cmd.OutOrStdout(), d)
		}
		if guideView {
			d := automationGuide(cmd.Root())
			if opts.json {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(d)
			}
			return writeAutomationGuide(cmd.OutOrStdout(), d)
		}
		if accuracyView {
			if opts.json {
				cards := atlas.RawJSON()
				n, err := cmd.OutOrStdout().Write(cards)
				if err == nil && n != len(cards) {
					return io.ErrShortWrite
				}
				// Ensure trailing newline
				if err == nil && (len(cards) == 0 || cards[len(cards)-1] != '\n') {
					_, err = fmt.Fprintln(cmd.OutOrStdout())
				}
				return err
			}
			cards, err := atlas.Load()
			if err != nil {
				return err
			}
			return writeAccuracyCards(cmd.OutOrStdout(), cards)
		}
		if cmd.Flags().Changed("schema") {
			if !slices.Contains(schema.Names(), schemaName) {
				return fmt.Errorf("unknown schema %q; choose: %s", diagnosticValue(schemaName), strings.Join(schema.Names(), ", "))
			}
			data, err := schema.Export(schemaName)
			if err != nil {
				return err
			}
			if err := cmd.Context().Err(); err != nil {
				return err
			}
			n, err := cmd.OutOrStdout().Write(data)
			if err == nil && n != len(data) {
				return io.ErrShortWrite
			}
			return err
		}
		d := capabilities.Dircue(Version)
		if err := d.Validate(); err != nil {
			return err
		}
		if opts.json {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(d)
		}
		return writeCapabilities(cmd.OutOrStdout(), d)
	}}
	command.Flags().BoolVar(&cliView, "cli", false, "Describe the actual CLI grammar and automation contracts")
	command.Flags().BoolVar(&guideView, "guide", false, "Print the automation guide without scanning or executing examples")
	command.Flags().BoolVar(&accuracyView, "accuracy", false, "Print embedded accuracy cards derived from hand-labeled ground truth")
	command.Flags().StringVar(&schemaName, "schema", "", "Export a bundled JSON Schema by exact name; always emits JSON")
	command.Long = "Describe planner-supported modules by default. Explicit --cli, --guide, --accuracy, and --schema views expose CLI contracts, workflow guidance, embedded accuracy cards, and offline schemas. These mutually exclusive views never scan directories, probe tools, or access the network. --cli=false, --guide=false, and --accuracy=false are rejected; omit the selector for planner modules."
	setSavedReportHelp(command)
	command.Example = "  dircue capabilities --json\n  dircue capabilities --cli --json\n  dircue capabilities --guide\n  dircue capabilities --accuracy --json\n  dircue capabilities --schema profile --json"
	return command
}

func newPlanCommand(opts *options) *cobra.Command {
	var modules, questions, projects, inputs []string
	command := &cobra.Command{Use: "plan <report.json>", Short: "Plan selected follow-up analysis from a saved profile", Args: func(_ *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("plan requires one saved aggregate report; use: dircue plan report.json --module declarations --json")
		}
		return nil
	}, RunE: func(cmd *cobra.Command, args []string) error {
		if err := rejectPlanningAnalysisFlags(cmd); err != nil {
			return err
		}
		selection := planning.Selection{Questions: questions, Modules: modules, Projects: projects, Inputs: inputs}
		registry := capabilities.Dircue(Version)
		if err := validatePlanningSelectionForCLI(selection, registry); err != nil {
			return err
		}
		file, err := openInputFile(args[0], "saved report")
		if err != nil {
			return &diagnosticError{message: savedReportOpenErrorMessage("saved report"), cause: err}
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("saved report must be a readable regular file")
		}
		if info.Size() > reportdiff.MaxInputBytes {
			return fmt.Errorf("saved report: %w", reportdiff.ErrLimit)
		}
		p, digest, err := reportdiff.ReadEvidence(file)
		if err != nil {
			return fmt.Errorf("saved report: %w", err)
		}
		report, err := planning.Build(cmd.Context(), planning.Input{Profile: p, ReportSHA256: digest, Capabilities: registry, Selection: selection})
		if err != nil {
			return err
		}
		if opts.json {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(report)
		}
		return writePlan(cmd.OutOrStdout(), report)
	}}
	command.Flags().StringSliceVar(&modules, "module", nil, "Planner-supported module to consider (repeatable)")
	command.Flags().StringSliceVar(&questions, "question", nil, "Follow-up question to consider (repeatable)")
	command.Flags().StringArrayVar(&projects, "project", nil, "Root-relative project selector for focus planning")
	command.Flags().StringSliceVar(&inputs, "input", nil, "Caller-supplied prerequisite name, such as structural-worker (repeatable)")
	command.Long = "Read a saved aggregate profile and plan only the requested follow-ups. Never scans its declared root, probes tools, or executes planned commands. Revalidate source identity, boundary, and freshness before replacing any argv placeholder."
	command.Example = "  dircue analyze discovery --json /checkout > first-pass.json\n  dircue plan first-pass.json --module declarations --json\n  dircue plan first-pass.json --question content-formats --json\n  dircue plan first-pass.json --module structure --input structural-worker --json"
	setSavedReportHelp(command)
	defaultHelp := command.HelpFunc()
	command.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		var modules, questions []string
		for _, module := range capabilities.Dircue(Version).Modules {
			modules = append(modules, module.ID)
			questions = append(questions, module.Question)
		}
		original := cmd.Long
		cmd.Long += "\n\nModules: " + strings.Join(modules, ", ") + ".\nQuestions: " + strings.Join(questions, ", ") + "."
		defer func() { cmd.Long = original }()
		defaultHelp(cmd, args)
	})
	return command
}

func validatePlanningSelectionForCLI(selection planning.Selection, registry capabilities.Descriptor) error {
	if len(selection.Modules)+len(selection.Questions) == 0 {
		return fmt.Errorf("%w: select at least one --module or --question; use: dircue plan report.json --module declarations --json; list choices with: dircue capabilities --json", planning.ErrInvalid)
	}
	if len(selection.Modules)+len(selection.Questions) > planning.MaxRequests || len(selection.Inputs) > planning.MaxRequests {
		return fmt.Errorf("%w: select at most %d module/question requests and %d inputs", planning.ErrLimit, planning.MaxRequests, planning.MaxRequests)
	}
	if len(selection.Projects) > planning.MaxProjects {
		return fmt.Errorf("%w: --project accepts one primary project", planning.ErrLimit)
	}
	for _, values := range [][]string{selection.Modules, selection.Questions, selection.Inputs, selection.Projects} {
		for _, value := range values {
			if value == "" || len(value) > planning.MaxKeyBytes {
				return fmt.Errorf("%w: selection values must contain 1..%d bytes", planning.ErrLimit, planning.MaxKeyBytes)
			}
		}
	}
	var modules, questions []string
	questionModule := map[string]string{}
	for _, module := range registry.Modules {
		modules = append(modules, module.ID)
		questions = append(questions, module.Question)
		questionModule[module.Question] = module.ID
	}
	selected := map[string]bool{}
	for _, group := range []struct {
		name          string
		values, valid []string
	}{{"module", selection.Modules, modules}, {"question", selection.Questions, questions}} {
		for _, value := range group.values {
			if !slices.Contains(group.valid, value) {
				hint := ""
				if nearby := nearbyName(value, group.valid); nearby != "" {
					hint = fmt.Sprintf("; did you mean --%s %s?", group.name, nearby)
				}
				return fmt.Errorf("%w: unknown --%s %q%s; list choices with: dircue capabilities --json", planning.ErrInvalid, group.name, diagnosticValue(value), hint)
			}
			module := value
			if group.name == "question" {
				module = questionModule[value]
			}
			selected[module] = true
		}
	}
	if len(selection.Projects) > 0 && !selected["focus"] {
		return fmt.Errorf("%w: --project requires --module focus or --question project-scope", planning.ErrInvalid)
	}
	for _, project := range selection.Projects {
		if path.IsAbs(project) || path.Clean(project) != project || project == "." || project == ".." || strings.HasPrefix(project, "-") || strings.HasPrefix(project, "../") || strings.Contains(project, "/../") || strings.ContainsAny(project, "\\\x00\r\n") {
			return fmt.Errorf("%w: --project requires a confined root-relative manifest path, such as app/app.csproj", planning.ErrInvalid)
		}
	}
	var allowedInputs []string
	for _, module := range registry.Modules {
		if selected[module.ID] {
			for _, input := range module.RequiredInputs {
				if input != "source" && input != "project" {
					allowedInputs = append(allowedInputs, input)
				}
			}
		}
	}
	for _, input := range selection.Inputs {
		if !slices.Contains(allowedInputs, input) {
			hint := "remove --input for modules without caller-supplied prerequisites"
			if nearby := nearbyName(input, allowedInputs); nearby != "" {
				hint = "did you mean --input " + nearby + "?"
			}
			if input == "structural-worker" && !selected["structure"] {
				hint = "--input structural-worker applies only when structure is selected"
			}
			return fmt.Errorf("%w: unsupported --input %q; %s; list prerequisites with: dircue capabilities --json", planning.ErrInvalid, diagnosticValue(input), hint)
		}
	}
	return nil
}

func rejectPlanningAnalysisFlags(cmd *cobra.Command) error {
	for _, flag := range analysisFlagNames {
		if cmd.Flags().Changed(flag) {
			return fmt.Errorf("--%s does not apply to %s; set scan options when creating a report with dircue analyze discovery --json /checkout", flag, cmd.Name())
		}
	}
	return nil
}

func writeAccuracyCards(out io.Writer, cards *atlas.AccuracyCards) error {
	if _, err := fmt.Fprintf(out, "Accuracy cards: %s (evaluated repos: %d)\n%s\n",
		cards.SchemaVersion, cards.EvaluatedRepos, cards.ScopeNote); err != nil {
		return err
	}
	// Sort kinds for deterministic output
	kinds := make([]string, 0, len(cards.Overall))
	for k := range cards.Overall {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		s := cards.Overall[kind]
		tp := s.TP
		labels := s.LabelCount
		ciLower := "N/A"
		if s.CILower != nil {
			ciLower = fmt.Sprintf("%.2f", *s.CILower)
		}
		suffNote := ""
		if s.Sufficiency == "insufficient_labels" {
			suffNote = fmt.Sprintf(" [INSUFFICIENT: %d < 30 labels; results directional only]", labels)
		}
		if _, err := fmt.Fprintf(out, "  %s: %d/%d correct (95%% CI lower=%s), %d repos, path-scoped%s\n",
			kind, tp, labels, ciLower, s.RepoCount, suffNote); err != nil {
			return err
		}
	}
	return nil
}

func writeCapabilities(out io.Writer, d capabilities.Descriptor) error {
	if _, err := fmt.Fprintf(out, "Planner capabilities: %s (%s)\n", d.ProviderVersion, d.SchemaVersion); err != nil {
		return err
	}
	for _, m := range d.Modules {
		if _, err := fmt.Fprintf(out, "  %s: %s [%s]\n", m.ID, m.Question, m.Cost.Inspection); err != nil {
			return err
		}
	}
	return nil
}

func writePlan(out io.Writer, r *planning.Report) error {
	if _, err := fmt.Fprintf(out, "Follow-up plan: %s\nSaved report: %s\nDeclared root is evidence only; every command requires caller revalidation.\n", r.Status, r.Identity.ReportSHA256); err != nil {
		return err
	}
	for _, d := range r.Decisions {
		if _, err := fmt.Fprintf(out, "  %s: %s", d.Module, d.Applicability); err != nil {
			return err
		}
		if len(d.Reasons) > 0 {
			if _, err := fmt.Fprintf(out, " — %s", strings.Join(d.Reasons, "; ")); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
	}
	for _, step := range r.Steps {
		argv, err := json.Marshal(step.Command.Argv)
		if err != nil {
			return err
		}
		// JSON escapes C0 controls; also escape C1 and terminal format characters while
		// preserving the JSON array syntax, including supplementary code points.
		var safeArgv strings.Builder
		for _, ch := range string(argv) {
			if unicode.IsControl(ch) || unicode.Is(unicode.Cf, ch) {
				if ch <= 0xffff {
					fmt.Fprintf(&safeArgv, "\\u%04x", ch)
				} else {
					v := ch - 0x10000
					fmt.Fprintf(&safeArgv, "\\u%04x\\u%04x", 0xd800+(v>>10), 0xdc00+(v&0x3ff))
				}
			} else {
				safeArgv.WriteRune(ch)
			}
		}
		if _, err := fmt.Fprintf(out, "\nStep: %s\n  Inspection: %s; external process: %t\n  Candidate evidence: %d files, %d bytes\n  Qualification: %s\n  Unresolved inputs: %s\n  Inert argv (not executable): %s\n  Revalidation required: %s\n", terminalValue(step.Module), terminalValue(step.Cost.Inspection), step.Cost.ExternalProcess, step.Cost.CandidateFiles, step.Cost.CandidateBytes, terminalValue(step.Cost.Qualification), terminalValue(strings.Join(step.UnresolvedInputs, ", ")), safeArgv.String(), terminalValue(strings.Join(step.Command.RevalidationRequired, ", "))); err != nil {
			return err
		}
	}
	return nil
}
