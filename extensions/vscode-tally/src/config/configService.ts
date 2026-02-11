import * as vscode from "vscode";

import {
  type BinaryResolutionSettings,
  type TallySettings,
  readEffectiveSettings,
  readUserBinarySettings,
} from "./vscodeConfig";

export interface WorkspaceFolderSettings {
  uri: string;
  name: string;
  settings: TallySettings;
}

export interface LspSettingsEnvelope {
  version: 1;
  global: TallySettings;
  workspaces: WorkspaceFolderSettings[];
}

export interface ConfigChange {
  requiresRestart: boolean;
}

export class ConfigService implements vscode.Disposable {
  private readonly emitter = new vscode.EventEmitter<ConfigChange>();
  public readonly onDidChange = this.emitter.event;

  private currentGlobal: TallySettings;
  private currentWorkspaces: WorkspaceFolderSettings[];
  private currentBinary: BinaryResolutionSettings;
  private currentUserBinary: BinaryResolutionSettings;

  private readonly disposables: vscode.Disposable[] = [];

  public constructor() {
    this.currentGlobal = readEffectiveSettings();
    this.currentWorkspaces = this.readWorkspaceFolders();
    this.currentUserBinary = readUserBinarySettings();
    this.currentBinary = {
      path: this.currentGlobal.path,
      importStrategy: this.currentGlobal.importStrategy,
    };

    this.disposables.push(
      vscode.workspace.onDidChangeConfiguration((e) => {
        if (e.affectsConfiguration("tally")) {
          this.refresh();
        }
      }),
      vscode.workspace.onDidChangeWorkspaceFolders(() => this.refresh()),
    );
  }

  public snapshot(): LspSettingsEnvelope {
    return {
      version: 1,
      global: this.currentGlobal,
      workspaces: this.currentWorkspaces,
    };
  }

  public lspSettings(): unknown {
    // Wrap under `tally` so the server can evolve without clashing with other clients.
    return { tally: this.snapshot() };
  }

  public binarySettings(): BinaryResolutionSettings {
    // Workspace trust gating: avoid executing workspace-local paths when untrusted.
    return vscode.workspace.isTrusted ? this.currentBinary : this.currentUserBinary;
  }

  public dispose(): void {
    for (const d of this.disposables) {
      d.dispose();
    }
    this.emitter.dispose();
  }

  private refresh(): void {
    const prevBinary = this.currentBinary;
    const prevUserBinary = this.currentUserBinary;
    const prevGlobal = this.currentGlobal;

    this.currentGlobal = readEffectiveSettings();
    this.currentWorkspaces = this.readWorkspaceFolders();
    this.currentBinary = {
      path: this.currentGlobal.path,
      importStrategy: this.currentGlobal.importStrategy,
    };
    this.currentUserBinary = readUserBinarySettings();

    const requiresRestart =
      !shallowEqualArray(prevBinary.path, this.currentBinary.path) ||
      prevBinary.importStrategy !== this.currentBinary.importStrategy ||
      !shallowEqualArray(prevUserBinary.path, this.currentUserBinary.path) ||
      prevUserBinary.importStrategy !== this.currentUserBinary.importStrategy ||
      prevGlobal.enable !== this.currentGlobal.enable;

    this.emitter.fire({ requiresRestart });
  }

  private readWorkspaceFolders(): WorkspaceFolderSettings[] {
    const folders = vscode.workspace.workspaceFolders ?? [];
    return folders.map((f) => ({
      uri: f.uri.toString(),
      name: f.name,
      settings: readEffectiveSettings(f.uri),
    }));
  }
}

function shallowEqualArray(a: readonly string[], b: readonly string[]): boolean {
  if (a === b) {
    return true;
  }
  if (a.length !== b.length) {
    return false;
  }
  for (let i = 0; i < a.length; i++) {
    if (a[i] !== b[i]) {
      return false;
    }
  }
  return true;
}
