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
const fixtures = {
  playback: "playback.d2",
  departure: "departure.d2",
  "layer-steps": "layer-steps.d2",
  "layer-route": "layer-route.d2",
  "layer-zoom": "../../../../docs/examples/layer-zoom.d2",
  "checkout-rollout": "../../../../docs/examples/connection-icons/checkout-rollout.d2",
};
const fixtureNames = Object.keys(fixtures);

// Set the actual CSS animation clocks. Waiting for paint after a seek avoids
// depending on when the worker, fonts or CI runner finished loading.
async function seek(page, ms) {
  await page.evaluate(async (ms) => {
    const animations = document.getAnimations();
    for (const animation of animations) {
      animation.pause();
      animation.currentTime = ms;
    }
    await document.fonts.ready;
    await new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)));
  }, ms);
}

const progressSelector = 'rect[class^="d2-progress-"]';

// Compare all of the board artwork, including the pixels underneath the moving line.
async function boardScreenshot(page) {
  const line = page.locator(progressSelector);
  await line.evaluate((rect) => rect.style.setProperty("visibility", "hidden"));
  try {
    return PNG.sync.read(await page.screenshot());
  } finally {
    await line.evaluate((rect) => rect.style.removeProperty("visibility"));
  }
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
    const progress = await page.locator(progressSelector).evaluate((line) => {
      const viewport = line.ownerSVGElement;
      return {
        visible: getComputedStyle(line).visibility,
        width: line.getBoundingClientRect().width,
        total: viewport.viewBox.baseVal.width * viewport.getScreenCTM().a,
      };
    });
    assert.equal(progress.visible, "visible", `progress hidden at ${ms}ms`);
    const expected = progress.total * ((ms % 2000) / 2000);
    assert.ok(
      Math.abs(progress.width - expected) <= 1,
      `progress at ${ms}ms: ${progress.width}px, expected ${expected}px`
    );
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
    const { data: pixels, width } = await boardScreenshot(page);
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

// An SVG motion-path URL can resolve at a different scale than its painted
// connection in a zoomed document. Check both an inline diagram and the
// gallery's iframe against the actual painted wires.
async function checkConnectionIconPositions(page, svg, embedded) {
  await page.goto(origin);
  let frame = page.mainFrame();
  if (embedded) {
    await page.setContent('<body style="zoom:1.5"><iframe style="width:600px;height:190px;border:0"></iframe>');
    await page.locator("iframe").evaluate((iframe, svg) => {
      iframe.srcdoc = '<style>body{margin:0}body>svg{width:100%;display:block}</style>' + svg;
    }, svg);
    frame = page.frames().find((f) => f.parentFrame() === page.mainFrame());
  } else {
    await page.setContent('<style>body{zoom:1.5}body>svg{width:600px;display:block}</style>' + svg);
  }
  await frame.locator(".d2-moving-icons").first().waitFor({ state: "attached" });
  // Paint a small magenta center in each real dot. A copy of the visible
  // connection drawn over the centers should cover every one of them.
  await frame.evaluate(() => {
    for (const wire of document.querySelectorAll("path.connection")) {
      const edge = wire.parentElement;
      if (!edge.querySelector(".d2-moving-icons")) continue;
      for (const dot of edge.querySelectorAll("defs circle")) {
        dot.style.fill = "#ff00ff";
        dot.setAttribute("r", "1.5");
      }
      const cover = document.createElementNS(wire.namespaceURI, "path");
      cover.setAttribute("d", wire.getAttribute("d"));
      cover.setAttribute("class", "test-wire-cover");
      cover.setAttribute("style", "fill:none;stroke:black;stroke-width:5;display:none");
      edge.append(cover);
    }
  });
  const centerPixels = async () => {
    const { data } = PNG.sync.read(await page.screenshot());
    let count = 0;
    for (let i = 0; i < data.length; i += 4) {
      if (data[i] > 210 && data[i + 1] < 70 && data[i + 2] > 210) count++;
    }
    return count;
  };
  for (const ms of [370, 6420, 9470]) {
    await seek(frame, ms);
    for (const covered of [false, true]) {
      await frame.locator(".test-wire-cover").evaluateAll((paths, show) => {
        for (const path of paths) path.style.display = show ? "inline" : "none";
      }, covered);
      const count = await centerPixels();
      if (covered) assert.equal(count, 0, `icon centers off their wires at ${ms}ms`);
      else assert.ok(count >= 8, `expected visible icon centers at ${ms}ms`);
    }
  }
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

async function checkLayerZoom(page) {
  const links = page.locator("a[href^='#']");
  async function scales(ms) {
    await seek(page, ms);
    return links.evaluateAll((links) => links.map((link) => link.getScreenCTM().a));
  }
  async function visible() {
    return links.evaluateAll((links) =>
      links.map((link) => getComputedStyle(link).visibility === "visible")
    );
  }

  assert.ok(
    await page.evaluate(() =>
      document.getAnimations().some((clock) => clock.playState === "running")
    ),
    "the positive interval starts playback without a click"
  );
  await seek(page, 500);
  assert.deepEqual(await visible(), [true, false], "playback starts on the overview");
  const entering = await scales(2800);
  const closer = await scales(2950);
  assert.ok(closer.every((scale, i) => scale > entering[i]), "autoplay zooms into the layer");
  await seek(page, 3500);
  assert.deepEqual(await visible(), [false, true], "playback reaches the inner board");
  const leaving = await scales(5100);
  const farther = await scales(5250);
  assert.ok(farther.every((scale, i) => scale < leaving[i]), "autoplay zooms out on wraparound");
  await seek(page, 6000);
  assert.deepEqual(await visible(), [true, false], "the next loop starts on the overview");

  await links.first().click();
  const clickEntering = await scales(800);
  const clickCloser = await scales(950);
  assert.ok(clickCloser.every((scale, i) => scale > clickEntering[i]), "clicking zooms in");
  await seek(page, 12500);
  assert.deepEqual(await visible(), [false, true], "manual selection holds the inner board");

  await links.last().click();
  const clickLeaving = await scales(100);
  const clickFarther = await scales(250);
  assert.ok(clickFarther.every((scale, i) => scale < clickLeaving[i]), "clicking back zooms out");
  await seek(page, 9500);
  assert.deepEqual(await visible(), [true, false], "back link restores the overview");

  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto(origin + "/layer-zoom.svg");
  assert.equal(
    await page.locator(progressSelector).evaluate((line) => getComputedStyle(line).visibility),
    "hidden",
    "reduced motion hides the moving progress line"
  );
  const reducedScales = await scales(0);
  for (const [ms, expected] of [
    [2999, [true, false]],
    [3000, [false, true]],
    [5999, [false, true]],
    [6000, [true, false]],
  ]) {
    assert.deepEqual(await scales(ms), reducedScales, "reduced motion keeps both boards still");
    assert.deepEqual(await visible(), expected, "reduced motion cuts at the interval boundary");
  }
  await links.first().click();
  await seek(page, 0);
  assert.deepEqual(await visible(), [false, true], "reduced motion enters the layer immediately");
  await links.last().click();
  await seek(page, 0);
  assert.deepEqual(await visible(), [true, false], "reduced motion returns immediately");
}

async function checkLayerSteps(page) {
  const links = page.locator("a");
  async function active(ms) {
    await seek(page, ms);
    return links.evaluateAll((links) =>
      links
        .filter((link) => {
          const r = link.getBoundingClientRect();
          const hit = document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2);
          return hit?.closest("a") === link;
        })
        .map((link) => link.querySelector("text").textContent)
    );
  }

  for (const [ms, label] of [
    [500, "Enter detail"],
    [1500, "Return to overview"],
    [2500, "Return from detail step"],
    [3500, "Enter from overview step"],
    [4500, "Enter detail"],
  ]) {
    assert.deepEqual(await active(ms), [label], `automatic board at ${ms}ms`);
  }
  await seek(page, 3500);
  await links.filter({ hasText: "Enter from overview step" }).click();
  assert.deepEqual(await active(500), ["Return to overview"]);
  assert.deepEqual(await active(1800), ["Return from detail step"]);
  await links.filter({ hasText: "Return from detail step" }).click();
  assert.deepEqual(await active(500), ["Enter detail"]);
  assert.deepEqual(await active(1800), ["Enter from overview step"]);
}

async function checkLayerRoute(page) {
  await page.mouse.move(1, 1);
  const frames = page.locator("g[id^='d2-frame-']");
  const pictures = [];
  async function picture(ms) {
    await seek(page, ms);
    return (await boardScreenshot(page)).data;
  }
  for (const ms of [500, 2500, 4500, 6500]) pictures.push(await picture(ms));

  async function accessibleBoard(board) {
    await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(resolve)));
    const snapshot = await page.locator("svg").first().ariaSnapshot();
    const labels = [
      "Catalog", "Go to shipping", "Product specifications", "Shipping queue", "Archived orders",
    ];
    for (const [i, label] of labels.entries()) {
      assert.equal(snapshot.includes(label), i === board, `accessible label for board ${i}`);
    }
  }
  async function atBoard(ms, board) {
    const pixels = await picture(ms);
    // Chromium can round an antialiased pixel by one level as the camera settles.
    assert.ok(
      pixels.every((value, i) => Math.abs(value - pictures[board][i]) <= 1),
      `board ${board} at ${ms}ms`
    );
  }
  async function scales(ms, from, to) {
    await seek(page, ms);
    return frames.evaluateAll(
      (frames, pair) => pair.map((i) => frames[i].querySelector("text").getScreenCTM().a),
      [from, to]
    );
  }
  async function checkLeg(start, duration, from, to, inward, portalLabel) {
    // The scale changes early on exit and late on entry. The other phase pans.
    const a = await scales(start + duration * (inward ? 0.8 : 0.1), from, to);
    const b = await scales(start + duration * (inward ? 0.95 : 0.25), from, to);
    assert.ok(
      b.every((scale, i) => (inward ? scale > a[i] : scale < a[i])),
      `zoom ${inward ? "in" : "out"} from board ${from} to ${to}`
    );
    const parent = frames.nth(inward ? from : to);
    const distance = await parent.locator("a").filter({ hasText: portalLabel }).evaluate((link) => {
      const svg = link.closest("svg.d2-svg");
      const view = svg.viewBox.baseVal;
      const center = new DOMPoint(view.x + view.width / 2, view.y + view.height / 2);
      const screen = center.matrixTransform(svg.getScreenCTM());
      const rect = link.querySelector(".shape rect").getBoundingClientRect();
      return Math.hypot(rect.x + rect.width / 2 - screen.x, rect.y + rect.height / 2 - screen.y);
    });
    assert.ok(distance < 1, `camera centers the enclosing board's ${portalLabel} portal`);
  }

  // Product detail → catalog → overview → shipping, in one 900ms window.
  await atBoard(5099, 2);
  await accessibleBoard(2);
  await checkLeg(5100, 300, 2, 1, false, "Product detail");
  await atBoard(5400, 1);
  // Switching motion preferences mid-route preserves the tour's current time.
  await page.emulateMedia({ reducedMotion: "reduce" });
  await accessibleBoard(2);
  await page.emulateMedia({ reducedMotion: "no-preference" });
  await accessibleBoard(1);
  await checkLeg(5400, 300, 1, 0, false, "Catalog");
  await atBoard(5700, 0);
  await checkLeg(5700, 300, 0, 3, true, "Shipping");
  await atBoard(6000, 3);

  // An arrowhead can be defined in another board that is currently hidden.
  const markerID = await frames.first().locator("marker").first().getAttribute("id");
  const arrow = frames.nth(3).locator("[marker-end]").first();
  await arrow.evaluate((edge, id) => edge.setAttribute("marker-end", `url(#${id})`), markerID);
  await atBoard(6500, 3);
  await seek(page, 8500);
  await page.emulateMedia({ reducedMotion: "reduce" });
  await accessibleBoard(4);
  await page.emulateMedia({ reducedMotion: "no-preference" });
  await accessibleBoard(4);

  await seek(page, 2500);
  await page.getByText("Go to shipping", { exact: true }).click();
  await page.mouse.move(1, 1);
  await checkLeg(0, 450, 1, 0, false, "Catalog");
  await atBoard(450, 0);
  await checkLeg(450, 450, 0, 3, true, "Shipping");
  await atBoard(900, 3);
  await atBoard(8500, 3);
  await accessibleBoard(3);
  await page.emulateMedia({ reducedMotion: "reduce" });
  await accessibleBoard(3);
  await page.emulateMedia({ reducedMotion: "no-preference" });

  // The return trip starts on the manually selected board.
  await page.getByRole("link", { name: "Back to overview" }).press("Enter");
  await atBoard(0, 3);
  await atBoard(900, 0);
  await accessibleBoard(0);

  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto(origin + "/layer-route.svg");
  await atBoard(5400, 2);
  await atBoard(5999, 2);
  await atBoard(6000.1, 3);
  await seek(page, 2500);
  await page.getByRole("link", { name: "Go to shipping" }).press("Enter");
  await accessibleBoard(3);
  await page.getByRole("link", { name: "Back to overview" }).press("Enter");
  await accessibleBoard(0);
  await page.goBack();
  await accessibleBoard(3);
  await page.goBack();
  await accessibleBoard(1);
  await page.goForward();
  await accessibleBoard(3);
  await page.emulateMedia({ reducedMotion: "no-preference" });
}

for (const engine of [chromium, webkit]) {
  test(engine.name() + " " + mode + " board playback", { timeout: 120_000 }, async (t) => {
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
        Object.values(fixtures).map((file) => readFile(path.join(import.meta.dirname, file), "utf8"))
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
    await page.goto(origin + "/layer-steps.svg");
    await checkLayerSteps(page);
    await page.goto(origin + "/layer-route.svg");
    await checkLayerRoute(page);
    await page.goto(origin + "/layer-zoom.svg");
    await checkLayerZoom(page);
    await page.emulateMedia({ reducedMotion: "no-preference" });
    const checkoutSVG = assets.get("/checkout-rollout.svg").body.toString();
    await checkConnectionIconPositions(page, checkoutSVG, true);
    await checkConnectionIconPositions(page, checkoutSVG, false);
  });
}
