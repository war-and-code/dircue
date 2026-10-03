package deployables

import (
	"bytes"
	"errors"
	"path"
	"strings"
	"unicode/utf8"
)

const (
	procfileLineBytes = 8192
	procfileMaxLines  = 512
)

// procfileIssuesError carries bounded, command-free diagnostics while leaving
// successfully parsed process declarations available to the caller.
type procfileIssuesError struct {
	counts map[string]int64
}

func (e *procfileIssuesError) Error() string {
	return "Procfile contains unsupported or malformed lines"
}

// parseProcfile records one deployable for each valid process declaration.
// It recognizes only simple literal launch forms and never retains argv,
// environment assignments, or shell text.
func parseProcfile(name string, content []byte) ([]Definition, bool, error) {
	if name != "Procfile" {
		return nil, false, nil
	}
	if !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
		return nil, false, errors.New("Procfile contains invalid text")
	}
	var definitions []Definition
	seenNames := map[string]bool{}
	issues := map[string]int64{}
	lines := strings.Split(string(content), "\n")
	processLines := 0
	for index, raw := range lines {
		lineNumber := index + 1
		line := strings.TrimSuffix(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if len(line) > procfileLineBytes {
			issues["procfile_line_limit"]++
			continue
		}
		processLines++
		if processLines > procfileMaxLines {
			issues["procfile_process_limit"]++
			continue
		}
		colon := strings.IndexByte(line, ':')
		if colon <= 0 {
			issues["procfile_malformed_line"]++
			continue
		}
		processName := strings.TrimSpace(line[:colon])
		command := strings.TrimSpace(line[colon+1:])
		if !safeProcfileName(processName) || command == "" {
			issues["procfile_malformed_line"]++
			continue
		}
		if seenNames[processName] {
			issues["procfile_duplicate_process"]++
			for i := range definitions {
				if definitions[i].Name != processName {
					continue
				}
				definitions[i].Coverage = "qualified"
				for j := range definitions[i].References {
					definitions[i].References[j] = Reference{Kind: "process_target", Value: "unresolved", Qualification: "unresolved", Evidence: definitions[i].References[j].Evidence}
				}
				break
			}
			continue
		}
		seenNames[processName] = true

		definition := Definition{
			Kind: "process", Provider: "procfile", Name: processName,
			Coverage: "qualified", Evidence: []Evidence{{Field: "process", Value: processName, Line: lineNumber, Basis: "procfile-process"}},
			References: []Reference{},
		}
		ref, recognized := procfileTarget(command, lineNumber)
		if recognized {
			definition.Coverage = "complete"
		} else {
			ref = Reference{Kind: "process_target", Value: "unresolved", Qualification: "unresolved", Evidence: Evidence{Field: "command target", Line: lineNumber, Basis: "procfile-target"}}
			issues["procfile_unrecognized_command"]++
		}
		definition.References = append(definition.References, ref)
		definitions = append(definitions, definition)
	}
	var issueErr error
	if len(issues) > 0 {
		issueErr = &procfileIssuesError{counts: issues}
	}
	return definitions, true, issueErr
}

