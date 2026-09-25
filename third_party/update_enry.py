#!/usr/bin/env python3
"""Regenerate the embedded Enry snapshot from pinned official sources.

The project-owned patch contains documented compatibility and performance
changes. The output is a package subtree in the qualified dircue module; the
upstream module files are retained under `upstream.go.mod` and `.sum`.
"""
import argparse
import hashlib
import json
import math
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import stat
import struct
import subprocess
import tarfile
import tempfile
import urllib.request

HERE = Path(__file__).resolve().parent
ENRY = 'github.com/go-enry/go-enry/v2'
VERSION = 'v2.9.6'
RUNTIME_MODULE = 'github.com/war-and-code/dircue/third_party/go-enry'
ENRY_SUM = 'h1:np63eOtMV56zfYDHnFVgpEVOk8fr2kmylcMnAZUDbSs='
ENRY_GO_MOD_SUM = 'h1:9yrj4ES1YrbNb1Wb7/PWYr2bpaCXUGRt0uafN0ISyG8='
GO_VERSION = 'go1.26.6'
LINGUIST_VERSION = '9.7.0'
LINGUIST_REF = 'e0c78d62c42abae6122235d8e68a7aa43eef89da'
ARCHIVE_URL = 'https://codeload.github.com/github-linguist/linguist/tar.gz/refs/tags/v9.7.0'
ARCHIVE_SHA = 'e7b85d06f5e61a810303b8d2e03fc199760525c079fef4dd6f8b7c86342234d9'
MODEL_SHA = '13a5bb39a79e60c5bdb08f49c069445e664b475e82a9a65b018590293cd75743'
CENTROID_MAGIC = b'AURACENT'
CENTROID_FORMAT_VERSION = 1
PREVIOUS_METADATA_FILES = {'PROVENANCE.json', 'GENERATOR_WARNINGS.txt'}
STAGED_METADATA_FILES = {'PROVENANCE.json'}
HEX_SHA256 = re.compile(r'[0-9a-f]{64}')
GO_CONTROLS = {
    'GOENV': 'off', 'GOWORK': 'off', 'GOFLAGS': '', 'GOEXPERIMENT': '',
    'CGO_ENABLED': '0', 'GOAMD64': 'v1', 'GOARM64': 'v8.0',
    'GOPROXY': 'https://proxy.golang.org,direct', 'GOSUMDB': 'sum.golang.org',
    'GOPRIVATE': '', 'GONOPROXY': '', 'GONOSUMDB': '', 'GOINSECURE': '',
    'GOAUTH': 'off', 'GOGC': '100', 'GOMEMLIMIT': 'off', 'GODEBUG': '',
}


def run(args, cwd=None, env=None):
    result = subprocess.run([str(a) for a in args], cwd=cwd, env=env,
                            capture_output=True, text=True)
    if result.returncode:
        raise RuntimeError(f'{args}: {result.stdout}\n{result.stderr}')
    return result.stdout, result.stderr


def file_sha256(path):
    digest = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            digest.update(block)
    return digest.hexdigest()


def file_manifest(root):
    """Hash every regular file in an upstream source tree by normalized path."""
    result = {}
    for path in sorted(root.rglob('*')):
        if path.is_symlink():
            raise RuntimeError(f'refusing symlink in upstream source: {path}')
        if path.is_file():
            relative = path.relative_to(root).as_posix()
            validate_relative_name(relative, 'upstream source')
            result[relative] = file_sha256(path)
    return result


def rewrite_import_paths(content, source_module=ENRY, runtime_module=RUNTIME_MODULE):
    """Rewrite exact self-imports while keeping source-attribution comments intact."""
    text = content.replace(b'\r\n', b'\n').decode('utf-8')
    old = re.escape(source_module)
    import_line = re.compile(
        r'(?P<prefix>\s*(?:import\s+)?(?:[A-Za-z_]\w*\s+)?)"'
        r'(?P<path>' + old + r'(?:/[^"\\]*)?)"')
    import_comment = re.compile(r'(//\s*import\s+")' + old + r'(\s*")')
    in_import_block = False
    lines = []
    for line in text.splitlines(keepends=True):
        stripped = line.lstrip()
        if stripped.startswith('import ('):
            in_import_block = True
            lines.append(line)
            continue
        if in_import_block and stripped.startswith(')'):
            in_import_block = False
            lines.append(line)
            continue
        if in_import_block or stripped.startswith('import ') or '// import "' in line:
            def replace(match):
                return (match.group('prefix') + '"' + runtime_module +
                        match.group('path')[len(source_module):] + '"')
            line = import_line.sub(replace, line)
            line = import_comment.sub(r'\g<1>' + runtime_module + r'\g<2>', line)
        lines.append(line)
    return ''.join(lines).encode('utf-8')


