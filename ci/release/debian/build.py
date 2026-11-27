#!/usr/bin/env python3
"""Build a Debian package from a checksummed native D2 Linux release archive."""

from datetime import datetime, timezone
from email.utils import format_datetime
import os
from pathlib import Path
import subprocess
import sys
import tempfile

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from linux_package import parse_args, read_payload, write_gzip


MAINTAINER = "D2 maintainers <hi@d2lang.com>"
REPOSITORY = "https://github.com/d2lang/d2"
PAYLOAD = {
    "bin/d2": "usr/bin/d2",
    "man/d2.1": "usr/share/man/man1/d2.1.gz",
    "LICENSE.txt": "usr/share/doc/d2/copyright",
    "THIRD_PARTY_NOTICES.txt": "usr/share/doc/d2/THIRD_PARTY_NOTICES.txt.gz",
}


def stage_payload(version, arch, archives, root):
    files, epoch = read_payload(version, arch, archives)
    for name, destination in PAYLOAD.items():
        output = root / destination
        output.parent.mkdir(parents=True, exist_ok=True)
        contents = files[name]
        if output.suffix == ".gz":
            write_gzip(output, contents)
        elif output.name == "copyright":
            output.write_bytes(f"Upstream source: {REPOSITORY}/tree/{version}\n\n".encode() + contents)
        else:
            output.write_bytes(contents)
    return epoch


def build(version, arch, archives, output):
    upstream = version[1:]
    deb_version = upstream.replace("-", "~", 1)
    output.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(dir=output) as temporary:
        stage = Path(temporary)
        root = stage / "root"
        epoch = stage_payload(version, arch, archives, root)
        date = format_datetime(datetime.fromtimestamp(epoch, timezone.utc))
        changelog = (
            f"d2 ({deb_version}) upstream; urgency=medium\n\n"
            f"  * Upstream release {version}.\n"
            f"    Release notes: {REPOSITORY}/releases/tag/{version}\n\n"
            f" -- {MAINTAINER}  {date}\n"
        )
        write_gzip(root / "usr/share/doc/d2/changelog.gz", changelog.encode())
        size = sum((p.stat().st_size + 1023) // 1024 if p.is_file() else 1 for p in root.rglob("*"))
        control = root / "DEBIAN" / "control"
        control.parent.mkdir()
        control.write_text(
            f"Package: d2\nVersion: {deb_version}\nArchitecture: {arch}\n"
            f"Maintainer: {MAINTAINER}\n"
            f"Section: graphics\nPriority: optional\nInstalled-Size: {size}\n"
            "Homepage: https://d2lang.com/\n"
            "Description: declarative diagram scripting language\n"
            " D2 turns text descriptions into diagrams, including SVG images.\n"
        )
        for path in [root, *root.rglob("*")]:
            path.chmod(0o755 if path.is_dir() or path == root / "usr/bin/d2" else 0o644)
            os.utime(path, (epoch, epoch))
        # Build outside the final path so a failed or concurrent retry never
        # leaves a partially written release asset.
        filename = f"d2_{upstream}_{arch}.deb"
        package = stage / filename
        subprocess.run(
            ["dpkg-deb", "--root-owner-group", "-Zxz", "-z6", "--threads-max=1", "--build", root, package],
            env={**os.environ, "SOURCE_DATE_EPOCH": str(epoch)},
            check=True,
        )
        package.replace(output / filename)


def main():
    args = parse_args(__doc__)
    build(args.version, args.arch, args.archives, args.output)


if __name__ == "__main__":
    main()
