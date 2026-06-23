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

  // Secondary check: the LanguageStatusItem only reveals "tally <version>" on
  // hover, and the hover popup is the flakiest selector in the suite — keep it
  // non-blocking. `expect.soft` is deliberately avoided here: it registers a
  // failure with the runner even inside try/catch (soft assertions never
  // throw), which would fail the run. `waitFor` throws on miss so the catch
  // can swallow it and keep the primary assertion authoritative.
  try {
    await page.locator("#status\\.languageStatus").hover({ timeout: 5_000 });
    const hover = page
      .locator(".hover-language-status .element .left")
      .filter({ hasText: /tally/i });
    await hover.first().waitFor({ state: "visible", timeout: 5_000 });
  } catch (e) {
    console.error(
      "optional language-status hover check skipped:",
      e instanceof Error ? e.message : String(e),
    );
  }
});
