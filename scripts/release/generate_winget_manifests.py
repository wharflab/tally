#!/usr/bin/env python3

from __future__ import annotations

import argparse
from pathlib import Path


PACKAGE_IDENTIFIER = "Wharflab.Tally"
PUBLISHER = "Wharflab"
PACKAGE_NAME = "Tally"
PACKAGE_LOCALE = "en-US"
MANIFEST_VERSION = "1.9.0"
SHORT_DESCRIPTION = "A fast, configurable linter for Dockerfiles and Containerfiles."
TAG_LIST = ("docker", "dockerfile", "containerfile", "linter")
WINDOWS_ASSETS = (
    ("x64", "tally_{version}_Windows_x86_64.zip"),
    ("arm64", "tally_{version}_Windows_arm64.zip"),
)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Generate WinGet manifests for a tally release.",
    )
    parser.add_argument("--version", required=True, help="Release version, with or without v prefix.")
    parser.add_argument("--repo-owner", default="wharflab")
    parser.add_argument("--repo-name", default="tally")
    parser.add_argument("--dist-root", default="dist")
    parser.add_argument("--output-root", required=True)
    return parser.parse_args()


def normalized_version(value: str) -> str:
    return value.removeprefix("v")


def read_checksums(path: Path) -> dict[str, str]:
    checksums: dict[str, str] = {}
    for line in path.read_text(encoding="utf-8").splitlines():
        parts = line.strip().split()
        if len(parts) != 2:
            raise SystemExit(f"invalid checksum line: {line!r}")
        checksums[parts[1]] = parts[0].upper()
    return checksums


def manifest_dir(output_root: Path, package_identifier: str, version: str) -> Path:
    parts = package_identifier.split(".")
    return output_root / parts[0][0].lower() / Path(*parts) / version


def github_release_url(owner: str, repo: str, version: str, filename: str) -> str:
    return f"https://github.com/{owner}/{repo}/releases/download/v{version}/{filename}"


def write_file(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")


def render_version_manifest(version: str) -> str:
    return f"""# Created by tally release automation
# yaml-language-server: $schema=https://aka.ms/winget-manifest.version.{MANIFEST_VERSION}.schema.json

PackageIdentifier: {PACKAGE_IDENTIFIER}
PackageVersion: {version}
DefaultLocale: {PACKAGE_LOCALE}
ManifestType: version
ManifestVersion: {MANIFEST_VERSION}
"""


def render_default_locale_manifest(version: str, owner: str, repo: str) -> str:
    tags = "\n".join(f"- {tag}" for tag in TAG_LIST)
    return f"""# Created by tally release automation
# yaml-language-server: $schema=https://aka.ms/winget-manifest.defaultLocale.{MANIFEST_VERSION}.schema.json

PackageIdentifier: {PACKAGE_IDENTIFIER}
PackageVersion: {version}
PackageLocale: {PACKAGE_LOCALE}
Publisher: {PUBLISHER}
PublisherUrl: https://github.com/{owner}
PublisherSupportUrl: https://github.com/{owner}/{repo}/issues
PackageName: {PACKAGE_NAME}
PackageUrl: https://github.com/{owner}/{repo}
ShortDescription: {SHORT_DESCRIPTION}
Moniker: tally
License: GPL-3.0-only
LicenseUrl: https://github.com/{owner}/{repo}/blob/main/LICENSE
ReleaseNotesUrl: https://github.com/{owner}/{repo}/releases/tag/v{version}
Tags:
{tags}
ManifestType: defaultLocale
ManifestVersion: {MANIFEST_VERSION}
"""


def render_installer_manifest(
    version: str,
    owner: str,
    repo: str,
    checksums: dict[str, str],
) -> str:
    installers = []
    for architecture, pattern in WINDOWS_ASSETS:
        filename = pattern.format(version=version)
        sha256 = checksums.get(filename)
        if not sha256:
            raise SystemExit(f"missing checksum for {filename}")
        installer = f"""- Architecture: {architecture}
  InstallerType: zip
  NestedInstallerType: portable
  InstallerUrl: {github_release_url(owner, repo, version, filename)}
  InstallerSha256: {sha256}
  NestedInstallerFiles:
    - RelativeFilePath: tally.exe
      PortableCommandAlias: tally"""
        installers.append(installer)

    rendered_installers = "\n".join(installers)
    return f"""# Created by tally release automation
# yaml-language-server: $schema=https://aka.ms/winget-manifest.installer.{MANIFEST_VERSION}.schema.json

PackageIdentifier: {PACKAGE_IDENTIFIER}
PackageVersion: {version}
Commands:
  - tally
Installers:
{rendered_installers}
ManifestType: installer
ManifestVersion: {MANIFEST_VERSION}
"""


def main() -> int:
    args = parse_args()
    version = normalized_version(args.version)
    repo_root = Path(__file__).resolve().parents[2]
    dist_root = (repo_root / args.dist_root).resolve()
    checksums = read_checksums(dist_root / "tally_checksums.txt")
    output_root = Path(args.output_root).resolve()
    out_dir = manifest_dir(output_root, PACKAGE_IDENTIFIER, version)

    write_file(
        out_dir / f"{PACKAGE_IDENTIFIER}.yaml",
        render_version_manifest(version),
    )
    write_file(
        out_dir / f"{PACKAGE_IDENTIFIER}.locale.{PACKAGE_LOCALE}.yaml",
        render_default_locale_manifest(version, args.repo_owner, args.repo_name),
    )
    write_file(
        out_dir / f"{PACKAGE_IDENTIFIER}.installer.yaml",
        render_installer_manifest(version, args.repo_owner, args.repo_name, checksums),
    )
    print(out_dir)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
