import type * as vscode from "vscode";

import { constants as fsConstants } from "node:fs";
import * as fs from "node:fs/promises";
import * as os from "node:os";
import * as path from "node:path";

import { type BinaryResolutionSettings } from "../config/vscodeConfig";
import { validateUserSuppliedPath } from "./pathValidator";

export type BinarySource =
  | "explicitPath"
  | "workspaceNpm"
  | "pythonEnvExt"
  | "workspaceVenv"
  | "envPath"
  | "bundled";

export interface ResolvedBinary {
  command: string;
  args: string[];
  key: string;
  source: BinarySource;
}

export interface FindTallyBinaryInput {
  extensionContext: vscode.ExtensionContext;
  isTrusted: boolean;
  settings: BinaryResolutionSettings;
  workspaceFolders: readonly vscode.WorkspaceFolder[];
  pythonEnvResolver?: (folder: vscode.WorkspaceFolder) => Promise<string | undefined>;
}

export async function findTallyBinary(input: FindTallyBinaryInput): Promise<ResolvedBinary> {
  const { extensionContext, isTrusted, settings, workspaceFolders, pythonEnvResolver } = input;

  // 1) Explicit path(s)
  for (const raw of settings.path) {
    const expanded = expandPath(raw, workspaceFolders);
    const validation = validateUserSuppliedPath(expanded);
    if (!validation.ok) {
      continue;
    }
    const candidate = resolveMaybeRelative(expanded, workspaceFolders, isTrusted);
    if (!candidate) {
      continue;
    }
    if (await isExecutableFile(candidate)) {
      return directBinary(candidate, "explicitPath");
    }
  }

  // 2) Bundled import strategy
  if (settings.importStrategy === "useBundled") {
    const bundled = bundledBinaryPath(extensionContext);
    if (await isExecutableFile(bundled)) {
      return directBinary(bundled, "bundled");
    }
    throw new Error("tally.importStrategy is useBundled, but bundled binary is missing");
  }

  // Workspace trust gating: skip workspace-local resolution and PATH in untrusted workspaces.
  if (!isTrusted) {
    const bundled = bundledBinaryPath(extensionContext);
    if (await isExecutableFile(bundled)) {
      return directBinary(bundled, "bundled");
    }
    throw new Error("workspace is untrusted and no bundled tally binary is available");
  }

  // 3) npm project-local binaries
  for (const folder of workspaceFolders) {
    const candidates = npmCandidates(folder.uri.fsPath);
    for (const candidate of candidates) {
      if (await isExecutableFile(candidate)) {
        const resolved = windowsCmdShimAwareBinary(candidate, "workspaceNpm");
        if (resolved) {
          return resolved;
        }
      }
    }
  }

  // 4) Python Environments extension
  if (pythonEnvResolver) {
    for (const folder of workspaceFolders) {
      try {
        const candidate = await pythonEnvResolver(folder);
        if (candidate && (await isExecutableFile(candidate))) {
          return directBinary(candidate, "pythonEnvExt");
        }
      } catch {
        // resolver failed for this folder; continue fallback chain
      }
    }
  }

  // 5) Python venv binaries
  for (const folder of workspaceFolders) {
    const candidates = venvCandidates(folder.uri.fsPath);
    for (const candidate of candidates) {
      if (await isExecutableFile(candidate)) {
        return directBinary(candidate, "workspaceVenv");
      }
    }
  }

  // 6) PATH
  const onPath = await findOnPATH("tally");
  if (onPath) {
    return directBinary(onPath, "envPath");
  }

  // 7) Bundled fallback
  const bundled = bundledBinaryPath(extensionContext);
  if (await isExecutableFile(bundled)) {
    return directBinary(bundled, "bundled");
  }

  throw new Error(
    "unable to resolve a tally executable (configure tally.path or install via npm/pip)",
  );
}

function directBinary(executablePath: string, source: BinarySource): ResolvedBinary {
  return {
    command: executablePath,
    args: ["lsp", "--stdio"],
    key: `direct:${executablePath}`,
    source,
  };
}

