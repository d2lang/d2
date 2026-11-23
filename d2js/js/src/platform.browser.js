import { compressedWasmBase64, wasmExecJs } from "./wasm-loader.browser.js";
import workerScript from "./worker.js" with { type: "text" };

export const isNode = false;

export async function loadFile(path) {
  if (path === "./d2.wasm") {
    return compressedWasmBase64;
  }
  return null;
}

export async function createWorker(workerURL) {
  if (workerURL === undefined) {
    const blob = new Blob([wasmExecJs, workerScript], {
      type: "text/javascript;charset=utf-8",
    });
    workerURL = URL.createObjectURL(blob);
  }
  const url = typeof workerURL === "string" ? workerURL : workerURL.href;
  return new Worker(url, { type: "module" });
}
