import * as vscode from 'vscode';
import {
  type Executable,
  LanguageClient,
  type LanguageClientOptions,
  type ServerOptions,
} from 'vscode-languageclient/node';

import { type ResolvedBinary } from '../binary/findBinary';

export interface TallyLanguageClientInit {
  output: vscode.OutputChannel;
  server: ResolvedBinary;
}

export class TallyLanguageClient {
  private readonly client: LanguageClient;
  private readonly server: ResolvedBinary;

  public constructor(init: TallyLanguageClientInit) {
    this.server = init.server;

    const executable: Executable = {
      command: init.server.command,
      args: init.server.args,
    };

    const serverOptions: ServerOptions = executable;

    const clientOptions: LanguageClientOptions = {
      outputChannel: init.output,
      documentSelector: [
        { language: 'dockerfile', scheme: 'file' },
        { language: 'dockerfile', scheme: 'untitled' },
      ],
      // VS Code supports the LSP 3.17 pull diagnostics model. Disable push
      // diagnostics to avoid duplicate diagnostics when both are enabled.
      initializationOptions: {
        disablePushDiagnostics: true,
      },
      middleware: {
        executeCommand: async (command, args, next) => {
          if (command !== 'tally.applyAllFixes') {
            return next(command, args);
          }

          const resolvedArgs = this.resolveApplyAllFixesArgs(args);
          if (resolvedArgs.length === 0) {
            return;
          }

          const result = await next(command, resolvedArgs);
          await applyWorkspaceEditResult(result);
          return result;
        },
      },
    };

    this.client = new LanguageClient('tally', 'Tally', serverOptions, clientOptions);
  }

  public serverKey(): string {
    return this.server.key;
  }

  public async start(): Promise<void> {
    await this.client.start();
  }

  public async stop(): Promise<void> {
    await this.client.stop();
  }

  public async sendConfiguration(settings: unknown): Promise<void> {
    await this.client.sendNotification('workspace/didChangeConfiguration', { settings });
  }

  private resolveApplyAllFixesArgs(args: unknown[]): unknown[] {
    if (args.length > 0) {
      return args;
    }

    const editor = vscode.window.activeTextEditor;
    if (!editor) {
      void vscode.window.showErrorMessage('Tally: no active editor to fix.');
      return [];
    }

    const uri = editor.document.uri.toString();
    const unsafe = vscode.workspace
      .getConfiguration('tally', editor.document.uri)
      .get<boolean>('fixUnsafe', false);

    return [{ uri, unsafe }];
  }
}

type WorkspaceEditWire = {
  changes?: Record<string, Array<{ range: unknown; newText: unknown }>>;
};

async function applyWorkspaceEditResult(result: unknown): Promise<void> {
  const edit = toWorkspaceEdit(result);
  if (!edit) {
    return;
  }

  const applied = await vscode.workspace.applyEdit(edit);
  if (!applied) {
    void vscode.window.showErrorMessage('Tally: failed to apply fixes.');
  }
}

function toWorkspaceEdit(result: unknown): vscode.WorkspaceEdit | undefined {
  if (!result || typeof result !== 'object') {
    return undefined;
  }

  const wire = result as WorkspaceEditWire;
  if (!wire.changes || typeof wire.changes !== 'object') {
    return undefined;
  }

  const edit = new vscode.WorkspaceEdit();

  for (const [uriString, edits] of Object.entries(wire.changes)) {
    if (!Array.isArray(edits) || edits.length === 0) {
      continue;
    }
    const uri = vscode.Uri.parse(uriString);
    const vscodeEdits: vscode.TextEdit[] = [];
    for (const e of edits) {
      if (!e || typeof e !== 'object') {
        continue;
      }
      const range = (e as any).range;
      const newText = (e as any).newText;
      if (
        !range ||
        typeof newText !== 'string' ||
        !range.start ||
        !range.end ||
        typeof range.start.line !== 'number' ||
        typeof range.start.character !== 'number' ||
        typeof range.end.line !== 'number' ||
        typeof range.end.character !== 'number'
      ) {
        continue;
      }

      vscodeEdits.push(
        new vscode.TextEdit(
          new vscode.Range(
            range.start.line,
            range.start.character,
            range.end.line,
            range.end.character,
          ),
          newText,
        ),
      );
    }

    if (vscodeEdits.length > 0) {
      edit.set(uri, vscodeEdits);
    }
  }

  return edit;
}
