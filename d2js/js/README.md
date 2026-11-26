# D2.js

[![npm version](https://badge.fury.io/js/%40d2lang%2Fd2.svg)](https://www.npmjs.com/package/@d2lang/d2)
[![License: MPL-2.0](https://img.shields.io/badge/License-MPL_2.0-brightgreen.svg)](https://mozilla.org/MPL/2.0/)

D2.js is a JavaScript wrapper around D2, the modern diagram scripting language. It enables running D2 directly in browsers and Node environments through WebAssembly.

For an example of usage, see [https://play.d2lang.com](https://play.d2lang.com), which is powered by d2.js and is [open-source](https://github.com/d2lang/d2-playground).

## Features

- 🌐 **Universal** - Works in both browser and Node environments
- 🚀 **Modern** - Built with ESM modules, with CJS fallback
- 🔄 **Isomorphic** - Same API everywhere
- ⚡ **Fast** - Powered by WebAssembly for near-native performance
- 📦 **Lightweight** - Minimal wrapper around the core D2 engine

## Installation

```bash
# npm
npm install @d2lang/d2

# yarn
yarn add @d2lang/d2

# pnpm
pnpm add @d2lang/d2

# bun
bun add @d2lang/d2
```

`@terrastruct/d2` remains available as a compatibility package for stable releases
during the namespace transition. New installations should use `@d2lang/d2`.

### Nightly

CI checks `master` daily and publishes a new `@d2lang/d2@nightly` when D2.js or its
WebAssembly build inputs have changed since the last published nightly.

```bash
npm install @d2lang/d2@nightly
```

## Usage

D2.js uses webworkers to call a WASM file.

### Basic Usage

```javascript
// Same for Node or browser
import { D2 } from '@d2lang/d2';
// Or using a CDN
// import { D2 } from 'https://esm.sh/@d2lang/d2';

const d2 = new D2();

try {
  const svg = await d2.renderSVG('x -> y');
  console.log(svg);
} finally {
  await d2.dispose();
}
```

Reuse the same instance for multiple diagrams before calling `dispose()`. Pass
options as the second argument, for example `d2.renderSVG('x -> y', { sketch: true })`.

### Open in Playground

Use `d2.encode(source)` to put a diagram into a shareable Playground URL.
It compresses the UTF-8 source with raw DEFLATE and encodes the result as padded,
URL-safe Base64.

```javascript
import { D2 } from '@d2lang/d2';

const d2 = new D2();
try {
  const url = new URL('https://play.d2lang.com/');
  url.searchParams.set('script', await d2.encode('client -> server'));
  console.log(url.href);
} finally {
  await d2.dispose();
}
```

To read a link, get its `script` value with `url.searchParams.get('script')`.
When present, pass that value to `await d2.decode(encoded)` to recover the original source.
You can set other parameters on the same URL, such as `sketch=1` or `theme=300`.
From the command line, `d2 play diagram.d2` opens a file directly in the Playground.

### Imports

In order to support [imports](https://d2lang.com/tour/imports), a mapping of D2 file paths to their content can be passed to the compiler.

```javascript
import { D2 } from '@d2lang/d2';

const d2 = new D2();

const fs = {
  "project.d2": "a: @import",
  "import.d2": "x: {shape: circle}",
}

const svg = await d2.renderSVG({
  fs,
  inputPath: "project.d2",
  options: { sketch: true },
});
await d2.dispose();
```

## API Reference

### `new D2(options?)`

Creates a D2 instance with its own worker. Browsers use a blob URL by default.
Calling the constructor from a dedicated browser worker starts a nested worker.
Node.js uses its bundled worker.

The browser option `workerURL` accepts a string or a `URL`. For a site with a
Content Security Policy such as `worker-src 'self'`, serve the JavaScript file
exported as `@d2lang/d2/worker` from the same origin. For example, if your app
serves `public/` at its root, copy the worker from the same installed package
during your build.

```sh
node -e "require('node:fs').copyFileSync(require.resolve('@d2lang/d2/worker'), 'public/d2-worker.js')"
```

```javascript
const d2 = new D2({ workerURL: '/d2-worker.js' });
```

The worker's Content Security Policy must also allow WebAssembly compilation.

### `format(source: string): Promise<string>`

Formats a single D2 source string with the same formatter as `d2 fmt`. It leaves
imports in place, so an editor can format the current file even when its imports
are unavailable. Syntax errors reject the promise.

```javascript
const formatted = await d2.format('client->server'); // "client -> server\n"
```

### `renderSVG(input: string | CompileRequest, options?: CompileOptions): Promise<string>`

Compiles D2 source and returns an SVG string in one worker call. It accepts the
same input and options as `compile`, including file imports and custom fonts.
For ASCII output, use `compile` and `render`. Passing `ascii: true` to
`renderSVG` rejects the promise.

### `compile(input: string | CompileRequest, options?: CompileOptions): Promise<CompileResponse>`

Compiles D2 markup into an intermediate representation. Pass options directly as the
second argument, for example `d2.compile('x -> y', { layout: 'tala' })`. For an object
input, `input.options` is optional and takes precedence over the second argument when
both specify the same option.

Use the two-step API to inspect the graph or render a diagram again with different
options.

```javascript
const { diagram, renderOptions } = await d2.compile('x -> y');
const svg = await d2.render(diagram, renderOptions);
```

### `render(diagram: Diagram, options?: RenderOptions): Promise<string>`

Renders a compiled diagram to SVG.

### `dispose(): Promise<void>`

Terminates the worker backing this D2 instance and rejects any pending operations. Call
this when the instance is no longer needed so Node processes can exit and browser worker
resources are released. Calling `dispose()` more than once is safe.

### `CompileOptions`

All [RenderOptions](#renderoptions) properties in addition to:

- `layout`: Layout engine to use (`'dagre' | 'elk' | 'tala'`) [default: `'dagre'`]
- `animateDuration`: Transition duration in milliseconds (0–600000, default 0). Numeric properties, solid colors, compatible gradients, variables, and grid layouts animate between steps. Underlines and Markdown strikethroughs draw or retract when the same text gains or loses its decoration across boards. Set this in `compile()` or `renderSVG()` to override `vars.d2-config.animate-duration`. A shorter transition uses the rest of the board interval to hold its final value. A longer transition extends the board.
- `fontRegular` A byte array containing .ttf file to use for the regular font. If none provided, Source Sans Pro Regular is used.
- `fontItalic` A byte array containing .ttf file to use for the italic font. If none provided, Source Sans Pro Italic is used.
- `fontBold` A byte array containing .ttf file to use for the bold font. If none provided, Source Sans Pro Bold is used.
- `fontSemibold` A byte array containing .ttf file to use for the semibold font. If none provided, Source Sans Pro Semibold is used.

### `RenderOptions`

- `sketch`: Enable sketch mode (default `false`). In an animated diagram, changed solid fills shade in with pencil strokes. You can enable sketch when compiling or when rendering an already compiled diagram.
- `themeID`: Theme ID to use [default: 0]
- `darkThemeID`: Theme ID to use when client is in dark mode
- `center`: Center the SVG in the containing viewbox [default: false]
- `pad`: Pixels padded around the rendered diagram [default: 100]
- `scale`: Overrides the diagram’s `vars.d2-config.scale`. A positive multiplier sets the SVG dimensions. For example, `0.5` halves its natural size. Use `-1` to fit the screen (default).
- `forceAppendix`: Adds a numbered SVG appendix for tooltips and links (default `false`). An explicit option overrides `vars.d2-config.force-appendix`, including `false`.
- `target`: Target board/s to render. If target ends with '*', it will be rendered with all of its scenarios, steps, and layers. Otherwise, only the target board will be rendered. E.g. `target: 'layers.x.*'` to render layer 'x' with all of its children. Pass '*' to render all scenarios, steps, and layers. By default, only the root board is rendered. Multi-board SVG requires a positive animation interval.
- `animateInterval`: Minimum milliseconds to display each board in an animated SVG. Defaults to the diagram’s `vars.d2-config.animate-interval` setting. An explicit option overrides the source setting, including `0` to disable animation.
- `salt`: Add a salt value to ensure the output uses unique IDs. This is useful when generating multiple identical diagrams to be included in the same HTML doc, so that duplicate IDs do not cause invalid HTML. The salt value is a string that will be appended to IDs in the output.
- `noXMLTag`: Omit XML tag `(<?xml ...?>)` from output SVG files. Useful when generating SVGs for direct HTML embedding.

### `CompileRequest`

- `fs`: A mapping of D2 file paths to their content
- `inputPath`: The path of the entry D2 file [default: index]
- `options`: Optional [CompileOptions](#compileoptions) to pass to the compiler

### `CompileResponse`

- `diagram`: `Diagram`: Compiled D2 diagram
- `renderOptions`: `RenderOptions`: Render options merged with configuration set in diagram
- `fs`
- `graph`

## Development

D2.js builds its browser and Node.js WebAssembly module with TinyGo. Install the
Go version in the root `go.mod`, TinyGo 0.44.0, Binaryen (CI uses version 133),
the Bun version in `package.json`, and Node.js 24 with npm. Put `tinygo` and
`wasm-opt` on PATH. The build bundles the JavaScript runtime from the same TinyGo
installation as the compiler.

### Building from source

```bash
git clone https://github.com/d2lang/d2.git
cd d2/d2js/js
./make.sh all
```

If you change the main D2 source code, you should regenerate the WASM file:
```bash
./make.sh build
```

### Running the dev server

You can browse the examples by running the dev server:

```bash
./make.sh dev
```

Visit `http://localhost:3000` to see the example page.

### Tests

```bash
./make.sh test
```

### Publishing

See the [npm release instructions](https://github.com/d2lang/d2/blob/master/ci/release/README.md#npm-publishing) for stable
releases and nightly publisher setup.

## Contributing

Contributions are welcome!

## License

This project is licensed under the Mozilla Public License Version 2.0.
