const assert = require("node:assert");
const fs = require("node:fs/promises");
const path = require("node:path");
const {scheduler} = require('node:timers/promises')

const vscode = require("vscode");

const DEFAULT_EXPECTED_DIAGNOSTICS = 72;

function normalizeNewlines(s) {
  return s.replaceAll("\r\n", "\n");
}

function diagnosticSignature(d) {
  const code =
    typeof d.code === "string" || typeof d.code === "number"
      ? String(d.code)
      : d.code && typeof d.code === "object"
        ? String(d.code.value)
        : "";
  const range = `${d.range.start.line}:${d.range.start.character}-${d.range.end.line}:${d.range.end.character}`;
  return `${code}|${range}|${d.message}`;
}

function assertNoDuplicateDiagnostics(diagnostics) {
  const seen = new Set();
  const duplicates = [];
  for (const d of diagnostics) {
    const sig = diagnosticSignature(d);
    if (seen.has(sig)) {
      duplicates.push(sig);
    }
    seen.add(sig);
  }
  assert.strictEqual(
    duplicates.length,
    0,
    `expected no duplicate diagnostics, got ${duplicates.length} duplicates:\n${duplicates.join("\n")}`,
  );
}

async function waitForStableDiagnostics(uri, opts) {
  const timeoutMs = opts?.timeoutMs ?? 60_000;
  const pollIntervalMs = opts?.pollIntervalMs ?? 200;
  const stableForMs = opts?.stableForMs ?? 1_000;

  const deadline = Date.now() + timeoutMs;
  let lastSig = "";
  let lastChangeAt = Date.now();

  while (Date.now() < deadline) {
    const diags = vscode.languages.getDiagnostics(uri);
    const sig = diags.map(diagnosticSignature).join("\n");

    if (sig !== lastSig) {
      lastSig = sig;
      lastChangeAt = Date.now();
    }

    if (diags.length > 0 && Date.now() - lastChangeAt >= stableForMs) {
      return diags;
    }

    await scheduler.wait(pollIntervalMs);
  }

  const final = vscode.languages.getDiagnostics(uri);
  throw new Error(`timed out waiting for stable diagnostics (count=${final.length})`);
}

async function runSmoke() {
  const expectedDiagnostics = Number.parseInt(
    process.env.TALLY_EXPECTED_DIAGNOSTICS ?? String(DEFAULT_EXPECTED_DIAGNOSTICS),
    10,
  );

  const workspaceRoot = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
  assert.ok(workspaceRoot, "expected VS Code to be launched with a workspace folder");

  const ext = vscode.extensions.getExtension("wharflab.tally");
  assert.ok(ext, "expected wharflab.tally extension to be available");
  await ext.activate();

  const dockerfilePath = path.join(
    workspaceRoot,
    "internal",
    "integration",
    "testdata",
    "benchmark-real-world-fix",
    "Dockerfile",
  );
  const uri = vscode.Uri.file(dockerfilePath);

  const doc = await vscode.workspace.openTextDocument(uri);
  await vscode.window.showTextDocument(doc, { preview: false });

  if (doc.languageId !== "dockerfile") {
    await vscode.languages.setTextDocumentLanguage(doc, "dockerfile");
  }

  // Give the extension + language server time to initialize and publish/pull
  // initial diagnostics before we start asserting.
  await scheduler.wait(5_000);

  const diagnostics = await waitForStableDiagnostics(uri, { timeoutMs: 90_000 });
  assertNoDuplicateDiagnostics(diagnostics);
  assert.strictEqual(
    diagnostics.length,
    expectedDiagnostics,
    `expected ${expectedDiagnostics} diagnostics, got ${diagnostics.length}`,
  );

  const formatEdits = await vscode.commands.executeCommand(
    "vscode.executeFormatDocumentProvider",
    uri,
    { tabSize: 4, insertSpaces: true },
  );
  assert.ok(Array.isArray(formatEdits), "expected formatting result to be an array of TextEdit");

  const edit = new vscode.WorkspaceEdit();
  edit.set(uri, formatEdits);
  const applied = await vscode.workspace.applyEdit(edit);
  assert.ok(applied, "expected formatting edits to apply successfully");

  const formatted = normalizeNewlines(doc.getText());

  const expectedPath =
    process.env.TALLY_EXPECTED_FORMAT_SNAPSHOT ??
    path.join(
      workspaceRoot,
      "internal",
      "lsptest",
      "__snapshots__",
      "TestLSP_FormattingRealWorld_1.snap.Dockerfile",
    );
  const expected = normalizeNewlines(await fs.readFile(expectedPath, "utf8"));

  assert.strictEqual(formatted, expected, "formatted output mismatch");

  // Verify command-based "fix all" path too (workspace/executeCommand).
  await vscode.commands.executeCommand("tally.applyAllFixes");

  const fixedViaCommand = normalizeNewlines(doc.getText());
  assert.strictEqual(fixedViaCommand, expected, "fix-all command output mismatch");

  // Keep the workspace clean for future tests.
  await vscode.commands.executeCommand("workbench.action.files.revert");
}

async function run() {
  await runSmoke();
}

module.exports = { run };
