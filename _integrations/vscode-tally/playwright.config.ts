import { defineConfig } from "@playwright/test";

// End-to-end UI tests that drive a *rendered* VS Code (via code-server) in a
// headless browser. This complements the in-extension-host smoke test under
// `test/` (@vscode/test-electron): that one asserts the programmatic LSP
// contract from inside the extension host, while this layer proves the
// behaviour actually surfaces in the workbench UI a user sees.
export default defineConfig({
  testDir: "./tests-ui",
  // VS Code UI work is stateful and chord-driven; keep it serialized.
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  // A single worker amortizes the one shared code-server and avoids the
  // focus races that plague parallel command-palette interactions.
  workers: 1,
  reporter: process.env.CI ? [["html", { open: "never" }], ["list"]] : "list",
  timeout: 60_000,
  expect: { timeout: 15_000 },
  use: {
    viewport: { width: 1512, height: 944 },
    actionTimeout: 15_000,
    video: "retain-on-failure",
    trace: "retain-on-failure",
    // Reading editor content goes through the clipboard (Monaco virtualizes
    // `.view-line`, so DOM scraping is unreliable).
    permissions: ["clipboard-read", "clipboard-write"],
    launchOptions: {
      // Needed only when running as root / in a container without user
      // namespaces; harmless on the non-root GitHub runner.
      args: process.env.CI ? ["--no-sandbox", "--disable-setuid-sandbox"] : [],
    },
  },
  projects: [
    // Builds the tally LSP binary and installs the packaged .vsix into a
    // private extensions dir (sha256-cached). Runs once before the specs.
    { name: "setup", testMatch: /extension\.setup\.ts$/ },
    {
      name: "ui",
      testMatch: /.*\.spec\.ts$/,
      dependencies: ["setup"],
      // channel:"chromium" = Chrome new-headless, which Playwright recommends
      // for high-accuracy UI/extension testing (needs the full chromium
      // download, not the headless shell).
      use: { browserName: "chromium", channel: "chromium", headless: true },
    },
    // Removes the .test_setup scratch dir after the specs finish.
    { name: "cleanup", testMatch: /extension\.teardown\.ts$/, dependencies: ["ui"] },
  ],
});
