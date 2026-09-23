package deployables

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"

	yaml "go.yaml.in/yaml/v2"
)

var (
	tfBlock        = regexp.MustCompile(`(?m)^\s*(resource|data|module|provider|terraform)\s+"([^"]+)"(?:\s+"([^"]+)")?\s*\{`)
	dockerFrom     = regexp.MustCompile(`(?i)^\s*FROM(?:\s+--platform=\S+)?\s+(\S+)(?:\s+AS\s+(\S+))?\s*$`)
	dockerCopyFrom = regexp.MustCompile(`(?i)^\s*COPY\s+--from=(\S+)\s+`)
)

func parse(name string, content []byte) ([]Definition, bool, error) {
	base := strings.ToLower(path.Base(name))
	switch {
	case base == "dockerfile" || strings.HasPrefix(base, "dockerfile."):
		return parseDockerfile(content)
	case strings.HasSuffix(base, ".tf"):
		return parseTerraform(content)
	case base == "jenkinsfile" || strings.HasPrefix(base, "jenkinsfile."):
		return parseJenkins(content)
	default:
		return parseYAML(name, content)
	}
}

func parseDockerfile(content []byte) ([]Definition, bool, error) {
	if bytes.IndexByte(content, 0) >= 0 {
		return nil, false, errors.New("Dockerfile contains binary data")
	}
	lines := strings.Split(string(content), "\n")
	d := Definition{Kind: "container_build", Provider: "dockerfile", Name: "default", Coverage: "complete", Evidence: []Evidence{}, References: []Reference{}}
	stages := map[string]bool{}
	for i, line := range lines {
		if m := dockerFrom.FindStringSubmatch(line); m != nil {
			qual := "external"
			if stages[strings.ToLower(m[1])] {
				qual = "local"
			}
			if dynamic(m[1]) {
				qual = "unresolved"
				d.Coverage = "qualified"
			}
			d.References = append(d.References, Reference{Kind: "base_image_or_stage", Value: bounded(m[1]), Qualification: qual, Evidence: Evidence{Field: "FROM", Value: bounded(m[1]), Line: i + 1, Basis: "dockerfile-instruction"}})
			if m[2] != "" {
				stages[strings.ToLower(m[2])] = true
				d.Evidence = append(d.Evidence, Evidence{Field: "stage", Value: bounded(m[2]), Line: i + 1, Basis: "dockerfile-instruction"})
			}
		} else if m := dockerCopyFrom.FindStringSubmatch(line); m != nil {
			qual := "external"
			if stages[strings.ToLower(m[1])] {
				qual = "local"
			}
			if dynamic(m[1]) {
				qual = "unresolved"
				d.Coverage = "qualified"
			}
			d.References = append(d.References, Reference{Kind: "copy_from", Value: bounded(m[1]), Qualification: qual, Evidence: Evidence{Field: "COPY --from", Value: bounded(m[1]), Line: i + 1, Basis: "dockerfile-instruction"}})
		}
	}
	if len(d.References) == 0 {
		return nil, false, nil
	}
	return []Definition{d}, true, nil
}

func parseTerraform(content []byte) ([]Definition, bool, error) {
	if bytes.IndexByte(content, 0) >= 0 {
		return nil, false, errors.New("Terraform file contains binary data")
	}
	matches := tfBlock.FindAllSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return nil, false, nil
	}
	defs := make([]Definition, 0, len(matches))
	for _, m := range matches {
		kind := string(content[m[2]:m[3]])
		first := string(content[m[4]:m[5]])
		second := ""
		if m[6] >= 0 {
			second = string(content[m[6]:m[7]])
		}
		name := first
		if second != "" {
			name = first + "." + second
		}
		if kind == "provider" {
			// Provider aliases are body attributes, not block labels. Include
			// the literal alias in the identity to distinguish configurations.
			open := bytes.IndexByte(content[m[0]:m[1]], '{') + m[0]
			if open >= m[0] {
				bodyEnd := terraformBlockEnd(content, open)
				if bodyEnd > open {
					if alias := terraformAlias.FindSubmatch(content[open+1 : bodyEnd]); alias != nil {
						name += ".alias=" + string(alias[1])
					}
				}
			}
		}
		line := 1 + bytes.Count(content[:m[0]], []byte("\n"))
		d := Definition{Kind: "infrastructure", Provider: "terraform", Name: bounded(kind + ":" + name), Coverage: "qualified", Evidence: []Evidence{{Field: kind, Value: bounded(name), Line: line, Basis: "terraform-literal-block"}}, References: []Reference{}}
		if kind == "module" {
			d.References = append(d.References, Reference{Kind: "module_source", Value: "uninspected", Qualification: "unresolved", Evidence: d.Evidence[0]})
		}
		defs = append(defs, d)
	}
	return defs, true, nil
}

