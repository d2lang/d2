#!/usr/bin/env python3
"""Regenerate the bundled appendix faces with FontTools 4.69.1 and Brotli 1.2.1."""
from copy import deepcopy
from hashlib import sha256
import os
from pathlib import Path

from fontTools import subset
from fontTools.ttLib import TTFont


def build(style):
    font = TTFont(f"ttf/SourceSansPro-{style}.ttf", recalcTimestamp=False, recalcBBoxes=False)
    original_cmap = deepcopy(font["cmap"])
    options = subset.Options()
    options.layout_features = ["*"]
    options.name_IDs = ["*"]
    options.name_languages = ["*"]
    options.glyph_names = True
    options.notdef_outline = True
    subsetter = subset.Subsetter(options=options)
    # FreeType uses alphabetic glyphs as autohint references for digits too.
    subsetter.populate(unicodes=range(0x20, 0x7F))
    subsetter.subset(font)

    # A retained glyph can also be encoded outside ASCII, such as the ff
    # ligature at U+FB00. FreeType uses these mappings when classifying glyphs
    # for autohinting. Preserve every original mapping to a retained outline.
    retained = set(font.getGlyphOrder())
    for table in original_cmap.tables:
        table.cmap = {code: name for code, name in table.cmap.items() if name in retained}
    font["cmap"] = original_cmap

    # Source is a reserved name. Keep the original copyright, license,
    # trademark and design credits, and name the two derived faces separately.
    family = "D2 Appendix"
    postscript = f"D2Appendix-{style}"
    names = {1: family, 3: f"{postscript}-1", 4: f"{family} {style}", 6: postscript}
    for record in font["name"].names:
        if record.nameID in names:
            record.string = names[record.nameID].encode(record.getEncoding())
    font.flavor = "woff2"
    path = Path(f"encoded/{postscript}.woff2")
    font.save(path)

    # The Go shaping test reads SFNT. Exercise the face decoded from the
    # shipped WOFF2, including its shaping tables, Unicode aliases and metrics.
    fixture = Path(f"testdata/appendix-{style.lower()}.ttf")
    with TTFont(path, recalcTimestamp=False, recalcBBoxes=False) as decoded:
        decoded.flavor = None
        decoded.save(fixture)
    digest = sha256(path.read_bytes() + fixture.read_bytes()).hexdigest()
    fixture.with_suffix(".sha256").write_text(digest + "\n", encoding="ascii")


if __name__ == "__main__":
    os.chdir(Path(__file__).resolve().parent)
    build("Regular")
    build("Bold")
