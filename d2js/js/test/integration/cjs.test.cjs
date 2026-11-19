const assert = require("node:assert/strict");
const test = require("node:test");
const { Worker } = require("node:worker_threads");
const { D2 } = require("@d2lang/d2");

test("installed CommonJS package compiles and renders", async () => {
  const d2 = new D2();
  try {
    const result = await d2.compile("x -> y");
    assert.ok(result.diagram);
    const svg = await d2.render(result.diagram);
    assert.match(svg, /<svg\b/);
    assert.match(svg, /<\/svg>/);
  } finally {
    await d2.dispose();
  }
});

for (const [scenario, run, reason] of [
  [
    "throws",
    async () => {
      throw new Error("startup failure");
    },
    "startup failure",
  ],
  ["exits", async () => {}, "Go exited before registering the D2 API"],
]) {
  test(`ready rejects when Go ${scenario} before exporting the API`, async (t) => {
    const postMessage = Worker.prototype.postMessage;
    t.mock.method(Worker.prototype, "postMessage", function (message, ...args) {
      if (message.type === "init") {
        // Exercise the installed worker and public D2 instance with a controlled Go startup.
        message.data.wasmExecContent += `\nglobalThis.Go.prototype.run = ${run};`;
      }
      return postMessage.call(this, message, ...args);
    });

    const d2 = new D2();
    try {
      const error = { message: `D2 WASM failed to initialize: ${reason}` };
      await assert.rejects(d2.ready, error);
      await assert.rejects(d2.version(), error);
    } finally {
      await d2.dispose();
    }
  });
}
