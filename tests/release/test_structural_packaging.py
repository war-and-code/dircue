"""Structural archive overwrite and source-snapshot checks; no compiler required."""
import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location("structural_packager", ROOT / "scripts/structural_worker_release.py")
worker = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(worker)


class StructuralPackagingTests(unittest.TestCase):
    def test_hotspot_source_enters_recursive_provenance(self):
        key = worker.WORKER.relative_to(worker.ROOT).as_posix() + "/src/hotspots.rs"
        self.assertEqual(worker.digest(worker.WORKER / "src/hotspots.rs"), worker.source_hashes()[key])

    def test_existing_archive_or_checksum_is_never_replaced(self):
        for platform in worker.TARGETS:
            with self.subTest(platform=platform), tempfile.TemporaryDirectory() as temporary:
                output = Path(temporary)
                archive, checksum = worker.artifact_paths(output, "0.3.0", platform)
                self.assertEqual(archive.suffix, ".zip" if platform.startswith("windows") else ".gz")
                for existing in (archive, checksum):
                    existing.write_bytes(b"preserve these bytes")
                    with self.assertRaises(FileExistsError):
                        worker.artifact_paths(output, "0.3.0", platform)
                    self.assertEqual(existing.read_bytes(), b"preserve these bytes")
                    existing.unlink()

    def test_staged_sources_must_match_build_snapshot(self):
        prefix = worker.WORKER.relative_to(worker.ROOT).as_posix()
        with tempfile.TemporaryDirectory() as temporary:
            stage = Path(temporary)
            entries = {
                f"{prefix}/Cargo.toml": "source/Cargo.toml",
                f"{prefix}/Cargo.lock": "source/Cargo.lock",
                f"{prefix}/src/main.rs": "source/src/main.rs",
                f"{prefix}/src/hotspots.rs": "source/src/hotspots.rs",
                "LICENSE": "LICENSE",
                "docs/STRUCTURE.md": "README.md",
            }
            expected = {}
            for original, packaged in entries.items():
                target = stage / packaged
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_text(original)
                expected[original] = worker.digest(target)
            with mock.patch.object(worker, "source_hashes", return_value=expected):
                worker.verify_staged_sources(stage, expected)
                # Even if original inputs are restored after an edit during copying,
                # the changed staged copy must fail the original snapshot check.
                (stage / "source/src/main.rs").write_text("changed during copying")
                with self.assertRaisesRegex(RuntimeError, "packaged source differs"):
                    worker.verify_staged_sources(stage, expected)
            with mock.patch.object(worker, "source_hashes", return_value={}):
                with self.assertRaisesRegex(RuntimeError, "source inputs changed"):
                    worker.verify_staged_sources(stage, expected)


if __name__ == "__main__":
    unittest.main()
