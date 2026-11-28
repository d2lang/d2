#### Features 🚀

- **MP4 export.** Run `d2 tour.d2 tour.mp4` to share an animated diagram as H.264 video. D2 samples motion at 30 fps and stores holds with their duration. Choose an exact resolution with `--video-size=1920x1080`, set `--video-quality` to `low`, `medium`, `high` (default), or `best`, and use `--video-duration=10s` for a fixed-length clip. MP4 export requires FFmpeg 5.1 or newer with libx264.
- **Layer zooms.** Animated SVG and GIF tours zoom through linked shapes as they enter or leave a layer. Moving between sibling layers passes through their shared parent. In a standalone SVG, readers can click a linked shape to explore the destination directly. D2.js supports the same SVG playback and navigation.
- **Annotations.** `annotation` draws a mark and a Markdown note over a shape or connection without changing the layout: a `circle`, `box`, `underline`, `brace` or `cross`, a `highlight` over a label, an `arrow` from the note, a `trace` along a connection, or a `lasso` round every shape that a class or a glob gives it to. Leave `shape` out and D2 picks the mark that suits the target. An annotation lasts for the board it is declared on, so each step points at something new. `annotation.style.stroke` is the pen: any color, for the mark and its note together. `annotation.style.font-color` colors the note alone. On a board's root, `annotation` is the caption, and a Markdown link to a shape (`[the replica](db.r2)`) marks that shape in the caption's pen. With `animate-duration`, marks are drawn and erased by hand, in the order the caption names them, and each step is held for as long as its text takes to read.
- **Animation review.** `d2 storyboard film.d2 board.png` puts timestamped frames from every board in one PNG. `d2 lint --motion film.d2` points to source lines for crowded transitions, short-lived labels, hidden caption targets, and possible shape ID replacements. Both are available to MCP clients as `storyboard` and `lint_motion`.

- style: add `style.fill-pattern.color` to customize the color of `dots` and `lines` fill patterns. [#2639](https://github.com/d2lang/d2/issues/2639)

#### Improvements 🧹

- SVG exports use 28.4% fewer bytes across the 157 documentation diagrams, or 35.1% fewer with gzip. D2 embeds smaller bundled fonts and writes more compact stylesheets, code blocks, arrowhead references, textures, sketch paths, and LaTeX glyphs.
- SVG appendices use precompressed WOFF2 fonts for ASCII footnotes and numbered markers while retaining their kerning, ligatures, and hinting.

#### Bugfixes ⛑️

- Labels that mention a fill pattern class such as `paper-overlay` no longer embed an unused texture in ordinary SVG exports.
- **GIF.** The ceiling on an animation's total frame area rose from 160 to 256 Mi-pixels, enough for a narrated explainer of about twenty seconds at 700 by 600.
- Animated SVG connections with crossing gaps render more smoothly on large diagrams. [#3091](https://github.com/d2lang/d2/issues/3091)

<!-- When this release changes D2.js, add a “#### D2.js <npm version>” section here. -->
