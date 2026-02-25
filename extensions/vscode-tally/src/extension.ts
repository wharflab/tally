import * as vscode from "vscode";
import { State } from "vscode-languageclient/node";

import { findTallyBinary } from "./binary/findBinary";
import { findTallyViaPythonEnvs, getPythonEnvApi } from "./binary/pythonEnvs";
import { ConfigService } from "./config/configService";
import { TallyLanguageClient } from "./lsp/client";

let client: TallyLanguageClient | undefined;
let starting: Promise<void> | undefined;
let expectedStopCount = 0;
let detachObserversForDeactivate: (() => void) | undefined;

function beginExpectedStop(): () => void {
  expectedStopCount += 1;
  let released = false;
  return () => {
    if (released) {
      return;
    }
    released = true;
    expectedStopCount = Math.max(0, expectedStopCount - 1);
  };
}

function isStopExpected(): boolean {
  return expectedStopCount > 0;
}

export async function activate(context: vscode.ExtensionContext): Promise<void> {
  await vscode.commands.executeCommand("setContext", "tally.serverRunning", false);

  const output = vscode.window.createOutputChannel("Tally", { log: true });
  const traceOutput = vscode.window.createOutputChannel("Tally (LSP)", { log: true });
  const configService = new ConfigService();
  context.subscriptions.push(output, traceOutput, configService);

  const statusBar = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Right, 100);
  statusBar.command = "tally.showOutput";
  statusBar.show();
  context.subscriptions.push(statusBar);

  const languageStatus = vscode.languages.createLanguageStatusItem("tally.server", "dockerfile");
  languageStatus.name = "Tally Language Server";
  context.subscriptions.push(languageStatus);

  let serverStopNoticeInFlight = false;
  let stateSubscription: vscode.Disposable | undefined;
  let watchdogSubscription: vscode.Disposable | undefined;
  let unexpectedStopTimer: ReturnType<typeof setTimeout> | undefined;
  let pendingRestart = false;

  function applyServerStateUi(state: "disabled" | "starting" | "running" | "stopped"): void {
    if (state === "disabled") {
      statusBar.text = "$(circle-slash) tally";
      statusBar.tooltip = "Tally language server is disabled";
      statusBar.backgroundColor = new vscode.ThemeColor("statusBarItem.warningBackground");
      languageStatus.text = "tally disabled";
      languageStatus.detail = "Enable `tally.enable` to start the language server.";
      languageStatus.severity = vscode.LanguageStatusSeverity.Information;
      return;
    }
    if (state === "starting") {
      statusBar.text = "$(sync~spin) tally";
      statusBar.tooltip = "Tally language server is starting";
      statusBar.backgroundColor = new vscode.ThemeColor("statusBarItem.warningBackground");
      languageStatus.text = "tally starting";
      languageStatus.detail = "Tally language server is starting.";
      languageStatus.severity = vscode.LanguageStatusSeverity.Information;
      return;
    }
    if (state === "running") {
      statusBar.text = "$(check) tally";
      statusBar.tooltip = "Tally language server is running";
      statusBar.backgroundColor = undefined;
      languageStatus.severity = vscode.LanguageStatusSeverity.Information;
      return;
    }
    statusBar.text = "$(error) tally";
    statusBar.tooltip = "Tally language server is stopped";
    statusBar.backgroundColor = new vscode.ThemeColor("statusBarItem.errorBackground");
    languageStatus.severity = vscode.LanguageStatusSeverity.Error;
  }

  function updateLanguageStatusForClient(current?: TallyLanguageClient): void {
    if (!current) {
      languageStatus.text = "tally unavailable";
      languageStatus.detail = "No Tally language server process is active.";
      languageStatus.severity = vscode.LanguageStatusSeverity.Warning;
      return;
    }

    const version = current.serverVersion() ?? "unknown";
    languageStatus.text = `tally ${version}`;
    languageStatus.detail = `source: ${current.serverSource()} • executable: ${current.executablePath()}`;
    languageStatus.severity = vscode.LanguageStatusSeverity.Information;
  }

  async function promptServerStopped(message: string): Promise<void> {
    if (serverStopNoticeInFlight) {
      return;
    }
    serverStopNoticeInFlight = true;
    try {
      const action = await vscode.window.showErrorMessage(
        `Tally: ${message}`,
        "Restart Server",
        "Show Output",
        "Show LSP Trace",
      );
      if (action === "Restart Server") {
        void startOrRestart("crash recovery");
      } else if (action === "Show Output") {
        output.show(true);
      } else if (action === "Show LSP Trace") {
        traceOutput.show(true);
      }
    } finally {
      serverStopNoticeInFlight = false;
    }
  }

  function clearUnexpectedStopTimer(): void {
    if (unexpectedStopTimer) {
      clearTimeout(unexpectedStopTimer);
      unexpectedStopTimer = undefined;
    }
  }

  function detachClientObservers(): void {
    clearUnexpectedStopTimer();
    stateSubscription?.dispose();
    stateSubscription = undefined;
    watchdogSubscription?.dispose();
    watchdogSubscription = undefined;
  }

  detachObserversForDeactivate = detachClientObservers;

  function attachClientObservers(current: TallyLanguageClient): void {
    detachClientObservers();

    watchdogSubscription = current.onWatchdogStop((message) => {
      if (current !== client) {
        return;
      }
      output.appendLine(`[tally] ${message}`);
      void promptServerStopped(message);
    });

    stateSubscription = current.onDidChangeState(({ oldState, newState }) => {
      if (current !== client) {
        return;
      }
      output.appendLine(
        `[tally] language client state: ${stateLabel(oldState)} -> ${stateLabel(newState)}`,
      );

      if (newState === State.Running) {
        clearUnexpectedStopTimer();
        void vscode.commands.executeCommand("setContext", "tally.serverRunning", true);
        applyServerStateUi("running");
        updateLanguageStatusForClient(current);
        return;
      }

      if (newState === State.Starting) {
        clearUnexpectedStopTimer();
        void vscode.commands.executeCommand("setContext", "tally.serverRunning", false);
        applyServerStateUi("starting");
        return;
      }

      void vscode.commands.executeCommand("setContext", "tally.serverRunning", false);
      applyServerStateUi(configService.snapshot().global.enable ? "stopped" : "disabled");

      if (isStopExpected() || !configService.snapshot().global.enable) {
        return;
      }

      clearUnexpectedStopTimer();
      unexpectedStopTimer = setTimeout(() => {
        unexpectedStopTimer = undefined;
        if (current !== client || isStopExpected()) {
          return;
        }
        if (current.state() !== State.Stopped || !configService.snapshot().global.enable) {
          return;
        }
        void promptServerStopped("language server stopped unexpectedly.");
      }, 750);
    });
  }

  applyServerStateUi("starting");
  updateLanguageStatusForClient();

  async function startOrRestart(reason: string): Promise<void> {
    let runReason = reason;
    while (true) {
      if (starting) {
        pendingRestart = true;
        await starting;
        if (!pendingRestart) {
          return;
        }
        runReason = "queued restart";
        continue;
      }

      pendingRestart = false;
      starting = (async () => {
        const cfg = configService.snapshot();
        if (!cfg.global.enable) {
          const release = beginExpectedStop();
          try {
            await client?.stop();
          } finally {
            release();
          }
          detachClientObservers();
          client = undefined;
          applyServerStateUi("disabled");
          updateLanguageStatusForClient();
          await vscode.commands.executeCommand("setContext", "tally.serverRunning", false);
          return;
        }

        const binarySettings = configService.binarySettings();
        const resolved = await findTallyBinary({
          extensionContext: context,
          isTrusted: vscode.workspace.isTrusted,
          settings: binarySettings,
          workspaceFolders: vscode.workspace.workspaceFolders ?? [],
          pythonEnvResolver: findTallyViaPythonEnvs,
          output,
        });

        const settingsEnvelope = configService.lspSettings();

        if (client && client.serverKey() === resolved.key) {
          await client.sendConfiguration(settingsEnvelope);
          updateLanguageStatusForClient(client);
          return;
        }

        const release = beginExpectedStop();
        try {
          await client?.stop();
        } finally {
          release();
        }

        detachClientObservers();
        client = new TallyLanguageClient({
          output,
          traceOutput,
          server: resolved,
        });
        attachClientObservers(client);
        updateLanguageStatusForClient(client);
        applyServerStateUi("starting");
        output.appendLine(
          `[tally] starting language server (${runReason}) using ${resolved.source}: ${resolved.executablePath}`,
        );

        try {
          await client.start();
          await client.sendConfiguration(settingsEnvelope);
        } catch (err) {
          const stopRelease = beginExpectedStop();
          try {
            await client.stop();
          } finally {
            stopRelease();
          }

          detachClientObservers();
          client = undefined;
          await vscode.commands.executeCommand("setContext", "tally.serverRunning", false);
          applyServerStateUi("stopped");
          updateLanguageStatusForClient();

          const msg = err instanceof Error ? err.message : String(err);
          output.appendLine(`[tally] failed to start language server (${runReason}): ${msg}`);
          void vscode.window.showErrorMessage(`Tally: failed to start language server: ${msg}`);
        }
      })().finally(() => {
        starting = undefined;
      });

      await starting;
      if (!pendingRestart) {
        return;
      }
      runReason = "queued restart";
    }
  }

  context.subscriptions.push(
    vscode.commands.registerCommand("tally.restartServer", async () => {
      await startOrRestart("manual restart");
    }),
  );

  context.subscriptions.push(
    vscode.commands.registerCommand("tally.showOutput", () => {
      output.show(true);
    }),
    vscode.commands.registerCommand("tally.showLspTrace", () => {
      traceOutput.show(true);
    }),
  );

  context.subscriptions.push(
    vscode.commands.registerCommand("tally.configureDefaultFormatterForDockerfile", async () => {
      const target =
        (vscode.workspace.workspaceFolders?.length ?? 0) > 0
          ? vscode.ConfigurationTarget.Workspace
          : vscode.ConfigurationTarget.Global;

      const editor = vscode.workspace.getConfiguration("editor", { languageId: "dockerfile" });

      const languageValueForTarget = <T>(
        result: { globalLanguageValue?: T; workspaceLanguageValue?: T } | undefined,
      ): T | undefined => {
        if (!result) {
          return undefined;
        }
        return target === vscode.ConfigurationTarget.Global
          ? result.globalLanguageValue
          : result.workspaceLanguageValue;
      };

      const defaultFormatterInspected = editor.inspect<string>("defaultFormatter");
      const formatOnSaveInspected = editor.inspect<boolean>("formatOnSave");
      const formatOnSaveModeInspected = editor.inspect<string>("formatOnSaveMode");
      const inspected = editor.inspect<unknown>("codeActionsOnSave");

      const originalDefaultFormatter = languageValueForTarget(defaultFormatterInspected);
      const originalFormatOnSave = languageValueForTarget(formatOnSaveInspected);
      const originalFormatOnSaveMode = languageValueForTarget(formatOnSaveModeInspected);
      const existing = languageValueForTarget(inspected);
      const next: Record<string, unknown> =
        existing && typeof existing === "object" && !Array.isArray(existing)
          ? { ...(existing as Record<string, unknown>) }
          : {};

      if (next["source.fixAll.tally"] === undefined) {
        next["source.fixAll.tally"] = "explicit";
      }

      try {
        await editor.update("defaultFormatter", "wharflab.tally", target, true);
        await editor.update("formatOnSave", true, target, true);
        await editor.update("formatOnSaveMode", "file", target, true);
        await editor.update("codeActionsOnSave", next, target, true);
      } catch (err) {
        try {
          await editor.update("defaultFormatter", originalDefaultFormatter, target, true);
          await editor.update("formatOnSave", originalFormatOnSave, target, true);
          await editor.update("formatOnSaveMode", originalFormatOnSaveMode, target, true);
          await editor.update("codeActionsOnSave", existing, target, true);
        } catch (restoreErr) {
          output.appendLine(
            `[tally] failed to restore editor settings after an error: ${String(restoreErr)}`,
          );
        }

        const msg = err instanceof Error ? err.message : String(err);
        output.appendLine(`[tally] failed to configure default Dockerfile formatter: ${msg}`);
        void vscode.window.showErrorMessage(
          `Tally: failed to configure default Dockerfile formatter: ${msg}`,
        );
        throw err;
      }

      const message =
        target === vscode.ConfigurationTarget.Global
          ? "Tally configured as the default Dockerfile formatter in User settings."
          : "Tally configured as the default Dockerfile formatter for this workspace.";
      void vscode.window.showInformationMessage(message);
    }),
  );

  configService.onDidChange(async (change) => {
    if (change.requiresRestart) {
      await startOrRestart("settings change");
      return;
    }
    await client?.sendConfiguration(configService.lspSettings());
  });

  context.subscriptions.push(
    vscode.workspace.onDidGrantWorkspaceTrust(() => {
      void startOrRestart("workspace trusted");
    }),
  );

  const pythonEnvApi = await getPythonEnvApi();
  if (pythonEnvApi) {
    context.subscriptions.push(
      pythonEnvApi.onDidChangeEnvironment(() => {
        void startOrRestart("python environment changed");
      }),
    );
  }

  await startOrRestart("activation");
}

export async function deactivate(): Promise<void> {
  detachObserversForDeactivate?.();
  detachObserversForDeactivate = undefined;
  const release = beginExpectedStop();
  try {
    await client?.stop();
  } finally {
    release();
  }
  client = undefined;
}

function stateLabel(state: State): string {
  if (state === State.Starting) {
    return "Starting";
  }
  if (state === State.Running) {
    return "Running";
  }
  return "Stopped";
}
