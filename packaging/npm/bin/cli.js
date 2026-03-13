#!/usr/bin/env node

const { execFileSync } = require("node:child_process");
const { existsSync } = require("node:fs");
const {
  getBinaryName,
  getPlatformPackageName,
  getPlatformTarget,
} = require("./platform-packages");

function main() {
  try {
    const target = getPlatformTarget();
    const pkgName = getPlatformPackageName(target);
    const binName = getBinaryName(target);

    let binPath;
    try {
      binPath = require.resolve(`${pkgName}/bin/${binName}`);
    } catch {
      process.exit(1);
    }

    if (!existsSync(binPath)) {
      process.exit(1);
    }

    const args = process.argv.slice(2);
    try {
      execFileSync(binPath, args, {
        stdio: "inherit",
        windowsHide: false,
      });
    } catch (execError) {
      if (execError.status !== undefined) {
        process.exit(execError.status);
      }
      process.exit(1);
    }
  } catch {
    process.exit(1);
  }
}

if (require.main === module) {
  main();
}

module.exports = { getPlatformPackageName };
