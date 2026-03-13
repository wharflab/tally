import tempfile
import unittest
from pathlib import Path
import sys
import types


if "hatchling.builders.hooks.plugin.interface" not in sys.modules:
    hatchling = types.ModuleType("hatchling")
    builders = types.ModuleType("hatchling.builders")
    hooks = types.ModuleType("hatchling.builders.hooks")
    plugin = types.ModuleType("hatchling.builders.hooks.plugin")
    builder_interface = types.ModuleType("hatchling.builders.hooks.plugin.interface")
    metadata = types.ModuleType("hatchling.metadata")
    metadata_plugin = types.ModuleType("hatchling.metadata.plugin")
    metadata_interface = types.ModuleType("hatchling.metadata.plugin.interface")

    class BuildHookInterface:
        def __init__(self, *args, **kwargs):
            pass

    class MetadataHookInterface:
        pass

    builder_interface.BuildHookInterface = BuildHookInterface
    metadata_interface.MetadataHookInterface = MetadataHookInterface

    sys.modules["hatchling"] = hatchling
    sys.modules["hatchling.builders"] = builders
    sys.modules["hatchling.builders.hooks"] = hooks
    sys.modules["hatchling.builders.hooks.plugin"] = plugin
    sys.modules["hatchling.builders.hooks.plugin.interface"] = builder_interface
    sys.modules["hatchling.metadata"] = metadata
    sys.modules["hatchling.metadata.plugin"] = metadata_plugin
    sys.modules["hatchling.metadata.plugin.interface"] = metadata_interface

sys.path.insert(0, str(Path(__file__).parent))

from hatch_build import CustomBuildHook


class CustomBuildHookTest(unittest.TestCase):
    def test_stage_target_binary_replaces_previous_target(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            repo_root = Path(temp_dir) / "repo"
            package_root = repo_root / "packaging" / "pypi"
            bin_dir = package_root / "tally_cli" / "bin"
            bin_dir.mkdir(parents=True)

            stale_dir = bin_dir / "tally-linux-x86_64"
            stale_dir.mkdir()
            (stale_dir / "stale").write_text("old")

            linux_source = repo_root / "dist" / "tally_linux_amd64_v1" / "tally"
            linux_source.parent.mkdir(parents=True)
            linux_source.write_text("linux")

            windows_source = (
                repo_root / "dist" / "tally_windows_arm64_v8.0" / "tally.exe"
            )
            windows_source.parent.mkdir(parents=True)
            windows_source.write_text("windows")

            hook = object.__new__(CustomBuildHook)
            hook.root = str(package_root)
            hook.target_platform = "linux"
            hook.target_arch = "x86_64"

            hook._stage_target_binary()
            self.assertTrue((bin_dir / "tally-linux-x86_64" / "tally").is_file())
            self.assertFalse((bin_dir / "tally-linux-x86_64" / "stale").exists())

            hook.target_platform = "windows"
            hook.target_arch = "arm64"
            hook._stage_target_binary()

            self.assertFalse((bin_dir / "tally-linux-x86_64").exists())
            self.assertTrue((bin_dir / "tally-windows-arm64" / "tally.exe").is_file())


if __name__ == "__main__":
    unittest.main()
