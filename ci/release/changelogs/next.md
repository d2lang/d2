#### Features 🚀

- **Annotations.** `annotation` draws a mark and a Markdown note over a shape or connection without changing the layout: a `circle`, `box`, `underline`, `brace` or `cross`, a `highlight` over a label, an `arrow` from the note, a `trace` along a connection, or a `lasso` round every shape that a class or a glob gives it to. Leave `shape` out and D2 picks the mark that suits the target. An annotation lasts for the board it is declared on, so each step points at something new. `annotation.style.stroke` is the pen: any color, for the mark and its note together. `annotation.style.font-color` colors the note alone. On a board's root, `annotation` is the caption, and a Markdown link to a shape (`[the replica](db.r2)`) marks that shape in the caption's pen. With `animate-duration`, marks are drawn and erased by hand, in the order the caption names them, and each step is held for as long as its text takes to read.

#### Improvements 🧹

- SVG exports use 28.3% fewer bytes across the 157 documentation diagrams, or 34.9% fewer with gzip. D2 embeds smaller bundled fonts and writes more compact stylesheets, code blocks, arrowhead references, textures, sketch paths, and LaTeX glyphs.
- SVG appendices use smaller fonts for ASCII footnotes and numbered markers while retaining their kerning, ligatures, and hinting.
- **GIF.** The ceiling on an animation's total frame area rose from 160 to 256 Mi-pixels, enough for a narrated explainer of about twenty seconds at 700 by 600.

#### Bugfixes ⛑️

#### Breaking Changes

- **`annotation` is a keyword.** A shape named `annotation` must now be quoted: `"annotation": note`. The compiler says so when it meets one.

<!-- When this release changes D2.js, add a “#### D2.js <npm version>” section here. -->
