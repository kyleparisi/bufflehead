"""Fail-closed packaging checks; no SDK, signing key, or real app required."""
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
LOCK = json.loads((ROOT / "packaging/duckdb.json").read_text())


class LockedEngineTests(unittest.TestCase):
    def reject_engine(self, change, expected):
        with tempfile.TemporaryDirectory() as tmp:
            base = Path(tmp)
            app = base / "Example.app"
            (app / "Contents").mkdir(parents=True)
            (app / "Contents/Info.plist").write_text("test")
            library = app / "Contents/test.dylib"
            library.write_bytes(b"original exported library")
            engine = base / "engine"
            engine.mkdir()
            archive = engine / "libduckdb.a"
            archive.write_bytes(b"test archive, never linked")
            provenance = {
                "schema": 2, "version": LOCK["version"],
                "source_commit": LOCK["source_commit"], "architecture": "arm64",
                "clean_source_build": True, "disable_extension_load": True,
                "static_extension_loader": True, "extensions": LOCK["extensions"],
                "sha256": hashlib.sha256(archive.read_bytes()).hexdigest(),
            }
            provenance.update(change)
            (engine / "provenance.json").write_text(json.dumps(provenance))
            result = subprocess.run([
                str(ROOT / "bin/prepare-mas-bundle"), "--app", str(app),
                "--arch", "arm64", "--duckdb", str(engine),
                "--library-path", "Contents/test.dylib",
            ], capture_output=True, text=True, timeout=10)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn(expected, result.stderr)
            self.assertEqual(library.read_bytes(), b"original exported library")

    def test_old_engine_rejected(self):
        self.reject_engine({"source_commit": "19864453f7d0ed095256d848b46e7b8630989bac"}, "locked static-loader")

    def test_missing_loader_rejected(self):
        self.reject_engine({"static_extension_loader": False}, "locked static-loader")

    def test_unapproved_extensions_rejected(self):
        self.reject_engine({"extensions": LOCK["extensions"] + ["httpfs"]}, "locked static-loader")

    def test_extension_loading_rejected(self):
        self.reject_engine({"disable_extension_load": False}, "clean locked-engine")

    def test_archive_tampering_rejected(self):
        self.reject_engine({"sha256": "0" * 64}, "checksum mismatch")

    def test_workflow_pin_matches_source_lock(self):
        workflow = (ROOT / ".github/workflows/build-mas.yml").read_text()
        self.assertIn("DUCKDB_COMMIT: " + LOCK["source_commit"], workflow)


if __name__ == "__main__":
    unittest.main()
