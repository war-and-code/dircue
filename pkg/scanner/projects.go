package scanner

import (
	"bytes"
	"context"
	"dircue/pkg/declarations"
	"dircue/pkg/discovery"
	"dircue/pkg/formats"
	"dircue/pkg/projects"
	"dircue/pkg/registries"
	"dircue/pkg/rules"
	"fmt"
	enry "github.com/go-enry/go-enry/v2"
	"os"
	"path"
	"strings"
)

// Project manifests have their own selection policy. In particular, XML
// manifests remain visible even when XML is excluded from language statistics.
func analyzeFile(ctx context.Context, root *os.Root, item job, opts Options) (result, error) {
	var formatFile *formats.Candidate
	if opts.Formats {
		formatFile = formatCandidate(root, item)
	}
	if opts.FormatsOnly {
		if err := ctx.Err(); err != nil {
			return result{}, err
		}
		return result{path: item.path, skipped: true, formatFile: formatFile}, nil
	}
	var declarationFile *declarations.Candidate
	if opts.Declarations {
		declarationFile = declarationCandidate(root, item)
	}
	if opts.DeclarationsOnly {
		if err := ctx.Err(); err != nil {
			return result{}, err
		}
		return result{path: item.path, skipped: true, declarationSelected: true, declarationFile: declarationFile}, nil
	}
	var registryFile *registries.Candidate
	if opts.Registries {
		registryFile = registryCandidate(root, item)
	}
	var ruleFile *rules.File
	originalRead := item.read
	if opts.Rules != nil {
		ruleFile = &rules.File{Path: item.path, Size: item.size}
	}
	var metadata *discovery.File
	if opts.Discovery {
		metadata = &discovery.File{Path: item.path, Size: item.size, Vendored: item.attrs.vendored, Generated: item.attrs.generated, Documentation: item.attrs.documentation}
		if opts.DiscoveryOnly {
			if err := ctx.Err(); err != nil {
				return result{}, err
			}
			return result{path: item.path, skipped: true, discoveryFile: metadata}, nil
		}
	}
	if opts.RegistriesOnly {
		if err := ctx.Err(); err != nil {
			return result{}, err
		}
		return result{path: item.path, skipped: true, discoveryFile: metadata, registryFile: registryFile}, nil
	}
	if opts.RulesOnly {
		if err := ctx.Err(); err != nil {
			return result{}, err
		}
		return result{path: item.path, skipped: true, discoveryFile: metadata, rulesFile: ruleFile, rulesRead: originalRead}, nil
	}
	if opts.Projects || opts.Structure != nil {
		item.read = cachedFileReader(root, item)
	}
	value, err := analyzeFileBase(ctx, root, item, opts)
	value.formatFile = formatFile
	value.declarationFile = declarationFile
	value.declarationSelected = opts.Declarations
	value.discoveryFile = metadata
	value.registryFile = registryFile
	if err != nil && registryFile != nil {
		err = registryReadError(err)
	}
	if ruleFile != nil {
		value.rulesFile, value.rulesRead = ruleFile, originalRead
	}
	if err != nil || !opts.Projects || !projects.IsManifest(item.path) {
		return value, err
	}
	limit := projects.MaxManifestBytes
	if opts.MaxFileBytes > 0 {
		limit = min(limit, opts.MaxFileBytes)
	}
	if item.size > limit {
		value.projectDocument.Diagnostics = append(value.projectDocument.Diagnostics, projects.Diagnostic{Path: item.path, Code: "manifest_too_large", Message: "manifest omitted because it exceeds the configured read limit"})
		return value, nil
	}
	var data []byte
	var size int64
	if item.read != nil {
		data, size, err = item.read(limit + 1)
	} else {
		data, _, size, err = readBoundedSize(root, item.path, limit)
	}
	if err != nil {
		if registryFile != nil {
			err = registryReadError(err)
		}
		return value, recoverable(err)
	}
	if size > limit || int64(len(data)) > limit {
		value.projectDocument.Diagnostics = append(value.projectDocument.Diagnostics, projects.Diagnostic{Path: item.path, Code: "manifest_too_large", Message: "manifest omitted because it exceeds the configured read limit"})
		return value, nil
	}
	if int64(len(data)) != size {
		value.projectDocument.Diagnostics = append(value.projectDocument.Diagnostics, projects.Diagnostic{Path: item.path, Code: "incomplete_manifest", Message: "manifest changed or could not be completely read"})
		return value, nil
	}
	value.inventorySize = size
	value.projectDocument = projects.Parse(item.path, data)
	if declarationFile != nil && (projects.IsDotnet(item.path) || projects.IsJVM(item.path)) {
		declarationFile.Legacy = &value.projectDocument
	}
	return value, nil
}

func contentRole(filename, language string, vendored, generated, documentation bool) string {
	if vendored {
		return "vendored"
	}
	if generated {
		return "generated"
	}
	if projects.IsManifest(filename) {
		return "configuration"
	}
	if documentation {
		return "documentation"
	}
	if language == "" {
		return "unknown"
	}
	switch enry.GetLanguageType(language) {
	case enry.Programming, enry.Markup:
		lower := "/" + strings.ToLower(filename)
		base := strings.ToLower(path.Base(filename))
		if strings.Contains(lower, "/test/") || strings.Contains(lower, "/tests/") || strings.Contains(lower, "/src/test/") || strings.HasSuffix(base, "_test.go") || strings.HasSuffix(base, "test.java") || strings.HasSuffix(base, "tests.cs") || strings.HasSuffix(base, "spec.cs") {
			return "test"
		}
		return "source"
	case enry.Data:
		return "data"
	case enry.Prose:
		return "documentation"
	default:
		return "unknown"
	}
}

// A per-file cache lets optional consumers reuse complete reads. It is released
// when that scanner job finishes and is never shared across goroutines.
func cachedFileReader(root *os.Root, item job) func(int64) ([]byte, int64, error) {
	original := item.read
	var cached []byte
	var size int64
	loaded := false
	return func(limit int64) ([]byte, int64, error) {
		if loaded && (int64(len(cached)) >= limit || int64(len(cached)) == size) {
			return cached[:min(int64(len(cached)), limit)], size, nil
		}
		var data []byte
		var actual int64
		var err error
		if original != nil {
			data, actual, err = original(limit)
		} else {
			data, _, actual, err = readBoundedSize(root, item.path, limit)
			if int64(len(data)) > limit {
				data = data[:limit]
			}
		}
		if err != nil {
			return nil, 0, err
		}
		if loaded && (actual != size || !bytes.HasPrefix(data, cached)) {
			return nil, 0, fmt.Errorf("%s changed during profiling", item.path)
		}
		cached, size, loaded = data, actual, true
		return data, actual, nil
	}
}