def prepare_embedded_module(root):
    """Create a temporary test module using the upstream dependency set."""
    upstream_mod = root / 'upstream.go.mod'
    upstream_sum = root / 'upstream.go.sum'
    content = upstream_mod.read_text()
    old_line, new_line = f'module {ENRY}', f'module {RUNTIME_MODULE}'
    if content.count(old_line) != 1:
        raise RuntimeError('upstream go.mod has an unexpected module declaration')
    content = content.replace(old_line, new_line, 1)
    # The embedded sources use the root module's language version.
    content = re.sub(r'(?m)^go [0-9.]+$', f'go {GO_VERSION.removeprefix("go")}', content, count=1)
    (root / 'go.mod').write_text(content)
    shutil.copyfile(upstream_sum, root / 'go.sum')


def remove_embedded_module(root):
    for name in ('go.mod', 'go.sum'):
        (root / name).unlink()


def validate_relative_name(name, description):
    path = PurePosixPath(name)
    if (not name or name == '.' or path.is_absolute() or '\\' in name or
            name != path.as_posix() or any(part in ('', '.', '..') for part in path.parts)):
        raise RuntimeError(f'unsafe {description} path: {name!r}')
    return path


def patched_test_files(patch):
    """Find compatibility and performance tests in real patch file headers."""
    tests = set()
    lines = patch.read_text().splitlines()
    for old, new, hunk in zip(lines, lines[1:], lines[2:]):
        # Requiring the adjacent old/new pair and following hunk marker avoids
        # treating patch-content text beginning with "+++ b/" as a file header.
        if not (old.startswith('--- ') and new.startswith('+++ b/') and
                hunk.startswith('@@')):
            continue
        name = new[len('+++ b/'):]
        path = validate_relative_name(name, 'patched test')
        if path.name.endswith('_test.go'):
            tests.add(path.as_posix())
    if 'auragaze_compat_test.go' not in tests:
        raise RuntimeError('compatibility patch must include auragaze_compat_test.go')
    return sorted(tests)


def validate_manifest(raw):
    if raw is None:
        return {}
    if not isinstance(raw, dict):
        raise RuntimeError('preceding provenance files must be an object')
    validated = {}
    for name, digest in raw.items():
        if not isinstance(name, str) or not isinstance(digest, str):
            raise RuntimeError('preceding provenance paths and hashes must be strings')
        validate_relative_name(name, 'preceding provenance')
        if not HEX_SHA256.fullmatch(digest):
            raise RuntimeError(f'invalid preceding provenance hash for {name!r}')
        validated[name] = digest
    return validated


def output_path(value):
    lexical = Path(os.path.abspath(os.path.expanduser(value)))
    if lexical.is_symlink():
        raise RuntimeError(f'refusing generated output symlink: {lexical}')
    return lexical.parent.resolve() / lexical.name


def managed_path(root, name):
    return root.joinpath(*PurePosixPath(name).parts)


def read_previous_manifest(output):
    provenance = output / 'PROVENANCE.json'
    if not provenance.exists():
        return {}
    if provenance.is_symlink() or not provenance.is_file():
        raise RuntimeError(f'invalid preceding provenance file: {provenance}')
    try:
        document = json.loads(provenance.read_text())
    except (OSError, UnicodeError, json.JSONDecodeError) as error:
        raise RuntimeError(f'invalid preceding provenance file: {provenance}') from error
    if not isinstance(document, dict):
        raise RuntimeError('preceding provenance must be an object')
    if 'files' not in document:
        raise RuntimeError('preceding provenance is missing its files manifest')
    return validate_manifest(document['files'])


def preflight_output(output, previous):
    if not output.exists():
        return None
    if output.is_symlink() or not output.is_dir():
        raise RuntimeError(f'generated output is not a regular directory: {output}')
    allowed = set(previous) | PREVIOUS_METADATA_FILES
    allowed_directories = {
        parent.as_posix()
        for name in allowed
        for parent in PurePosixPath(name).parents
        if parent.as_posix() != '.'
    }
    snapshot = {}
    for path in output.rglob('*'):
        if path.is_symlink():
            raise RuntimeError(f'refusing symlink in generated output: {path}')
        relative = path.relative_to(output).as_posix()
        mode = stat.S_IMODE(path.stat().st_mode)
        if path.is_dir():
            if relative not in allowed_directories:
                raise RuntimeError(f'unmanaged directory in generated output: {path}')
            snapshot[relative] = ('directory', mode)
        elif path.is_file():
            if relative not in allowed:
                raise RuntimeError(f'unmanaged file in generated output: {path}')
            snapshot[relative] = ('file', mode, file_sha256(path))
        else:
            raise RuntimeError(f'special file in generated output: {path}')
    # Check every existing managed file, including files that the next refresh
    # will retain. Missing generated files are repaired by a complete refresh.
    for name, digest in previous.items():
        entry = snapshot.get(name)
        if entry is not None and (entry[0] != 'file' or entry[2] != digest):
            raise RuntimeError(f'locally modified generated file: {managed_path(output, name)}')
    return snapshot