func safeProcfileName(name string) bool {
	if name == "" {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func procfileTarget(command string, line int) (Reference, bool) {
	fields, ok := procfileFields(command)
	if !ok || len(fields) < 2 {
		return Reference{}, false
	}
	ref := Reference{Kind: "procfile_pending_target", Qualification: "unresolved", Evidence: Evidence{Field: "command target", Line: line, Basis: "procfile-target"}}
	switch fields[0] {
	case "gunicorn", "uvicorn":
		module, callable, found := strings.Cut(fields[1], ":")
		if !found || !safeDottedIdentifier(module) || !safeDottedIdentifier(callable) || procfileChangesPythonSearchPath(fields[0], fields[2:]) {
			return Reference{}, false
		}
		ref.Value = module
		ref.Evidence.Value = module
		ref.Kind = "procfile_python_import"
		return ref, true
	case "node":
		target, valid := safeProcfileFile(fields[1], ".js", ".mjs", ".cjs")
		if !valid {
			return Reference{}, false
		}
		ref.Value = target
		ref.Evidence.Value = target
		ref.Kind = "procfile_node_file"
		return ref, true
	default:
		if !isProcfilePython(fields[0]) {
			return Reference{}, false
		}
		if len(fields) >= 3 && fields[1] == "-m" && safeDottedIdentifier(fields[2]) {
			ref.Value = fields[2]
			ref.Evidence.Value = fields[2]
			ref.Kind = "procfile_python_module"
			return ref, true
		}
		if len(fields) >= 2 {
			target, valid := safeProcfileFile(fields[1], ".py")
			if valid {
				ref.Value = target
				ref.Evidence.Value = target
				ref.Kind = "procfile_python_file"
				return ref, true
			}
		}
	}
	return Reference{}, false
}

func procfileChangesPythonSearchPath(executable string, arguments []string) bool {
	for _, argument := range arguments {
		flag := argument
		if before, _, ok := strings.Cut(argument, "="); ok {
			flag = before
		}
		switch flag {
		case "--chdir", "--pythonpath", "--app-dir", "-c", "--config":
			return true
		}
		if executable == "gunicorn" && (flag == "-C" || flag == "-c") {
			return true
		}
	}
	return false
}

func isProcfilePython(executable string) bool {
	if executable == "python" || executable == "python3" {
		return true
	}
	if !strings.HasPrefix(executable, "python3.") || len(executable) == len("python3.") {
		return false
	}
	for _, c := range executable[len("python3."):] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func procfileFields(command string) ([]string, bool) {
	if strings.ContainsAny(command, "\r\n\x00\\\"'`;&|<>(){}[]") || strings.Contains(command, "$(") || strings.Contains(command, "${") {
		return nil, false
	}
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return nil, false
	}
	// The leading executable and target must be literal. Remaining arguments
	// are intentionally opaque, but shell operators cannot turn them into a
	// second or conditional command.
	for _, field := range fields {
		if field == "#" || strings.ContainsAny(field, ";&|<>`\\\"'(){}[]") || strings.Contains(field, "$(") || strings.Contains(field, "${") {
			return nil, false
		}
	}
	return fields, true
}

func safeDottedIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for _, part := range strings.Split(value, ".") {
		if part == "" || !identifierStart(part[0]) {
			return false
		}
		for i := 1; i < len(part); i++ {
			if !identifierStart(part[i]) && !(part[i] >= '0' && part[i] <= '9') {
				return false
			}
		}
	}
	return true
}

func identifierStart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_'
}

func safeProcfileFile(value string, extensions ...string) (string, bool) {
	if value == "" || strings.HasPrefix(value, "/") || strings.ContainsAny(value, "\\:$*?[]") {
		return "", false
	}
	clean := path.Clean(strings.TrimPrefix(value, "./"))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || clean != strings.TrimPrefix(value, "./") {
		return "", false
	}
	validExt := false
	for _, ext := range extensions {
		if strings.HasSuffix(clean, ext) {
			validExt = true
			break
		}
	}
	if !validExt {
		return "", false
	}
	for _, c := range clean {
		if c != '/' && !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.') {
			return "", false
		}
	}
	if strings.HasPrefix(path.Base(clean), "-") || strings.HasPrefix(path.Base(clean), ".") {
		return "", false
	}
	return clean, true
}

// resolveProcfileTargets binds a parsed target only to a unique selected file.
// It retains no command text and keeps the matched source path private for the
// map's owning-component pass.
func resolveProcfileTargets(definitions []Definition, selected map[string]bool) int64 {
	var unresolved int64
	for i := range definitions {
		if definitions[i].Provider != "procfile" {
			continue
		}
		for j := range definitions[i].References {
			ref := &definitions[i].References[j]
			kind := ref.Kind
			if kind != "procfile_node_file" && kind != "procfile_python_file" && kind != "procfile_python_import" && kind != "procfile_python_module" {
				continue
			}
			var matches []string
			switch kind {
			case "procfile_node_file", "procfile_python_file":
				if selected[ref.Value] {
					matches = append(matches, ref.Value)
				}
			case "procfile_python_import":
				base := strings.ReplaceAll(ref.Value, ".", "/")
				for _, candidate := range []string{base + ".py", path.Join(base, "__init__.py")} {
					if selected[candidate] {
						matches = append(matches, candidate)
					}
				}
			case "procfile_python_module":
				base := strings.ReplaceAll(ref.Value, ".", "/")
				for _, candidate := range []string{base + ".py", path.Join(base, "__main__.py")} {
					if selected[candidate] {
						matches = append(matches, candidate)
					}
				}
			}
			ref.Kind = "process_target"
			if len(matches) != 1 {
				ref.Qualification = "unresolved"
				ref.SourcePath = ""
				definitions[i].Coverage = "qualified"
				unresolved++
				if ref.Value == "" {
					ref.Value = "unresolved"
				}
				continue
			}
			ref.Qualification = "local"
			ref.SourcePath = matches[0]
		}
	}
	return unresolved
}

func recordProcfileIssues(report *Report, file string, issue *procfileIssuesError) {
	if issue == nil {
		return
	}
	message := "Some Procfile declarations were malformed, duplicated, unrecognized, or exceeded a parser limit; command text was withheld."
	for _, kind := range []string{"procfile_line_limit", "procfile_process_limit", "procfile_malformed_line", "procfile_duplicate_process", "procfile_unrecognized_command"} {
		report.omit(kind, issue.counts[kind], file, message)
	}
}
