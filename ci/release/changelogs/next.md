#### Features 🚀

- shape: `cycle` lays a container's children out on a circle and routes their connections as arcs that start and stop on the shape borders

#### Improvements 🧹

- SVG exports use 28.4% fewer bytes across the 157 documentation diagrams, or 35.1% fewer with gzip. D2 embeds smaller bundled fonts and writes more compact stylesheets, code blocks, arrowhead references, textures, sketch paths, and LaTeX glyphs.
- SVG appendices use precompressed WOFF2 fonts for ASCII footnotes and numbered markers while retaining their kerning, ligatures, and hinting.

#### Bugfixes ⛑️

- Labels that mention a fill pattern class such as `paper-overlay` no longer embed an unused texture in ordinary SVG exports.

<!-- When this release changes D2.js, add a “#### D2.js <npm version>” section here. -->
