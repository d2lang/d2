import { parentPort } from "node:worker_threads";

let currentPort;
let d2;

function loadScript(content) {
  const func = new Function(content);
  func.call(globalThis);
}

export function setupMessageHandler(isNode, port, initWasm) {
  currentPort = port;

  const handleMessage = async (e) => {
    const { id, type, data } = e;

    switch (type) {
      case "init":
        try {
          if (isNode) {
            loadScript(data.wasmExecContent);
          }
          d2 = await initWasm(data.wasm);
          currentPort.postMessage({ type: "ready" });
        } catch (err) {
          currentPort.postMessage({ type: "error", error: err.message });
        }
        break;

      case "compile":
        try {
          const result = await d2.compile(JSON.stringify(data));
          const response = JSON.parse(result);
          if (response.error) throw new Error(response.error.message);
          currentPort.postMessage({ id, type: "result", data: response.data });
        } catch (err) {
          currentPort.postMessage({ id, type: "error", error: err.message });
        }
        break;

      case "render":
        try {
          const result = await d2.render(JSON.stringify(data));
          const response = JSON.parse(result);
          if (response.error) throw new Error(response.error.message);
          const decoded = new TextDecoder().decode(
            Uint8Array.from(atob(response.data), (c) => c.charCodeAt(0))
          );
          currentPort.postMessage({ id, type: "result", data: decoded });
        } catch (err) {
          currentPort.postMessage({ id, type: "error", error: err.message });
        }
        break;

      case "encode":
        try {
          const result = d2.encode(data);
          const response = JSON.parse(result);
          if (response.error) throw new Error(response.error.message);
          currentPort.postMessage({
            id,
            type: "result",
            data: response.data.result,
          });
        } catch (err) {
          currentPort.postMessage({ id, type: "error", error: err.message });
        }
        break;

      case "decode":
        try {
          const result = d2.decode(data);
          const response = JSON.parse(result);
          if (response.error) throw new Error(response.error.message);
          currentPort.postMessage({
            id,
            type: "result",
            data: response.data.result,
          });
        } catch (err) {
          currentPort.postMessage({ id, type: "error", error: err.message });
        }
        break;

      case "version":
        try {
          const result = d2.version();
          const response = JSON.parse(result);
          if (response.error) throw new Error(response.error.message);
          currentPort.postMessage({ id, type: "result", data: response.data });
        } catch (err) {
          currentPort.postMessage({ id, type: "error", error: err.message });
        }
        break;

      case "jsVersion":
        try {
          const result = d2.jsVersion();
          const response = JSON.parse(result);
          if (response.error) throw new Error(response.error.message);
          currentPort.postMessage({ id, type: "result", data: response.data });
        } catch (err) {
          currentPort.postMessage({ id, type: "error", error: err.message });
        }
        break;
    }
  };

  if (isNode) {
    port.on("message", handleMessage);
  } else {
    port.onmessage = (e) => handleMessage(e.data);
  }
}

async function initWasmNode(wasmBinary) {
  const go = new Go();
  const result = await WebAssembly.instantiate(wasmBinary, go.importObject);
  let runError;
  go.run(result.instance).catch((err) => {
    runError = err;
  });
  // go.run executes main synchronously until it blocks, so d2 is registered by
  // now unless the Go program failed during startup (for example, a JS stack
  // overflow in package init). Let a startup rejection settle, then fail init
  // instead of reporting ready with no API.
  await Promise.resolve();
  if (!global.d2) {
    const reason = runError
      ? `: ${runError.message || runError}`
      : go.exited
        ? " (Go program exited)"
        : "";
    throw new Error(`D2 WASM failed to initialize${reason}`);
  }
  return global.d2;
}

setupMessageHandler(true, parentPort, initWasmNode);
