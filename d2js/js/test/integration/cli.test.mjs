import assert from "node:assert/strict";
import { execSync, spawnSync } from "node:child_process";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const packageRoot = new URL("./node_modules/@d2lang/d2/", import.meta.url);
const pkg = JSON.parse(await readFile(new URL("package.json", packageRoot), "utf8"));
const cli = fileURLToPath(new URL(pkg.bin["d2-svg"], packageRoot));

function run(args, options) {
  // A leaked WASM worker keeps the process alive even after the SVG is written.
  const result = spawnSync(process.execPath, [cli, ...args], {
    encoding: "utf8",
    timeout: 15000,
    ...options,
  });
  assert.ifError(result.error);
  assert.equal(result.signal, null);
  return result;
}

test("the package installs only the d2-svg command", () => {
  assert.deepEqual(Object.keys(pkg.bin), ["d2-svg"]);
  const bin = join("node_modules", ".bin", "d2-svg");
  const help = execSync(`${bin} --help`, { encoding: "utf8" });
  assert.match(help, /^Usage: d2-svg /);
});

test("npx renders piped input with the packed package", () => {
  // The shell finds npx on Unix and npx.cmd on Windows.
  const svg = execSync("npx --offline @d2lang/d2", {
    input: "client -> café",
    encoding: "utf8",
    timeout: 15000,
  });
  assert.match(svg, /^(?:<\?xml[^>]*>\s*)?<svg\b/);
  assert.match(svg, />café<\/text>/);
  assert.match(svg, /<\/svg>\s*$/);
});

test("the CLI reports its installed package version", () => {
  const result = run(["--version"]);
  assert.equal(result.status, 0, result.stderr);
  assert.equal(result.stdout.trim(), pkg.version);
});

test("the CLI reads a file and writes its sibling SVG", async (t) => {
  const cwd = await mkdtemp(join(tmpdir(), "d2 CLI "));
  t.after(() => rm(cwd, { recursive: true, force: true }));
  await writeFile(join(cwd, "my diagram.d2"), "client -> server");
  const result = run(["my diagram.d2"], { cwd });
  assert.equal(result.status, 0, result.stderr);
  assert.equal(result.stdout, "");
  const svg = await readFile(join(cwd, "my diagram.svg"), "utf8");
  assert.match(svg, /<svg\b/);
  assert.match(svg, />client<\/text>/);
  assert.match(svg, />server<\/text>/);
  assert.match(svg, /<\/svg>\s*$/);
});

test("the CLI reports syntax errors on stderr and preserves output on a failed import", async (t) => {
  const invalid = run(["-", "-"], { input: "broken: {" });
  assert.notEqual(invalid.status, 0);
  assert.equal(invalid.stdout, "");
  assert.match(invalid.stderr, /maps must be terminated/);

  const cwd = await mkdtemp(join(tmpdir(), "d2 CLI "));
  t.after(() => rm(cwd, { recursive: true, force: true }));
  await writeFile(join(cwd, "diagram.d2"), "service: @unavailable");
  await writeFile(join(cwd, "previous.svg"), "previous output");
  const missing = run(["diagram.d2", "previous.svg"], { cwd });
  assert.notEqual(missing.status, 0);
  assert.equal(missing.stdout, "");
  assert.match(missing.stderr, /unavailable/);
  assert.match(missing.stderr, /native D2 CLI/i);
  assert.equal(await readFile(join(cwd, "previous.svg"), "utf8"), "previous output");
});
