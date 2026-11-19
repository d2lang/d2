export async function initWasm(wasmBinary) {
  try {
    const go = new Go();
    const { instance } = await WebAssembly.instantiate(wasmBinary, go.importObject);

    // Go calls onWasmInitialized after exporting the API. Its run promise
    // settles when the program exits, which can also happen before registration.
    return await new Promise((resolve, reject) => {
      globalThis.onWasmInitialized = () => resolve(globalThis.d2);
      go.run(instance).then(
        () => reject(new Error("Go exited before registering the D2 API")),
        reject
      );
    });
  } catch (error) {
    throw new Error(`D2 WASM failed to initialize: ${error?.message ?? error}`, {
      cause: error,
    });
  } finally {
    delete globalThis.onWasmInitialized;
  }
}
