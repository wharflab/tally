import { expect, test } from "./fixtures";
import { openFile, openWorkspace, problemRows, runCommand } from "./utils";

test("tally diagnostics surface in the Problems panel", async ({
  sharedCodeServer,
  projectDir,
  page,
}) => {
  await openWorkspace(page, sharedCodeServer.url, projectDir);
  await openFile(page, "Dockerfile");

  await runCommand(page, "Problems: Focus on Problems View");

  const rows = problemRows(page);

  // Absorb LSP startup latency by polling rather than sleeping.
  await expect.poll(() => rows.count(), { timeout: 30_000 }).toBeGreaterThan(0);
  await expect(rows.first()).toBeVisible();

  // Bind the assertion to a real tally message: lowercase instructions trip the
  // casing rule, whose message mentions matching the majority casing.
  await expect(rows.filter({ hasText: /casing|uppercase/i }).first()).toBeVisible();
});
