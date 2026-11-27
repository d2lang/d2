"""Read the verified D2 Linux archive used to build native packages."""

import argparse
import gzip
import hashlib
import io
from pathlib import Path
import re
import tarfile


RELEASE_TAG = re.compile(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?")
MEMBERS = {"bin/d2", "man/d2.1", "LICENSE.txt", "THIRD_PARTY_NOTICES.txt"}
RPM_ARCH = {"amd64": "x86_64", "arm64": "aarch64"}


def rpm_version_release(tag):
    version, _, prerelease = tag[1:].partition("-")
    # A 0-prefixed RPM release sorts before the stable package's release 1.
    release = "0." + prerelease.replace("-", ".") if prerelease else "1"
    return version, release


def parse_args(description):
    parser = argparse.ArgumentParser(description=description)
    parser.add_argument("version")
    parser.add_argument("arch", choices=("amd64", "arm64"))
    parser.add_argument("archives", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    if not RELEASE_TAG.fullmatch(args.version):
        parser.error("version must be a v-prefixed release tag such as v1.2.3 or v1.2.3-rc.1")
    return args


def write_gzip(path, contents):
    with path.open("wb") as raw, gzip.GzipFile(filename="", fileobj=raw, mode="wb", mtime=0) as compressed:
        compressed.write(contents)


def read_payload(version, arch, archives):
    archive = archives / f"d2-{version}-linux-{arch}.tar.gz"
    data = archive.read_bytes()
    manifest = (archives / "SHA256SUMS").read_text().splitlines()
    matches = [line for line in manifest if line.endswith(f"  {archive.name}")]
    if matches != [f"{hashlib.sha256(data).hexdigest()}  {archive.name}"]:
        raise ValueError(f"missing, duplicate, or incorrect SHA-256 for {archive.name}")

    # Read only regular files as data. No archive-controlled paths or links
    # reach the filesystem. The binary timestamp is the release build epoch.
    prefix = f"d2-{version}/"
    files = {}
    with tarfile.open(fileobj=io.BytesIO(data), mode="r|gz") as tar:
        for member in tar:
            if not member.name.startswith(prefix):
                continue
            name = member.name[len(prefix):]
            if name not in MEMBERS:
                continue
            if name in files or not member.isfile():
                raise ValueError(f"expected one regular archive member: {member.name}")
            with tar.extractfile(member) as stream:
                files[name] = stream.read()
            if name == "bin/d2":
                epoch = int(member.mtime)
    missing = MEMBERS - files.keys()
    if missing:
        raise ValueError(f"missing archive members: {', '.join(sorted(missing))}")
    return files, epoch
