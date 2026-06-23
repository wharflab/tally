import { cpSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { test as base } from "@playwright/test";

import { type CodeServerContext, startCodeServer, stopCodeServer } from "./utils_code_server";

// Shared scratch dir populated by extension.setup.ts: the installed extension
// and the freshly built tally LSP binary.
const SETUP_DIR = join(__dirname, "..", ".test_setup");
const EXTENSIONS_DIR = join(SETUP_DIR, "extensions");
const TALLY_BIN =
  process.env.TALLY_BIN ?? join(SETUP_DIR, process.platform === "win32" ? "tally.exe" : "tally");
const FIXTURE_DOCKERFILE = join(__dirname, "fixtures", "Dockerfile");

interface WorkerFixtures {
  sharedCodeServer: CodeServerContext;
}

interface TestFixtures {
  /** An isolated workspace folder seeded with the lint-bait Dockerfile. */
  projectDir: string;
}

export const test = base.extend<TestFixtures, WorkerFixtures>({
  // One code-server per worker (workers:1 → effectively once per run), using a
  // throwaway user-data dir. The extensions dir is shared and read-only here.
  sharedCodeServer: [
    async ({}, use) => {
      const userDataDir = mkdtempSync(join(tmpdir(), "tally-e2e-udd-"));
      const ctx = await startCodeServer({
        extensionsDir: EXTENSIONS_DIR,
        userDataDir,
        tallyBinaryPath: TALLY_BIN,
      });
      await use(ctx);
      await stopCodeServer(ctx);
      rmSync(userDataDir, { recursive: true, force: true });
    },
    { scope: "worker" },
  ],

  // A fresh project folder per test so edits/fixes never bleed across cases.
  projectDir: async ({}, use) => {
    const dir = mkdtempSync(join(tmpdir(), "tally-e2e-proj-"));
    cpSync(FIXTURE_DOCKERFILE, join(dir, "Dockerfile"));
    await use(dir);
    rmSync(dir, { recursive: true, force: true });
  },
});

export { expect } from "@playwright/test";
