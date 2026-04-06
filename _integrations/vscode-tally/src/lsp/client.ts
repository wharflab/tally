import * as path from "node:path";

import * as vscode from "vscode";
import {
  CloseAction,
  DidChangeConfigurationNotification,
  DocumentDiagnosticRequest,
  ErrorCodes,
  ErrorAction,
  type Executable,
  type ErrorHandler,
  type MessageSignature,
  type ResponseError,
  LanguageClient,
  LSPErrorCodes,
  type LanguageClientOptions,
  NotebookDocumentFilter,
  SemanticTokensDeltaRequest,
  SemanticTokensRangeRequest,
  SemanticTokensRequest,
  type State,
  type ServerOptions,
  type StateChangeEvent,
  TextDocumentFilter,
  WorkspaceDiagnosticRequest,
} from "vscode-languageclient/node";

import { type BinarySource, type ResolvedBinary } from "../binary/findBinary";

export interface TallyLanguageClientInit {
  output: vscode.OutputChannel;
  traceOutput: vscode.OutputChannel;
  server: ResolvedBinary;
  settings: unknown;
}

export class TallyLanguageClient {
  private readonly client: LanguageClient;
  private readonly server: ResolvedBinary;
  private readonly watchdogStopEmitter = new vscode.EventEmitter<string>();
  public readonly onWatchdogStop = this.watchdogStopEmitter.event;

