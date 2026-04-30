#!/usr/bin/env node

const { spawnSync } = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");
const {
  getBinaryName,
  getDistBinaryPath,
  getPlatformPackageDirName,
  getPlatformPackageName,
  platformTargets,
} = require("../lib/platform-packages");

const packageRoot = path.resolve(__dirname, "..");
const repoRoot = process.env.TALLY_REPO_ROOT
  ? path.resolve(process.env.TALLY_REPO_ROOT)
  : path.resolve(packageRoot, "..", "..");
const distRoot = process.env.TALLY_DIST_DIR
  ? path.resolve(process.env.TALLY_DIST_DIR)
  : path.join(repoRoot, "dist");
const generatedRoot = path.join(packageRoot, ".generated");
const generatedPackagesRoot = path.join(generatedRoot, "platform-packages");
const manifestPath = process.env.npm_package_json ?? path.join(packageRoot, "package.json");
const manifestBackupPath = path.join(generatedRoot, "package.json.backup");
const rootLegalFiles = ["LICENSE", "NOTICE"];

function readJSON(filePath) {
  return JSON.parse(fs.readFileSync(filePath, "utf8"));
}

function writeJSON(filePath, value) {
  fs.writeFileSync(filePath, `${JSON.stringify(value, null, 2)}\n`);
}

function ensureDir(dirPath) {
  fs.mkdirSync(dirPath, { recursive: true });
}

function removeIfExists(targetPath) {
  fs.rmSync(targetPath, { recursive: true, force: true });
}

function copyLegalFiles(targetDir) {
  ensureDir(targetDir);
  for (const fileName of rootLegalFiles) {
    fs.copyFileSync(path.join(repoRoot, fileName), path.join(targetDir, fileName));
  }
}

function resolveVersion(manifest) {
  const version = (process.env.npm_package_version ?? manifest.version).replace(/^v/, "");
  if (!version) {
    throw new Error("NPM version must not be empty");
  }
  return version;
}

function isDryRun() {
  return process.env.npm_config_dry_run === "true";
}

function runOrThrow(command, args, options = {}) {
  const result = spawnSync(command, args, {
    stdio: "inherit",
    ...options,
  });
  if (result.status !== 0) {
    throw new Error(`${command} ${args.join(" ")} failed with exit ${result.status}`);
  }
}

function packageVersionExists(packageName, version) {
  const result = spawnSync("npm", ["view", `${packageName}@${version}`, "version"], {
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
  });
  if (result.status === 0) {
    return true;
  }
  const stderr = `${result.stderr ?? ""}${result.stdout ?? ""}`;
  if (stderr.includes("E404") || stderr.includes("404 No match found")) {
    return false;
  }
  return false;
}

function buildPlatformManifest(target, version) {
  return {
    name: getPlatformPackageName(target),
    version,
    description: `tally binary for ${target.nodeOs}-${target.nodeArch}`,
    repository: {
      type: "git",
      url: "git+https://github.com/wharflab/tally.git",
    },
    license: "AGPL-3.0-only",
    bugs: {
      url: "https://github.com/wharflab/tally/issues",
    },
    homepage: "https://tally.wharflab.com/",
    os: [target.nodeOs === "windows" ? "win32" : target.nodeOs],
    cpu: [target.nodeArch],
    files: ["bin/", "README.md"],
    publishConfig: {
      access: "public",
    },
  };
}

function buildPublishedRootManifest(manifest, version, availableTargets, publishTag) {
  const nextManifest = structuredClone(manifest);
  nextManifest.optionalDependencies = Object.fromEntries(
    availableTargets.map((target) => [getPlatformPackageName(target), version]),
  );
  return nextManifest;
}

function availableTargets() {
  return platformTargets.filter((target) =>
    fs.existsSync(getDistBinaryPath(distRoot, target)),
  );
}

function publishPlatformPackages(targets, version) {
  const dryRun = isDryRun();
  for (const target of targets) {
    const packageDir = path.join(
      generatedPackagesRoot,
      getPlatformPackageDirName(target),
    );
    removeIfExists(packageDir);
    ensureDir(path.join(packageDir, "bin"));
    copyLegalFiles(packageDir);
    fs.copyFileSync(
      path.join(packageRoot, "README.md"),
      path.join(packageDir, "README.md"),
    );
    fs.copyFileSync(
      getDistBinaryPath(distRoot, target),
      path.join(packageDir, "bin", getBinaryName(target)),
    );
    if (target.nodeOs !== "windows") {
      fs.chmodSync(path.join(packageDir, "bin", getBinaryName(target)), 0o755);
    }
    writeJSON(path.join(packageDir, "package.json"), buildPlatformManifest(target, version));

    if (!dryRun && packageVersionExists(getPlatformPackageName(target), version)) {
      
      continue;
    }

    const args = ["publish", "--access", "public"];
    if (dryRun) {
      args.push("--dry-run");
    }
    
    runOrThrow("npm", args, { cwd: packageDir });
  }
}

function updateMainManifest(manifest, version, targets) {
  ensureDir(generatedRoot);
  if (!fs.existsSync(manifestBackupPath)) {
    fs.copyFileSync(manifestPath, manifestBackupPath);
  }
  writeJSON(
    manifestPath,
    buildPublishedRootManifest(manifest, version, targets),
  );
}

function prepublish() {
  const manifest = readJSON(manifestPath);
  const version = resolveVersion(manifest);
  const targets = availableTargets();
  if (targets.length === 0) {
    throw new Error(`No release binaries found under ${distRoot}`);
  }

  removeIfExists(generatedPackagesRoot);
  ensureDir(generatedPackagesRoot);
  publishPlatformPackages(targets, version);
  updateMainManifest(manifest, version, targets);
}

function restore() {
  if (fs.existsSync(manifestBackupPath)) {
    fs.copyFileSync(manifestBackupPath, manifestPath);
  }
  removeIfExists(generatedRoot);
}

const command = process.argv[2];
try {
  if (command === "prepublish") {
    prepublish();
  } else if (command === "restore") {
    restore();
  } else {
    throw new Error(`Unknown command: ${command}`);
  }
} catch (error) {
  if (command === "prepublish") {
    restore();
  }
  throw error;
}
