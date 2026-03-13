#!/usr/bin/env python3

from __future__ import annotations

import argparse
import shutil
import tarfile
import zipfile
from pathlib import Path


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Package a built tally binary into the release dist layout.",
    )
    parser.add_argument("--version", required=True)
    parser.add_argument("--goos", required=True)
    parser.add_argument("--goarch", required=True)
    parser.add_argument("--variant", required=True)
    parser.add_argument("--binary-name", required=True)
    parser.add_argument("--archive-os", required=True)
    parser.add_argument("--archive-arch", required=True)
    parser.add_argument("--dist-root", default="dist")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    repo_root = Path(__file__).resolve().parents[2]
    dist_root = (repo_root / args.dist_root).resolve()
    dist_root.mkdir(parents=True, exist_ok=True)

    platform_dir = dist_root / f"tally_{args.goos}_{args.goarch}_{args.variant}"
    binary_path = platform_dir / args.binary_name
    if not binary_path.exists():
        raise SystemExit(f"built binary not found: {binary_path}")

    for filename in ("LICENSE", "NOTICE"):
        src = repo_root / filename
        dest = platform_dir / filename
        shutil.copy2(src, dest)

    archive_suffix = ".zip" if args.goos == "windows" else ".tar.gz"
    archive_name = (
        f"tally_{args.version}_{args.archive_os}_{args.archive_arch}{archive_suffix}"
    )
    archive_path = dist_root / archive_name
    if archive_path.exists():
        archive_path.unlink()

    package_files = [binary_path, platform_dir / "LICENSE", platform_dir / "NOTICE"]
    if args.goos == "windows":
        with zipfile.ZipFile(
            archive_path,
            mode="w",
            compression=zipfile.ZIP_DEFLATED,
        ) as zf:
            for path in package_files:
                zf.write(path, arcname=path.name)
    else:
        with tarfile.open(archive_path, mode="w:gz") as tf:
            for path in package_files:
                tf.add(path, arcname=path.name)

    print(archive_path)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
