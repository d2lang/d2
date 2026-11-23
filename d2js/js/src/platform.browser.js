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
  if (workerURL !== undefined) {
    const url = typeof workerURL === "string" ? workerURL : workerURL.href;
    return { worker: new Worker(url, { type: "module" }) };
  }

  const blob = new Blob([wasmExecJs, workerScript], {
    type: "text/javascript;charset=utf-8",
  });
  const url = URL.createObjectURL(blob);
  const releaseURL = () => URL.revokeObjectURL(url);
  try {
    return { worker: new Worker(url, { type: "module" }), releaseURL };
  } catch (error) {
    releaseURL();
    throw error;
  }
}
