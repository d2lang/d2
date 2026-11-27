#### Features 🚀

#### Improvements 🧹

- SVG exports embed smaller subsets of D2's bundled fonts. The 157 documentation examples use about 17% fewer bytes in total, or 27% fewer with gzip.
- SVG exports use fewer bytes for element tags and Markdown coordinates and omit unused XLink declarations.
- SVG exports combine and compact the generated CSS for fonts, themes, and animations.
- SVG code blocks use fewer bytes for indentation and quoted strings.
- Sketch SVGs omit redundant zeros and separators in hand-drawn paths while retaining their original coordinates.

#### Bugfixes ⛑️

<!-- When this release changes D2.js, add a “#### D2.js <npm version>” section here. -->
