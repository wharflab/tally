import os
import platform
import shutil
import sys
from pathlib import Path

from hatchling.builders.hooks.plugin.interface import BuildHookInterface
from hatchling.metadata.plugin.interface import MetadataHookInterface


PLATFORM_MAPPING = {
    "linux": "linux",
    "linux2": "linux",
    "darwin": "darwin",
    "win32": "windows",
    "windows": "windows",
    "freebsd": "freebsd",
}

ARCH_MAPPING = {
    "x86_64": "x86_64",
    "amd64": "x86_64",
    "arm64": "arm64",
    "aarch64": "arm64",
}


PEP425_TAGS = {
    ("linux", "x86_64"): "py3-none-manylinux_2_17_x86_64",
    ("linux", "arm64"): "py3-none-manylinux_2_17_aarch64",
    ("darwin", "x86_64"): "py3-none-macosx_10_15_x86_64",
    ("darwin", "arm64"): "py3-none-macosx_11_0_arm64",
    ("windows", "x86_64"): "py3-none-win_amd64",
    ("windows", "arm64"): "py3-none-win_arm64",
}


DIST_BINARY_MAPPING = {
    ("linux", "x86_64"): ("linux", "amd64", "v1", ""),
    ("linux", "arm64"): ("linux", "arm64", "v8.0", ""),
    ("darwin", "x86_64"): ("darwin", "amd64", "v1", ""),
    ("darwin", "arm64"): ("darwin", "arm64", "v8.0", ""),
    ("windows", "x86_64"): ("windows", "amd64", "v1", ".exe"),
    ("windows", "arm64"): ("windows", "arm64", "v8.0", ".exe"),
}


VERSION_ENV_VARS = ("TALLY_PYPI_VERSION", "PYPI_VERSION", "VERSION")


def normalize_platform(value: str) -> str:
    if not value:
        return value
    return PLATFORM_MAPPING.get(value.lower(), value.lower())


def normalize_arch(value: str) -> str:
    if not value:
        return value
    return ARCH_MAPPING.get(value.lower(), value.lower())


def get_platform_info():
    target_platform = os.environ.get("TALLY_TARGET_PLATFORM")
    target_arch = os.environ.get("TALLY_TARGET_ARCH")

    if target_platform and target_arch:
        normalized_platform = normalize_platform(target_platform)
        normalized_arch = normalize_arch(target_arch)
        print(f"[HOOK] Using target: {normalized_platform}-{normalized_arch}")
        return normalized_platform, normalized_arch

    system = normalize_platform(sys.platform) or normalize_platform(platform.system())
    machine = normalize_arch(platform.machine())
    result = system, machine
    print(f"[HOOK] Auto-detected: {result[0]}-{result[1]}")
    return result


def resolve_version() -> str:
    for key in VERSION_ENV_VARS:
        value = os.environ.get(key)
        if value:
            return value.removeprefix("v")
    raise RuntimeError(
        "No package version configured. Set one of: "
        + ", ".join(VERSION_ENV_VARS)
    )


class CustomMetadataHook(MetadataHookInterface):
    PLUGIN_NAME = "custom"

    def update(self, metadata: dict) -> None:
        metadata["version"] = resolve_version()


class CustomBuildHook(BuildHookInterface):
    PLUGIN_NAME = "custom"

    def __init__(self, *args, **kwargs) -> None:
        super().__init__(*args, **kwargs)
        self.target_platform = None
        self.target_arch = None

    def initialize(self, version, build_data):
        target_platform, target_arch = get_platform_info()
        self.target_platform = target_platform
        self.target_arch = target_arch

        tag = PEP425_TAGS.get((target_platform, target_arch))
        if tag:
            build_data["tag"] = tag
            self._stage_target_binary()
            print(f"[HOOK] Building platform wheel {tag}")
        else:
            print(
                "[HOOK] No PEP425 tag for "
                f"{target_platform}-{target_arch}; building universal wheel."
            )

        print(f"[HOOK] Initialized for {target_platform}-{target_arch}")

    def finalize(self, version, build_data, artifact_path) -> None:
        print(f"[HOOK] Built artifact: {artifact_path}")
        self._restore_binaries()

    def _stage_target_binary(self):
        if not self.target_platform or not self.target_arch:
            raise RuntimeError("Target platform is not set before staging binaries.")

        dist_mapping = DIST_BINARY_MAPPING.get((self.target_platform, self.target_arch))
        if dist_mapping is None:
            raise RuntimeError(
                f"No dist mapping for {self.target_platform}-{self.target_arch}."
            )

        goos, goarch, variant, ext = dist_mapping
        repo_root = Path(self.root).parents[1]
        source = (
            repo_root
            / "dist"
            / f"tally_{goos}_{goarch}_{variant}"
            / f"tally{ext}"
        )
        if not source.exists():
            raise FileNotFoundError(
                f"Release binary missing for {self.target_platform}-{self.target_arch}: {source}"
            )

        bin_dir = Path(self.root) / "tally_cli" / "bin"
        if bin_dir.is_dir():
            shutil.rmtree(bin_dir)
        bin_dir.mkdir(parents=True)

        target_dir_name = f"tally-{self.target_platform}-{self.target_arch}"
        target_dir = bin_dir / target_dir_name
        target_dir.mkdir()
        shutil.copy2(source, target_dir / source.name)
        print(f"[HOOK] Staged binary from {source}")
