// Package detectors provides passive, stateless profiling hooks. Ecosystems are
// observations based on conventional manifest names, not dependency inventories.
// Framework evidence comes only from declared dependency names in package.json,
// composer.json, or direct Python requirement lines. Other manifest formats are
// not parsed or validated. Included requirement files are never followed.
package detectors

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/war-and-code/dircue/pkg/profile"
)

// Default returns independent, concurrency-safe hooks. These hooks neither read
// other files nor run repository code, dependency tools, or network requests.
func Default() []profile.Detector { return []profile.Detector{manifests{}, layout{}} }

type manifests struct{}

func (manifests) Name() string { return "manifests" }

func (d manifests) Detect(ctx context.Context, file profile.File) ([]profile.Finding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	base := path.Base(file.Path)
	ecosystem := manifestEcosystem(base)
	if ecosystem == "" {
		return nil, nil
	}
	root := path.Dir(file.Path)
	findings := []profile.Finding{
		finding("ecosystem", ecosystem, root, file.Path, d.Name()),
		finding("layout", "project-root", root, file.Path, d.Name()),
	}
	var frameworks []string
	var err error
	switch {
	case base == "package.json" || base == "composer.json":
		if file.Size > int64(len(file.Content)) {
			return findings, fmt.Errorf("%s: manifest content is truncated; dependency detection skipped", file.Path)
		}
		frameworks, err = jsonFrameworks(file.Content, base)
	case isRequirements(base):
		content := string(file.Content)
		if file.Size > int64(len(file.Content)) {
			// A partial last line cannot establish a dependency name.
			if i := strings.LastIndexByte(content, '\n'); i >= 0 {
				content = content[:i]
			} else {
				content = ""
			}
		}
		frameworks = requirementsFrameworks(content)
	}
	if err != nil {
		return findings, fmt.Errorf("%s: %w", file.Path, err)
	}
	for _, framework := range frameworks {
		findings = append(findings, finding("framework", framework, root, file.Path, d.Name()))
	}
	return findings, ctx.Err()
}

func finding(kind, name, root, evidence, detector string) profile.Finding {
	return profile.Finding{Kind: kind, Name: name, Root: root, Evidence: []string{evidence}, Detector: detector}
}

func manifestEcosystem(base string) string {
	switch base {
	case "go.mod", "go.sum", "go.work":
		return "go"
	case "package.json", "package-lock.json", "npm-shrinkwrap.json", "yarn.lock", "pnpm-lock.yaml", "bun.lock", "bun.lockb":
		return "npm"
	case "pyproject.toml", "setup.py", "setup.cfg", "Pipfile", "Pipfile.lock", "poetry.lock", "uv.lock":
		return "python"
	case "Cargo.toml", "Cargo.lock":
		return "cargo"
	case "pom.xml":
		return "maven"
	case "build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts":
		return "gradle"
	case "Gemfile", "Gemfile.lock":
		return "bundler"
	case "composer.json", "composer.lock":
		return "composer"
	case "packages.config", "packages.lock.json", "Directory.Packages.props":
		return "nuget"
	}
	if isRequirements(base) {
		return "python"
	}
	switch path.Ext(base) {
	case ".csproj", ".fsproj", ".vbproj":
		return "nuget"
	case ".gemspec":
		return "bundler"
	}
	return ""
}

func isRequirements(base string) bool {
	return base == "requirements.txt" || (strings.HasPrefix(base, "requirements-") && strings.HasSuffix(base, ".txt"))
}

func jsonFrameworks(content []byte, manifest string) ([]string, error) {
	var document map[string]json.RawMessage
	if err := json.Unmarshal(content, &document); err != nil {
		return nil, fmt.Errorf("invalid JSON manifest: %w", err)
	}
	if document == nil {
		return nil, fmt.Errorf("invalid JSON manifest: expected an object")
	}
	keys := []string{"dependencies", "devDependencies", "peerDependencies", "optionalDependencies"}
	known := map[string]string{
		"react": "React", "next": "Next.js", "vue": "Vue", "nuxt": "Nuxt",
		"@angular/core": "Angular", "express": "Express", "svelte": "Svelte",
		"@sveltejs/kit": "SvelteKit", "@nestjs/core": "NestJS", "astro": "Astro",
	}
	if manifest == "composer.json" {
		keys = []string{"require", "require-dev"}
		known = map[string]string{"laravel/framework": "Laravel", "symfony/framework-bundle": "Symfony"}
	}
	names := map[string]struct{}{}
	for _, key := range keys {
		raw, exists := document[key]
		if !exists {
			continue
		}
		var dependencies map[string]json.RawMessage
		if err := json.Unmarshal(raw, &dependencies); err != nil {
			return nil, fmt.Errorf("invalid %s dependency object: %w", key, err)
		}
		if dependencies == nil {
			return nil, fmt.Errorf("invalid %s dependency object: expected an object", key)
		}
		dependencyNames := make([]string, 0, len(dependencies))
		for dependency := range dependencies {
			dependencyNames = append(dependencyNames, dependency)
		}
		sort.Strings(dependencyNames)
		for _, dependency := range dependencyNames {
			rawVersion := dependencies[dependency]
			var version *string
			if err := json.Unmarshal(rawVersion, &version); err != nil || version == nil {
				return nil, fmt.Errorf("invalid %s dependency %q: expected a version string", key, dependency)
			}
			if name, ok := known[dependency]; ok {
				names[name] = struct{}{}
			}
		}
	}
	return sortedNames(names), nil
}

// Match direct requirement declarations only; comments, recursive includes,
// options, editable installs, URLs and arbitrary source text are not evidence.
var requirement = regexp.MustCompile(`(?i)^([a-z0-9][a-z0-9._-]*)(?:\[[a-z0-9_,. -]+\])?\s*(?:$|[<>=!~;@])`)

func requirementsFrameworks(content string) []string {
	known := map[string]string{"django": "Django", "flask": "Flask", "fastapi": "FastAPI"}
	names := map[string]struct{}{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		match := requirement.FindStringSubmatch(line)
		if len(match) != 2 {
			continue
		}
		if name, ok := known[strings.ToLower(match[1])]; ok {
			names[name] = struct{}{}
		}
	}
	return sortedNames(names)
}

func sortedNames(names map[string]struct{}) []string {
	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

type layout struct{}

func (layout) Name() string { return "layout" }

func (d layout) Detect(ctx context.Context, file profile.File) ([]profile.Finding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	base, dir := path.Base(file.Path), path.Dir(file.Path)
	name, root := "", dir
	switch {
	case path.Base(dir) == "workflows" && path.Base(path.Dir(dir)) == ".github" && (path.Ext(base) == ".yml" || path.Ext(base) == ".yaml"):
		name, root = "github-actions", path.Dir(path.Dir(dir))
	case base == ".gitlab-ci.yml":
		name = "gitlab-ci"
	case base == "Dockerfile" || strings.HasPrefix(base, "Dockerfile."):
		name = "docker"
	case base == "compose.yaml" || base == "compose.yml" || base == "docker-compose.yml" || base == "docker-compose.yaml":
		name = "docker-compose"
	case base == "Jenkinsfile":
		name = "jenkins"
	}
	if name == "" {
		return nil, nil
	}
	return []profile.Finding{finding("layout", name, root, file.Path, d.Name())}, nil
}
