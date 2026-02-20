import * as fs from "node:fs/promises";
import * as path from "node:path";
import * as vscode from "vscode";

import type { PythonEnvironment, PythonEnvironmentApi } from "../vendor/pythonEnvsApi";
import { isExecutableFile } from "./findBinary";

const EXT_ID = "ms-python.vscode-python-envs";

let cachedApi: PythonEnvironmentApi | undefined;

export async function getPythonEnvApi(): Promise<PythonEnvironmentApi | undefined> {
  if (cachedApi) {
    return cachedApi;
  }

  const ext = vscode.extensions.getExtension<PythonEnvironmentApi>(EXT_ID);
  if (!ext) {
    return undefined;
  }

  try {
    const api = ext.isActive ? ext.exports : await ext.activate();
    cachedApi = api;
    return api;
  } catch {
    return undefined;
  }
}

export async function findTallyViaPythonEnvs(
  folder: vscode.WorkspaceFolder,
): Promise<string | undefined> {
  const api = await getPythonEnvApi();
  if (!api) {
    return undefined;
  }

  let env: PythonEnvironment | undefined;
  try {
    env = await api.getEnvironment(folder.uri);
  } catch {
    return undefined;
  }
  if (!env) {
    return undefined;
  }

  const envPath = env.environmentPath.fsPath;

  try {
    const s = await fs.stat(envPath);

    if (s.isDirectory()) {
      // environmentPath is a directory (e.g. the venv root)
      const candidate =
        process.platform === "win32"
          ? path.join(envPath, "Scripts", "tally.exe")
          : path.join(envPath, "bin", "tally");
      if (await isExecutableFile(candidate)) {
        return candidate;
      }
    } else if (s.isFile()) {
      // environmentPath is the python binary itself
      const dir = path.dirname(envPath);
      const candidate =
        process.platform === "win32" ? path.join(dir, "tally.exe") : path.join(dir, "tally");
      if (await isExecutableFile(candidate)) {
        return candidate;
      }
    }
  } catch {
    return undefined;
  }

  return undefined;
}
