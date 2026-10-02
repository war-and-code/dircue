package deployables

import (
	"bytes"
	"path"
	"regexp"
	"strings"
)

var makeAssignment = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*[:?+]?=\s*(.*)$`)

// parseMakefile observes only literal Docker build command lines. It never
// expands Make or shell expressions; variables are accepted solely when every
// static assignment begins with the same supported Docker build prefix.
func parseMakefile(name string, content []byte) ([]Definition, bool, error) {
	// A nested Makefile can be invoked with an unknown working directory, so
	// its literal -f/context pair has no stable repository-relative base.
	if path.Dir(name) != "." {
		return nil, false, nil
	}
	if bytes.IndexByte(content, 0) >= 0 {
		return nil, false, nil
	}
	assignments := map[string][]bool{}
	for _, raw := range bytes.Split(content, []byte("\n")) {
		line := strings.TrimSpace(string(raw))
		if strings.HasPrefix(line, "#") {
			continue
		}
		if m := makeAssignment.FindStringSubmatch(line); m != nil {
			name := strings.TrimSpace(m[1])
			if name == "" {
				continue
			}
			assignments[name] = append(assignments[name], safeDockerBuildAssignment(m[2]))
		}
	}
	defs := []Definition{}
	lineNo := 0
	for _, raw := range bytes.Split(content, []byte("\n")) {
		lineNo++
		original := string(raw)
		if !strings.HasPrefix(original, "\t") {
			continue
		}
		command := strings.TrimSpace(original)
		command = strings.TrimPrefix(command, "@")
		if comment := strings.Index(command, " #"); comment >= 0 {
			command = strings.TrimSpace(command[:comment])
		}
		if command == "" || strings.HasSuffix(command, "\\") || strings.ContainsAny(command, ";&|`<>\n") {
			continue
		}
		fields := strings.Fields(command)
		if len(fields) == 0 {
			continue
		}
		first := fields[0]
		prefixLen := 0
		if strings.HasPrefix(first, "$(") && strings.HasSuffix(first, ")") {
			variable := first[2 : len(first)-1]
			values := assignments[variable]
			if len(values) == 0 {
				continue
			}
			valid := true
			for _, recognized := range values {
				valid = valid && recognized
			}
			if !valid {
				continue
			}
			prefixLen = 1
		} else {
			prefixLen = directDockerBuildPrefix(fields)
			if prefixLen == 0 {
				continue
			}
		}
		args := fields[prefixLen:]
		file := ""
		invalidFile := false
		positionals := []string{}
		for i := 0; i < len(args); i++ {
			switch {
			case args[i] == "-f" || args[i] == "--file":
				if i+1 >= len(args) || file != "" {
					invalidFile = true
					continue
				}
				i++
				file = args[i]
			case strings.HasPrefix(args[i], "--file="):
				if file != "" {
					invalidFile = true
					continue
				}
				file = strings.TrimPrefix(args[i], "--file=")
			case args[i] == "--load" || args[i] == "--push" || args[i] == "--no-cache" || args[i] == "--pull":
				continue
			case strings.HasPrefix(args[i], "--") && strings.Contains(args[i], "="):
				continue
			case args[i] == "--platform" || args[i] == "--build-arg" || args[i] == "--tag" || args[i] == "-t" || args[i] == "--target" || args[i] == "--network" || args[i] == "--label" || args[i] == "--secret" || args[i] == "--ssh" || args[i] == "--output" || args[i] == "--cache-from" || args[i] == "--cache-to":
				if i+1 >= len(args) {
					invalidFile = true
					continue
				}
				i++
			case strings.HasPrefix(args[i], "-"):
				invalidFile = true
			default:
				positionals = append(positionals, args[i])
			}
		}
		if file == "" || invalidFile || len(positionals) != 1 {
			continue
		}
		context := positionals[0]
		if !safeRelative(file) || !safeRelative(context) || strings.HasPrefix(context, "-") || strings.ContainsAny(file+context, "$%{}*?[]'\"") {
			continue
		}
		evidence := Evidence{Field: "docker build", Value: "declared Docker build", Line: lineNo, Basis: "makefile-docker-build"}
		defs = append(defs, Definition{
			Kind: "build_invocation", Provider: "makefile", Name: bounded("dockerfile:" + file), Coverage: "qualified",
			Evidence: []Evidence{evidence}, References: []Reference{
				{Kind: "dockerfile", Value: bounded(file), Qualification: "local", Evidence: Evidence{Field: "-f", Value: bounded(file), Line: lineNo, Basis: "makefile-docker-build"}},
				{Kind: "build_context", Value: bounded(context), Qualification: "local", Evidence: Evidence{Field: "context", Value: bounded(context), Line: lineNo, Basis: "makefile-docker-build"}},
			},
		})
	}
	return defs, len(defs) > 0, nil
}

func dockerBuildPrefix(value string) bool { return directDockerBuildPrefix(strings.Fields(value)) > 0 }

// safeDockerBuildAssignment accepts the simple Docker command prefix and
// option-only trailing expansions used by build Makefiles. It rejects shell
// control syntax and positional/file arguments, either of which could change
// which Dockerfile or context the recipe actually selects.
func safeDockerBuildAssignment(value string) bool {
	if strings.ContainsAny(value, ";&|`<>\\\n\r") {
		return false
	}
	fields := strings.Fields(value)
	prefix := directDockerBuildPrefix(fields)
	if prefix == 0 {
		return false
	}
	for _, arg := range fields[prefix:] {
		if arg == "-f" || arg == "--file" || strings.HasPrefix(arg, "--file=") {
			return false
		}
	}
	for i := prefix; i < len(fields); i++ {
		arg := fields[i]
		if strings.HasPrefix(arg, "--") {
			if strings.Contains(arg, "=") || arg == "--load" || arg == "--push" {
				continue
			}
			if arg == "--build-arg" || arg == "--platform" {
				if i+1 >= len(fields) || !safeMakeArgument(fields[i+1]) {
					return false
				}
				i++
				continue
			}
			return false
		}
		if isSimpleMakeVariable(arg) {
			continue
		}
		if !strings.HasPrefix(arg, "-") {
			return false
		}
	}
	return true
}

func safeMakeArgument(value string) bool {
	return value != "" && !strings.ContainsAny(value, ";&|`<>\\\n\r") && !strings.HasPrefix(value, "-")
}

func isSimpleMakeVariable(value string) bool {
	return strings.HasPrefix(value, "$(") && strings.HasSuffix(value, ")") && !strings.ContainsAny(value[2:len(value)-1], "()")
}

func directDockerBuildPrefix(fields []string) int {
	i := 0
	for i < len(fields) && strings.Contains(fields[i], "=") && !strings.HasPrefix(fields[i], "-") {
		i++ // static environment assignment before docker
	}
	if i+1 < len(fields) && fields[i] == "docker" {
		if fields[i+1] == "build" {
			return i + 2
		}
		if i+2 < len(fields) && fields[i+1] == "buildx" && fields[i+2] == "build" {
			return i + 3
		}
	}
	return 0
}
