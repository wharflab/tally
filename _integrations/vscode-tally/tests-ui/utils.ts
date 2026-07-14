import { type Locator, type Page } from "@playwright/test";

const isMac = process.platform === "darwin";

/**
 * Rows in the Problems panel. Uses a defensive OR over the markers panel
 * container classes so the selector survives VS Code version differences.
 */
export function problemRows(page: Page): Locator {
  return page
    .locator(".markers-panel-container .monaco-list-row")
    .or(page.locator(".markers-panel .monaco-list-row"));
}

/**
 * Navigate code-server to a workspace folder and wait for the workbench to be
 * interactive. The readiness gate is a DOM chain (`[role="application"]` →
 * `.monaco-workbench` → `.statusbar`), never `networkidle` — code-server holds
 * a persistent websocket so the network never goes idle.
 */
export async function openWorkspace(page: Page, baseUrl: string, folder: string): Promise<void> {
  await page.goto(`${baseUrl}/?folder=${encodeURIComponent(folder)}`);
  await page.waitForSelector('[role="application"]', { timeout: 30_000 });
  await page.locator(".monaco-workbench").waitFor({ state: "visible", timeout: 30_000 });
  // The status bar is one of the last workbench parts to render.
  await page.locator(".statusbar").waitFor({ state: "visible", timeout: 30_000 });

  // Workspace trust is pre-disabled in settings, so the dialog should not
  // appear; dismiss it best-effort in case a build still shows it.
  try {
    await page.getByRole("button", { name: /trust the authors/i }).click({ timeout: 3_000 });
  } catch {
    // Expected path: dialog absent.
  }
}

/** Open the command palette and run `command` by its visible label. */
export async function runCommand(page: Page, command: string): Promise<void> {
  await withPaletteRetry(page, async () => {
    await page.keyboard.press(isMac ? "Meta+Shift+P" : "Control+Shift+P");
    const input = commandPaletteInput(page);
    await input.waitFor({ state: "visible", timeout: 5_000 });
    // Leading ">" forces command mode regardless of any sticky quick-open state.
    await input.fill(`>${command}`);
    await clickQuickInputRow(page, command, 15_000);
  });
}

/** Open a file and wait for the editor to mount. */
export async function openFile(page: Page, filename: string): Promise<void> {
  if (await openExplorerFile(page, filename)) {
    return;
  }

  await withPaletteRetry(page, async () => {
    await page.keyboard.press(isMac ? "Meta+P" : "Control+P");
    const input = page
      .getByRole("textbox", { name: /Search files by name/ })
      .or(page.locator(".quick-input-box input"));
    await input.waitFor({ state: "visible", timeout: 5_000 });
    await input.fill(filename);
    await clickQuickInputRow(page, filename, 30_000);
    await page.locator(".monaco-editor").first().waitFor({ state: "visible" });
  });
}

/**
 * Read the active editor's text via the clipboard. Monaco virtualizes
 * `.view-line` (only the visible window exists in the DOM and spaces render as
 * NBSP), so a clipboard round-trip is the robust way to read full content.
 * Uses the command-palette "Select All" to avoid OS-chord focus flakiness.
 */
export async function readEditorText(page: Page): Promise<string> {
  await page.getByRole("code").first().click();
  await runCommand(page, "Select All");
  await page.keyboard.press(isMac ? "Meta+C" : "Control+C");
  const text = await page.evaluate(() => navigator.clipboard.readText());
  // Normalize the NBSP (U+00A0) that Monaco uses for rendered whitespace.
  return text.replaceAll("\u00a0", " ");
}

function commandPaletteInput(page: Page): Locator {
  // The explicit role="textbox" was removed upstream, but the aria-label
  // placeholder is stable; fall back to the quick-input box container.
  return page
    .locator('input[aria-label="Type the name of a command to run."]')
    .or(page.locator(".quick-input-box input"));
}

function quickInputRow(page: Page, label: string): Locator {
  return page.locator(".quick-input-list .monaco-list-row").filter({ hasText: label }).first();
}

async function clickQuickInputRow(page: Page, label: string, timeout: number): Promise<void> {
  const row = quickInputRow(page, label);
  await row.waitFor({ state: "visible", timeout });
  await row.click({ timeout: 5_000 });
}

async function openExplorerFile(page: Page, filename: string): Promise<boolean> {
  const file = page.getByRole("treeitem", { name: filename }).first();
  try {
    await file.waitFor({ state: "visible", timeout: 3_000 });
    await file.click({ timeout: 5_000 });
    await page.locator(".monaco-editor").first().waitFor({ state: "visible", timeout: 30_000 });
    return true;
  } catch {
    return false;
  }
}

/** Retry a palette/quick-open interaction up to 3 times, pressing Escape between attempts. */
async function withPaletteRetry(page: Page, action: () => Promise<void>): Promise<void> {
  for (let attempt = 0; attempt < 3; attempt++) {
    try {
      await action();
      return;
    } catch (err) {
      if (attempt === 2) {
        throw err;
      }
      if (page.isClosed()) {
        throw err;
      }
      await page.keyboard.press("Escape").catch(() => {});
      await page.waitForTimeout(500);
    }
  }
}
