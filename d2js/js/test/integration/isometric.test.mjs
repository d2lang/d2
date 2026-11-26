import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { describe, test } from "node:test";
import { D2 as ESMD2 } from "@d2lang/d2";

const require = createRequire(import.meta.url);
const { D2: CJSD2 } = require("@d2lang/d2");

// D2.js does not include isometric rendering; the CLI and the Go library do.
const unavailable = /isometric rendering is not available in D2\.js/;

for (const [format, D2] of [
  ["ESM", ESMD2],
  ["CJS", CJSD2],
]) {
  describe(`installed ${format} package without isometric rendering`, () => {
    test("an isometric source compiles but does not render", async () => {
      const d2 = new D2();
      try {
        const source =
          "vars: {d2-config: {isometric: true}}\nsystem: {api -> database}";
        const result = await d2.compile(source);
        assert.equal(result.renderOptions.isometric, true);
        await assert.rejects(
          d2.render(result.diagram, result.renderOptions),
          unavailable
        );
        await assert.rejects(d2.renderSVG(source), unavailable);

        const flat = await d2.render(result.diagram, {
          ...result.renderOptions,
          isometric: false,
        });
        assert.match(flat, /<svg\b/);
        assert.doesNotMatch(flat, /d2-isometric/);
      } finally {
        await d2.dispose();
      }
    });

    test("the isometric option is rejected", async () => {
      const d2 = new D2();
      try {
        const enabled = await d2.compile("a -> b", { isometric: true });
        assert.equal(enabled.renderOptions.isometric, true);
        await assert.rejects(
          d2.render(enabled.diagram, enabled.renderOptions),
          unavailable
        );
        await assert.rejects(
          d2.renderSVG("a -> b", { layout: "tala", isometric: true }),
          unavailable
        );
      } finally {
        await d2.dispose();
      }
    });
  });
}
