import { expect, spyOn, test } from "bun:test";
import { D2 } from "../../dist/browser/index.js";
import packageJson from "../../package.json" assert { type: "json" };

test("the browser worker starts from the embedded base64 payload without a window", async () => {
  expect(typeof window).toBe("undefined");
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
  }
}, 20000);

const workerURL = new URL("../../dist/browser/worker.js", import.meta.url);

test.each([workerURL, workerURL.href])(
  "renders with a supplied worker URL (%p)",
  async (url) => {
    const createObjectURL = spyOn(URL, "createObjectURL");
    const d2 = new D2({ workerURL: url });
    try {
      const result = await d2.compile("a -> b");
      expect(await d2.render(result.diagram, result.renderOptions)).toContain("<svg");
      expect(createObjectURL).not.toHaveBeenCalled();
    } finally {
      await d2.dispose();
      createObjectURL.mockRestore();
    }
  },
  20000
);
