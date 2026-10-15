import { build } from 'esbuild';
import { mkdir, cp } from 'node:fs/promises';
await mkdir('output', {recursive:true});
await build({entryPoints:['src/worker.mjs'],outfile:'output/workflow-runtime.mjs',bundle:true,platform:'node',format:'esm',target:'node22',banner:{js:'import { createRequire } from "node:module"; import { fileURLToPath } from "node:url"; import { dirname } from "node:path"; const require = createRequire(import.meta.url); const __filename = fileURLToPath(import.meta.url); const __dirname = dirname(__filename);'}});
await cp('vendor/ZCODE-LICENSE','output/ZCODE-LICENSE');
await cp('vendor/SOURCE.md','output/SOURCE.md');
