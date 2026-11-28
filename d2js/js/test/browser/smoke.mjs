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
const fixtureNames = ["playback", "departure", "sequence"];

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

// Compare each frame with the same frame after hiding one fixture paint. This
// measures pixels that paint actually contributed, including SVG <use> copies.
// Unrelated text antialiasing and the browser's color profile cancel out.
async function checkDeparture(page) {
  const probes = {
    client: '[fill="#fca311"]',
    api: '[fill="#02c39a"]',
    message: '[stroke="#8000ff"], [fill="#8000ff"]',
  };
  const style = await page.evaluateHandle(() => {
    const style = document.createElementNS("http://www.w3.org/2000/svg", "style");
    document.documentElement.append(style);
    return style;
  });
  const samples = [500];
  for (let ms = 1001; ms < 1400; ms += 16) samples.push(ms);
  samples.push(1399.9, 1450);
  for (const ms of samples) {
    await seek(page, ms);
    const baseline = PNG.sync.read(await page.screenshot()).data;
    const painted = {};
    for (const [role, selector] of Object.entries(probes)) {
      await style.evaluate((style, selector) => {
        style.textContent = `${selector} { opacity: 0 !important }`;
      }, selector);
      const withoutRole = PNG.sync.read(await page.screenshot()).data;
      painted[role] = !baseline.equals(withoutRole);
    }
    await style.evaluate((style) => {
      style.textContent = "";
    });
    if (ms === 500) {
      assert.deepEqual(
        painted,
        { client: true, api: true, message: true },
        "initial sequence paint"
      );
    }
    if (painted.message) {
      assert.ok(
        painted.client && painted.api,
        `sequence message outlived an actor at ${ms}ms: ${JSON.stringify(painted)}`
      );
    }
    if (ms === 1450) {
      assert.deepEqual(
        painted,
        { client: false, api: false, message: false },
        "departed sequence paint"
      );
    }
  }
}

async function checkSequence(page) {
  async function checkPaint(expected) {
    const paint = await page.evaluate(() => {
      const texts = [...document.querySelectorAll("text")];
      const label = (name) => texts.find((el) => el.textContent === name);
      const line = (stroke) => document.querySelector(`path.connection[stroke="${stroke}"]`);
      // SVG group opacity multiplies the opacity of its children.
      const state = (el) => {
        if (!el) throw new Error("Missing sequence label or connection");
        if (getComputedStyle(el).visibility === "hidden") return "hidden";
        let opacity = 1;
        for (; el; el = el.parentElement) opacity *= Number(getComputedStyle(el).opacity);
        return opacity === 0 ? "hidden" : opacity === 1 ? "shown" : "fading";
      };
      return {
        actors: [label("Client"), label("API")].map(state),
        lifeline: state(line("#007777")),
        connections: ["#8000ff", "#d04600", "#147ac0"].map((color) => state(line(color))),
        labels: ["Request", "Validate", "Response"].map((name) => state(label(name))),
        masks: [1, 2, 3].map((n) => state(document.querySelector(`mask [data-d2-sequence-step="${n}"] rect`))),
      };
    });
    if (expected[0] === "shown") assert.deepEqual(paint.actors, ["shown", "shown"]);
    if (expected[0] === "shown") assert.equal(paint.lifeline, "shown");
    const shafts = expected.map((state) => state === "drawing" ? "shown" : state);
    const arrived = expected.map((state) => state === "drawing" ? "hidden" : state);
    assert.deepEqual(paint.connections, shafts, "sequence shafts");
    assert.deepEqual(paint.labels, arrived, "sequence labels");
    assert.deepEqual(paint.masks, arrived, "sequence label masks");
  }

  for (const [ms, expected] of [
    [0, ["hidden", "hidden", "hidden"]],
    [325, ["drawing", "hidden", "hidden"]],
    [649, ["drawing", "hidden", "hidden"]],
    [700, ["shown", "hidden", "hidden"]],
    [1325, ["shown", "drawing", "hidden"]],
    [1700, ["shown", "shown", "hidden"]],
    [2325, ["shown", "shown", "drawing"]],
    [2700, ["shown", "shown", "shown"]],
    [3900, ["shown", "shown", "shown"]],
    [4000, ["hidden", "hidden", "hidden"]],
    [4700, ["shown", "hidden", "hidden"]],
  ]) {
    await seek(page, ms);
    await checkPaint(expected);
  }
  // Sample painted pixels on both sides of the moving tip. On a self-call
  // this follows the bends. The dashed reply travels along its path in reverse.
  for (const [step, color] of [[1, "8000ff"], [2, "d04600"], [3, "147ac0"]]) {
    for (const progress of [0.25, 0.5, 0.75]) {
      await seek(page, (step - 1) * 1000 + 650 * progress);
      const points = await page.evaluate(({ step, progress }) => {
        const shaft = document.querySelector(`[data-d2-sequence-trace="${step}"]`);
        const flight = document.querySelector(`[data-d2-sequence-flight="${step}"]`);
        const matrix = document.querySelector(`[data-d2-sequence-shaft="${step}"]`).getScreenCTM();
        const at = (fraction) => {
          const p = shaft.getPointAtLength(shaft.getTotalLength() * (step === 3 ? 1 - fraction : fraction));
          const { x, y } = new DOMPoint(p.x, p.y).matrixTransform(matrix);
          return { x, y };
        };
        const tip = new DOMPoint().matrixTransform(flight.getScreenCTM());
        const expected = at(progress);
        if (Math.hypot(tip.x - expected.x, tip.y - expected.y) > 1) {
          throw new Error("Arrowhead left its route at step " + step);
        }
        return { tip: expected, later: at(progress + 0.22) };
      }, { step, progress });
      const image = PNG.sync.read(await page.screenshot());
      const shaft = page.locator(`[data-d2-sequence-shaft="${step}"]`);
      await shaft.evaluate((el) => { el.style.opacity = "0"; });
      const headOnly = PNG.sync.read(await page.screenshot());
      await shaft.evaluate((el) => { el.style.removeProperty("opacity"); });
      assert.ok(!image.data.equals(headOnly.data), `step ${step} drew no shaft at ${progress}`);
      const rgb = color.match(/../g).map((v) => parseInt(v, 16));
      function painted(image, { x, y }, radius) {
        let count = 0;
        const left = Math.max(0, Math.round(x) - radius);
        const right = Math.min(image.width - 1, Math.round(x) + radius);
        const top = Math.max(0, Math.round(y) - radius);
        const bottom = Math.min(image.height - 1, Math.round(y) + radius);
        for (let row = top; row <= bottom; row++) {
          for (let col = left; col <= right; col++) {
            const i = (row * image.width + col) * 4;
            if (rgb.every((v, c) => Math.abs(image.data[i + c] - v) < 40)) count++;
          }
        }
        return count;
      }
      assert.ok(painted(headOnly, points.tip, 5) > 4, `step ${step} tip at ${progress}`);
      assert.equal(painted(image, points.later, 2), 0, `step ${step} leaked past its tip at ${progress}`);
    }
  }
  await page.emulateMedia({ reducedMotion: "reduce" });
  await checkPaint(["shown", "shown", "shown"]);
  assert.equal(await page.evaluate(() => document.getAnimations().length), 0);
}

for (const engine of [chromium, webkit]) {
  test(engine.name() + " " + mode + " diagram playback", { timeout: 120_000 }, async (t) => {
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
    await page.goto(origin + "/sequence.svg");
    await checkSequence(page);
  });
}
