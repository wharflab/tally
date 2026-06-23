import { cpSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { test as base } from "@playwright/test";

import { type CodeServerContext, startCodeServer } from "./utils_code_server";

interface TempDir extends Disposable {
  path: string;
}

// A temp dir that removes itself when it leaves a `using` scope, even if the
// surrounding fixture throws.
function makeTempDir(prefix: string): TempDir {
  const path = mkdtempSync(join(tmpdir(), prefix));
  return {
    path,
    [Symbol.dispose]() {
      rmSync(path, { recursive: true, force: true });
    },
  };
}

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
      // `using`/`await using` guarantee the temp dir is removed and the server
      // stopped on scope exit, even if startCodeServer or the test throws.
      using userData = makeTempDir("tally-e2e-udd-");
      await using ctx = await startCodeServer({
        extensionsDir: EXTENSIONS_DIR,
        userDataDir: userData.path,
        tallyBinaryPath: TALLY_BIN,
      });
      await use(ctx);
    },
    { scope: "worker" },
  ],

  // A fresh project folder per test so edits/fixes never bleed across cases.
  projectDir: async ({}, use) => {
    using project = makeTempDir("tally-e2e-proj-");
    cpSync(FIXTURE_DOCKERFILE, join(project.path, "Dockerfile"));
    await use(project.path);
  },
});

export { expect } from "@playwright/test";
