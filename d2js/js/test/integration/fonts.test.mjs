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
