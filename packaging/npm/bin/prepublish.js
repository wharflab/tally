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
} = require("./platform-packages");

const packageRoot = path.resolve(__dirname, "..");
const repoRoot = path.resolve(packageRoot, "..", "..");
const distRoot = process.env.TALLY_DIST_DIR
  ? path.resolve(process.env.TALLY_DIST_DIR)
  : path.join(repoRoot, "dist");
const generatedRoot = path.join(packageRoot, ".generated");
const generatedPackagesRoot = path.join(generatedRoot, "platform-packages");
const generatedMainPackageRoot = path.join(generatedRoot, "main-package");
const manifestPath = path.join(packageRoot, "package.json");
const rootDocFiles = ["README.md", "LICENSE", "NOTICE"];

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

function copySharedDocs(targetDir) {
  ensureDir(targetDir);
  for (const fileName of rootDocFiles) {
    fs.copyFileSync(path.join(repoRoot, fileName), path.join(targetDir, fileName));
  }
}

function resolveVersion(manifest) {
  const version = (process.env.NPM_VERSION || manifest.version).replace(/^v/, "");
  if (!version) {
    throw new Error("NPM version must not be empty");
  }
  return version;
}

function resolvePublishTag(version) {
  if (process.env.NPM_PUBLISH_TAG) {
    return process.env.NPM_PUBLISH_TAG;
  }
  return version.includes("-") ? "next" : null;
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
  const stderr = `${result.stderr || ""}${result.stdout || ""}`;
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
    license: "GPL-3.0-only",
    bugs: {
      url: "https://github.com/wharflab/tally/issues",
    },
    homepage: "https://github.com/wharflab/tally#readme",
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
  nextManifest.version = version;
  nextManifest.optionalDependencies = Object.fromEntries(
    availableTargets.map((target) => [getPlatformPackageName(target), version]),
  );
  nextManifest.publishConfig = {
    ...(nextManifest.publishConfig || {}),
    access: "public",
  };
  if (publishTag) {
    nextManifest.publishConfig.tag = publishTag;
  } else {
    delete nextManifest.publishConfig.tag;
  }
  return nextManifest;
}

function availableTargets() {
  return platformTargets.filter((target) =>
    fs.existsSync(getDistBinaryPath(distRoot, target)),
  );
}

function publishPlatformPackages(targets, version, publishTag) {
  const dryRun = isDryRun();
  for (const target of targets) {
    const packageDir = path.join(
      generatedPackagesRoot,
      getPlatformPackageDirName(target),
    );
    removeIfExists(packageDir);
    ensureDir(path.join(packageDir, "bin"));
    copySharedDocs(packageDir);
    fs.copyFileSync(
      getDistBinaryPath(distRoot, target),
      path.join(packageDir, "bin", getBinaryName(target)),
    );
    writeJSON(path.join(packageDir, "package.json"), buildPlatformManifest(target, version));

    if (!dryRun && packageVersionExists(getPlatformPackageName(target), version)) {
      console.log(
        `Skipping ${getPlatformPackageName(target)}@${version}; version already exists on npm.`,
      );
      continue;
    }

    const args = ["publish", "--access", "public"];
    if (publishTag) {
      args.push("--tag", publishTag);
    }
    if (dryRun) {
      args.push("--dry-run");
    }
    console.log(`Publishing ${getPlatformPackageName(target)} from ${packageDir}`);
    runOrThrow("npm", args, { cwd: packageDir });
  }
}

function createMainPackage(manifest, version, targets, publishTag) {
  removeIfExists(generatedMainPackageRoot);
  ensureDir(path.join(generatedMainPackageRoot, "bin"));
  copySharedDocs(generatedMainPackageRoot);
  fs.copyFileSync(
    path.join(packageRoot, "bin", "cli.js"),
    path.join(generatedMainPackageRoot, "bin", "cli.js"),
  );
  fs.copyFileSync(
    path.join(packageRoot, "bin", "platform-packages.js"),
    path.join(generatedMainPackageRoot, "bin", "platform-packages.js"),
  );
  fs.copyFileSync(
    path.join(packageRoot, "platform-targets.json"),
    path.join(generatedMainPackageRoot, "platform-targets.json"),
  );
  writeJSON(
    path.join(generatedMainPackageRoot, "package.json"),
    buildPublishedRootManifest(manifest, version, targets, publishTag),
  );
}

function publishMainPackage(version, publishTag) {
  const args = ["publish", "--access", "public"];
  if (publishTag) {
    args.push("--tag", publishTag);
  }
  if (isDryRun()) {
    args.push("--dry-run");
  }
  if (!isDryRun() && packageVersionExists("tally-cli", version)) {
    console.log(`Skipping tally-cli@${version}; version already exists on npm.`);
    return;
  }
  runOrThrow("npm", args, { cwd: generatedMainPackageRoot });
}

function publishRelease() {
  const manifest = readJSON(manifestPath);
  const version = resolveVersion(manifest);
  const publishTag = resolvePublishTag(version);
  const targets = availableTargets();
  if (targets.length === 0) {
    throw new Error(`No release binaries found under ${distRoot}`);
  }

  removeIfExists(generatedPackagesRoot);
  ensureDir(generatedPackagesRoot);
  publishPlatformPackages(targets, version, publishTag);
  createMainPackage(manifest, version, targets, publishTag);
  publishMainPackage(version, publishTag);
}

function restore() {
  removeIfExists(generatedRoot);
}

const command = process.argv[2];
try {
  if (command === "publish-release") {
    publishRelease();
  } else if (command === "restore") {
    restore();
  } else {
    throw new Error(`Unknown command: ${command}`);
  }
} catch (error) {
  if (command === "publish-release") {
    restore();
  }
  throw error;
}
