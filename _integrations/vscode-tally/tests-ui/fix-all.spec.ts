import { expect, test } from "./fixtures";
import { openFile, openWorkspace, problemRows, readEditorText, runCommand } from "./utils";

test("'Tally: Fix all auto-fixable issues' applies fixes to the document", async ({
  sharedCodeServer,
  projectDir,
  page,
}) => {
  await openWorkspace(page, sharedCodeServer.url, projectDir);
  await openFile(page, "Dockerfile");

  // Make sure diagnostics have been computed before invoking the fix command.
  await runCommand(page, "Problems: Focus on Problems View");
  const rows = problemRows(page);
  await expect.poll(() => rows.count(), { timeout: 30_000 }).toBeGreaterThan(0);
  const before = await rows.count();

  await runCommand(page, "Tally: Fix all auto-fixable issues");

  // The casing fix is FixSafe, so the minority lowercase instructions become
  // uppercase to match the majority (copy -> COPY).
  await expect
    .poll(async () => await readEditorText(page), { timeout: 20_000 })
    .toContain("COPY . /app");

  // ...and the number of reported problems should drop once fixes land.
  await expect.poll(() => rows.count(), { timeout: 20_000 }).toBeLessThan(before);
});
