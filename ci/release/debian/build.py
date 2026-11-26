#!/usr/bin/env python3
"""Build a Debian package from a checksummed native D2 Linux release archive."""

import argparse
from datetime import datetime, timezone
from email.utils import format_datetime
import gzip
import hashlib
import io
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile


RELEASE_TAG = re.compile(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?")
MAINTAINER = "D2 maintainers <hi@d2lang.com>"
REPOSITORY = "https://github.com/d2lang/d2"
PAYLOAD = {
    "bin/d2": "usr/bin/d2",
    "man/d2.1": "usr/share/man/man1/d2.1.gz",
    "LICENSE.txt": "usr/share/doc/d2/copyright",
    "THIRD_PARTY_NOTICES.txt": "usr/share/doc/d2/THIRD_PARTY_NOTICES.txt.gz",
}


def write_gzip(path, contents):
    with path.open("wb") as raw, gzip.GzipFile(filename="", fileobj=raw, mode="wb", mtime=0) as compressed:
        compressed.write(contents)


def stage_payload(version, arch, archives, root):
    archive = archives / f"d2-{version}-linux-{arch}.tar.gz"
    data = archive.read_bytes()
    manifest = (archives / "SHA256SUMS").read_text().splitlines()
    matches = [line for line in manifest if line.endswith(f"  {archive.name}")]
    if matches != [f"{hashlib.sha256(data).hexdigest()}  {archive.name}"]:
        raise ValueError(f"missing, duplicate, or incorrect SHA-256 for {archive.name}")

    # Read selected members as data so archive paths and links never control
    # the destination tree. Use the binary's release timestamp for the .deb.
    members = {f"d2-{version}/{name}": output for name, output in PAYLOAD.items()}
    seen = set()
    with tarfile.open(fileobj=io.BytesIO(data), mode="r|gz") as tar:
        for member in tar:
            if member.name not in members:
                continue
            if member.name in seen or not member.isfile():
                raise ValueError(f"expected one regular archive member: {member.name}")
            seen.add(member.name)
            output = root / members[member.name]
            output.parent.mkdir(parents=True, exist_ok=True)
            with tar.extractfile(member) as stream:
                contents = stream.read()
            if output.suffix == ".gz":
                write_gzip(output, contents)
            elif output.name == "copyright":
                output.write_bytes(f"Upstream source: {REPOSITORY}/tree/{version}\n\n".encode() + contents)
            else:
                output.write_bytes(contents)
            if members[member.name] == "usr/bin/d2":
                epoch = int(member.mtime)
    missing = members.keys() - seen
    if missing:
        raise ValueError(f"missing archive members: {', '.join(sorted(missing))}")
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
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("version")
    parser.add_argument("arch", choices=("amd64", "arm64"))
    parser.add_argument("archives", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    if not RELEASE_TAG.fullmatch(args.version):
        parser.error("version must be a v-prefixed release tag such as v1.2.3 or v1.2.3-rc.1")
    build(args.version, args.arch, args.archives, args.output)


if __name__ == "__main__":
    main()
