import { expect, spyOn, test } from "bun:test";
import { D2 } from "../../dist/browser/index.js";
import packageJson from "../../package.json" assert { type: "json" };

test("the browser worker starts from the embedded base64 payload", async () => {
  globalThis.window = globalThis;
  const postMessage = spyOn(Worker.prototype, "postMessage");
  const d2 = new D2();
  try {
    expect(await d2.jsVersion()).toBe(packageJson.version);
    const [init] = postMessage.mock.calls[0];
    expect(init.type).toBe("init");
    expect(typeof init.data.wasm).toBe("string");
  } finally {
    await d2.dispose();
    postMessage.mockRestore();
    delete globalThis.window;
  }
}, 20000);
