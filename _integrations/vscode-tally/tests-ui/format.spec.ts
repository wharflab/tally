import { expect, test } from "./fixtures";
import { openFile, openWorkspace, readEditorText, runCommand } from "./utils";

test("Format Document rewrites the Dockerfile via the tally formatter", async ({
  sharedCodeServer,
  projectDir,
  page,
}) => {
  await openWorkspace(page, sharedCodeServer.url, projectDir);
  await openFile(page, "Dockerfile");

  // Wait until the language server is ready (diagnostics published) before
  // formatting — otherwise Format Document fires before tally has registered
  // its formatting provider and returns no edits.
  await runCommand(page, "Problems: Focus on Problems View");
  const rows = page.locator(".markers-panel-container .monaco-list-row");
  await expect.poll(() => rows.count(), { timeout: 30_000 }).toBeGreaterThan(0);

  const before = await readEditorText(page);
  // The fixture's lone lowercase instructions are the minority case.
  expect(before).toContain("copy . /app");

  // Re-issue Format Document inside the poll: the formatter routes through the
  // tally provider (configured as the default Dockerfile formatter) and
  // uppercases the minority-case instructions to match the majority.
  await expect
    .poll(
      async () => {
        await runCommand(page, "Format Document");
        return await readEditorText(page);
      },
      { timeout: 20_000 },
    )
    .toContain("COPY . /app");

  const after = await readEditorText(page);
  expect(after).not.toEqual(before);
});
