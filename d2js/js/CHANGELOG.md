# Changelog

All notable changes to only the d2.js package will be documented in this file. **Does not
include changes to the main d2 project.**

## Next

- Release the embedded browser worker's blob URL after startup, including when startup fails.
- Add `d2.format(source)` to format a D2 file without compiling or rendering it.
- Add `d2.renderSVG(source, options)` to compile D2 source or a multi-file project directly to SVG.
- Fix passing custom TrueType font bytes as a `Uint8Array` in compile options.
- Support calling `new D2()` from a dedicated browser worker and loading a self-hosted worker script with `new D2({ workerURL })`.
- Support `forceAppendix` with `animateInterval` to show each board's numbered tooltips and links in its animation frame.
- Fix WebAssembly startup in Safari 27.2 workers. `D2.ready` now reports the startup error if Go exits before registering the API.
- Build the browser and Node WebAssembly module with TinyGo, reducing the Brotli-compressed WASM payload by about 40%.
- Build that module without a goroutine scheduler and store its tables more compactly: it is a further 50.2% smaller, 32.7% when compressed with Brotli, and takes about half the time, with identical output. Building from source needs TinyGo from [d2lang/tinygo](https://github.com/d2lang/tinygo/releases).

## [0.1.34]
### September 7, 2026

- Support D2 0.9.0, including the bundled open-source TALA layout engine (`layout: "tala"`) and configurable TALA seeds.
- Add `D2.dispose()` for terminating the backing worker
- Fix concurrent calls sharing a D2 instance
- Fix completions after Unicode characters
- Deprecate the raw `d2.getELKGraph` and `d2.getObjOrder` compatibility exports. They remain callable for one release and emit one migration warning per export.
- Fix TypeScript signatures
- Fix theme-overrides not applying
- Fix grids in ELK layouts
- Significantly reduce bundle size

## [0.1.22]
### March 20, 2025

- Support `d2-config`. Support additional options. [#2343](https://github.com/terrastruct/d2/pull/2343)
  - `themeID`
  - `darkThemeID`
  - `center`
  - `pad`
  - `scale`
  - `forceAppendix`
  - `target`
  - `animateInterval`
  - `salt`
  - `noXMLTag`
- Support relative imports. Improve elk error handling [#2382](https://github.com/terrastruct/d2/pull/2382)
- Support fonts (`fontRegular`, `fontItalic`, `fontBold`, `fontSemiBold`) [#2384](https://github.com/terrastruct/d2/pull/2384)
- Add TypeScript signatures

## [0.1.21]
### January 12, 2025

First public release
