import { type ChildProcess, spawn } from "node:child_process";
import { existsSync, mkdirSync, writeFileSync } from "node:fs";
import { type AddressInfo, createServer } from "node:net";
import { join } from "node:path";

// Prefer the published entrypoint; fall back to the `.bin` shim. Launching the
// entrypoint directly with the current node binary avoids depending on the
// shim's shebang resolving to a node 22 on PATH.
const ENTRY = join(__dirname, "..", "node_modules", "code-server", "out", "node", "entry.js");
const BIN_SHIM = join(__dirname, "..", "node_modules", ".bin", "code-server");
const useEntry = existsSync(ENTRY);
export const CODE_SERVER_BIN = useEntry ? ENTRY : BIN_SHIM;

export interface CodeServerContext {
  proc: ChildProcess;
  port: number;
  url: string;
  userDataDir: string;
}

// Ask the OS for a free port by binding to :0, then release it for code-server.
// There is a small TOCTOU window, but it is far less collision-prone than a
// random pick and keeps the launch deterministic.
async function getFreePort(): Promise<number> {
  return new Promise<number>((resolve, reject) => {
    const server = createServer();
    server.unref();
    server.on("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const address = server.address() as AddressInfo | null;
      const port = address?.port;
      if (port) {
        server.close(() => resolve(port));
      } else {
        server.close(() => reject(new Error("failed to allocate a free port")));
      }
    });
  });
}

function writeSettings(userDataDir: string, tallyBinaryPath: string): void {
  // code-server reads both Machine and User scopes; seed both so the workbench
  // comes up trusted, quiet, and already pointed at our built tally binary.
  const settings = {
    "security.workspace.trust.enabled": false,
    "workbench.startupEditor": "none",
    "telemetry.telemetryLevel": "off",
    "tally.enable": true,
    "tally.path": [tallyBinaryPath],
    "tally.configurationPreference": "editorOnly",
    "[dockerfile]": { "editor.defaultFormatter": "wharflab.tally" },
  };
  const serialized = JSON.stringify(settings, null, 2);
  for (const sub of ["Machine", "User"]) {
    const dir = join(userDataDir, sub);
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, "settings.json"), serialized);
  }
}

export interface StartCodeServerOptions {
  extensionsDir: string;
  userDataDir: string;
  tallyBinaryPath: string;
}

export async function startCodeServer(opts: StartCodeServerOptions): Promise<CodeServerContext> {
  const port = await getFreePort();
  writeSettings(opts.userDataDir, opts.tallyBinaryPath);

  const args = [
    "--bind-addr",
    `127.0.0.1:${port}`,
    "--auth",
    "none",
    "--extensions-dir",
    opts.extensionsDir,
    "--user-data-dir",
    opts.userDataDir,
    "--disable-telemetry",
    "--disable-update-check",
    "--disable-workspace-trust",
    "--disable-getting-started-override",
  ];
  const command = useEntry ? process.execPath : CODE_SERVER_BIN;
  const argv = useEntry ? [CODE_SERVER_BIN, ...args] : args;

  const proc = spawn(command, argv, {
    stdio: "pipe",
    // code-server is browser-served; an empty DISPLAY keeps any child from
    // trying to reach an X server that does not exist in CI.
    env: { ...process.env, DISPLAY: "" },
  });

  await new Promise<void>((resolve, reject) => {
    const timer = setTimeout(() => {
      cleanup();
      reject(new Error(`code-server did not start within 30s on port ${port}`));
    }, 30_000);

    const onData = (chunk: Buffer): void => {
      const text = chunk.toString();
      // Readiness gate: the stdout banner, NOT networkidle. code-server keeps a
      // websocket open forever, so networkidle never fires.
      if (text.includes("HTTP server listening on")) {
        cleanup();
        resolve();
      }
    };
    const onError = (err: Error): void => {
      cleanup();
      reject(err);
    };
    const onExit = (code: number | null): void => {
      cleanup();
      reject(new Error(`code-server exited early with code ${code}`));
    };
    function cleanup(): void {
      clearTimeout(timer);
      proc.stdout?.off("data", onData);
      proc.stderr?.off("data", onData);
      proc.off("error", onError);
      proc.off("exit", onExit);
    }

    proc.stdout?.on("data", onData);
    // Some builds log the listening banner to stderr.
    proc.stderr?.on("data", onData);
    proc.once("error", onError);
    proc.once("exit", onExit);
  });

  return { proc, port, url: `http://127.0.0.1:${port}`, userDataDir: opts.userDataDir };
}

export async function stopCodeServer(ctx: CodeServerContext): Promise<void> {
  if (ctx.proc.exitCode !== null || ctx.proc.killed) {
    return;
  }
  await new Promise<void>((resolve) => {
    const timer = setTimeout(() => {
      ctx.proc.kill("SIGKILL");
      resolve();
    }, 5_000);
    // Register the exit listener BEFORE signalling, so an instant exit cannot
    // fire before we are listening (which would hang until the SIGKILL fallback).
    ctx.proc.once("exit", () => {
      clearTimeout(timer);
      resolve();
    });
    ctx.proc.kill("SIGTERM");
  });
}
