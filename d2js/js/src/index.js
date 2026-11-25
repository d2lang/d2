import { createWorker, isNode, loadFile } from "./platform.js";

function compileRequest(input, options) {
  return typeof input === "string"
    ? { fs: { index: input }, options }
    : { ...input, options: { ...options, ...input.options } };
}

export class D2 {
  constructor(options = {}) {
    this.nextRequestId = 0;
    this.pendingRequests = new Map();
    this.disposed = false;
    this.disposePromise = null;
    this.ready = this.init(options.workerURL);
  }

  rejectPendingRequests(error) {
    for (const request of this.pendingRequests.values()) {
      request.reject(error);
    }
    this.pendingRequests.clear();
  }

  setupMessageHandler(isNodeWorker = isNode) {
    return new Promise((resolve, reject) => {
      let isReady = false;

      const handleWorkerError = (error) => {
        const workerError =
          error instanceof Error
            ? error
            : new Error(error?.message || "D2 worker encountered an error");
        if (!isReady) reject(workerError);
        this.rejectPendingRequests(workerError);
        console.error(
          `Worker${isNodeWorker ? " (node)" : ""} encountered an error:`,
          workerError.message
        );
      };

      const handleMessage = (data) => {
        if (data.type === "ready") {
          isReady = true;
          resolve();
          return;
        }

        if (data.type !== "result" && data.type !== "error") return;

        if (data.id === undefined) {
          if (data.type === "error") {
            const error = new Error(data.error);
            if (!isReady) reject(error);
            this.rejectPendingRequests(error);
          }
          return;
        }

        const request = this.pendingRequests.get(data.id);
        if (!request) return;

        this.pendingRequests.delete(data.id);
        if (data.type === "result") {
          request.resolve(data.data);
        } else {
          request.reject(new Error(data.error));
        }
      };

      if (isNodeWorker) {
        this.worker.on("message", handleMessage);
        this.worker.on("error", handleWorkerError);
      } else {
        this.worker.onmessage = (e) => handleMessage(e.data);
        this.worker.onerror = handleWorkerError;
      }
    });
  }

  async init(workerURL) {
    const { worker, releaseURL } = await createWorker(workerURL);
    this.worker = worker;
    try {
      const wasmExecContent = isNode ? await loadFile("./wasm_exec.js") : null;
      const wasm = await loadFile("./d2.wasm");
      const ready = this.setupMessageHandler();
      worker.postMessage({
        type: "init",
        data: {
          wasm,
          wasmExecContent: isNode ? wasmExecContent.toString() : null,
        },
      });
      return await ready;
    } finally {
      // The browser may still be loading the module when new Worker() returns.
      releaseURL?.();
    }
  }

  async sendMessage(type, data) {
    if (this.disposed) {
      throw new Error("D2 instance has been disposed");
    }
    await this.ready;
    if (this.disposed) {
      throw new Error("D2 instance has been disposed");
    }
    return new Promise((resolve, reject) => {
      const id = this.nextRequestId++;
      this.pendingRequests.set(id, { resolve, reject });
      try {
        this.worker.postMessage({ id, type, data });
      } catch (error) {
        this.pendingRequests.delete(id);
        reject(error);
      }
    });
  }

  async format(source) {
    return this.sendMessage("format", source);
  }

  async compile(input, options = {}) {
    return this.sendMessage("compile", compileRequest(input, options));
  }

  async renderSVG(input, options = {}) {
    return this.sendMessage("renderSVG", compileRequest(input, options));
  }

  async render(diagram, options = {}) {
    return this.sendMessage("render", { diagram, options });
  }

  async encode(script) {
    return this.sendMessage("encode", script);
  }

  async decode(encoded) {
    return this.sendMessage("decode", encoded);
  }

  async version() {
    return this.sendMessage("version");
  }

  async jsVersion() {
    return this.sendMessage("jsVersion");
  }

  async dispose() {
    if (this.disposePromise) return this.disposePromise;

    this.disposed = true;
    this.rejectPendingRequests(new Error("D2 instance has been disposed"));
    this.disposePromise = (async () => {
      try {
        await this.ready;
      } catch {
        // Initialization errors are already exposed through ready. The worker,
        // if one was created, still needs to be terminated.
      }
      const worker = this.worker;
      this.worker = undefined;
      if (worker) await worker.terminate();
    })();
    return this.disposePromise;
  }
}
