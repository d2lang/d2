import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { test } from "node:test";
import { chromium, webkit } from "playwright-core";
import { PNG } from "pngjs";

const [mode, inputDir] = process.argv.slice(2);
assert.ok(
  ["packed", "native"].includes(mode) && inputDir,
  "usage: node smoke.mjs packed|native INPUT_DIR"
);
const origin = "http://localhost:4173";
const fixtureNames = ["playback", "departure"];

// Set the actual CSS animation clocks. Waiting for paint after a seek avoids
// depending on when the worker, fonts or CI runner finished loading.
async function seek(page, ms) {
  await page.evaluate(async (ms) => {
    const animations = document.getAnimations();
    if (!animations.length) throw new Error("SVG has no playback animations");
    for (const animation of animations) {
      animation.pause();
      animation.currentTime = ms;
    }
    await document.fonts.ready;
    await new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)));
  }, ms);
}

async function checkBoundary(page) {
  const pictures = new Map();
  for (const [ms, stage] of [
    [500, "before"],
    [999.9, "before"],
    [1000, "after"],
    [1000.1, "after"],
    [1500, "after"],
    [1999.9, "after"],
    [2000, "before"],
  ]) {
    await seek(page, ms);
    const hit = await page.evaluate((ms) => {
      const links = [...document.querySelectorAll("a")].filter(
        (a) => getComputedStyle(a).visibility === "visible"
      );
      if (links.length !== 1) {
        throw new Error(`Expected one visible link at ${ms}ms, found ${links.length}`);
      }
      const rect = links[0].getBoundingClientRect();
      const x = Math.floor(rect.left + rect.width / 4);
      const y = Math.floor(rect.top + rect.height / 2);
      return { href: document.elementFromPoint(x, y)?.closest("a")?.getAttribute("href"), x, y };
    }, ms);
    assert.equal(hit.href, "https://example.org/" + stage, `active actor link at ${ms}ms`);
    const { data: pixels, width } = PNG.sync.read(await page.screenshot());
    const offset = (width * hit.y + hit.x) * 4;
    assert.equal(
      pixels.toString("hex", offset, offset + 3),
      stage === "before" ? "fca311" : "0088ff",
      `actor paint at ${ms}ms`
    );
    if (pictures.has(stage)) {
      assert.ok(pictures.get(stage).equals(pixels), "board flashed at " + ms + "ms");
    } else {
      pictures.set(stage, pixels);
    }
  }
  assert.ok(
    !pictures.get("before").equals(pictures.get("after")),
    "both boards painted identically"
  );
}

// The actors and message have distinct hues. Their fills and stroke remain
// identifiable while fading against white, without tying this test to one
// browser's glyph antialiasing or exact layout coordinates.
async function checkDeparture(page) {
  const samples = [500];
  for (let ms = 1001; ms < 1400; ms += 16) samples.push(ms);
  samples.push(1399.9, 1450);
  for (const ms of samples) {
    await seek(page, ms);
    const pixels = PNG.sync.read(await page.screenshot()).data;
    let client = false;
    let api = false;
    let message = false;
    for (let i = 0; i < pixels.length; i += 4) {
      const r = pixels[i];
      const g = pixels[i + 1];
      const b = pixels[i + 2];
      if (r > g + 6 && g > b + 6) client = true;
      if (g > b + 6 && b > r + 6) api = true;
      if (b > r + 6 && r > g + 6) message = true;
    }
    if (ms === 500) assert.ok(client && api && message, "initial sequence was not painted");
    if (message) assert.ok(client && api, "sequence message outlived an actor at " + ms + "ms");
    if (ms === 1450) assert.ok(!client && !api && !message, "departed sequence is still painted");
  }
}

for (const engine of [chromium, webkit]) {
  test(engine.name() + " " + mode + " step playback", { timeout: 120_000 }, async (t) => {
    const browser = await engine.launch();
    t.after(() => browser.close());
    const page = await browser.newPage({
      viewport: { width: 720, height: 500 },
      reducedMotion: "no-preference",
    });
    // Only the local consumer page and the files actually shipped by npm
    // are available to the browser. No dev server or CDN can mask a bad pack.
    const assets = new Map([
      ["/", { contentType: "text/html", body: "<!doctype html><title>D2 consumer</title>" }],
    ]);
    await page.route("**/*", (route) => {
      const url = new URL(route.request().url());
      if (url.protocol === "blob:" || url.protocol === "data:") return route.continue();
      const asset = url.origin === origin && assets.get(url.pathname);
      return asset ? route.fulfill(asset) : route.abort();
    });
    if (mode === "packed") {
      const pkg = JSON.parse(await readFile(path.join(inputDir, "package.json"), "utf8"));
      const entry = pkg.exports["."].browser;
      assets.set("/d2.js", {
        contentType: "text/javascript",
        body: await readFile(path.join(inputDir, entry)),
      });
      const sources = await Promise.all(
        fixtureNames.map((name) => readFile(path.join(import.meta.dirname, name + ".d2"), "utf8"))
      );
      await page.goto(origin);
      const workerStarted = page.waitForEvent("worker", { timeout: 30_000 });
      const [svgs] = await Promise.all([
        page.evaluate(async (sources) => {
          const { D2 } = await import("/d2.js");
          const d2 = new D2();
          try {
            const output = [];
            // D2.js includes child steps when the target is "*".
            for (const source of sources) output.push(await d2.renderSVG(source, { target: "*" }));
            return output;
          } finally {
            await d2.dispose();
          }
        }, sources),
        workerStarted,
      ]);
      for (const [i, name] of fixtureNames.entries()) {
        assets.set("/" + name + ".svg", { contentType: "image/svg+xml", body: svgs[i] });
      }
    } else {
      for (const name of fixtureNames) {
        assets.set("/" + name + ".svg", {
          contentType: "image/svg+xml",
          body: await readFile(path.join(inputDir, name + ".svg")),
        });
      }
    }
    await page.goto(origin + "/playback.svg");
    await checkBoundary(page);
    await page.goto(origin + "/departure.svg");
    await checkDeparture(page);
  });
}