  public constructor(init: TallyLanguageClientInit) {
    this.server = init.server;

    const executable: Executable = {
      command: init.server.command,
      args: init.server.args,
    };

    const serverOptions: ServerOptions = executable;

    const watchdogMaxRestartCount = 4;
    const watchdogWindowMs = 3 * 60 * 1000;
    const restarts: number[] = [];

    const errorHandler: ErrorHandler = {
      error: (_error, _message, count) => {
        if (count && count <= 3) {
          return { action: ErrorAction.Continue };
        }
        return { action: ErrorAction.Shutdown };
      },
      closed: () => {
        restarts.push(Date.now());
        if (restarts.length <= watchdogMaxRestartCount) {
          return { action: CloseAction.Restart };
        }

        const elapsedMs = restarts[restarts.length - 1] - restarts[0];
        if (elapsedMs <= watchdogWindowMs) {
          const message = `The Tally server crashed ${watchdogMaxRestartCount + 1} times in the last 3 minutes. The server will not be restarted.`;
          this.watchdogStopEmitter.fire(message);
          return { action: CloseAction.DoNotRestart, message };
        }

        restarts.shift();
        return { action: CloseAction.Restart };
      },
    };

    const clientOptions: LanguageClientOptions = {
      outputChannel: init.output,
      traceOutputChannel: init.traceOutput,
      documentSelector: [
        { language: "dockerfile", scheme: "file" },
        { language: "dockerfile", scheme: "untitled" },
      ],
      connectionOptions: {
        maxRestartCount: watchdogMaxRestartCount,
      },
      // VS Code supports the LSP 3.17 pull diagnostics model. Disable push
      // diagnostics to avoid duplicate diagnostics when both are enabled.
      initializationOptions: {
        disablePushDiagnostics: true,
        ...(typeof init.settings === "object" && init.settings !== null
          ? (init.settings as Record<string, unknown>)
          : {}),
      },
      diagnosticPullOptions: {
        onChange: true,
        onSave: true,
        onTabs: true,
        match(documentSelector, resource) {
          if (!isLikelyDockerfileResource(resource)) {
            return false;
          }

          for (const selector of documentSelector) {
            if (typeof selector === "string") {
              if (selector === "dockerfile") {
                return true;
              }
              continue;
            }

            if (NotebookDocumentFilter.is(selector)) {
              continue;
            }

            if (TextDocumentFilter.is(selector)) {
              if (selector.language !== undefined && selector.language !== "dockerfile") {
                continue;
              }
              if (selector.scheme !== undefined && selector.scheme !== resource.scheme) {
                continue;
              }
              if (selector.pattern !== undefined) {
                continue;
              }
              return true;
            }
          }

          return false;
        },
      },
      errorHandler,
      middleware: {
        executeCommand: async (command, args, next) => {
          if (command !== "tally.applyAllFixes") {
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

    this.client = new TallyVsCodeLanguageClient(
      "tally",
      "Tally",
      serverOptions,
      clientOptions,
      init.output,
    );
  }

  public serverKey(): string {
    return this.server.key;
  }

  public serverSource(): BinarySource {
    return this.server.source;
  }

  public serverVersion(): string | undefined {
    return this.server.version;
  }

  public executablePath(): string {
    return this.server.executablePath;
  }

  public state(): State {
    return this.client.state;
  }

  public onDidChangeState(listener: (event: StateChangeEvent) => void): vscode.Disposable {
    return this.client.onDidChangeState(listener);
  }

  public async start(): Promise<void> {
    await this.client.start();
  }

  public async stop(): Promise<void> {
    await this.client.stop();
  }

  public async sendConfiguration(settings: unknown): Promise<void> {
    await this.client.sendNotification(DidChangeConfigurationNotification.type, { settings });
  }

  private resolveApplyAllFixesArgs(args: unknown[]): unknown[] {
    if (args.length === 0) {
      const editor = vscode.window.activeTextEditor;
      if (!editor) {
        void vscode.window.showErrorMessage("Tally: no active editor to fix.");
        return [];
      }

      return [this.applyAllFixesArgsForUri(editor.document.uri.toString())];
    }

    const [first, second, ...rest] = args;
    if (typeof first === "string") {
      const unsafe =
        typeof second === "boolean" ? second : this.fixUnsafeForUri(vscode.Uri.parse(first));
      return [{ uri: first, unsafe }, ...rest];
    }

    if (isApplyAllFixesArg(first) && typeof first.unsafe !== "boolean") {
      return [{ ...first, unsafe: this.fixUnsafeForUri(vscode.Uri.parse(first.uri)) }, ...rest];
    }

    return args;
  }

  private applyAllFixesArgsForUri(uri: string): { uri: string; unsafe: boolean } {
    return { uri, unsafe: this.fixUnsafeForUri(vscode.Uri.parse(uri)) };
  }

  private fixUnsafeForUri(uri: vscode.Uri): boolean {
    return vscode.workspace.getConfiguration("tally", uri).get("fixUnsafe", false);
  }
}

class TallyVsCodeLanguageClient extends LanguageClient {
  private readonly output: vscode.OutputChannel;

  public constructor(
    id: string,
    name: string,
    serverOptions: ServerOptions,
    clientOptions: LanguageClientOptions,
    output: vscode.OutputChannel,
  ) {
    super(id, name, serverOptions, clientOptions);
    this.output = output;
  }

  public override handleFailedRequest<T>(
    type: MessageSignature,
    token: vscode.CancellationToken | undefined,
    error: unknown,
    defaultValue: T,
    showNotification = true,
  ): T {
    if (isSilentRequestFailure(type, token, error)) {
      return defaultValue;
    }

    if (isBackgroundRequest(type)) {
      this.output.appendLine(
        `[tally] background request ${type.method} failed: ${describeError(error)}`,
      );
      return defaultValue;
    }

    return super.handleFailedRequest(type, token, error, defaultValue, showNotification);
  }
}

function isLikelyDockerfileResource(resource: vscode.Uri): boolean {
  if (resource.scheme !== "file" && resource.scheme !== "untitled") {
    return false;
  }

  const name = path.posix.basename(resource.path).toLowerCase();

  if (resource.scheme === "untitled") {
    if (!name) {
      return false;
    }
    return (
      name === "dockerfile" ||
      name === "containerfile" ||
      name.startsWith("dockerfile.") ||
      name.startsWith("containerfile.")
    );
  }

  if (!name) {
    return false;
  }

  return (
    name === "dockerfile" ||
    name === "containerfile" ||
    name.startsWith("dockerfile.") ||
    name.startsWith("containerfile.") ||
    name.endsWith(".dockerfile")
  );
}

function isBackgroundRequest(type: MessageSignature): boolean {
  return (
    type.method === DocumentDiagnosticRequest.method ||
    type.method === WorkspaceDiagnosticRequest.method ||
    type.method === SemanticTokensRequest.method ||
    type.method === SemanticTokensDeltaRequest.method ||
    type.method === SemanticTokensRangeRequest.method
  );
}

function isSilentRequestFailure(
  type: MessageSignature,
  token: vscode.CancellationToken | undefined,
  error: unknown,
): boolean {
  if (!isBackgroundRequest(type)) {
    return false;
  }

  if (token?.isCancellationRequested) {
    return true;
  }

  const responseError = asResponseError(error);
  if (responseError) {
    switch (responseError.code) {
      case LSPErrorCodes.RequestCancelled:
      case LSPErrorCodes.ServerCancelled:
      case LSPErrorCodes.ContentModified:
      case ErrorCodes.PendingResponseRejected:
      case ErrorCodes.ConnectionInactive:
        return true;
      default:
        break;
    }
  }
  return false;
}

function asResponseError(error: unknown): ResponseError<unknown> | undefined {
  if (!error || typeof error !== "object") {
    return undefined;
  }

  if (!("code" in error) || typeof (error as { code?: unknown }).code !== "number") {
    return undefined;
  }

  if (!("message" in error) || typeof (error as { message?: unknown }).message !== "string") {
    return undefined;
  }

  return error as ResponseError<unknown>;
}

function describeError(error: unknown): string {
  if (error instanceof Error && error.message) {
    return error.message;
  }
  if (typeof error === "string") {
    return error;
  }
  const responseError = asResponseError(error);
  if (responseError) {
    return responseError.message;
  }
  return String(error);
}

type WorkspaceEditWire = {
  changes?: Record<string, Array<{ range: unknown; newText: unknown }>>;
};

type ApplyAllFixesArg = {
  uri: string;
  unsafe?: boolean;
};

function isApplyAllFixesArg(value: unknown): value is ApplyAllFixesArg {
  return (
    !!value &&
    typeof value === "object" &&
    "uri" in value &&
    typeof (value as { uri?: unknown }).uri === "string"
  );
}

async function applyWorkspaceEditResult(result: unknown): Promise<void> {
  const edit = toWorkspaceEdit(result);
  if (!edit) {
    return;
  }

  const applied = await vscode.workspace.applyEdit(edit);
  if (!applied) {
    void vscode.window.showErrorMessage("Tally: failed to apply fixes.");
  }
}

function toWorkspaceEdit(result: unknown): vscode.WorkspaceEdit | undefined {
  if (!result || typeof result !== "object") {
    return undefined;
  }

  const wire = result as WorkspaceEditWire;
  if (!wire.changes || typeof wire.changes !== "object") {
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
      if (!e || typeof e !== "object") {
        continue;
      }
      const range = (e as any).range;
      const newText = (e as any).newText;
      if (
        !range ||
        typeof newText !== "string" ||
        !range.start ||
        !range.end ||
        typeof range.start.line !== "number" ||
        typeof range.start.character !== "number" ||
        typeof range.end.line !== "number" ||
        typeof range.end.character !== "number"
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
