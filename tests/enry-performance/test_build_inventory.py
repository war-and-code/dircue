"""Receipt integrity checks; uses tiny local fixtures, never Go or the network."""
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

spec=importlib.util.spec_from_file_location('enry_builder',Path(__file__).with_name('build.py'))
builder=importlib.util.module_from_spec(spec);spec.loader.exec_module(builder)


class ForkInventoryTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
        self.parent=Path(self.temp.name);self.fork=self.parent/'go-enry';(self.fork/'data').mkdir(parents=True)
        (self.parent/'patches').mkdir();(self.parent/'update_enry.py').write_bytes(b'generator')
        (self.parent/'patches/enry-linguist-9.7.patch').write_bytes(b'patch')
        for name,data in {'main.go':b'package enry','data/model.bin':b'\0\xffmodel','data/legacy.json.gz':b'compressed','README.md':b'notes'}.items():
            (self.fork/name).write_bytes(data)
        self.regenerate()

    def regenerate(self):
        self.manifest={'files':{n:builder.digest(self.fork/n) for n in ['main.go','data/model.bin','data/legacy.json.gz']},
            'generator_script_sha256':builder.digest(self.parent/'update_enry.py'),
            'patch_sha256':builder.digest(self.parent/'patches/enry-linguist-9.7.patch')}
        self.save()

    def save(self):
        (self.fork/'PROVENANCE.json').write_text(json.dumps(self.manifest))

    def test_all_embedded_formats_are_included(self):
        inventory=builder.verified_fork_inventory(self.fork)
        self.assertEqual(set(inventory),{'main.go','data/model.bin','data/legacy.json.gz','README.md','PROVENANCE.json'})
        self.assertEqual(inventory['data/model.bin'],hashlib.sha256(b'\0\xffmodel').hexdigest())
        self.assertEqual(builder.unchanged_fork(self.fork,inventory),inventory)

    def test_binary_mutation_is_rejected(self):
        (self.fork/'data/model.bin').write_bytes(b'changed model')
        with self.assertRaisesRegex(RuntimeError,'differs from provenance'):
            builder.verified_fork_inventory(self.fork)

    def test_new_embedded_file_is_rejected(self):
        (self.fork/'data/extra.bin').write_bytes(b'new model')
        with self.assertRaisesRegex(RuntimeError,'unmanaged'):
            builder.verified_fork_inventory(self.fork)

    def test_validly_regenerated_change_during_build_is_rejected(self):
        before=builder.verified_fork_inventory(self.fork)
        (self.fork/'data/model.bin').write_bytes(b'new valid model');self.regenerate()
        with self.assertRaisesRegex(RuntimeError,'changed during build'):
            builder.unchanged_fork(self.fork,before)

    def test_symlink_source_is_rejected(self):
        (self.fork/'data/link.bin').symlink_to('model.bin')
        with self.assertRaisesRegex(RuntimeError,'nonregular'):
            builder.verified_fork_inventory(self.fork)

    def test_parent_path_in_manifest_is_rejected(self):
        self.manifest['files']['../update_enry.py']=builder.digest(self.parent/'update_enry.py');self.save()
        with self.assertRaisesRegex(RuntimeError,'unsafe'):
            builder.verified_fork_inventory(self.fork)

    def test_generation_input_change_is_rejected(self):
        (self.parent/'patches/enry-linguist-9.7.patch').write_bytes(b'changed patch')
        with self.assertRaisesRegex(RuntimeError,'generation input'):
            builder.verified_fork_inventory(self.fork)


if __name__=='__main__':unittest.main()
