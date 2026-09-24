import hashlib
import importlib.util
import io
import json
import math
import os
from pathlib import Path
import tarfile
import tempfile
import struct
import unittest
from unittest import mock


ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location(
    'update_enry', ROOT/'third_party/update_enry.py')
UPDATE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(UPDATE)


def digest(content):
    return hashlib.sha256(content).hexdigest()


class ManifestTests(unittest.TestCase):
    def test_rewrites_only_enry_import_paths_and_keeps_attribution(self):
        source = (b'package enry // import "github.com/go-enry/go-enry/v2"\n'
                  b'import "github.com/go-enry/go-enry/v2/data"\n'
                  b'import (\n\t"github.com/go-enry/go-enry/v2/internal/tokenizer"\n)\n'
                  b'// github.com/go-enry/go-enry/v2 appears in this attribution.\n')
        actual = UPDATE.rewrite_import_paths(source)
        self.assertIn(b'package enry // import "github.com/war-and-code/dircue/third_party/go-enry"', actual)
        self.assertIn(b'import "github.com/war-and-code/dircue/third_party/go-enry/data"', actual)
        self.assertIn(b'"github.com/war-and-code/dircue/third_party/go-enry/internal/tokenizer"', actual)
        self.assertIn(b'// github.com/go-enry/go-enry/v2 appears in this attribution.', actual)

    def test_embedded_test_manifest_preserves_upstream_module_files(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root/'upstream.go.mod').write_text(
                f'module {UPDATE.ENRY}\n\ngo 1.24.0\n')
            (root/'upstream.go.sum').write_text('example.test/module v1.0.0 h1:abc=\n')
            UPDATE.prepare_embedded_module(root)
            # The temporary test module uses the root module's Go version;
            # the retained upstream manifest itself is left untouched.
            self.assertEqual((root/'go.mod').read_text(),
                             f'module {UPDATE.RUNTIME_MODULE}\n\ngo {UPDATE.GO_VERSION.removeprefix("go")}\n')
            self.assertEqual((root/'upstream.go.mod').read_text(),
                             f'module {UPDATE.ENRY}\n\ngo 1.24.0\n')
            self.assertEqual((root/'go.sum').read_text(),
                             (root/'upstream.go.sum').read_text())
            UPDATE.remove_embedded_module(root)
            self.assertFalse((root/'go.mod').exists())
            self.assertFalse((root/'go.sum').exists())

    def test_accepts_normalized_relative_paths_and_hashes(self):
        value = {'data/generated.go': 'a' * 64}
        self.assertEqual(UPDATE.validate_manifest(value), value)

    def test_rejects_paths_outside_generated_tree(self):
        invalid = ['../victim', 'data/../../victim', '/absolute',
                   'data//file', 'data/./file', r'data\file', '.', '']
        for name in invalid:
            with self.subTest(name=name):
                with self.assertRaises(RuntimeError):
                    UPDATE.validate_manifest({name: 'a' * 64})

    def test_rejects_invalid_manifest_shape_and_digest(self):
        for value in ([], {'file': 7}, {'file': 'A' * 64}, {'file': 'abc'}):
            with self.subTest(value=value):
                with self.assertRaises(RuntimeError):
                    UPDATE.validate_manifest(value)

    def test_derives_all_patched_tests_from_file_headers(self):
        patch = '''diff --git a/common.go b/common.go
--- a/common.go
+++ b/common.go
@@ -1 +1 @@
-old
+++ b/not-a-real-header_test.go
diff --git a/auragaze_compat_test.go b/auragaze_compat_test.go
--- a/auragaze_compat_test.go
+++ b/auragaze_compat_test.go
@@ -1 +1 @@
 package enry
diff --git a/internal/tokenizer/identifier_test.go b/internal/tokenizer/identifier_test.go
--- /dev/null
+++ b/internal/tokenizer/identifier_test.go
@@ -0,0 +1 @@
+package tokenizer
diff --git a/removed_test.go b/removed_test.go
--- a/removed_test.go
+++ /dev/null
'''
        with tempfile.TemporaryDirectory() as temporary:
            filename = Path(temporary)/'change.patch'
            filename.write_text(patch)
            self.assertEqual(
                UPDATE.patched_test_files(filename),
                ['auragaze_compat_test.go',
                 'internal/tokenizer/identifier_test.go'])

    def test_rejects_unsafe_or_missing_patched_test_headers(self):
        patches = [
            '''diff --git a/x b/../escape_test.go
--- /dev/null
+++ b/../escape_test.go
''',
            '''diff --git a/common.go b/common.go
--- a/common.go
+++ b/common.go
''',
        ]
        for content in patches:
            with self.subTest(content=content):
                with tempfile.TemporaryDirectory() as temporary:
                    filename = Path(temporary)/'change.patch'
                    filename.write_text(content)
                    with self.assertRaises(RuntimeError):
                        UPDATE.patched_test_files(filename)

    def test_preflight_checks_retained_managed_files(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)/'fork'
            output.mkdir()
            content = b'locally edited'
            (output/'managed.go').write_bytes(content)
            (output/'PROVENANCE.json').write_text('{}')
            with self.assertRaisesRegex(RuntimeError, 'locally modified'):
                UPDATE.preflight_output(output, {'managed.go': digest(b'original')})
            self.assertEqual((output/'managed.go').read_bytes(), content)

    def test_preflight_rejects_unmanaged_files_and_directories(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)/'fork'
            output.mkdir()
            (output/'extra').write_text('unexpected')
            with self.assertRaisesRegex(RuntimeError, 'unmanaged'):
                UPDATE.preflight_output(output, {})
            (output/'extra').unlink()
            (output/'empty').mkdir()
            with self.assertRaisesRegex(RuntimeError, 'unmanaged directory'):
                UPDATE.preflight_output(output, {})

    def test_preflight_rejects_symlinks(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)/'fork'
            output.mkdir()
            try:
                os.symlink('missing', output/'link')
            except OSError as error:
                self.skipTest(f'host cannot create test symlink: {error}')
            with self.assertRaisesRegex(RuntimeError, 'symlink'):
                UPDATE.preflight_output(output, {})