def snapshot_output(output):
    if not output.exists():
        if output.is_symlink():
            raise RuntimeError(f'refusing generated output symlink: {output}')
        return None
    if output.is_symlink() or not output.is_dir():
        raise RuntimeError(f'generated output is not a regular directory: {output}')
    snapshot = {}
    for path in output.rglob('*'):
        if path.is_symlink():
            raise RuntimeError(f'refusing symlink in generated output: {path}')
        relative = path.relative_to(output).as_posix()
        mode = stat.S_IMODE(path.stat().st_mode)
        if path.is_dir():
            snapshot[relative] = ('directory', mode)
        elif path.is_file():
            snapshot[relative] = ('file', mode, file_sha256(path))
        else:
            raise RuntimeError(f'special file in generated output: {path}')
    return snapshot


def publish_paths(output):
    return (output.parent / f'.{output.name}.dircue-new',
            output.parent / f'.{output.name}.dircue-backup')


def path_present(path):
    return path.exists() or path.is_symlink()


def recover_publish(output):
    candidate, backup = publish_paths(output)
    if path_present(backup) and path_present(output):
        raise RuntimeError(
            f'ambiguous interrupted update: both {output} and {backup} exist; '
            'inspect them and remove the rejected tree explicitly')
    if path_present(backup):
        if backup.is_symlink() or not backup.is_dir():
            raise RuntimeError(f'invalid interrupted update backup: {backup}')
        os.replace(backup, output)
        if path_present(candidate):
            if candidate.is_symlink() or not candidate.is_dir():
                raise RuntimeError(f'invalid incomplete staged update: {candidate}')
            shutil.rmtree(candidate)
        raise RuntimeError(f'restored {output} after an interrupted update; rerun regeneration')
    if path_present(candidate):
        raise RuntimeError(
            f'incomplete staged update exists at {candidate}; inspect or remove it before retrying')


def fsync_directory(path):
    if os.name == 'nt':
        return
    descriptor = os.open(path, os.O_RDONLY)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def publish_tree(output, candidate, expected_output):
    expected_candidate, backup = publish_paths(output)
    if candidate != expected_candidate:
        raise RuntimeError('candidate output is not the reserved sibling path')
    if not candidate.is_dir() or candidate.is_symlink():
        raise RuntimeError(f'invalid staged generated output: {candidate}')
    if path_present(backup):
        raise RuntimeError(f'refusing existing update backup: {backup}')
    if snapshot_output(output) != expected_output:
        raise RuntimeError('generated output changed while regeneration was running')
    moved_old = False
    if output.exists():
        os.replace(output, backup)
        moved_old = True
        fsync_directory(output.parent)
    try:
        os.replace(candidate, output)
        fsync_directory(output.parent)
    except BaseException:
        if moved_old and not path_present(output) and path_present(backup):
            os.replace(backup, output)
            fsync_directory(output.parent)
        raise
    if moved_old:
        shutil.rmtree(backup)
        fsync_directory(output.parent)


def validate_module(module, module_cache):
    expected = {'Path': ENRY, 'Version': VERSION, 'Sum': ENRY_SUM,
                'GoModSum': ENRY_GO_MOD_SUM}
    for key, value in expected.items():
        if module.get(key) != value:
            raise RuntimeError(f'Enry module {key} mismatch: expected {value!r}')
    source = Path(module.get('Dir', '')).resolve()
    try:
        source.relative_to(module_cache.resolve())
    except ValueError as error:
        raise RuntimeError('Enry module directory escaped the isolated module cache') from error
    if not source.is_dir():
        raise RuntimeError('Enry module directory is missing')
    return source


def require_uint32(value, description):
    if (not isinstance(value, int) or isinstance(value, bool) or
            not 0 <= value <= 0xffffffff):
        raise RuntimeError(f'{description} does not fit uint32')
    return value


