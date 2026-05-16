const fs = require("node:fs");
const path = require("node:path");

const packageDir = process.cwd();
const [outArg, ...vsceArgs] = process.argv.slice(2);
const out = path.resolve(outArg || path.join(packageDir, "tally.vsix"));
const bundle = fs
  .readdirSync(packageDir)
  .find((name) => name.startsWith("extension_bundle__") && name.endsWith(".js"));

if (!bundle) {
  throw new Error("Bazel extension bundle was not staged");
}

fs.mkdirSync(path.join(packageDir, "dist"), { recursive: true });
fs.copyFileSync(path.join(packageDir, bundle), path.join(packageDir, "dist", "extension.cjs"));

const license = path.resolve(packageDir, "../../LICENSE");
if (fs.existsSync(license)) {
  fs.copyFileSync(license, path.join(packageDir, "LICENSE"));
}

const vsce = require("@vscode/vsce/out/main");
vsce(["node", "vsce", "package", "--no-dependencies", "--out", out, ...vsceArgs]);
