import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

import { test as setup } from "@playwright/test";

import { CODE_SERVER_BIN } from "./utils_code_server";

const EXTENSION_ROOT = join(__dirname, "..");
const REPO_ROOT = join(EXTENSION_ROOT, "..", "..");
const SETUP_DIR = join(EXTENSION_ROOT, ".test_setup");
const EXTENSIONS_DIR = join(SETUP_DIR, "extensions");
const HASH_FILE = join(SETUP_DIR, "vsix.sha256");
// Mirror the resolution in fixtures.ts: an explicit TALLY_BIN override wins,
// otherwise we build into the scratch dir.
const DEFAULT_TALLY_BIN = join(SETUP_DIR, process.platform === "win32" ? "tally.exe" : "tally");
const TALLY_BIN = process.env.TALLY_BIN ?? DEFAULT_TALLY_BIN;

// Build flags required to compile tally with the image-handling stubs (see
// CLAUDE.md). The electron smoke test passes the same set indirectly via make.
const GO_BUILD_TAGS =
  "containers_image_openpgp,containers_image_storage_stub,containers_image_docker_daemon_stub";

setup("build tally LSP binary + install vsix into code-server", () => {
  // A cold `go build` (downloading modules + compiling) can take a few minutes,
  // well beyond the global 60s test timeout.
  setup.setTimeout(600_000);

  mkdirSync(EXTENSIONS_DIR, { recursive: true });

  buildTallyBinary();
  installVsix();
});

function buildTallyBinary(): void {
  // Honor an explicit TALLY_BIN override (documented in DEVELOPMENT.md): reuse
  // the prebuilt binary instead of running `go build`, which also lets the
  // suite run where Go/modules are unavailable.
  if (process.env.TALLY_BIN) {
    if (!existsSync(TALLY_BIN)) {
      throw new Error(`TALLY_BIN points at a missing binary: ${TALLY_BIN}`);
    }
    return;
  }

  const args = ["build"];
  // Collect coverage from the live LSP process when CI asks for it.
  if (process.env.GOCOVERDIR) {
    args.push("-cover");
  }
  args.push("-tags", GO_BUILD_TAGS, "-o", TALLY_BIN, "github.com/wharflab/tally");

  execFileSync("go", args, {
    cwd: REPO_ROOT,
    env: { ...process.env, GOEXPERIMENT: "jsonv2" },
    stdio: "inherit",
  });
}

function installVsix(): void {
  const pkg = JSON.parse(readFileSync(join(EXTENSION_ROOT, "package.json"), "utf8")) as {
    version: string;
  };
  const vsix = process.env.VSIX_PATH
    ? join(EXTENSION_ROOT, process.env.VSIX_PATH)
    : join(EXTENSION_ROOT, `tally-${pkg.version}.vsix`);

  if (!existsSync(vsix)) {
    throw new Error(`vsix not found at ${vsix}; run "bun run vsce:package" first`);
  }

  // Skip the (slow) reinstall when the packaged extension is byte-identical.
  const hash = createHash("sha256").update(readFileSync(vsix)).digest("hex");
  if (existsSync(HASH_FILE) && readFileSync(HASH_FILE, "utf8").trim() === hash) {
    return;
  }

  // Launch via the current node binary when we resolved the entrypoint; the
  // shim is invoked directly otherwise (see CODE_SERVER_BIN resolution).
  const isEntry = CODE_SERVER_BIN.endsWith(".js");
  const installArgs = ["--install-extension", vsix, "--extensions-dir", EXTENSIONS_DIR, "--force"];
  if (isEntry) {
    execFileSync(process.execPath, [CODE_SERVER_BIN, ...installArgs], {
      stdio: "inherit",
      env: { ...process.env, DISPLAY: "" },
    });
  } else {
    execFileSync(CODE_SERVER_BIN, installArgs, {
      stdio: "inherit",
      env: { ...process.env, DISPLAY: "" },
    });
  }

  writeFileSync(HASH_FILE, hash);
}
