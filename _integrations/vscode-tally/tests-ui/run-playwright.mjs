// Launches the Playwright CLI under the real Node.js runtime.
//
// This package's bunfig.toml sets `[run] bun = true`, which makes `bun run`
// scripts execute under Bun and prepend a `node` shim (a temp dir like
// /private/tmp/bun-node-*) to PATH that points back at Bun. Playwright's test
// runner cannot transform the TypeScript specs under the Bun runtime — it dies
// with "AggregateError: N errors building ...". So we locate the real `node`
// on PATH (skipping Bun's shim) and exec the Playwright CLI with it.
//
// Running this file under Bun is fine; it only spawns a child. Running it under
// Node also works (process.execPath is already real Node, found first below).
import { spawnSync } from "node:child_process";
import { accessSync, constants } from "node:fs";
import { createRequire } from "node:module";
import { delimiter, join } from "node:path";

function findRealNode() {
  const exe = process.platform === "win32" ? "node.exe" : "node";
  for (const dir of (process.env.PATH ?? "").split(delimiter)) {
    if (!dir || dir.includes("bun-node")) {
      continue; // skip Bun's `node` shim
    }
    const candidate = join(dir, exe);
    try {
      accessSync(candidate, constants.X_OK);
      return candidate;
    } catch {
      // not here; keep looking
    }
  }
  // Fall back to whatever launched us (real Node when invoked directly).
  return process.execPath;
}

const require = createRequire(import.meta.url);
const cli = require.resolve("@playwright/test/cli");

const result = spawnSync(findRealNode(), [cli, "test", ...process.argv.slice(2)], {
  stdio: "inherit",
});

if (result.error) {
  throw result.error;
}
process.exit(result.status ?? 1);