def finite_float(value, description):
    if (not isinstance(value, (int, float)) or isinstance(value, bool) or
            not math.isfinite(float(value))):
        raise RuntimeError(f'{description} is not a finite number')
    return float(value)


def utf8_bytes(value, description):
    if not isinstance(value, str):
        raise RuntimeError(f'{description} is not a string')
    encoded = value.encode('utf-8')
    require_uint32(len(encoded), f'{description} length')
    return encoded


def normalized_centroid_model(model):
    if (not isinstance(model, dict) or
            set(model) != {'vocabulary', 'icf', 'centroids'}):
        raise RuntimeError('invalid centroid model object')
    vocabulary, icf, centroids = (model[name] for name in
                                  ('vocabulary', 'icf', 'centroids'))
    if (not isinstance(vocabulary, dict) or not isinstance(icf, list) or
            not isinstance(centroids, dict)):
        raise RuntimeError('invalid centroid model section type')
    require_uint32(len(vocabulary), 'vocabulary count')
    require_uint32(len(centroids), 'language count')
    if len(icf) != len(vocabulary):
        raise RuntimeError('ICF length does not match vocabulary count')
    tokens = [None] * len(vocabulary)
    for token, index in vocabulary.items():
        encoded = utf8_bytes(token, 'vocabulary token')
        require_uint32(index, 'vocabulary index')
        if index >= len(tokens) or tokens[index] is not None:
            raise RuntimeError('vocabulary indices are not unique and dense')
        tokens[index] = encoded
    if any(token is None for token in tokens):
        raise RuntimeError('vocabulary indices are not dense')
    icf_values = [finite_float(value, 'ICF value') for value in icf]
    languages = []
    total_entries = 0
    for language, raw_entries in centroids.items():
        encoded_language = utf8_bytes(language, 'language name')
        if not isinstance(raw_entries, dict):
            raise RuntimeError(f'centroid for {language!r} is not an object')
        require_uint32(len(raw_entries), 'language entry count')
        entries = []
        for raw_index, raw_value in raw_entries.items():
            if (not isinstance(raw_index, str) or not raw_index.isascii() or
                    not raw_index.isdecimal()):
                raise RuntimeError(f'invalid centroid index {raw_index!r}')
            index = require_uint32(int(raw_index, 10), 'centroid index')
            if index >= len(tokens):
                raise RuntimeError('centroid index is outside the vocabulary')
            entries.append((index, finite_float(raw_value, 'centroid value')))
        entries.sort()
        if any(left[0] == right[0] for left, right in zip(entries, entries[1:])):
            raise RuntimeError('duplicate normalized centroid index')
        total_entries = require_uint32(total_entries + len(entries),
                                       'total centroid entry count')
        languages.append((encoded_language, entries))
    languages.sort(key=lambda item: item[0])
    return tokens, icf_values, languages, total_entries


def decode_centroid_wire(encoded):
    """Independently decode generated bytes for the updater's round-trip gate."""
    offset = 0

    def take(size):
        nonlocal offset
        if not isinstance(size, int) or size < 0 or size > len(encoded) - offset:
            raise RuntimeError('truncated generated centroid wire data')
        value = encoded[offset:offset + size]
        offset += size
        return value

    def uint32():
        return struct.unpack('<I', take(4))[0]

    def float64():
        return struct.unpack('<d', take(8))[0]

    def text():
        return take(uint32()).decode('utf-8')

    if take(len(CENTROID_MAGIC)) != CENTROID_MAGIC:
        raise RuntimeError('generated centroid wire magic mismatch')
    version = uint32()
    source_sha = take(hashlib.sha256().digest_size).hex()
    vocabulary_count, language_count, entry_count = uint32(), uint32(), uint32()
    tokens = [text() for _ in range(vocabulary_count)]
    icf = [float64() for _ in range(vocabulary_count)]
    centroids = []
    decoded_entries = 0
    for _ in range(language_count):
        language = text()
        count = uint32()
        entries = [(uint32(), float64()) for _ in range(count)]
        decoded_entries += count
        centroids.append((language, entries))
    if decoded_entries != entry_count or offset != len(encoded):
        raise RuntimeError('generated centroid wire structure mismatch')
    return version, source_sha, tokens, icf, centroids