class PublishTests(unittest.TestCase):
    def make_tree(self, root, name, content):
        path = root/name
        path.mkdir()
        (path/'value').write_text(content)
        return path

    def test_publish_replaces_complete_directory(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            output = self.make_tree(root, 'fork', 'old')
            candidate, _ = UPDATE.publish_paths(output)
            candidate.mkdir()
            (candidate/'value').write_text('new')
            UPDATE.publish_tree(output, candidate, UPDATE.snapshot_output(output))
            self.assertEqual((output/'value').read_text(), 'new')
            self.assertFalse(UPDATE.publish_paths(output)[1].exists())

    def test_failed_second_rename_restores_old_tree(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            output = self.make_tree(root, 'fork', 'old')
            candidate, _ = UPDATE.publish_paths(output)
            candidate.mkdir()
            (candidate/'value').write_text('new')
            real_replace = os.replace
            calls = 0

            def replace(source, target):
                nonlocal calls
                calls += 1
                if calls == 2:
                    raise OSError('injected publish failure')
                return real_replace(source, target)

            with mock.patch.object(UPDATE.os, 'replace', side_effect=replace):
                with self.assertRaisesRegex(OSError, 'injected'):
                    UPDATE.publish_tree(output, candidate, UPDATE.snapshot_output(output))
            self.assertEqual((output/'value').read_text(), 'old')

    def test_publish_refuses_late_output_change(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            output = self.make_tree(root, 'fork', 'old')
            before = UPDATE.snapshot_output(output)
            candidate, _ = UPDATE.publish_paths(output)
            candidate.mkdir()
            (candidate/'value').write_text('new')
            (output/'value').write_text('late edit')
            with self.assertRaisesRegex(RuntimeError, 'changed while'):
                UPDATE.publish_tree(output, candidate, before)
            self.assertEqual((output/'value').read_text(), 'late edit')
            self.assertEqual((candidate/'value').read_text(), 'new')

    def test_recovery_restores_unambiguously_interrupted_swap(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            output = self.make_tree(root, 'fork', 'old')
            candidate, backup = UPDATE.publish_paths(output)
            candidate.mkdir()
            (candidate/'value').write_text('new')
            os.replace(output, backup)
            with self.assertRaisesRegex(RuntimeError, 'restored'):
                UPDATE.recover_publish(output)
            self.assertEqual((output/'value').read_text(), 'old')
            self.assertFalse(candidate.exists())

    def test_recovery_refuses_ambiguous_two_tree_state(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            output = self.make_tree(root, 'fork', 'new')
            _, backup = UPDATE.publish_paths(output)
            backup.mkdir()
            (backup/'value').write_text('old')
            with self.assertRaisesRegex(RuntimeError, 'ambiguous'):
                UPDATE.recover_publish(output)
            self.assertEqual((output/'value').read_text(), 'new')
            self.assertEqual((backup/'value').read_text(), 'old')


class SourcePinTests(unittest.TestCase):
    def test_go_environment_discards_poisoned_inherited_configuration(self):
        with tempfile.TemporaryDirectory() as temporary:
            environment = UPDATE.build_go_environment(
                Path(temporary),
                {'PATH': '/safe/bin', 'HOME': '/home/test', 'GOWORK': '/evil',
                 'GOENV': '/evil/env', 'GOFLAGS': '-mod=mod', 'GOROOT': '/evil/go',
                 'GOPRIVATE': 'evil.example', 'CGO_CFLAGS': '-DPOISON'})
            self.assertEqual(environment['PATH'], '/safe/bin')
            self.assertEqual(environment['HOME'], '/home/test')
            self.assertEqual(environment['GOWORK'], 'off')
            self.assertEqual(environment['GOENV'], 'off')
            self.assertEqual(environment['GOFLAGS'], '')
            self.assertEqual(environment['GOPRIVATE'], '')
            self.assertEqual(environment['CGO_ENABLED'], '0')
            self.assertNotIn('GOROOT', environment)
            self.assertNotIn('CGO_CFLAGS', environment)
            self.assertEqual(environment['GOTOOLCHAIN'], UPDATE.GO_VERSION)

    def test_module_identity_and_sums_are_pinned(self):
        with tempfile.TemporaryDirectory() as temporary:
            cache = Path(temporary)/'cache'
            source = cache/'github.com/go-enry/go-enry/v2@v2.9.6'
            source.mkdir(parents=True)
            module = {'Path': UPDATE.ENRY, 'Version': UPDATE.VERSION,
                      'Sum': UPDATE.ENRY_SUM,
                      'GoModSum': UPDATE.ENRY_GO_MOD_SUM, 'Dir': str(source)}
            self.assertEqual(UPDATE.validate_module(module, cache), source.resolve())
            for field in ('Path', 'Version', 'Sum', 'GoModSum'):
                changed = dict(module)
                changed[field] = 'wrong'
                with self.subTest(field=field):
                    with self.assertRaises(RuntimeError):
                        UPDATE.validate_module(changed, cache)

    def archive(self, path, comment):
        content = b'MIT license\n'
        with tarfile.open(path, 'w:gz') as archive:
            member = tarfile.TarInfo(
                f'linguist-{UPDATE.LINGUIST_VERSION}/LICENSE')
            member.size = len(content)
            member.pax_headers = {'comment': comment}
            archive.addfile(member, io.BytesIO(content))

    def test_archive_pax_header_binds_tag_to_pinned_git_ref(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            archive = root/'source.tar.gz'
            self.archive(archive, UPDATE.LINGUIST_REF)
            destination = root/'source'
            UPDATE.extract_linguist(archive, destination)
            self.assertEqual((destination/'LICENSE').read_bytes(), b'MIT license\n')

    def test_archive_rejects_different_git_ref(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            archive = root/'source.tar.gz'
            self.archive(archive, '0' * 40)
            with self.assertRaisesRegex(RuntimeError, 'Git ref mismatch'):
                UPDATE.extract_linguist(archive, root/'source')


class GenericExtensionsTests(unittest.TestCase):
    EXTENSIONS = [
        '.1', '.2', '.3', '.4', '.5', '.6', '.7', '.8', '.9',
        '.action', '.alg', '.app', '.cmp', '.msg', '.network', '.resource',
        '.sd', '.sol', '.srv', '.stl', '.tag', '.target', '.url',
    ]

    def source(self, root, lines):
        directory = root/'lib/linguist'
        directory.mkdir(parents=True)
        (directory/'generic.yml').write_text(
            '# pinned fixture\n---\nextensions:\n' + ''.join(lines))

    def test_parses_and_generates_all_pinned_extensions(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self.source(root, [f'- {json.dumps(value)}\n'
                               for value in self.EXTENSIONS])
            parsed = UPDATE.parse_generic_extensions(root)
            self.assertEqual(parsed, self.EXTENSIONS)
            self.assertEqual(len(parsed), 23)
            generated = UPDATE.generic_extensions_go(parsed)
            for extension in self.EXTENSIONS:
                self.assertIn(f'strings.HasSuffix(filename, {json.dumps(extension)})',
                              generated)

    def test_rejects_duplicate_malformed_and_unsupported_syntax(self):
        invalid = [
            ['- ".app"\n', '- ".app"\n'],
            ['- "app"\n'],
            ['- not-json\n'],
            ['nested:\n'],
        ]
        for lines in invalid:
            with self.subTest(lines=lines):
                with tempfile.TemporaryDirectory() as temporary:
                    root = Path(temporary)
                    self.source(root, lines)
                    with self.assertRaises(RuntimeError):
                        UPDATE.parse_generic_extensions(root)


class CentroidWireTests(unittest.TestCase):
    def canonical_model(self):
        return json.dumps({
            'vocabulary': {'beta': 1, 'alpha': 0},
            'icf': [-0.0, 1.25],
            'centroids': {
                'Zulu': {'1': 0.5},
                'Alpha': {'1': 2.0, '0': math.ldexp(1.0, -1074)},
            },
        }, separators=(',', ':'), sort_keys=True).encode()

    def test_encoding_is_deterministic_and_bit_exact(self):
        source = self.canonical_model()
        source_sha = digest(source)
        first, metadata = UPDATE.encode_centroid_model(source, source_sha)
        second, repeated = UPDATE.encode_centroid_model(source, source_sha)
        self.assertEqual(first, second)
        self.assertEqual(metadata, repeated)
        self.assertEqual(digest(first), metadata['binary_sha256'])
        version, decoded_sha, tokens, icf, languages = UPDATE.decode_centroid_wire(first)
        self.assertEqual(version, UPDATE.CENTROID_FORMAT_VERSION)
        self.assertEqual(decoded_sha, source_sha)
        self.assertEqual(tokens, ['alpha', 'beta'])
        self.assertEqual([struct.pack('<d', value) for value in icf],
                         [struct.pack('<d', -0.0), struct.pack('<d', 1.25)])
        self.assertEqual([name for name, _ in languages], ['Alpha', 'Zulu'])
        self.assertEqual(struct.pack('<d', languages[0][1][0][1]),
                         struct.pack('<d', math.ldexp(1.0, -1074)))
        self.assertEqual(metadata['vocabulary_count'], 2)
        self.assertEqual(metadata['language_count'], 2)
        self.assertEqual(metadata['centroid_entry_count'], 3)
        generated = UPDATE.centroid_model_go(metadata)
        self.assertIn(metadata['source_sha256'], generated)
        self.assertIn(metadata['binary_sha256'], generated)

    def test_source_hash_is_required(self):
        source = self.canonical_model()
        with self.assertRaisesRegex(RuntimeError, 'checksum'):
            UPDATE.encode_centroid_model(source, '0' * 64)

    def test_rejects_invalid_schema_indices_and_numbers(self):
        models = [
            {'vocabulary': {}, 'icf': []},
            {'vocabulary': {'a': 1}, 'icf': [1], 'centroids': {}},
            {'vocabulary': {'a': 0}, 'icf': [], 'centroids': {}},
            {'vocabulary': {'a': 0}, 'icf': [math.inf], 'centroids': {}},
            {'vocabulary': {'a': 0}, 'icf': [1], 'centroids': {'A': {'1': 1}}},
            {'vocabulary': {'a': 0, 'b': 1}, 'icf': [1, 1],
             'centroids': {'A': {'01': 1, '1': 2}}},
        ]
        for model in models:
            with self.subTest(model=model):
                source = json.dumps(model, separators=(',', ':'), sort_keys=True).encode()
                with self.assertRaises(RuntimeError):
                    UPDATE.encode_centroid_model(source, digest(source))

    def test_independent_decoder_rejects_truncation_and_trailing_data(self):
        source = self.canonical_model()
        encoded, _ = UPDATE.encode_centroid_model(source, digest(source))
        for broken in (encoded[:-1], encoded + b'\x00'):
            with self.subTest(size=len(broken)):
                with self.assertRaises(RuntimeError):
                    UPDATE.decode_centroid_wire(broken)

    def test_per_refresh_go_oracle_checks_mapping_presence_and_float_bits(self):
        source = UPDATE.centroid_migration_go_test()
        self.assertIn('generatedIndex, exists := generated.Vocabulary[token]', source)
        self.assertIn('generatedValue, exists := generatedCentroid[index]', source)
        self.assertIn('math.Float64bits(generatedValue)', source)


if __name__ == '__main__':
    unittest.main()