function windowsCmdShimAwareBinary(
  executablePath: string,
  source: BinarySource,
): ResolvedBinary | undefined {
  if (process.platform !== "win32") {
    return directBinary(executablePath, source);
  }

  const lower = executablePath.toLowerCase();
  if (!lower.endsWith(".cmd") && !lower.endsWith(".bat")) {
    return directBinary(executablePath, source);
  }

  const validation = validateUserSuppliedPath(executablePath);
  if (!validation.ok) {
    return undefined;
  }

  // Avoid `shell: true` by routing through cmd.exe with conservative quoting.
  const commandLine = quoteCmd(executablePath) + " lsp --stdio";
  return {
    command: "cmd.exe",
    args: ["/d", "/s", "/c", commandLine],
    key: `cmd:${executablePath}`,
    source,
  };
}

function quoteCmd(p: string): string {
  // `cmd.exe /s /c` expects a single command line string.
  // Double-quote the path and escape embedded quotes defensively.
  const escaped = p.replaceAll('"', '\\"');
  return `"${escaped}"`;
}

function bundledBinaryPath(context: vscode.ExtensionContext): string {
  const platform = process.platform === "win32" ? "windows" : process.platform;
  const arch = process.arch;
  const name = process.platform === "win32" ? "tally.exe" : "tally";
  return context.asAbsolutePath(path.join("bundled", "bin", platform, arch, name));
}

function npmCandidates(workspaceRoot: string): string[] {
  const binRoot = path.join(workspaceRoot, "node_modules", ".bin");
  if (process.platform === "win32") {
    return [
      path.join(binRoot, "tally.cmd"),
      path.join(binRoot, "tally.bat"),
      path.join(binRoot, "tally.exe"),
      path.join(binRoot, "tally"),
    ];
  }
  return [path.join(binRoot, "tally")];
}

function venvCandidates(workspaceRoot: string): string[] {
  if (process.platform === "win32") {
    return [
      path.join(workspaceRoot, ".venv", "Scripts", "tally.exe"),
      path.join(workspaceRoot, "venv", "Scripts", "tally.exe"),
    ];
  }
  return [
    path.join(workspaceRoot, ".venv", "bin", "tally"),
    path.join(workspaceRoot, "venv", "bin", "tally"),
  ];
}

export async function isExecutableFile(p: string): Promise<boolean> {
  try {
    const stat = await fs.stat(p);
    if (!stat.isFile()) {
      return false;
    }
    // X_OK is best-effort on Windows, but is still a useful check on POSIX.
    await fs.access(p, fsConstants.X_OK);
    return true;
  } catch {
    return false;
  }
}

async function findOnPATH(baseName: string): Promise<string | undefined> {
  const pathEnv = process.env.PATH;
  if (!pathEnv) {
    return undefined;
  }

  const dirs = pathEnv.split(path.delimiter).filter(Boolean);

  const candidates = process.platform === "win32" ? windowsPathCandidates(baseName) : [baseName];

  for (const dir of dirs) {
    for (const c of candidates) {
      const full = path.join(dir, c);
      if (await isExecutableFile(full)) {
        return full;
      }
    }
  }
  return undefined;
}

function windowsPathCandidates(baseName: string): string[] {
  const pathext = process.env.PATHEXT?.split(";").filter(Boolean) ?? [".EXE", ".CMD", ".BAT"];
  const exts = pathext.map((e) => e.toLowerCase());
  const nameLower = baseName.toLowerCase();
  if (exts.some((e) => nameLower.endsWith(e))) {
    return [baseName];
  }
  return exts.map((e) => baseName + e);
}

function expandPath(raw: string, workspaceFolders: readonly vscode.WorkspaceFolder[]): string {
  let out = raw;

  if (out.startsWith("~/") || out.startsWith("~\\")) {
    out = path.join(os.homedir(), out.slice(2));
  }

  const firstWorkspace = workspaceFolders[0]?.uri.fsPath;
  if (firstWorkspace) {
    out = out.replaceAll("${workspaceFolder}", firstWorkspace);
  }

  out = out.replaceAll("${userHome}", os.homedir());

  out = out.replaceAll(
    /\$\{env:([A-Za-z_][A-Za-z0-9_]*)\}/g,
    (_, name: string) => process.env[name] ?? "",
  );

  return out;
}

function resolveMaybeRelative(
  candidate: string,
  workspaceFolders: readonly vscode.WorkspaceFolder[],
  isTrusted: boolean,
): string | undefined {
  if (path.isAbsolute(candidate)) {
    return candidate;
  }
  if (!isTrusted) {
    return undefined;
  }
  const root = workspaceFolders[0]?.uri.fsPath;
  if (!root) {
    return undefined;
  }
  return path.join(root, candidate);
}
