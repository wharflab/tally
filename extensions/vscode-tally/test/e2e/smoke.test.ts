import { runTests } from "@vscode/test-electron";
import Bun from "bun";
import { test } from "bun:test";
import * as fs from "node:fs/promises";
import * as os from "node:os";
import * as path from "node:path";

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

async function writeUserSettings(userDataDir: string, settings: unknown): Promise<void> {
  const userDir = path.join(userDataDir, "User");
  await fs.mkdir(userDir, { recursive: true });
  await fs.writeFile(path.join(userDir, "settings.json"), JSON.stringify(settings, null, 2));
}

test(
  "vscode smoke: diagnostics + formatting",
  async () => {
    const extensionRoot = path.resolve(import.meta.dir, "..", "..");
    const repoRoot = path.resolve(extensionRoot, "..", "..");

    // Build the VS Code extension bundle (dist/extension.cjs).
    runOrThrow(["bun", "run", "compile"], { cwd: extensionRoot });

    // Build a tally binary for the language server.
    const binDir = await fs.mkdtemp(path.join(os.tmpdir(), "tally-vscode-bin-"));
    const binaryName = process.platform === "win32" ? "tally.exe" : "tally";
    const binaryPath = path.join(binDir, binaryName);

    const goBuildArgs = ["go", "build"];
    if (process.env.GOCOVERDIR) {
      goBuildArgs.push("-cover");
    }
    goBuildArgs.push("-o", binaryPath, "github.com/tinovyatkin/tally");

    runOrThrow(goBuildArgs, {
      cwd: repoRoot,
      env: { GOEXPERIMENT: "jsonv2" },
    });

    // Isolated VS Code profile to avoid local settings/extensions interference.
    const userDataDir = await fs.mkdtemp(path.join(os.tmpdir(), "tally-vscode-userdata-"));
    const extensionsDir = await fs.mkdtemp(path.join(os.tmpdir(), "tally-vscode-extensions-"));

    await writeUserSettings(userDataDir, {
      "tally.enable": true,
      "tally.path": [binaryPath],
      // Ensure VS Code uses the formatter from this extension if other providers exist.
      "[dockerfile]": {
        "editor.defaultFormatter": "wharflab.tally",
      },
    });

    process.env.TALLY_EXPECTED_DIAGNOSTICS = "71";
    process.env.TALLY_EXPECTED_FORMAT_SNAPSHOT = path.join(
      repoRoot,
      "internal",
      "lsptest",
      "__snapshots__",
      "TestLSP_FormattingRealWorld_1.snap.Dockerfile",
    );

    const vscodeVersion = process.env.VSCODE_TEST_VERSION;
    const vscodeDownloadTimeoutMsRaw = process.env.VSCODE_TEST_DOWNLOAD_TIMEOUT_MS;
    const vscodeDownloadTimeoutMsParsed = Number.parseInt(
      vscodeDownloadTimeoutMsRaw ?? "120000",
      10,
    );
    const vscodeDownloadTimeoutMs = Number.isFinite(vscodeDownloadTimeoutMsParsed)
      ? vscodeDownloadTimeoutMsParsed
      : 120_000;

    await runTests({
      extensionDevelopmentPath: extensionRoot,
      extensionTestsPath: path.join(extensionRoot, "test", "suite", "index.js"),
      ...(vscodeVersion ? { version: vscodeVersion } : {}),
      timeout: vscodeDownloadTimeoutMs,
      extractSync: true,
      launchArgs: [
        repoRoot,
        "--disable-extensions",
        "--disable-workspace-trust",
        "--skip-welcome",
        "--skip-release-notes",
        "--user-data-dir",
        userDataDir,
        "--extensions-dir",
        extensionsDir,
      ],
    });
  },
  { timeout: 10 * 60 * 1000 },
);
