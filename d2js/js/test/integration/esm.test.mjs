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
