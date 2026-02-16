import os
import sys
import platform
import subprocess

ISSUE_URL = "https://github.com/wharflab/tally/issues/new"
ARCH_MAPPING = {
    'amd64': 'x86_64',
    'aarch64': 'arm64',
}

def main():
    os_name = platform.system().lower()
    arch = platform.machine().lower()
    arch = ARCH_MAPPING.get(arch, arch)
    ext = os_name == "windows" and ".exe" or ""
    subfolder = f"tally-{os_name}-{arch}"
    executable = os.path.join(os.path.dirname(__file__), "bin", subfolder, "tally"+ext)
    if not os.path.isfile(executable):
        print(f"Couldn't find binary {executable}. Please create an issue: {ISSUE_URL}", file=sys.stderr)
        return 1

    result = subprocess.run([executable] + sys.argv[1:])
    return result.returncode
