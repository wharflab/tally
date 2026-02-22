import * as vscode from "vscode";

import { findTallyBinary } from "./binary/findBinary";
import { findTallyViaPythonEnvs, getPythonEnvApi } from "./binary/pythonEnvs";
import { ConfigService } from "./config/configService";
import { TallyLanguageClient } from "./lsp/client";

let client: TallyLanguageClient | undefined;
let starting: Promise<void> | undefined;

export async function activate(context: vscode.ExtensionContext): Promise<void> {
  const output = vscode.window.createOutputChannel("Tally");
  const configService = new ConfigService();
  context.subscriptions.push(output, configService);

  async function startOrRestart(reason: string): Promise<void> {
    if (starting) {
      await starting;
      return;
    }
    starting = (async () => {
      const cfg = configService.snapshot();
      if (!cfg.global.enable) {
        await client?.stop();
        client = undefined;
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
        return;
      }

      await client?.stop();
      client = new TallyLanguageClient({
        output,
        server: resolved,
      });

      try {
        await client.start();
        await client.sendConfiguration(settingsEnvelope);
      } catch (err) {
        await client.stop();
        client = undefined;

        const msg = err instanceof Error ? err.message : String(err);
        output.appendLine(`[tally] failed to start language server (${reason}): ${msg}`);
        void vscode.window.showErrorMessage(`Tally: failed to start language server: ${msg}`);
      }
    })().finally(() => {
      starting = undefined;
    });
    await starting;
  }

  context.subscriptions.push(
    vscode.commands.registerCommand("tally.restartServer", async () => {
      await startOrRestart("manual restart");
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
  await client?.stop();
  client = undefined;
}
