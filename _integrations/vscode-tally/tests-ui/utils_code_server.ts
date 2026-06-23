import { type ChildProcess, spawn } from "node:child_process";
import { once } from "node:events";
import { existsSync, mkdirSync, writeFileSync } from "node:fs";
import { type AddressInfo, createServer } from "node:net";
import { join } from "node:path";
import { setTimeout as sleep } from "node:timers/promises";

const STARTUP_TIMEOUT_MS = 30_000;
const SHUTDOWN_TIMEOUT_MS = 5_000;
const HEALTHZ_POLL_MS = 150;
const HEALTHZ_REQUEST_TIMEOUT_MS = 2_000;

// Prefer the published entrypoint; fall back to the `.bin` shim. Launching the
// entrypoint with the current node binary avoids depending on the shim's
// shebang resolving to a node 22 on PATH.
const ENTRY = join(__dirname, "..", "node_modules", "code-server", "out", "node", "entry.js");
const BIN_SHIM = join(__dirname, "..", "node_modules", ".bin", "code-server");
const useEntry = existsSync(ENTRY);
export const CODE_SERVER_BIN = useEntry ? ENTRY : BIN_SHIM;

export interface CodeServerContext extends AsyncDisposable {
  proc: ChildProcess;
  port: number;
  url: string;
  userDataDir: string;
}

export interface StartCodeServerOptions {
  extensionsDir: string;
  userDataDir: string;
  tallyBinaryPath: string;
}

export async function startCodeServer(opts: StartCodeServerOptions): Promise<CodeServerContext> {
  const port = await getFreePort();
  const url = `http://127.0.0.1:${port}`;
  writeSettings(opts.userDataDir, opts.tallyBinaryPath);

  const args = [
    "--bind-addr", `127.0.0.1:${port}`,
    "--auth", "none",
    "--extensions-dir", opts.extensionsDir,
    "--user-data-dir", opts.userDataDir,
    "--disable-telemetry",
    "--disable-update-check",
    "--disable-workspace-trust",
    "--disable-getting-started-override",
  ]; // prettier-ignore
  const command = useEntry ? process.execPath : CODE_SERVER_BIN;
  const proc = spawn(command, useEntry ? [CODE_SERVER_BIN, ...args] : args, {
    // No stdout scraping (we poll /healthz), so inherit the streams: it both
    // surfaces code-server logs in CI and avoids the deadlock an undrained
    // pipe would cause. DISPLAY="" keeps any child off a non-existent X server.
    stdio: ["ignore", "inherit", "inherit"],
    env: { ...process.env, DISPLAY: "" },
  });

  // Become ready when /healthz answers, fail fast if the process dies first,
  // and give up at the deadline — whichever happens first.
  const deadline = new AbortController();
  const signal = AbortSignal.any([deadline.signal, AbortSignal.timeout(STARTUP_TIMEOUT_MS)]);
  const pending = [waitForHealthz(url, signal), rejectWhenProcessExits(proc, signal)];
  try {
    await Promise.race(pending);
  } catch (cause) {
    await stopCodeServer(proc);
    throw new Error(`code-server did not become ready on port ${port}`, { cause });
  } finally {
    // Cancel the loser of the race and drain both so neither lingers as an
    // unhandled rejection once startup has settled.
    deadline.abort();
    await Promise.allSettled(pending);
  }

  return {
    proc,
    port,
    url,
    userDataDir: opts.userDataDir,
    // `await using` stops the server on scope exit — callers never have to.
    [Symbol.asyncDispose]: () => stopCodeServer(proc),
  };
}

// Ask the OS for a free port by binding to :0, then release it for code-server.
// A small TOCTOU window remains, but it is far less collision-prone than a
// random pick and keeps the launch deterministic.
async function getFreePort(): Promise<number> {
  const server = createServer();
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const { port } = server.address() as AddressInfo;
  server.close();
  await once(server, "close");
  return port;
}

// Poll GET /healthz until code-server answers 200. The overall `signal` bounds
// the whole wait; each request also gets its own short timeout so one hung
// connection can't stall the loop.
async function waitForHealthz(url: string, signal: AbortSignal): Promise<void> {
  while (true) {
    try {
      const response = await fetch(`${url}/healthz`, {
        signal: AbortSignal.any([signal, AbortSignal.timeout(HEALTHZ_REQUEST_TIMEOUT_MS)]),
      });
      if (response.ok) {
        return;
      }
    } catch {
      // Connection refused or per-request timeout while still starting up.
    }
    // Back off, but wake immediately (and reject) when the deadline aborts.
    await sleep(HEALTHZ_POLL_MS, undefined, { signal });
  }
}

async function rejectWhenProcessExits(proc: ChildProcess, signal: AbortSignal): Promise<never> {
  const [code] = (await once(proc, "exit", { signal })) as [number | null];
  throw new Error(`code-server exited during startup (code ${String(code)})`);
}

function writeSettings(userDataDir: string, tallyBinaryPath: string): void {
  // code-server reads both Machine and User scopes; seed both so the workbench
  // comes up trusted, quiet, and already pointed at our built tally binary.
  const settings = JSON.stringify(
    {
      "security.workspace.trust.enabled": false,
      "workbench.startupEditor": "none",
      "telemetry.telemetryLevel": "off",
      "tally.enable": true,
      "tally.path": [tallyBinaryPath],
      "tally.configurationPreference": "editorOnly",
      "[dockerfile]": { "editor.defaultFormatter": "wharflab.tally" },
    },
    null,
    2,
  );
  for (const scope of ["Machine", "User"]) {
    const dir = join(userDataDir, scope);
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, "settings.json"), settings);
  }
}

async function stopCodeServer(proc: ChildProcess): Promise<void> {
  if (proc.exitCode !== null || proc.killed) {
    return;
  }
  // Attach the exit listener before signalling so an instant exit can't slip
  // through before we are listening.
  const exited = once(proc, "exit", { signal: AbortSignal.timeout(SHUTDOWN_TIMEOUT_MS) });
  proc.kill("SIGTERM");
  try {
    await exited;
  } catch {
    // Didn't exit within the grace period — force it and wait for the real exit.
    proc.kill("SIGKILL");
    await once(proc, "exit");
  }
}
