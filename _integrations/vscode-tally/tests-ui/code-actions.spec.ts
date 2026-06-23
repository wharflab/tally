import { expect, test } from "./fixtures";
import { openFile, openWorkspace, problemRows, runCommand } from "./utils";

test("the quick-fix menu offers a tally code action", async ({
  sharedCodeServer,
  projectDir,
  page,
}) => {
  await openWorkspace(page, sharedCodeServer.url, projectDir);
  await openFile(page, "Dockerfile");

  // Wait until diagnostics exist before asking for fixes, otherwise the action
  // widget may open with nothing to offer.
  await runCommand(page, "Problems: Focus on Problems View");
  const rows = problemRows(page);
  await expect.poll(() => rows.count(), { timeout: 30_000 }).toBeGreaterThan(0);

  // Clicking the casing diagnostic row navigates the editor cursor straight to
  // it — more robust than hunting for a token in the virtualized editor.
  const casingRow = rows.filter({ hasText: /casing|uppercase/i }).first();
  await expect(casingRow).toBeVisible({ timeout: 15_000 });
  await casingRow.click();

  await runCommand(page, "Quick Fix...");

  // The code-action widget lists actions as `.monaco-list-row.action`.
  const actions = page.locator(".action-widget .monaco-list-row.action");
  await expect(actions.first()).toBeVisible({ timeout: 15_000 });

  // Require a Tally-specific action so a generic editor action can't satisfy
  // the assertion. The casing fix's title is "Change 'copy' to 'COPY' to match
  // majority casing"; tally also contributes "Suppress tally/<rule>" actions.
  await expect(
    actions.filter({ hasText: /match majority casing|Suppress tally\//i }).first(),
  ).toBeVisible();
});