def encode_centroid_model(canonical_json, expected_source_sha=MODEL_SHA):
    source_sha = hashlib.sha256(canonical_json).hexdigest()
    if source_sha != expected_source_sha:
        raise RuntimeError('Linguist centroid model checksum mismatch')
    model = json.loads(canonical_json)
    tokens, icf, languages, total_entries = normalized_centroid_model(model)
    encoded = bytearray(CENTROID_MAGIC)
    encoded += struct.pack('<I', CENTROID_FORMAT_VERSION)
    encoded += bytes.fromhex(source_sha)
    encoded += struct.pack('<III', len(tokens), len(languages), total_entries)
    for token in tokens:
        encoded += struct.pack('<I', len(token)) + token
    for value in icf:
        encoded += struct.pack('<d', value)
    for language, entries in languages:
        encoded += struct.pack('<I', len(language)) + language
        encoded += struct.pack('<I', len(entries))
        for index, value in entries:
            encoded += struct.pack('<Id', index, value)

    decoded = decode_centroid_wire(encoded)
    decoded_version, decoded_source, decoded_tokens, decoded_icf, decoded_languages = decoded
    expected_languages = [
        (language.decode('utf-8'), entries) for language, entries in languages]
    if (decoded_version != CENTROID_FORMAT_VERSION or decoded_source != source_sha or
            decoded_tokens != [token.decode('utf-8') for token in tokens] or
            len(decoded_icf) != len(icf) or len(decoded_languages) != len(languages)):
        raise RuntimeError('generated centroid wire round-trip mismatch')
    for actual, expected in zip(decoded_icf, icf):
        if struct.pack('<d', actual) != struct.pack('<d', expected):
            raise RuntimeError('generated centroid ICF bit mismatch')
    for (actual_name, actual_entries), (expected_name, expected_entries) in zip(
            decoded_languages, expected_languages):
        if actual_name != expected_name or len(actual_entries) != len(expected_entries):
            raise RuntimeError('generated centroid language mismatch')
        for (actual_index, actual_value), (expected_index, expected_value) in zip(
                actual_entries, expected_entries):
            if (actual_index != expected_index or
                    struct.pack('<d', actual_value) != struct.pack('<d', expected_value)):
                raise RuntimeError('generated centroid entry bit mismatch')
    binary_sha = hashlib.sha256(encoded).hexdigest()
    metadata = {
        'format_version': CENTROID_FORMAT_VERSION,
        'source_sha256': source_sha,
        'binary_sha256': binary_sha,
        'vocabulary_count': len(tokens),
        'language_count': len(languages),
        'centroid_entry_count': total_entries,
    }
    return bytes(encoded), metadata


def centroid_model_go(metadata):
    return f'''// Code generated by third_party/update_enry.py; DO NOT EDIT.

package data

const CentroidModelFormatVersion uint32 = {metadata['format_version']}
const CentroidModelSourceSHA256 = "{metadata['source_sha256']}"
const CentroidModelBinarySHA256 = "{metadata['binary_sha256']}"
const CentroidModelVocabularyCount uint32 = {metadata['vocabulary_count']}
const CentroidModelLanguageCount uint32 = {metadata['language_count']}
const CentroidModelEntryCount uint32 = {metadata['centroid_entry_count']}
'''


def parse_generic_extensions(linguist):
    """Parse the deliberately simple pinned generic.yml without a YAML dependency."""
    source = linguist/'lib/linguist/generic.yml'
    extensions = []
    found_key = False
    for line in source.read_text().splitlines():
        value = line.strip()
        if not value or value.startswith('#') or value == '---':
            continue
        if value == 'extensions:':
            if found_key:
                raise RuntimeError('duplicate generic.yml extensions key')
            found_key = True
            continue
        if not found_key or not value.startswith('- '):
            raise RuntimeError(f'unsupported generic.yml syntax: {line!r}')
        try:
            extension = json.loads(value[2:])
        except json.JSONDecodeError as error:
            raise RuntimeError(f'invalid generic.yml extension: {line!r}') from error
        if (not isinstance(extension, str) or not extension.startswith('.') or
                extension in extensions):
            raise RuntimeError(f'invalid generic.yml extension: {extension!r}')
        extensions.append(extension)
    if not found_key or not extensions:
        raise RuntimeError('generic.yml has no extensions')
    return extensions


def generic_extensions_go(extensions):
    checks = ',\n\t\t'.join(
        f'strings.HasSuffix(filename, {json.dumps(extension)})'
        for extension in extensions)
    return f'''// Code generated by third_party/update_enry.py from Linguist generic.yml; DO NOT EDIT.

package data

import "strings"

// IsGenericExtension reports whether a lower- or mixed-case filename ends in
// an extension that Linguist requires another strategy to confirm.
func IsGenericExtension(filename string) bool {{
\tfilename = strings.ToLower(filename)
\tswitch {{
\tcase {checks}:
\t\treturn true
\tdefault:
\t\treturn false
\t}}
}}
'''


