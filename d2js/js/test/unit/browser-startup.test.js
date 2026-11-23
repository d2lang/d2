import { afterAll, afterEach, expect, mock, spyOn, test } from "bun:test";
import { D2 } from "../../dist/browser/index.js";
import packageJson from "../../package.json" assert { type: "json" };

afterEach(() => mock.restore());

function trackWorkerURL() {
  const create = spyOn(URL, "createObjectURL");
  const revoke = spyOn(URL, "revokeObjectURL");
  return () => {
    expect(create).toHaveBeenCalledTimes(1);
    expect(revoke.mock.calls).toEqual([[create.mock.results[0].value]]);
  };
}

test("the browser worker starts from the embedded base64 payload without a window", async () => {
  expect(typeof window).toBe("undefined");
  const postMessage = spyOn(Worker.prototype, "postMessage");
  const expectReleased = trackWorkerURL();
  const d2 = new D2();
  try {
    expect(await d2.jsVersion()).toBe(packageJson.version);
    expectReleased();
    const [init] = postMessage.mock.calls[0];
    expect(init.type).toBe("init");
    expect(typeof init.data.wasm).toBe("string");
  } finally {
    await Promise.all([d2.dispose(), d2.dispose()]);
  }
  expectReleased();
}, 20000);

test.each(["constructor", "initialization"])(
  "releases the embedded worker URL on %s failure",
  async (stage) => {
    const expectReleased = trackWorkerURL();
    if (stage === "constructor") {
      spyOn(globalThis, "Worker").mockImplementation(() => {
        throw new Error("worker construction failed");
      });
    } else {
      const post = Worker.prototype.postMessage;
      spyOn(Worker.prototype, "postMessage").mockImplementation(function (init) {
        post.call(this, { ...init, data: { ...init.data, wasm: new Uint8Array() } });
      });
    }
    const d2 = new D2();
    try {
      await expect(d2.ready).rejects.toThrow(/worker construction failed|D2 WASM failed/);
      expectReleased();
    } finally {
      await d2.dispose();
    }
    expectReleased();
  },
  20000
);

const workerURL = new URL("../../dist/browser/worker.js", import.meta.url);
const callerBlobURL = URL.createObjectURL(
  new Blob([await Bun.file(workerURL).text()], { type: "text/javascript" })
);
afterAll(() => URL.revokeObjectURL(callerBlobURL));

test.each([workerURL, workerURL.href, callerBlobURL])(
  "renders with a supplied worker URL (%p)",
  async (url) => {
    const createObjectURL = spyOn(URL, "createObjectURL");
    const revokeObjectURL = spyOn(URL, "revokeObjectURL");
    const d2 = new D2({ workerURL: url });
    try {
      const result = await d2.compile("a -> b");
      expect(await d2.render(result.diagram, result.renderOptions)).toContain("<svg");
    } finally {
      await d2.dispose();
    }
    expect(createObjectURL).not.toHaveBeenCalled();
    expect(revokeObjectURL).not.toHaveBeenCalled();
  },
  20000
);
