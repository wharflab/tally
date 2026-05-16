import { test } from "bun:test";
import * as fs from "node:fs/promises";
import * as os from "node:os";
import * as path from "node:path";

import { downloadAndUnzipVSCode, runTests } from "@vscode/test-electron";
import Bun from "bun";
import { backOff } from "exponential-backoff";

function runOrThrow(cmd: string[], opts: { cwd: string; env?: Record<string, string> }): void {
  const proc = Bun.spawnSync({
    cmd,
    cwd: opts.cwd,
    env: { ...process.env, ...opts.env },
    stdout: "pipe",
    stderr: "pipe",
  });
  if (proc.exitCode !== 0) {
    const stderr = new TextDecoder().decode(proc.stderr);
    const stdout = new TextDecoder().decode(proc.stdout);
    throw new Error(
      `command failed: ${cmd.join(" ")}\nexitCode=${proc.exitCode}\nstdout:\n${stdout}\nstderr:\n${stderr}`,
    );
  }
}

function runAndRead(cmd: string[], opts: { cwd: string }): string {
  const proc = Bun.spawnSync({
    cmd,
    cwd: opts.cwd,
    env: process.env,
    stdout: "pipe",
    stderr: "pipe",
  });
  const stdout = new TextDecoder().decode(proc.stdout).trim();
  if (proc.exitCode !== 0) {
    const stderr = new TextDecoder().decode(proc.stderr);
    throw new Error(
      `command failed: ${cmd.join(" ")}\nexitCode=${proc.exitCode}\nstdout:\n${stdout}\nstderr:\n${stderr}`,
    );
  }
  return stdout.split(/\r?\n/).filter(Boolean).at(-1) ?? "";
}

async function writeUserSettings(userDataDir: string, settings: unknown): Promise<void> {
  const userDir = path.join(userDataDir, "User");
  await fs.mkdir(userDir, { recursive: true });
  await fs.writeFile(path.join(userDir, "settings.json"), JSON.stringify(settings, null, 2));
}

async function fileExists(file: string): Promise<boolean> {
  try {
    await fs.access(file);
    return true;
  } catch {
    return false;
  }
}

async function prepareBazelArtifacts(repoRoot: string, extensionRoot: string): Promise<string> {
  const configuredBundle = process.env.TALLY_VSCODE_BUNDLE;
  let bundlePath = configuredBundle ? path.resolve(configuredBundle) : "";

  const configuredBinary = process.env.TALLY_VSCODE_BINARY;
  let binaryPath = configuredBinary ? path.resolve(configuredBinary) : "";

  if (!bundlePath || !binaryPath) {
    runOrThrow(
      [
        "bazel",
        "build",
        "--config=release",
        "//:tally",
        "//_integrations/vscode-tally:extension_bundle",
      ],
      { cwd: repoRoot },
    );
  }

  if (!bundlePath) {
    const bazelBin = path.join(repoRoot, "bazel-bin", "_integrations", "vscode-tally");
    const entries = await fs.readdir(bazelBin);
    const bundle = entries.find(
      (entry) => entry.startsWith("extension_bundle__") && entry.endsWith(".js"),
    );
    if (!bundle) {
      throw new Error(`Bazel extension bundle not found in ${bazelBin}`);
    }
    bundlePath = path.join(bazelBin, bundle);
  }

  if (!binaryPath) {
    binaryPath = path.resolve(
      repoRoot,
      runAndRead(["tools/bazel/target_output.sh", "--config=release", "//:tally"], {
        cwd: repoRoot,
      }),
    );
  }

  if (!(await fileExists(binaryPath))) {
    throw new Error(`Bazel tally binary not found at ${binaryPath}`);
  }

  const distDir = path.join(extensionRoot, "dist");
  await fs.mkdir(distDir, { recursive: true });
  await fs.copyFile(bundlePath, path.join(distDir, "extension.cjs"));

  return binaryPath;
}

test(
  "vscode smoke: diagnostics + formatting",
  async () => {
    const extensionRoot = path.resolve(import.meta.dir, "..", "..");
    const repoRoot = path.resolve(extensionRoot, "..", "..");

    const binaryPath = await prepareBazelArtifacts(repoRoot, extensionRoot);

    // Isolated VS Code profile to avoid local settings/extensions interference.
    const userDataDir = await fs.mkdtemp(path.join(os.tmpdir(), "tally-vscode-userdata-"));
    const extensionsDir = await fs.mkdtemp(path.join(os.tmpdir(), "tally-vscode-extensions-"));

    await writeUserSettings(userDataDir, {
      "tally.enable": true,
      "tally.path": [binaryPath],
      "tally.configurationPreference": "editorOnly",
      // Ensure VS Code uses the formatter from this extension if other providers exist.
      "[dockerfile]": {
        "editor.defaultFormatter": "wharflab.tally",
      },
    });

    process.env.VSCODE_SMOKE_EXPECTED_DIAGNOSTICS = "268";
    process.env.VSCODE_SMOKE_EXPECTED_FORMAT_SNAPSHOT = path.join(
      repoRoot,
      "internal",
      "lsptest",
      "__snapshots__",
      "TestLSP_FormattingRealWorld_1.snap.Dockerfile",
    );

    const vscodeVersion = process.env.VSCODE_TEST_VERSION;

    const vscodeExecutablePath = await backOff(() =>
      downloadAndUnzipVSCode({ version: vscodeVersion, extractSync: true }),
    );

    await runTests({
      vscodeExecutablePath,
      extensionDevelopmentPath: extensionRoot,
      extensionTestsPath: path.join(extensionRoot, "test", "suite", "index.js"),
      timeout: 120_000,
      launchArgs: [
        repoRoot,
        "--disable-extensions",
        "--disable-updates",
        "--disable-crash-reporter",
        "--disable-telemetry",
        "--disable-workspace-trust",
        "--skip-welcome",
        "--skip-release-notes",
        "--disable-chromium-sandbox",
        "--use-inmemory-secretstorage",
        "--sync=off",
        "--logExtensionHostCommunication",
        "--user-data-dir",
        userDataDir,
        "--extensions-dir",
        extensionsDir,
      ],
    });
  },
  { timeout: 10 * 60 * 1000 },
);