def centroid_migration_go_test():
    """Return the unpublished Go oracle used on every regenerated model."""
    return r'''package enry

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"testing"
)

func TestDircueGeneratedCentroidMigration(t *testing.T) {
	raw, err := os.ReadFile(".dircue-centroid-model.json")
	if err != nil {
		t.Fatal(err)
	}
	var source centroidModel
	if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&source); err != nil {
		t.Fatal(err)
	}
	if err := verifyCentroidModelSHA256(centroidModelBinary, generatedCentroidModelMetadata.binarySHA256); err != nil {
		t.Fatal(err)
	}
	generated, err := decodeCentroidModel(centroidModelBinary, generatedCentroidModelMetadata)
	if err != nil {
		t.Fatal(err)
	}
	if len(generated.Vocabulary) != len(source.Vocabulary) || len(generated.ICF) != len(source.ICF) || len(generated.Centroids) != len(source.Centroids) {
		t.Fatal("centroid section count mismatch")
	}
	for token, sourceIndex := range source.Vocabulary {
		generatedIndex, exists := generated.Vocabulary[token]
		if !exists || generatedIndex != sourceIndex {
			t.Fatalf("vocabulary mismatch for %q", token)
		}
	}
	for index, sourceValue := range source.ICF {
		if math.Float64bits(generated.ICF[index]) != math.Float64bits(sourceValue) {
			t.Fatalf("ICF bit mismatch at %d", index)
		}
	}
	for language, sourceCentroid := range source.Centroids {
		generatedCentroid, exists := generated.Centroids[language]
		if !exists || len(generatedCentroid) != len(sourceCentroid) {
			t.Fatalf("centroid mismatch for %q", language)
		}
		for index, sourceValue := range sourceCentroid {
			generatedValue, exists := generatedCentroid[index]
			if !exists || math.Float64bits(generatedValue) != math.Float64bits(sourceValue) {
				t.Fatalf("centroid bit mismatch for %q index %d", language, index)
			}
		}
	}
}
'''


def build_go_environment(temporary, inherited=None):
    inherited = os.environ if inherited is None else inherited
    environment = {key: value for key, value in inherited.items()
                   if not key.startswith(('GO', 'CGO_'))}
    environment.update(
        GO_CONTROLS,
        GOTOOLCHAIN=GO_VERSION,
        GOCACHE=str(temporary/'go-build-cache'),
        GOMODCACHE=str(temporary/'go-module-cache'),
    )
    return environment


def resolve_go(launcher, temporary):
    environment = build_go_environment(temporary)
    selection = dict(environment)
    selection.pop('GOMODCACHE')
    compiler_root = subprocess.check_output(
        [str(launcher), 'env', 'GOROOT'], cwd=temporary, env=selection,
        text=True).strip()
    compiler = Path(compiler_root)/'bin'/('go.exe' if os.name == 'nt' else 'go')
    formatter = Path(compiler_root)/'bin'/('gofmt.exe' if os.name == 'nt' else 'gofmt')
    environment['GOTOOLCHAIN'] = 'local'
    version = subprocess.check_output(
        [str(compiler), 'version'], cwd=temporary, env=environment,
        text=True).strip()
    if not version.startswith(f'go version {GO_VERSION} '):
        raise RuntimeError(f'generator requires {GO_VERSION}')
    if not formatter.is_file():
        raise RuntimeError(f'resolved {GO_VERSION} toolchain has no gofmt')
    return compiler, formatter, environment


def extract_linguist(archive, destination):
    expected_root = f'linguist-{LINGUIST_VERSION}'
    with tarfile.open(archive, 'r:gz') as tar:
        for member in tar:
            # GitHub codeload records the resolved Git object in every PAX
            # header, binding this tag archive to the immutable pinned ref.
            if member.pax_headers.get('comment') != LINGUIST_REF:
                raise RuntimeError('Linguist archive Git ref mismatch')
            if not member.isfile():
                continue
            path = PurePosixPath(member.name)
            if (not path.parts or path.parts[0] != expected_root or path.is_absolute() or
                    any(part in ('', '.', '..') for part in path.parts)):
                raise RuntimeError('unsafe upstream archive member')
            target = destination.joinpath(*path.parts[1:])
            target.parent.mkdir(parents=True, exist_ok=True)
            source = tar.extractfile(member)
            if source is None:
                raise RuntimeError('unreadable upstream archive member')
            target.write_bytes(source.read())


