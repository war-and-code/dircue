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
			assignments[name] = append(assignments[name], dockerBuildPrefix(m[2]))
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
			}
		}
		if file == "" || invalidFile {
			continue
		}
		context := args[len(args)-1]
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
