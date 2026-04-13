import * as vscode from "vscode";

export type ImportStrategy = "fromEnvironment" | "useBundled";
export type ConfigurationPreference = "editorFirst" | "filesystemFirst" | "editorOnly";

export type FixAllMode = "all" | "problems";

export interface TallySettings {
  enable: boolean;
  path: string[];
  importStrategy: ImportStrategy;
  configuration?: unknown;
  configurationPreference: ConfigurationPreference;
  fixUnsafe: boolean;
  suppressRuleEnabled: boolean;
  showDocumentationEnabled: boolean;
  fixAllMode: FixAllMode;
}

export interface TallyLspSettings extends TallySettings {
  workspaceTrusted: boolean;
}

export interface BinaryResolutionSettings {
  path: string[];
  importStrategy: ImportStrategy;
}

const DEFAULTS: TallyLspSettings = {
  enable: true,
  path: [],
  importStrategy: "fromEnvironment",
  configuration: null,
  configurationPreference: "editorFirst",
  fixUnsafe: false,
  suppressRuleEnabled: true,
  showDocumentationEnabled: true,
  fixAllMode: "all",
  workspaceTrusted: false,
};

export function readEffectiveSettings(scope?: vscode.ConfigurationScope): TallyLspSettings {
  const cfg = vscode.workspace.getConfiguration("tally", scope);
  return {
    enable: cfg.get("enable", DEFAULTS.enable),
    path: cfg.get("path", DEFAULTS.path),
    importStrategy: cfg.get<ImportStrategy>("importStrategy", DEFAULTS.importStrategy),
    configuration: cfg.get("configuration", DEFAULTS.configuration),
    configurationPreference: cfg.get<ConfigurationPreference>(
      "configurationPreference",
      DEFAULTS.configurationPreference,
    ),
    fixUnsafe: cfg.get("fixUnsafe", DEFAULTS.fixUnsafe),
    suppressRuleEnabled: cfg.get(
      "codeAction.suppressRule.enable",
      DEFAULTS.suppressRuleEnabled,
    ),
    showDocumentationEnabled: cfg.get(
      "codeAction.showDocumentation.enable",
      DEFAULTS.showDocumentationEnabled,
    ),
    fixAllMode: cfg.get<FixAllMode>("fixAll.mode", DEFAULTS.fixAllMode),
    workspaceTrusted: vscode.workspace.isTrusted,
  };
}

export function readUserBinarySettings(): BinaryResolutionSettings {
  const cfg = vscode.workspace.getConfiguration("tally");
  const pathInspect = cfg.inspect<string[]>("path");
  const importInspect = cfg.inspect<ImportStrategy>("importStrategy");

  return {
    path: pathInspect?.globalValue ?? pathInspect?.defaultValue ?? DEFAULTS.path,
    importStrategy:
      importInspect?.globalValue ?? importInspect?.defaultValue ?? DEFAULTS.importStrategy,
  };
}
