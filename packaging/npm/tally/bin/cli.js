#!/usr/bin/env node

const { execFileSync } = require('node:child_process');
const { existsSync } = require('node:fs');

/**
 * Get the platform-specific package name for the current system
 * @returns {string} the npm package name for this platform
 */
function getPlatformPackageName() {
  const platform = process.platform;
  const arch = process.arch;

  // Map Node.js platform/arch to our package naming convention
  let pkgPlatform;
  let pkgArch;

  switch (platform) {
    case 'linux':
      pkgPlatform = 'linux';
      break;
    case 'darwin':
      pkgPlatform = 'darwin';
      break;
    case 'win32':
    case 'cygwin':
      pkgPlatform = 'windows';
      break;
    case 'freebsd':
      pkgPlatform = 'freebsd';
      break;
    default:
      throw new Error(`Unsupported platform: ${platform}`);
  }

  switch (arch) {
    case 'x64':
      pkgArch = 'x64';
      break;
    case 'arm64':
      pkgArch = 'arm64';
      break;
    default:
      throw new Error(`Unsupported architecture: ${arch}`);
  }

  // FreeBSD only supports x64
  if (pkgPlatform === 'freebsd' && pkgArch !== 'x64') {
    throw new Error(`FreeBSD only supports x64 architecture, not ${arch}`);
  }

  return `@wharflab/tally-${pkgPlatform}-${pkgArch}`;
}

/**
 * Find and execute the platform-specific tally binary
 */
function main() {
  try {
    const pkgName = getPlatformPackageName();
    const binName = process.platform === 'win32' || process.platform === 'cygwin'
      ? 'tally.exe'
      : 'tally';

    // Try to resolve the binary path from the platform package
    let binPath;
    try {
      binPath = require.resolve(`${pkgName}/bin/${binName}`);
    } catch  {
      // Platform package not found or binary missing
      
      
      
      
      
      
      
      
      
      
      
      
      
      
      
      
      
      
      
      process.exit(1);
    }

    // Verify the binary exists and is executable
    if (!existsSync(binPath)) {
      
      
      
      process.exit(1);
    }

    // Execute the binary with the same arguments passed to this script
    // Skip the first two arguments (node and script path)
    const args = process.argv.slice(2);

    try {
      execFileSync(binPath, args, {
        stdio: 'inherit',  // Forward stdin/stdout/stderr to the user
        windowsHide: false // On Windows, don't hide the console window
      });
    } catch (execError) {
      // If the binary exits with a non-zero code, preserve that exit code
      if (execError.status !== undefined) {
        process.exit(execError.status);
      }
      // If there was an execution error (e.g., binary corrupted), report it
      
      process.exit(1);
    }

  } catch  {
    
    process.exit(1);
  }
}

// Only run if this script is executed directly (not required as a module)
if (require.main === module) {
  main();
}

module.exports = { getPlatformPackageName };