def validate_staged_output(candidate, manifest):
    manifest = validate_manifest(manifest)
    actual = set()
    for path in candidate.rglob('*'):
        if path.is_symlink():
            raise RuntimeError(f'symlink in staged generated output: {path}')
        if path.is_file():
            actual.add(path.relative_to(candidate).as_posix())
    expected = set(manifest) | STAGED_METADATA_FILES
    if actual != expected:
        raise RuntimeError('staged generated output does not match its manifest')
    for name, digest in manifest.items():
        if file_sha256(managed_path(candidate, name)) != digest:
            raise RuntimeError(f'staged generated file hash mismatch: {name}')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--archive', type=Path, help='Cached Linguist9.7 source archive (hash verified)')
    parser.add_argument('--model', type=Path, help='Cached JSON export of official Linguist Samples.cache (canonical model hash verified)')
    parser.add_argument('--image', default='dircue-linguist:9.7.0', help='Pinned reference image to export the centroid model')
    parser.add_argument('--output', type=Path, default=HERE/'go-enry')
    parser.add_argument('--go', default='go', help='Go launcher used to resolve the pinned compiler')
    args = parser.parse_args()

    output = output_path(args.output)
    output.parent.mkdir(parents=True, exist_ok=True)
    recover_publish(output)
    previous = read_previous_manifest(output)
    output_snapshot = preflight_output(output, previous)

    with tempfile.TemporaryDirectory(prefix='dircue-enry-generation-') as temporary_name:
        temporary = Path(temporary_name)
        compiler, formatter, go_env = resolve_go(args.go, temporary)
        module_cache = Path(go_env['GOMODCACHE'])
        (temporary/'go.mod').write_text(
            f'module dircue-enry-generation\n\ngo 1.26.6\n\nrequire {ENRY} {VERSION}\n')
        module = json.loads(run(
            [compiler, 'mod', 'download', '-json', ENRY+'@'+VERSION], temporary, go_env)[0])
        source = validate_module(module, module_cache)
        upstream_files = file_manifest(source)
        run([compiler, 'mod', 'verify'], temporary, go_env)
        stage = temporary/'enry'
        shutil.copytree(source, stage)
        stage.chmod(0o755)
        for path in stage.rglob('*'):
            path.chmod(0o755 if path.is_dir() else 0o644)

        archive = args.archive.resolve() if args.archive else temporary/'linguist.tar.gz'
        if not args.archive:
            urllib.request.urlretrieve(ARCHIVE_URL, archive)
        if file_sha256(archive) != ARCHIVE_SHA:
            raise RuntimeError('Linguist source archive checksum mismatch')
        linguist = stage/'.linguist'
        extract_linguist(archive, linguist)
        (linguist/'.git').mkdir(exist_ok=True)
        (linguist/'.git/HEAD').write_text(LINGUIST_REF+'\n')
        # Populate and verify the generator module's dependencies separately so
        # Go download notices cannot enter the generator-warning artifact.
        run([compiler, 'mod', 'download'], stage, go_env)
        run([compiler, 'mod', 'verify'], stage, go_env)
        _, warnings = run([compiler, 'run', 'internal/code-generator/main.go'], stage, go_env)
        (stage/'data/generic.go').write_text(
            generic_extensions_go(parse_generic_extensions(linguist)))
        patch = HERE/'patches/enry-linguist-9.7.patch'
        project_test_files = patched_test_files(patch)
        run(['git', 'apply', '--check', patch], stage)
        run(['git', 'apply', patch], stage)
        run([formatter, '-w', 'common.go', 'regex/standard.go', 'centroid.go',
             'data/generated.go', 'data/generic.go', 'internal/tokenizer/linguist.go',
             *project_test_files], stage)
        raw_model = (args.model.read_text() if args.model else run(
            ['docker', 'run', '--rm', '--network=none', args.image, 'ruby',
             '-rlinguist', '-rjson', '-e',
             'print JSON.generate(Linguist::Samples.cache)'])[0])
        source_model = json.loads(raw_model)
        model = json.dumps(
            {key: source_model[key] for key in ['vocabulary', 'icf', 'centroids']},
            separators=(',', ':'), sort_keys=True).encode()
        if hashlib.sha256(model).hexdigest() != MODEL_SHA:
            raise RuntimeError('Linguist centroid model checksum mismatch')
        centroid_binary, centroid_metadata = encode_centroid_model(model)
        (stage/'data/centroid_model.bin').write_bytes(centroid_binary)
        (stage/'data/centroid_model.go').write_text(
            centroid_model_go(centroid_metadata))
        run([formatter, '-w', 'data/centroid_model.go'], stage)
        files = [p for p in stage.glob('*.go') if not p.name.endswith('_test.go')]
        for directory in ['data', 'regex', 'internal/tokenizer']:
            files.extend(p for p in (stage/directory).rglob('*.go')
                         if not p.name.endswith('_test.go'))
        files = [path for path in files
                 if path.relative_to(stage) != Path('data/frequencies.go')]
        files.extend(stage/name for name in
                     ['go.mod', 'go.sum', 'LICENSE', 'data/centroid_model.bin',
                      *project_test_files])

        clean_warnings = '\n'.join(
            line[20:] if len(line) > 20 and line[4] == '/' and line[7] == '/' else line
            for line in warnings.splitlines()) + '\n'
        candidate, _ = publish_paths(output)
        candidate.mkdir()
        try:
            manifest = {}
            rewritten_import_files = []
            for path in sorted(files):
                relative = path.relative_to(stage)
                output_relative = relative
                if relative.as_posix() == 'go.mod':
                    output_relative = Path('upstream.go.mod')
                elif relative.as_posix() == 'go.sum':
                    output_relative = Path('upstream.go.sum')
                target = candidate/output_relative
                target.parent.mkdir(parents=True, exist_ok=True)
                content = path.read_bytes()
                upstream = source/relative
                if output_relative.name in ('upstream.go.mod', 'upstream.go.sum'):
                    # Retain the downloaded manifests byte for byte; the
                    # embedded package tree has no module of its own.
                    content = upstream.read_bytes()
                if path.suffix == '.go' and upstream.is_file() and content != upstream.read_bytes():
                    content = (b'// Modified for dircue; see PROVENANCE.json '
                               b'in this maintained fork.\n\n' + content)
                if path.suffix == '.go':
                    rewritten = rewrite_import_paths(content)
                    if rewritten != content:
                        rewritten_import_files.append(output_relative.as_posix())
                    content = rewritten
                target.write_bytes(content)
                manifest[output_relative.as_posix()] = file_sha256(target)
            shutil.copyfile(linguist/'LICENSE', candidate/'LINGUIST_LICENSE')
            manifest['LINGUIST_LICENSE'] = file_sha256(candidate/'LINGUIST_LICENSE')
            (candidate/'GENERATOR_WARNINGS.txt').write_text(clean_warnings)
            manifest['GENERATOR_WARNINGS.txt'] = file_sha256(
                candidate/'GENERATOR_WARNINGS.txt')
            migration_source = candidate/'.dircue-centroid-model.json'
            migration_test = candidate/'centroid_migration_generated_test.go'
            migration_source.write_bytes(model)
            migration_test.write_text(centroid_migration_go_test())
            run([formatter, '-w', migration_test.name], candidate)
            prepare_embedded_module(candidate)
            run([compiler, 'test', '.', '-run',
                 '^TestDircueGeneratedCentroidMigration$', '-count=1'],
                candidate, go_env)
            migration_test.unlink()
            migration_source.unlink()
            remove_embedded_module(candidate)
            provenance = {
                'enry_module': ENRY, 'enry_version': VERSION,
                'enry_sum': ENRY_SUM, 'enry_go_mod_sum': ENRY_GO_MOD_SUM,
                'generator_go_version': GO_VERSION,
                'linguist_version': LINGUIST_VERSION, 'linguist_ref': LINGUIST_REF,
                'linguist_archive_url': ARCHIVE_URL,
                'linguist_archive_sha256': ARCHIVE_SHA,
                'linguist_centroid_model_sha256': MODEL_SHA,
                'linguist_centroid_binary_format': CENTROID_FORMAT_VERSION,
                'linguist_centroid_binary_sha256': centroid_metadata['binary_sha256'],
                'patch_sha256': file_sha256(patch),
                'generator_script_sha256': file_sha256(Path(__file__)),
                'runtime_module': RUNTIME_MODULE,
                'module_path_rewrite': {'from': ENRY, 'to': RUNTIME_MODULE},
                'rewritten_import_files': rewritten_import_files,
                'upstream_files': upstream_files,
                'files': manifest,
            }
            (candidate/'PROVENANCE.json').write_text(json.dumps(provenance, indent=2)+'\n')
            validate_staged_output(candidate, manifest)
            prepare_embedded_module(candidate)
            run([compiler, 'test', './...'], candidate, go_env)
            remove_embedded_module(candidate)
            publish_tree(output, candidate, output_snapshot)
        except BaseException:
            if candidate.exists():
                shutil.rmtree(candidate)
            raise
        print(f'Generated {len(manifest)} pinned source files in {output}')


if __name__ == '__main__':
    main()
