import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { test } from "node:test";
import { chromium, webkit } from "playwright-core";

const viewer = await readFile(
  new URL("../../../../d2cli/mcpassets/viewer.html", import.meta.url),
  "utf8"
);

for (const engine of [chromium, webkit]) {
  test(engine.name() + " fits the MCP diagram after zooming", { timeout: 30_000 }, async (t) => {
    const browser = await engine.launch();
    t.after(() => browser.close());
    const page = await browser.newPage();
    await page.evaluate((html) => {
      const iframe = document.createElement("iframe");
      iframe.style.cssText = "width: 500px; height: 300px; border: 0";
      window.addEventListener("message", ({ data, source }) => {
        if (source !== iframe.contentWindow) return;
        if (data.method === "ui/initialize") {
          source.postMessage(
            {
              jsonrpc: "2.0",
              id: data.id,
              result: { hostContext: { containerDimensions: { maxHeight: 300 } } },
            },
            "*"
          );
        } else if (data.method === "ui/notifications/initialized") {
          source.postMessage(
            {
              jsonrpc: "2.0",
              method: "ui/notifications/tool-result",
              params: {
                structuredContent: { board: "root", layout: "dagre" },
                _meta: {
                  "com.d2lang/svg":
                    '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 80 8000"><rect width="80" height="8000"/></svg>',
                },
              },
            },
            "*"
          );
        }
      });
      iframe.srcdoc = html;
      document.body.append(iframe);
    }, viewer);

    const frame = page.frameLocator("iframe");
    await frame.getByText("root · dagre").waitFor();
    const image = frame.getByRole("img");
    const bounds = await image.boundingBox();
    const stage = await frame.locator("#stage").boundingBox();
    assert.ok(
      bounds.height > 0 && bounds.height <= stage.height + 1,
      "full diagram fits vertically"
    );
    assert.ok(bounds.width <= stage.width + 1, "full diagram fits horizontally");
    await frame.getByRole("button", { name: "Zoom out" }).click();
    assert.ok(
      (await image.boundingBox()).height <= bounds.height,
      "zoom out never enlarges the diagram"
    );
    await frame.getByRole("button", { name: "Zoom in" }).click();
    assert.ok((await image.boundingBox()).height > bounds.height, "zoom enlarged the drawing");
    await frame.getByRole("button", { name: "Fit", exact: true }).click();
    assert.deepEqual(await image.boundingBox(), bounds);
  });
}
