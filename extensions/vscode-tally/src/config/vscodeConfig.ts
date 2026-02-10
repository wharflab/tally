import * as vscode from 'vscode';

export type ImportStrategy = 'fromEnvironment' | 'useBundled';
export type ConfigurationPreference = 'editorFirst' | 'filesystemFirst' | 'editorOnly';

export interface TallySettings {
  enable: boolean;
  path: string[];
  importStrategy: ImportStrategy;
  configuration: unknown | null;
  configurationPreference: ConfigurationPreference;
  fixUnsafe: boolean;
}

export interface BinaryResolutionSettings {
  path: string[];
  importStrategy: ImportStrategy;
}

const DEFAULTS: TallySettings = {
  enable: true,
  path: [],
  importStrategy: 'fromEnvironment',
  configuration: null,
  configurationPreference: 'editorFirst',
  fixUnsafe: false,
};

export function readEffectiveSettings(scope?: vscode.ConfigurationScope): TallySettings {
  const cfg = vscode.workspace.getConfiguration('tally', scope);
  return {
    enable: cfg.get<boolean>('enable', DEFAULTS.enable),
    path: cfg.get<string[]>('path', DEFAULTS.path),
    importStrategy: cfg.get<ImportStrategy>('importStrategy', DEFAULTS.importStrategy),
    configuration: cfg.get<unknown>('configuration', DEFAULTS.configuration),
    configurationPreference: cfg.get<ConfigurationPreference>(
      'configurationPreference',
      DEFAULTS.configurationPreference,
    ),
    fixUnsafe: cfg.get<boolean>('fixUnsafe', DEFAULTS.fixUnsafe),
  };
}

export function readUserBinarySettings(): BinaryResolutionSettings {
  const cfg = vscode.workspace.getConfiguration('tally');
  const pathInspect = cfg.inspect<string[]>('path');
  const importInspect = cfg.inspect<ImportStrategy>('importStrategy');

  return {
    path: pathInspect?.globalValue ?? pathInspect?.defaultValue ?? DEFAULTS.path,
    importStrategy:
      importInspect?.globalValue ?? importInspect?.defaultValue ?? DEFAULTS.importStrategy,
  };
}
