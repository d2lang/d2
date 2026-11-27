#### Features 🚀

#### Improvements 🧹

- SVG exports embed smaller subsets of D2's bundled fonts. The 157 documentation examples use about 17% fewer bytes in total, or 27% fewer with gzip.
- SVG exports use fewer bytes for element tags and Markdown coordinates and omit unused XLink declarations.
- SVG exports combine and compact the generated CSS for fonts, themes, and animations.
- SVG code blocks use fewer bytes for indentation and quoted strings.
- Sketch SVGs omit redundant zeros and separators in hand-drawn paths while retaining their original coordinates.
- SVG exports use shorter path data for paper textures and sketch fills.
- SVG connections use shorter references to arrowhead markers.
- SVG exports use shorter path coordinates for LaTeX formulas.
- Standalone SVGs with ASCII footnotes or numbered appendix markers can embed smaller fonts while preserving kerning and ligatures.
- SVG exports embed D2's bundled font subsets as WOFF2, preserving outlines, metrics, and hinting.

#### Bugfixes ⛑️

<!-- When this release changes D2.js, add a “#### D2.js <npm version>” section here. -->
