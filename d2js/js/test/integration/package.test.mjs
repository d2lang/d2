import assert from "node:assert/strict";
import { once } from "node:events";
import { access, readFile, readdir } from "node:fs/promises";
import { createRequire } from "node:module";
import { basename, join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { Worker } from "node:worker_threads";

const packageRoot = new URL("./node_modules/@d2lang/d2/", import.meta.url);

test("the packed entry points resolve and share one Node runtime", async () => {
  const require = createRequire(import.meta.url);
  const pkg = JSON.parse(await readFile(new URL("package.json", packageRoot), "utf8"));
  assert.equal(
    require.resolve("@d2lang/d2"),
    fileURLToPath(new URL(pkg.main, packageRoot))
  );
  assert.equal(import.meta.resolve("@d2lang/d2"), new URL(pkg.module, packageRoot).href);
  assert.equal(pkg.browser, pkg.exports["."].browser);
  await access(new URL(pkg.browser, packageRoot));

  const files = await readdir(new URL("dist/", packageRoot), { recursive: true });
  for (const name of ["d2.wasm", "wasm_exec.js"]) {
    assert.deepEqual(
      files.filter((file) => basename(file) === name),
      [join("wasm", name)]
    );
  }
});

test("the exported browser worker renders SVG and formats source with the packaged WASM", async (t) => {
  // Adapt Node's message port to the browser worker API.
  const worker = new Worker(
    `const { parentPort, workerData } = require("node:worker_threads");
     globalThis.self = { postMessage: (message) => parentPort.postMessage(message) };
     import(workerData).then(() => {
       parentPort.on("message", (data) => self.onmessage({ data }));
     });`,
    { eval: true, workerData: import.meta.resolve("@d2lang/d2/worker") }
  );
  t.after(() => worker.terminate());
  const wasm = await readFile(new URL("dist/wasm/d2.wasm", packageRoot));
  const ready = once(worker, "message");
  worker.postMessage({ type: "init", data: { wasm } });
  assert.deepEqual(await ready, [{ type: "ready" }]);

  const rendered = once(worker, "message");
  worker.postMessage({
    id: 1,
    type: "renderSVG",
    data: { fs: { index: "x -> y" }, options: {} },
  });
  const [response] = await rendered;
  assert.equal(response.id, 1);
  assert.equal(response.type, "result");
  assert.match(response.data, /<svg\b/);

  const formatted = once(worker, "message");
  worker.postMessage({ id: 2, type: "format", data: "x-> y" });
  assert.deepEqual(await formatted, [{ id: 2, type: "result", data: "x -> y\n" }]);
});
