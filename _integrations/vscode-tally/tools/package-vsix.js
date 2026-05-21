const fs = require("node:fs");
const path = require("node:path");
const { createVSIX } = require("@vscode/vsce");

function resolvePackageDir() {
  for (const candidate of [
    process.cwd(),
    path.join(process.cwd(), "_integrations", "vscode-tally"),
    path.resolve(__dirname, ".."),
  ]) {
    if (fs.existsSync(path.join(candidate, "package.json"))) {
      return candidate;
    }
  }
  throw new Error("unable to locate VS Code extension package root");
}

const packageDir = resolvePackageDir();
const [outArg, ...vsceArgs] = process.argv.slice(2);
const out = path.resolve(outArg || path.join(packageDir, "tally.vsix"));
const bundles = fs
  .readdirSync(packageDir)
  .filter((name) => name.startsWith("extension_bundle__") && name.endsWith(".js"));

if (bundles.length !== 1) {
  throw new Error(`expected exactly one Bazel extension bundle, found ${bundles.length}`);
}
const [bundle] = bundles;

fs.mkdirSync(path.join(packageDir, "dist"), { recursive: true });
fs.copyFileSync(path.join(packageDir, bundle), path.join(packageDir, "dist", "extension.cjs"));

const licenseDest = path.join(packageDir, "LICENSE");
for (const license of [path.join(packageDir, "LICENSE"), path.resolve(packageDir, "../../LICENSE")]) {
  if (fs.existsSync(license)) {
    if (license !== licenseDest) {
      fs.copyFileSync(license, licenseDest);
    }
    break;
  }
}

const options = {
  cwd: packageDir,
  dependencies: false,
  packagePath: out,
};

for (let index = 0; index < vsceArgs.length; index += 1) {
  const arg = vsceArgs[index];
  if (arg === "--target") {
    const target = vsceArgs[index + 1];
    if (!target) {
      throw new Error("--target requires a value");
    }
    options.target = target;
    index += 1;
    continue;
  }
  throw new Error(`unsupported vsce package argument: ${arg}`);
}

void createVSIX(options).catch((err) => {
  console.error(err);
  process.exit(1);
});