var terraformAlias = regexp.MustCompile(`(?m)^\s*alias\s*=\s*"([A-Za-z0-9_-]+)"\s*(?:#.*)?$`)

func terraformBlockEnd(content []byte, open int) int {
	depth := 0
	quoted, escaped, comment := false, false, false
	for i := open; i < len(content); i++ {
		c := content[i]
		if comment {
			if c == '\n' {
				comment = false
			}
			continue
		}
		if quoted {
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				quoted = false
			}
			continue
		}
		switch c {
		case '"':
			quoted = true
		case '#':
			comment = true
		case '/':
			if i+1 < len(content) && content[i+1] == '/' {
				comment = true
				i++
			}
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func parseJenkins(content []byte) ([]Definition, bool, error) {
	if !regexp.MustCompile(`(?m)^\s*pipeline\s*\{`).Match(content) {
		return nil, false, nil
	}
	if bytes.Count(content, []byte("{")) != bytes.Count(content, []byte("}")) {
		return nil, false, errors.New("malformed declarative Jenkins pipeline")
	}
	d := Definition{Kind: "workflow", Provider: "jenkins", Name: "pipeline", Coverage: "qualified", Evidence: []Evidence{{Field: "pipeline", Line: lineOf(content, "pipeline"), Basis: "declarative-pipeline-block"}}, References: []Reference{}}
	return []Definition{d}, true, nil
}

func parseYAML(name string, content []byte) ([]Definition, bool, error) {
	if bytes.IndexByte(content, 0) >= 0 {
		return nil, false, errors.New("YAML candidate contains binary data")
	}
	if tooDeep(content, 64) {
		return nil, false, errors.New("YAML nesting exceeds the supported depth")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.SetStrict(true)
	var defs []Definition
	recognized := false
	for document := 0; ; document++ {
		var doc map[interface{}]interface{}
		err := decoder.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// Helm templates are intentionally not rendered. Retain only literal
			// apiVersion/kind evidence and qualify the unexpanded declaration.
			if strings.Contains(name, "/templates/") && strings.Contains(string(content), "{{") {
				api, kind := literalField(content, "apiVersion"), literalField(content, "kind")
				if api != "" && kind != "" {
					d := Definition{Kind: "workload", Provider: "helm-template", Name: bounded(kind), Coverage: "qualified", Evidence: []Evidence{{Field: "apiVersion", Value: api, Line: lineOf(content, "apiVersion:"), Basis: "literal-template-field"}, {Field: "kind", Value: kind, Line: lineOf(content, "kind:"), Basis: "literal-template-field"}}, References: []Reference{{Kind: "template_expression", Value: "unexpanded", Qualification: "unresolved", Evidence: Evidence{Field: "template", Basis: "helm-template-not-rendered"}}}}
					return []Definition{d}, true, nil
				}
			}
			return nil, false, fmt.Errorf("malformed or unsupported YAML: %w", err)
		}
		if document >= 128 {
			return nil, false, errors.New("YAML document limit exceeded (128)")
		}
		if doc == nil {
			continue
		}
		found, ok, parseErr := parseYAMLDocument(name, doc, content)
		if parseErr != nil {
			return nil, false, parseErr
		}
		if ok {
			recognized = true
			defs = append(defs, found...)
		}
	}
	return disambiguateDocumentDefinitions(defs), recognized, nil
}

func parseYAMLDocument(name string, doc map[interface{}]interface{}, content []byte) ([]Definition, bool, error) {
	base := strings.ToLower(path.Base(name))
	switch {
	case base == "compose.yml" || base == "compose.yaml" || base == "docker-compose.yml" || base == "docker-compose.yaml":
		return composeDefinitions(name, doc, content)
	case strings.HasPrefix(name, ".github/workflows/") && strings.Count(strings.TrimPrefix(name, ".github/workflows/"), "/") == 0:
		return githubDefinitions(doc, content)
	case base == ".gitlab-ci.yml" || base == ".gitlab-ci.yaml":
		return gitlabDefinitions(doc, content)
	case base == "chart.yaml":
		return helmChartDefinition(doc, content)
	case base == "serverless.yml" || base == "serverless.yaml":
		return serverlessDefinitions(doc, content)
	}
	api, _ := stringValue(doc, "apiVersion")
	kind, _ := stringValue(doc, "kind")
	if strings.HasPrefix(api, "tekton.dev/") {
		return resourceDefinition(doc, content, "workflow", "tekton", kind)
	}
	if api != "" && kind != "" {
		return resourceDefinition(doc, content, "workload", "kubernetes", kind)
	}
	if _, ok := lookup(doc, "Resources"); ok {
		return cloudFormationDefinitions(doc, content)
	}
	return nil, false, nil
}

func disambiguateDocumentDefinitions(defs []Definition) []Definition {
	seen := make(map[string]int, len(defs))
	for i := range defs {
		key := defs[i].Kind + "\x00" + defs[i].Provider + "\x00" + defs[i].Name
		seen[key]++
		if seen[key] > 1 {
			defs[i].Name = bounded(fmt.Sprintf("%s#document-%d", defs[i].Name, seen[key]))
		}
	}
	return defs
}

func composeDefinitions(filename string, doc map[interface{}]interface{}, content []byte) ([]Definition, bool, error) {
	services, ok := object(doc, "services")
	if !ok || len(services) == 0 {
		return nil, false, nil
	}
	defs := []Definition{}
	for _, name := range sortedKeys(services) {
		body, ok := asObject(services[name])
		if !ok {
			continue
		}
		d := Definition{Kind: "service", Provider: "compose", Name: bounded(name), Coverage: "complete", Evidence: []Evidence{{Field: "service", Value: bounded(name), Line: lineOf(content, name+":"), Basis: "compose-services-map"}}, References: []Reference{}}
		if build, exists := lookup(body, "build"); exists {
			if s, ok := build.(string); ok {
				d.References = append(d.References, composeRef(filename, "build_context", s, content))
			} else if b, ok := asObject(build); ok {
				if s, ok := stringValue(b, "context"); ok {
					d.References = append(d.References, composeRef(filename, "build_context", s, content))
				}
				if s, ok := stringValue(b, "dockerfile"); ok {
					d.References = append(d.References, composeRef(filename, "dockerfile", s, content))
				}
				if s, ok := stringValue(b, "target"); ok {
					d.References = append(d.References, composeRef(filename, "build_target", s, content))
				}
			}
		}
		if image, ok := stringValue(body, "image"); ok {
			q := "external"
			if dynamic(image) {
				q = "unresolved"
				d.Coverage = "qualified"
			}
			d.References = append(d.References, Reference{Kind: "image", Value: bounded(image), Qualification: q, Evidence: Evidence{Field: "image", Value: bounded(image), Line: lineOf(content, "image:"), Basis: "compose-field"}})
		}
		if deps, exists := lookup(body, "depends_on"); exists {
			for _, dep := range namesOf(deps) {
				d.References = append(d.References, Reference{Kind: "service_dependency", Value: bounded(dep), Qualification: "local", Evidence: Evidence{Field: "depends_on", Value: bounded(dep), Line: lineOf(content, "depends_on:"), Basis: "compose-field"}})
			}
		}
		defs = append(defs, d)
	}
	return defs, len(defs) > 0, nil
}

func githubDefinitions(doc map[interface{}]interface{}, content []byte) ([]Definition, bool, error) {
	jobs, ok := object(doc, "jobs")
	if !ok || len(jobs) == 0 {
		return nil, false, nil
	}
	workflowName, _ := stringValue(doc, "name")
	if workflowName == "" {
		workflowName = path.Base("workflow")
	}
	d := Definition{Kind: "workflow", Provider: "github-actions", Name: bounded(workflowName), Coverage: "qualified", Evidence: []Evidence{{Field: "jobs", Value: fmt.Sprint(len(jobs)), Line: lineOf(content, "jobs:"), Basis: "github-workflow-map"}}, References: []Reference{}}
	if line := topLevelKeyLine(content, "on"); line > 0 {
		d.Evidence = append(d.Evidence, Evidence{Field: "triggers", Value: "declared", Line: line, Basis: "github-workflow-field"})
	}
	for _, jobName := range sortedKeys(jobs) {
		job, ok := asObject(jobs[jobName])
		if !ok {
			continue
		}
		if uses, ok := stringValue(job, "uses"); ok {
			d.References = append(d.References, workflowRef("reusable_workflow", uses, content))
		}
		if perms, exists := lookup(job, "permissions"); exists {
			d.Evidence = append(d.Evidence, Evidence{Field: "job_permissions", Value: structuralType(perms), Line: lineOf(content, "permissions:"), Basis: "github-workflow-field"})
		}
		if steps, ok := sequence(job, "steps"); ok {
			for _, raw := range steps {
				step, ok := asObject(raw)
				if !ok {
					continue
				}
				if uses, ok := stringValue(step, "uses"); ok {
					d.References = append(d.References, workflowRef("action", uses, content))
				}
				if wd, ok := stringValue(step, "working-directory"); ok {
					q := "local"
					if dynamic(wd) || !safeRelative(wd) {
						q = "unresolved"
					}
					d.References = append(d.References, Reference{Kind: "working_directory", Value: bounded(wd), Qualification: q, Evidence: Evidence{Field: "working-directory", Value: bounded(wd), Line: lineOf(content, "working-directory:"), Basis: "github-step-field"}})
				}
			}
		}
	}
	if _, exists := lookup(doc, "permissions"); exists {
		d.Evidence = append(d.Evidence, Evidence{Field: "workflow_permissions", Line: lineOf(content, "permissions:"), Basis: "github-workflow-field"})
	}
	if bytes.Contains(content, []byte("${{")) {
		d.References = append(d.References, Reference{Kind: "expression", Value: "unresolved", Qualification: "unresolved", Evidence: Evidence{Field: "expression", Line: lineOf(content, "${{"), Basis: "unexpanded-expression"}})
	}
	return []Definition{d}, true, nil
}

func gitlabDefinitions(doc map[interface{}]interface{}, content []byte) ([]Definition, bool, error) {
	reserved := map[string]bool{"stages": true, "variables": true, "include": true, "workflow": true, "default": true, "image": true, "services": true, "before_script": true, "after_script": true, "cache": true}
	d := Definition{Kind: "workflow", Provider: "gitlab-ci", Name: "pipeline", Coverage: "qualified", Evidence: []Evidence{}, References: []Reference{}}
	for _, key := range sortedKeys(doc) {
		if reserved[key] || strings.HasPrefix(key, ".") {
			continue
		}
		if job, ok := asObject(doc[key]); ok {
			if _, script := lookup(job, "script"); script {
				d.Evidence = append(d.Evidence, Evidence{Field: "job", Value: bounded(key), Line: lineOf(content, key+":"), Basis: "gitlab-job-map"})
			}
		}
	}
	if include, ok := lookup(doc, "include"); ok {
		for _, v := range namesOf(include) {
			q := "external"
			if strings.HasPrefix(v, "local:") || strings.HasPrefix(v, ".") {
				q = "local"
			}
			if dynamic(v) {
				q = "unresolved"
			}
			d.References = append(d.References, Reference{Kind: "include", Value: bounded(v), Qualification: q, Evidence: Evidence{Field: "include", Value: bounded(v), Line: lineOf(content, "include:"), Basis: "gitlab-field"}})
		}
	}
	if len(d.Evidence) == 0 && len(d.References) == 0 {
		return nil, false, nil
	}
	return []Definition{d}, true, nil
}

func helmChartDefinition(doc map[interface{}]interface{}, content []byte) ([]Definition, bool, error) {
	api, a := stringValue(doc, "apiVersion")
	name, n := stringValue(doc, "name")
	if !a || !n {
		return nil, false, nil
	}
	d := Definition{Kind: "infrastructure", Provider: "helm", Name: bounded(name), Coverage: "qualified", Evidence: []Evidence{{Field: "apiVersion", Value: bounded(api), Line: lineOf(content, "apiVersion:"), Basis: "helm-chart-field"}, {Field: "name", Value: bounded(name), Line: lineOf(content, "name:"), Basis: "helm-chart-field"}}, References: []Reference{}}
	for _, dep := range namesOfKey(doc, "dependencies") {
		d.References = append(d.References, Reference{Kind: "chart_dependency", Value: bounded(dep), Qualification: "external", Evidence: Evidence{Field: "dependencies", Value: bounded(dep), Line: lineOf(content, "dependencies:"), Basis: "helm-chart-field"}})
	}
	return []Definition{d}, true, nil
}

func serverlessDefinitions(doc map[interface{}]interface{}, content []byte) ([]Definition, bool, error) {
	service, ok := stringValue(doc, "service")
	if !ok {
		return nil, false, nil
	}
	d := Definition{Kind: "service", Provider: "serverless-framework", Name: bounded(service), Coverage: "qualified", Evidence: []Evidence{{Field: "service", Value: bounded(service), Line: lineOf(content, "service:"), Basis: "serverless-field"}}, References: []Reference{}}
	if p, ok := object(doc, "provider"); ok {
		if n, ok := stringValue(p, "name"); ok {
			d.References = append(d.References, Reference{Kind: "cloud_provider", Value: bounded(n), Qualification: "external", Evidence: Evidence{Field: "provider.name", Value: bounded(n), Line: lineOf(content, "provider:"), Basis: "serverless-field"}})
		}
	}
	if f, ok := object(doc, "functions"); ok {
		for _, n := range sortedKeys(f) {
			d.Evidence = append(d.Evidence, Evidence{Field: "function", Value: bounded(n), Line: lineOf(content, n+":"), Basis: "serverless-functions-map"})
		}
	}
	if resources, ok := object(doc, "Resources"); ok {
		for _, resourceName := range sortedKeys(resources) {
			resource, ok := asObject(resources[resourceName])
			if !ok {
				continue
			}
			typ, _ := stringValue(resource, "Type")
			if typ != "AWS::Serverless::Function" {
				continue
			}
			props, ok := object(resource, "Properties")
			if !ok {
				continue
			}
			if code, ok := lookup(props, "CodeUri"); ok {
				if codePath, ok := code.(string); ok && codePath != "" {
					ref := localBuildRef("CodeUri", codePath, content, "sam-function-code-uri")
					ref.Kind = "code_uri"
					d.References = append(d.References, ref)
				}
			}
		}
	}
	if dynamic(service) {
		d.Coverage = "qualified"
	}
	return []Definition{d}, true, nil
}

func resourceDefinition(doc map[interface{}]interface{}, content []byte, kind, provider, fallback string) ([]Definition, bool, error) {
	name := fallback
	if meta, ok := object(doc, "metadata"); ok {
		if n, ok := stringValue(meta, "name"); ok {
			name = n
		}
	}
	if name == "" {
		return nil, false, nil
	}
	d := Definition{Kind: kind, Provider: provider, Name: bounded(name), Coverage: "qualified", Evidence: []Evidence{{Field: "kind", Value: bounded(fallback), Line: lineOf(content, "kind:"), Basis: provider + "-field"}}, References: []Reference{}}
	if spec, ok := object(doc, "spec"); ok {
		for _, image := range kubernetesImages(spec, 0) {
			q := "external"
			if dynamic(image) {
				q = "unresolved"
			}
			d.References = append(d.References, Reference{Kind: "image", Value: bounded(image), Qualification: q, Evidence: Evidence{Field: "image", Value: bounded(image), Line: lineOf(content, "image:"), Basis: "kubernetes-container-field"}})
		}
	}
	return []Definition{d}, true, nil
}

func kubernetesImages(value interface{}, depth int) []string {
	if depth > 64 {
		return nil
	}
	out := []string{}
	switch node := value.(type) {
	case map[interface{}]interface{}:
		for key, child := range node {
			if key == "containers" || key == "initContainers" {
				if items, ok := child.([]interface{}); ok {
					for _, item := range items {
						if container, ok := asObject(item); ok {
							if image, ok := stringValue(container, "image"); ok {
								out = append(out, image)
							}
						}
					}
				}
			}
			out = append(out, kubernetesImages(child, depth+1)...)
		}
	case []interface{}:
		for _, child := range node {
			out = append(out, kubernetesImages(child, depth+1)...)
		}
	}
	return out
}

func localBuildRef(kind, value string, content []byte, basis string) Reference {
	qualification := "local"
	if dynamic(value) || !safeRelative(value) {
		qualification = "unresolved"
	}
	return Reference{Kind: kind, Value: bounded(value), Qualification: qualification, Evidence: Evidence{Field: kind, Value: bounded(value), Line: lineOf(content, kind+":"), Basis: basis}}
}
func cloudFormationDefinitions(doc map[interface{}]interface{}, content []byte) ([]Definition, bool, error) {
	resources, ok := object(doc, "Resources")
	if !ok {
		return nil, false, nil
	}
	defs := []Definition{}
	for _, n := range sortedKeys(resources) {
		r, ok := asObject(resources[n])
		if !ok {
			continue
		}
		typ, _ := stringValue(r, "Type")
		if typ == "" {
			continue
		}
		d := Definition{Kind: "infrastructure", Provider: "cloudformation", Name: bounded(n), Coverage: "qualified", Evidence: []Evidence{{Field: "Type", Value: bounded(typ), Line: lineOf(content, "Type:"), Basis: "cloudformation-resource"}}, References: []Reference{}}
		if typ == "AWS::Serverless::Function" {
			if props, ok := object(r, "Properties"); ok {
				if code, ok := lookup(props, "CodeUri"); ok {
					if codePath, ok := code.(string); ok && codePath != "" {
						d.References = append(d.References, localBuildRef("CodeUri", codePath, content, "sam-function-code-uri"))
						d.References[len(d.References)-1].Kind = "code_uri"
					}
				}
			}
		}
		defs = append(defs, d)
	}
	return defs, len(defs) > 0, nil
}

func workflowRef(kind, value string, content []byte) Reference {
	q := "external"
	if strings.HasPrefix(value, "./") {
		q = "local"
	}
	if dynamic(value) {
		q = "unresolved"
	}
	return Reference{Kind: kind, Value: bounded(value), Qualification: q, Evidence: Evidence{Field: "uses", Value: bounded(value), Line: lineOf(content, "uses:"), Basis: "github-workflow-field"}}
}
func composeRef(filename, kind, value string, content []byte) Reference {
	q := "local"
	if dynamic(value) || !safeComposePath(filename, kind, value) {
		q = "unresolved"
	}
	return Reference{Kind: kind, Value: bounded(value), Qualification: q, Evidence: Evidence{Field: kind, Value: bounded(value), Line: lineOf(content, strings.ReplaceAll(kind, "_", "-")+":"), Basis: "compose-build-field"}}
}

func safeComposePath(filename, kind, value string) bool {
	if kind == "build_target" {
		return value != "" && !dynamic(value)
	}
	if value == "" || strings.Contains(value, "\\") || strings.HasPrefix(value, "/") || regexp.MustCompile(`^[A-Za-z]:`).MatchString(value) {
		return false
	}
	// Build context is relative to the Compose file. Dockerfile is relative to
	// the build context, whose value is unavailable here, so only reject an
	// obvious root escape.
	resolved := path.Clean(path.Join(path.Dir(filename), value))
	return resolved != ".." && !strings.HasPrefix(resolved, "../")
}
func dynamic(s string) bool {
	return strings.Contains(s, "${") || strings.Contains(s, "{{") || strings.Contains(s, "$[")
}
func safeRelative(s string) bool {
	return s != "" && !strings.HasPrefix(s, "/") && !strings.HasPrefix(s, "\\") && !strings.Contains(s, "\\") && !strings.HasPrefix(path.Clean(s), "../") && !regexp.MustCompile(`^[A-Za-z]:`).MatchString(s)
}
func bounded(s string) string {
	if len(s) > DefaultStringBytes {
		return s[:DefaultStringBytes]
	}
	return s
}
func lineOf(content []byte, needle string) int {
	i := bytes.Index(content, []byte(needle))
	if i < 0 {
		return 0
	}
	return 1 + bytes.Count(content[:i], []byte("\n"))
}
func literalField(content []byte, key string) string {
	re := regexp.MustCompile(`(?m)^[ \t]*` + regexp.QuoteMeta(key) + `:[ \t]*([^#\s{][^\r\n#]*)[ \t]*(?:#.*)?$`)
	m := re.FindSubmatch(content)
	if m == nil {
		return ""
	}
	return bounded(strings.TrimSpace(string(m[1])))
}
func tooDeep(content []byte, maxDepth int) bool {
	for _, line := range strings.Split(string(content), "\n") {
		spaces := len(line) - len(strings.TrimLeft(line, " "))
		if spaces/2 > maxDepth {
			return true
		}
	}
	return false
}

func topLevelKeyLine(content []byte, key string) int {
	for i, raw := range bytes.Split(content, []byte("\n")) {
		line := string(raw)
		if strings.HasPrefix(line, key+":") {
			return i + 1
		}
	}
	return 0
}
func lookup(m map[interface{}]interface{}, key string) (interface{}, bool) {
	v, ok := m[key]
	return v, ok
}
func object(m map[interface{}]interface{}, key string) (map[interface{}]interface{}, bool) {
	v, ok := lookup(m, key)
	if !ok {
		return nil, false
	}
	return asObject(v)
}
func asObject(v interface{}) (map[interface{}]interface{}, bool) {
	m, ok := v.(map[interface{}]interface{})
	return m, ok
}
func stringValue(m map[interface{}]interface{}, key string) (string, bool) {
	v, ok := lookup(m, key)
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}
func sequence(m map[interface{}]interface{}, key string) ([]interface{}, bool) {
	v, ok := lookup(m, key)
	if !ok {
		return nil, false
	}
	s, ok := v.([]interface{})
	return s, ok
}
func sortedKeys(m map[interface{}]interface{}) []string {
	out := []string{}
	for k := range m {
		if s, ok := k.(string); ok {
			out = append(out, s)
		}
	}
	slicesSort(out)
	return out
}
func slicesSort(v []string) {
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j] < v[j-1]; j-- {
			v[j], v[j-1] = v[j-1], v[j]
		}
	}
}
func namesOf(v interface{}) []string {
	out := []string{}
	switch x := v.(type) {
	case string:
		out = append(out, x)
	case []interface{}:
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			} else if m, ok := asObject(e); ok {
				for _, k := range sortedKeys(m) {
					out = append(out, k+":"+fmt.Sprint(m[k]))
				}
			}
		}
	case map[interface{}]interface{}:
		out = append(out, sortedKeys(x)...)
	}
	slicesSort(out)
	return out
}
func namesOfKey(m map[interface{}]interface{}, key string) []string {
	v, ok := lookup(m, key)
	if !ok {
		return nil
	}
	return namesOf(v)
}
func structuralType(v interface{}) string {
	switch v.(type) {
	case string:
		return "scalar"
	case map[interface{}]interface{}:
		return "map"
	default:
		return "declared"
	}
}
