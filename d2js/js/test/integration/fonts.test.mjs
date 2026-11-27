import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { D2 } from "@d2lang/d2";

test("both rendering paths accept custom font bytes from a typed array view", async () => {
  const ttf = await readFile(new URL("./RockSalt-Regular.ttf", import.meta.url));
  const storage = new Uint8Array(ttf.length + 2);
  storage.set(ttf, 1);
  const font = storage.subarray(1, 1 + ttf.length);
  const options = {
    fontRegular: font,
    fontItalic: font,
    fontBold: font,
    fontSemibold: font,
  };
  const d2 = new D2();
  try {
    const source = "hello world";
    const compiled = await d2.compile(source, options);
    assert.equal(compiled.diagram.fontFamily, "custom");
    const svg = await d2.render(compiled.diagram, compiled.renderOptions);
    assert.equal(await d2.renderSVG(source, options), svg);
    assert.notEqual(await d2.renderSVG(source), svg);
  } finally {
    await d2.dispose();
  }
});


test("compiled diagrams retain their custom faces across compilations and workers", async () => {
  const font = new Uint8Array(await readFile(new URL("./RockSalt-Regular.ttf", import.meta.url)));
  const source = "hello; layers: {details: {hello}}";
  const d2 = new D2();
  const fresh = new D2();
  try {
    const a = await d2.compile(source, { fontRegular: font, fontBold: font, fontSemibold: font });
    const before = await d2.render(a.diagram);
    const layer = a.diagram.layers[0];
    const beforeLayer = await d2.render(layer);
    const saved = JSON.stringify(a.diagram);
    assert.equal((saved.match(/"customFonts"/g) ?? []).length, 1);

    // The second diagram falls back to the bundled face for the styles used
    // in these labels. The italic face still makes this a custom family.
    const b = await d2.compile(source, { fontItalic: font });
    assert.notEqual(await d2.render(b.diagram), before);
    assert.equal(await d2.render(a.diagram), before);
    assert.equal(await fresh.render(JSON.parse(saved)), before);
    assert.equal(await fresh.render(layer), beforeLayer);
    assert.equal(await fresh.render(JSON.parse(saved), { target: "layers.details" }), beforeLayer);
  } finally {
    await Promise.all([d2.dispose(), fresh.dispose()]);
  }
});
