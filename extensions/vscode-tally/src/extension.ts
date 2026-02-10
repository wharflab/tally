import * as vscode from 'vscode';

import { findTallyBinary } from './binary/findBinary';
import { ConfigService } from './config/configService';
import { TallyLanguageClient } from './lsp/client';

let client: TallyLanguageClient | undefined;
let starting: Promise<void> | undefined;

export async function activate(context: vscode.ExtensionContext): Promise<void> {
  const output = vscode.window.createOutputChannel('Tally');
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
    vscode.commands.registerCommand('tally.restartServer', async () => {
      await startOrRestart('manual restart');
    }),
  );

  context.subscriptions.push(
    vscode.commands.registerCommand('tally.configureDefaultFormatterForDockerfile', async () => {
      const target =
        (vscode.workspace.workspaceFolders?.length ?? 0) > 0
          ? vscode.ConfigurationTarget.Workspace
          : vscode.ConfigurationTarget.Global;

      const editor = vscode.workspace.getConfiguration('editor', { languageId: 'dockerfile' });

      await editor.update('defaultFormatter', 'wharflab.tally', target, true);
      await editor.update('formatOnSave', true, target, true);
      await editor.update('formatOnSaveMode', 'file', target, true);

      const inspected = editor.inspect<unknown>('codeActionsOnSave');
      const existing =
        target === vscode.ConfigurationTarget.Global
          ? inspected?.globalLanguageValue
          : inspected?.workspaceLanguageValue;
      const next: Record<string, unknown> =
        existing && typeof existing === 'object' && !Array.isArray(existing)
          ? { ...(existing as Record<string, unknown>) }
          : {};

      if (next['source.fixAll.tally'] === undefined) {
        next['source.fixAll.tally'] = 'explicit';
      }

      await editor.update('codeActionsOnSave', next, target, true);

      const message =
        target === vscode.ConfigurationTarget.Global
          ? 'Tally configured as the default Dockerfile formatter in User settings.'
          : 'Tally configured as the default Dockerfile formatter for this workspace.';
      void vscode.window.showInformationMessage(
        message,
      );
    }),
  );

  configService.onDidChange(async (change) => {
    if (change.requiresRestart) {
      await startOrRestart('settings change');
      return;
    }
    await client?.sendConfiguration(configService.lspSettings());
  });

  context.subscriptions.push(
    vscode.workspace.onDidGrantWorkspaceTrust(() => {
      void startOrRestart('workspace trusted');
    }),
  );

  await startOrRestart('activation');
}

export async function deactivate(): Promise<void> {
  await client?.stop();
  client = undefined;
}
