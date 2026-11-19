import { compressedWasmBase64, wasmExecJs } from "./wasm-loader.browser.js";
import workerScript from "./worker.js" with { type: "text" };

export async function loadFile(path) {
  if (path === "./d2.wasm") {
    return compressedWasmBase64;
  }
  return null;
}

export async function createWorker() {
  let blob = new Blob([wasmExecJs, workerScript], {
    type: "text/javascript;charset=utf-8",
  });

  const worker = new Worker(URL.createObjectURL(blob), {
    type: "module",
  });
  return worker;
}
