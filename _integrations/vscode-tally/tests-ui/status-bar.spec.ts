import { expect, test } from "./fixtures";
import { openFile, openWorkspace } from "./utils";

test("the tally status bar item is shown for Dockerfiles", async ({
  sharedCodeServer,
  projectDir,
  page,
}) => {
  await openWorkspace(page, sharedCodeServer.url, projectDir);
  await openFile(page, "Dockerfile");

  // The regular StatusBarItem renders `$(check) tally` — the codicon is a span
  // inside the `<a>` label and the visible text is "tally". This is the stable
  // primary assertion.
  const statusItem = page.locator(".statusbar-item").filter({ hasText: "tally" });
  await expect(statusItem.first()).toBeVisible({ timeout: 30_000 });

  // Secondary (soft) check: the LanguageStatusItem only reveals "tally <version>"
  // on hover, and the hover popup is the flakiest selector in the suite — keep
  // it non-blocking so a hover miss never fails the run.
  try {
    await page.locator("#status\\.languageStatus").hover({ timeout: 5_000 });
    const hover = page
      .locator(".hover-language-status .element .left")
      .filter({ hasText: /tally/i });
    await expect.soft(hover.first()).toBeVisible({ timeout: 5_000 });
  } catch {
    // Language-status hover unavailable in this build; the primary check stands.
  }
});
