import assert from "node:assert/strict";
import fs from "node:fs";
import { pipeline } from 'node:stream/promises';
import url from "node:url";

// Pinned to the 3.18.0 release of vscode-languageserver-node
// (commit dc94993a5e9333f588e1720057c091de795e967a — "Merge 3.18 release into main").
const hash = "dc94993a5e9333f588e1720057c091de795e967a";

const metaModelURL = `https://raw.githubusercontent.com/microsoft/vscode-languageserver-node/${hash}/protocol/metaModel.json`;
const metaModelSchemaURL = `https://raw.githubusercontent.com/microsoft/vscode-languageserver-node/${hash}/tools/src/metaModel.ts`;

await pipeline(
    (await fetch(metaModelURL)).body!,
    fs.createWriteStream(url.fileURLToPath(import.meta.resolve('./metaModel.json'))),
)

const metaModelSchemaResponse = await fetch(metaModelSchemaURL);
let metaModelSchema = await metaModelSchemaResponse.text();

// Patch the schema to add omitzeroValue property to Property type
metaModelSchema = metaModelSchema.replace(
    /(\t \* Whether the property is deprecated or not\. If deprecated\n\t \* the property contains the deprecation message\.\n\t \*\/\n\tdeprecated\?: string;)\n}/m,
    `$1\n\n\t/**\n\t * Whether this property uses omitzero without being a pointer.\n\t * Custom extension for special value types.\n\t */\n\tomitzeroValue?: boolean;\n}`,
);
assert.ok(metaModelSchema.includes("omitzeroValue?: boolean;"), "Failed to patch metaModelSchema with omitzeroValue property");

fs.writeFileSync(url.fileURLToPath(import.meta.resolve("./metaModelSchema.mts")), metaModelSchema);
