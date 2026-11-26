#!/usr/bin/env node
import {
  lstat,
  mkdtemp,
  readFile,
  realpath,
  rename,
  rm,
  stat,
  writeFile,
} from "node:fs/promises";
import { dirname, extname, join, parse, sep } from "node:path";
import { text } from "node:stream/consumers";
import { pipeline } from "node:stream/promises";

const help = `Usage: d2 [input.d2|-] [output.svg|-]

Render one D2 source file to SVG with WebAssembly.
File input defaults to a sibling .svg file. Piped input defaults to stdout.
Use - for stdin or stdout. Use the native D2 CLI for multi-file imports.

  -h, --help      Print this help
  -v, --version   Print the installed D2.js package version`;

function diagnostic(error) {
  // The WASM compiler returns parse errors as a JSON array. Each message
  // already includes its source position.
  try {
    const errors = JSON.parse(error.message);
    if (
      Array.isArray(errors) &&
      errors.length &&
      errors.every((item) => typeof item?.errmsg === "string")
    ) {
      const messages = errors.map((item) => item.errmsg);
      if (messages.some((message) => message.includes(": failed to import "))) {
        messages.push(
          "The D2.js CLI renders one source file. Use the native D2 CLI for local imports."
        );
      }
      return messages.join("\n");
    }
  } catch {
    // Worker and filesystem errors are already plain text.
  }
  return error.message;
}

async function writeSVG(input, output, svg) {
  let existing;
  try {
    existing = await lstat(output);
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
  }
  if (existing?.isSymbolicLink()) {
    output = await realpath(output);
    existing = await stat(output);
  }
  if (existing) {
    if (!existing.isFile()) throw new Error(`Output is not a regular file: ${output}`);
    if (input !== "-") {
      const source = await stat(input);
      if (source.dev === existing.dev && source.ino === existing.ino) {
        throw new Error(`Input and output refer to the same file: ${output}`);
      }
    }
  }
  const temp = await mkdtemp(join(dirname(output), ".d2-"));
  try {
    const staged = join(temp, "output.svg");
    await writeFile(staged, svg, { mode: existing?.mode });
    await rename(staged, output);
  } finally {
    await rm(temp, { recursive: true, force: true });
  }
}

async function main(args) {
  if (
    (!args.length && process.stdin.isTTY) ||
    (args.length === 1 && ["-h", "--help"].includes(args[0]))
  ) {
    console.log(help);
    return;
  }
  if (args.length === 1 && ["-v", "--version"].includes(args[0])) {
    const manifest = JSON.parse(
      await readFile(new URL("../package.json", import.meta.url), "utf8")
    );
    console.log(manifest.version);
    return;
  }
  if (args.length > 2 || args.some((arg) => arg !== "-" && arg.startsWith("-"))) {
    throw new Error(
      "Expected an input file and optional SVG output. Run d2 --help for usage."
    );
  }
  const [input = "-", requestedOutput] = args;
  const { dir, name } = parse(input);
  const output = requestedOutput ?? (input === "-" ? "-" : join(dir, `${name}.svg`));
  if (output !== "-" && extname(output).toLowerCase() !== ".svg") {
    throw new Error(`Expected an .svg output file or -, got ${output}`);
  }
  const source =
    input === "-" ? await text(process.stdin) : await readFile(input, "utf8");
  const inputPath = input === "-" ? "<stdin>" : input.split(sep).join("/");
  const { D2 } = await import("../dist/node-esm/index.js");
  const d2 = new D2();
  try {
    const svg = await d2.renderSVG({ fs: { [inputPath]: source }, inputPath });
    if (output === "-") await pipeline([svg], process.stdout);
    else await writeSVG(input, output, svg);
  } finally {
    await d2.dispose();
  }
}

main(process.argv.slice(2)).catch((error) => {
  console.error(diagnostic(error));
  process.exitCode = 1;
});
