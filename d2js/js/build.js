import { build } from "bun";
import { copyFile, mkdir, writeFile, readFile, rm } from "node:fs/promises";
import { join, resolve } from "node:path";
import { brotliCompressSync } from "node:zlib";

const __dirname = new URL(".", import.meta.url).pathname;
const ROOT_DIR = resolve(__dirname);
const SRC_DIR = resolve(ROOT_DIR, "src");

await rm("./dist", { recursive: true, force: true });
await mkdir("./dist/browser", { recursive: true });
await mkdir("./dist/node-esm", { recursive: true });
await mkdir("./dist/node-cjs", { recursive: true });
await mkdir("./dist/wasm", { recursive: true });

const wasmBinary = await readFile("./wasm/d2.wasm");
const wasmExecJs = await readFile("./wasm/wasm_exec.js", "utf8");
await writeFile("./dist/wasm/d2.wasm", wasmBinary);
await writeFile("./dist/wasm/wasm_exec.js", wasmExecJs);

const compressedWasm = brotliCompressSync(wasmBinary);
console.log(
  `WASM compression: ${(wasmBinary.length / 1024 / 1024).toFixed(2)}MB → ${(
    compressedWasm.length /
    1024 /
    1024
  ).toFixed(2)}MB`
);

// Store compressed WASM as base64 and include brotli decoder in the loader
// Don't decompress immediately - let the consumer decompress when needed
const brotliDecoder = await readFile("./vendor/decode.min.js", "utf8");

await writeFile(
  join(SRC_DIR, "wasm-loader.browser.js"),
  `${brotliDecoder}

export const wasmBinaryCompressed = "${Buffer.from(compressedWasm).toString("base64")}";
export function getWasmBinary() {
  const compressedBytes = Uint8Array.from(atob(wasmBinaryCompressed), c => c.charCodeAt(0));
  return BrotliDecode(compressedBytes);
}
export const wasmExecJs = ${JSON.stringify(wasmExecJs)};`
);

const commonConfig = {
  minify: true,
};

const nodeEsmBanner = `import { dirname as __d2Dirname } from "node:path";
import { fileURLToPath as __d2FileURLToPath } from "node:url";
const __D2_NODE_MODULE_DIR__ = __d2Dirname(__d2FileURLToPath(import.meta.url));`;
const nodeCjsBanner = `const __D2_NODE_MODULE_DIR__ = __dirname;`;

async function buildDynamicFiles(platform) {
  const platformContent =
    platform === "node"
      ? `export * from "./platform.node.js";`
      : `export * from "./platform.browser.js";`;

  await writeFile(join(SRC_DIR, "platform.js"), platformContent);

  const entrypoint = join(SRC_DIR, `worker.${platform}.js`);
  const workerFile = join(SRC_DIR, "worker.js");
  if (platform === "browser") {
    // The browser loads this worker from a blob URL, so bundle its imports first.
    const worker = await build({
      entrypoints: [entrypoint],
      target: "browser",
      format: "esm",
    });
    const workerScript = await worker.outputs[0].text();
    await writeFile(workerFile, workerScript);
    await writeFile("./dist/browser/worker.js", `${wasmExecJs}\n${workerScript}`);
  } else {
    await copyFile(entrypoint, workerFile);
  }
}

async function buildAndCopy(buildType) {
  const configs = {
    browser: {
      outdir: resolve(ROOT_DIR, "dist/browser"),
      splitting: false,
      format: "esm",
      target: "browser",
      platform: "browser",
      entrypoints: [resolve(SRC_DIR, "index.js")],
    },
    "node-esm": {
      outdir: resolve(ROOT_DIR, "dist/node-esm"),
      splitting: true,
      format: "esm",
      target: "node",
      platform: "node",
      banner: nodeEsmBanner,
      entrypoints: [resolve(SRC_DIR, "index.js"), resolve(SRC_DIR, "worker.js")],
    },
    "node-cjs": {
      outdir: resolve(ROOT_DIR, "dist/node-cjs"),
      splitting: false,
      format: "cjs",
      target: "node",
      platform: "node",
      banner: nodeCjsBanner,
      entrypoints: [resolve(SRC_DIR, "index.js"), resolve(SRC_DIR, "worker.js")],
    },
  };

  const config = configs[buildType];
  await buildDynamicFiles(config.platform);

  const result = await build({
    ...commonConfig,
    ...config,
  });

  if (!result.outputs || result.outputs.length === 0) {
    throw new Error(
      `No outputs generated for ${buildType} build. Result: ${JSON.stringify(result)}`
    );
  }

  if (buildType === "node-cjs") {
    await writeFile(join(config.outdir, "package.json"), '{"type":"commonjs"}\n');
  }
}

try {
  await buildAndCopy("browser");
  await buildAndCopy("node-esm");
  await buildAndCopy("node-cjs");
} catch (error) {
  console.error("Build failed:", error);
  process.exit(1);
}
