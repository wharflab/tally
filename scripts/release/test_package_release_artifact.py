from __future__ import annotations

import subprocess
import tempfile
import unittest
import zipfile
from pathlib import Path


class PackageReleaseArtifactTest(unittest.TestCase):
    def test_windows_build_emits_zip_and_exe_assets(self) -> None:
        repo_root = Path(__file__).resolve().parents[2]
        with tempfile.TemporaryDirectory() as tmpdir:
            tmp = Path(tmpdir)
            dist_root = tmp / "dist"
            platform_dir = dist_root / "tally_windows_amd64_v1"
            platform_dir.mkdir(parents=True)
            (platform_dir / "tally.exe").write_bytes(b"MZ")

            subprocess.run(
                [
                    "python",
                    str(repo_root / "scripts" / "release" / "package_release_artifact.py"),
                    "--version",
                    "0.26.0",
                    "--goos",
                    "windows",
                    "--goarch",
                    "amd64",
                    "--variant",
                    "v1",
                    "--binary-name",
                    "tally.exe",
                    "--archive-os",
                    "Windows",
                    "--archive-arch",
                    "x86_64",
                    "--dist-root",
                    str(dist_root),
                ],
                cwd=repo_root,
                check=True,
            )

            zip_path = dist_root / "tally_0.26.0_Windows_x86_64.zip"
            exe_path = dist_root / "tally_0.26.0_Windows_x86_64.exe"
            self.assertTrue(zip_path.exists())
            self.assertTrue(exe_path.exists())
            self.assertEqual(exe_path.read_bytes(), b"MZ")
            with zipfile.ZipFile(zip_path) as zf:
                self.assertEqual(
                    sorted(zf.namelist()),
                    ["LICENSE", "NOTICE", "tally.exe"],
                )


if __name__ == "__main__":
    unittest.main()
