const path = require("node:path");
const platformTargets = require("../platform-targets.json");

const ROOT_PACKAGE_NAME = "tally-cli";
const PLATFORM_PACKAGE_SCOPE = "@wharflab";

function normalizeNodePlatform(platform) {
  switch (platform) {
    case "linux":
      return "linux";
    case "darwin":
      return "darwin";
    case "win32":
    case "cygwin":
      return "windows";
    case "freebsd":
      return "freebsd";
    default:
      throw new Error(`Unsupported platform: ${platform}`);
  }
}

function normalizeNodeArch(arch) {
  switch (arch) {
    case "x64":
      return "x64";
    case "arm64":
      return "arm64";
    default:
      throw new Error(`Unsupported architecture: ${arch}`);
  }
}

function getPlatformTarget(platform = process.platform, arch = process.arch) {
  const nodeOs = normalizeNodePlatform(platform);
  const nodeArch = normalizeNodeArch(arch);
  const target = platformTargets.find(
    (candidate) =>
      candidate.nodeOs === nodeOs && candidate.nodeArch === nodeArch,
  );
  if (!target) {
    throw new Error(`Unsupported platform/architecture: ${platform}/${arch}`);
  }
  return target;
}

function getPlatformPackageName(platform = process.platform, arch = process.arch) {
  const target =
    typeof platform === "object" && platform !== null
      ? platform
      : getPlatformTarget(platform, arch);
  return `${PLATFORM_PACKAGE_SCOPE}/tally-${target.nodeOs}-${target.nodeArch}`;
}

function getPlatformPackageDirName(target) {
  return `tally-${target.nodeOs}-${target.nodeArch}`;
}

function getBinaryName(target) {
  return target.nodeOs === "windows" ? "tally.exe" : "tally";
}

function getDistDirName(target) {
  return `tally_${target.goos}_${target.goarch}_${target.variant}`;
}

function getDistBinaryPath(distRoot, target) {
  return path.join(distRoot, getDistDirName(target), getBinaryName(target));
}

module.exports = {
  ROOT_PACKAGE_NAME,
  PLATFORM_PACKAGE_SCOPE,
  platformTargets,
  getBinaryName,
  getDistBinaryPath,
  getDistDirName,
  getPlatformPackageDirName,
  getPlatformPackageName,
  getPlatformTarget,
};
