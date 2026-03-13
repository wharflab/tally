from __future__ import annotations

import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from generate_winget_manifests import (
    PACKAGE_IDENTIFIER,
    manifest_dir,
    normalized_version,
    read_checksums,
    render_default_locale_manifest,
    render_installer_manifest,
    render_version_manifest,
)


class GenerateWingetManifestsTest(unittest.TestCase):
    def test_normalized_version(self) -> None:
        self.assertEqual(normalized_version("v0.26.0"), "0.26.0")
        self.assertEqual(normalized_version("0.26.0"), "0.26.0")

    def test_read_checksums(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            path = Path(tmpdir) / "tally_checksums.txt"
            path.write_text(
                "ABCDEF tally_0.26.0_Windows_x86_64.zip\n"
                "123456 tally_0.26.0_Windows_arm64.zip\n",
                encoding="utf-8",
            )
            self.assertEqual(
                read_checksums(path),
                {
                    "tally_0.26.0_Windows_x86_64.zip": "ABCDEF",
                    "tally_0.26.0_Windows_arm64.zip": "123456",
                },
            )

    def test_manifest_dir(self) -> None:
        root = Path("/tmp/manifests")
        self.assertEqual(
            manifest_dir(root, PACKAGE_IDENTIFIER, "0.26.0"),
            Path("/tmp/manifests/w/Wharflab/Tally/0.26.0"),
        )

    def test_rendered_manifests_include_expected_release_data(self) -> None:
        version_manifest = render_version_manifest("0.26.0")
        self.assertIn("PackageIdentifier: Wharflab.Tally", version_manifest)
        self.assertIn("DefaultLocale: en-US", version_manifest)

        locale_manifest = render_default_locale_manifest("0.26.0", "wharflab", "tally")
        self.assertIn("Publisher: Wharflab", locale_manifest)
        self.assertIn(
            "ReleaseNotesUrl: https://github.com/wharflab/tally/releases/tag/v0.26.0",
            locale_manifest,
        )

        installer_manifest = render_installer_manifest(
            "0.26.0",
            "wharflab",
            "tally",
            {
                "tally_0.26.0_Windows_x86_64.zip": "ABCDEF",
                "tally_0.26.0_Windows_arm64.zip": "123456",
            },
        )
        self.assertIn("InstallerType: zip", installer_manifest)
        self.assertIn("NestedInstallerType: portable", installer_manifest)
        self.assertIn("PortableCommandAlias: tally", installer_manifest)
        self.assertIn(
            "InstallerUrl: https://github.com/wharflab/tally/releases/download/v0.26.0/tally_0.26.0_Windows_x86_64.zip",
            installer_manifest,
        )


if __name__ == "__main__":
    unittest.main()
