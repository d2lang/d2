import assert from "node:assert/strict";
import test from "node:test";
import { D2 } from "@d2lang/d2";

test("installed ESM package compiles and renders", async () => {
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

test("Playground links round-trip D2 source through the installed package", async (t) => {
  const d2 = new D2();
  t.after(() => d2.dispose());
  const source = "client -> café: 🚀\n";
  const url = new URL("https://play.d2lang.com/");
  url.searchParams.set("script", await d2.encode(source));
  const shared = new URL(url.href);
  assert.equal(await d2.decode(shared.searchParams.get("script")), source);
  // A previously shared Playground link must remain readable.
  assert.equal(await d2.decode("qlDQtVOo5AIEAAD__w=="), "x -> y\n");
});

test("format returns canonical source without resolving imports", async (t) => {
  const d2 = new D2();
  t.after(() => d2.dispose());
  const formatted = "# café\nservice: @missing\nuser -> service\n";
  assert.equal(await d2.format("# café\n  service:@missing\nuser   ->service"), formatted);
  assert.equal(await d2.format(formatted), formatted);
  await assert.rejects(d2.format("broken: {"), /maps must be terminated/);
  assert.equal(await d2.format(""), "");
});

test("renderSVG preserves imports, source configuration, and option precedence", async (t) => {
  const d2 = new D2();
  t.after(() => d2.dispose());
  const input = {
    fs: {
      "diagrams/main.d2": "vars: {d2-config: {theme-id: 4}}\nservice: @../shared",
      "shared.d2": "label: café",
    },
    inputPath: "diagrams/main.d2",
    options: { pad: 10 },
  };
  const options = { pad: 25, noXMLTag: true };
  const compiled = await d2.compile(input, options);
  const svg = await d2.renderSVG(input, options);
  assert.equal(svg, await d2.render(compiled.diagram, compiled.renderOptions));
  assert.match(svg, /^<svg\b/);
  assert.match(svg, /café/);
});

test("renderSVG propagates compile and render errors and rejects ASCII output", async (t) => {
  const d2 = new D2();
  t.after(() => d2.dispose());
  await assert.rejects(d2.renderSVG("broken: {"), /maps must be terminated/);
  await assert.rejects(
    d2.renderSVG("x -> y", { target: "layers.missing" }),
    /render target 'layers\.missing' not found/
  );
  await assert.rejects(
    d2.renderSVG({ fs: { index: "a" }, options: { ascii: true } }, { ascii: false }),
    /ASCII/
  );
});

test("connection icon rates use the duration of the selected animation", async (t) => {
  const d2 = new D2();
  t.after(() => d2.dispose());
  const source = "a -> b: {icon.style.animated: 0.25/s}\nsteps.next: {(a -> b)[0].label: next}";
  const short = { target: "*", animateInterval: 1000 };
  const enough = { target: "*", animateInterval: 2000 };
  await assert.rejects(d2.compile(source, short), /0\.25\/s/);
  await assert.rejects(d2.renderSVG(source, short), /0\.25\/s/);
  const compiled = await d2.compile(source, enough);
  assert.equal(compiled.diagram.connections[0].iconShape, "circle");
  assert.deepEqual(compiled.diagram.connections[0].iconAnimation, { rate: 0.25 });
  assert.equal(compiled.graph.edges[0].attributes.iconStyle.animated.value, "0.25/s");
  await assert.rejects(d2.render(compiled.diagram, short), /0\.25\/s/);
  assert.match(await d2.render(compiled.diagram, enough), /d2-moving-icons/);

  const standalone = "a -> b: {icon.style.animated: 0.25/s}";
  assert.match(await d2.renderSVG(standalone, short), /animateMotion/);
});

test("connection icon trip durations survive compilation and select the loop period", async (t) => {
  const d2 = new D2();
  t.after(() => d2.dispose());
  const loop = "a -> b: {icon.style.animated: 1500ms}";
  const rate = "a -> b: {icon.style.animated: 2/s}";
  const short = { target: "*", animateInterval: 500 };
  const compiled = await d2.compile(loop);
  assert.deepEqual(compiled.diagram.connections[0].iconAnimation, { durationMs: 1500 });
  assert.match(await d2.render(compiled.diagram), /dur="1\.5s"/);
  const steps = "\nsteps.next: {(a -> b)[0].label: next}";
  await assert.rejects(d2.compile(loop + steps, short), /animation loop/);
  await assert.rejects(d2.renderSVG(loop + steps, short), /animation loop/);
  const stream = await d2.compile(rate + steps, short);
  assert.deepEqual(stream.diagram.connections[0].iconAnimation, { rate: 2 });
  assert.match(await d2.render(stream.diagram, short), /d2-moving-icons/);
  await assert.rejects(d2.compile("a -> b: {icon.style.animated: 2/s in 200ms}"), /icon\.style\.animated/);
  compiled.diagram.connections[0].iconAnimation.rate = 2;
  await assert.rejects(d2.render(compiled.diagram), /iconAnimation/);
});
