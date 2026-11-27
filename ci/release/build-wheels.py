#!/usr/bin/env python3
"""Package the six verified D2 release archives as native d2lang wheels."""

import argparse
import base64
from hashlib import sha256
import json
from pathlib import Path
import re
import struct
import tarfile
import tempfile
import zipfile


LICENSES = ("LICENSE.txt", "THIRD_PARTY_NOTICES.txt")
PACKAGE_NAME = "d2lang"
ARCHITECTURES = {"amd64": "x86_64", "arm64": "aarch64"}


def macos_minimum(binary):
    # The Go linker records the deployment target in the 64-bit Mach-O header.
    offset = 32
    count = struct.unpack_from("<I", binary, 16)[0]
    for _ in range(count):
        command, size = struct.unpack_from("<II", binary, offset)
        if command == 0x32:  # LC_BUILD_VERSION
            platform, version = struct.unpack_from("<II", binary, offset + 8)
            if platform != 1:  # PLATFORM_MACOS
                raise ValueError("D2 was built for an unexpected Apple platform")
            return version >> 16, (version >> 8) & 255
        if command == 0x24:  # LC_VERSION_MIN_MACOSX
            version = struct.unpack_from("<I", binary, offset + 8)[0]
            return version >> 16, (version >> 8) & 255
        offset += size
    raise ValueError("D2 does not declare its minimum macOS version")


def platform_tags(system, arch, binary):
    if system == "macos":
        major, minor = macos_minimum(binary)
        cpu = "arm64" if arch == "arm64" else "x86_64"
        return [f"macosx_{major}_{minor}_{cpu}"]
    if system == "windows":
        return [f"win_{arch}"]
    # Release builds use CGO_ENABLED=0. A static Go binary runs on either libc.
    cpu = ARCHITECTURES[arch]
    return [f"manylinux_2_17_{cpu}", f"musllinux_1_2_{cpu}"]


def read_member(archive, path):
    member = archive.getmember(path)
    if not member.isfile():
        raise ValueError(f"{path} must be a regular release archive member")
    with archive.extractfile(member) as stream:
        return stream.read()


def mcp_manifest(version):
    if tuple(map(int, version.split("."))) < (0, 10, 0):
        return None
    manifest = json.loads((Path(__file__).resolve().parents[2] / "server.json").read_text())
    manifest["version"] = version
    manifest["packages"][0]["version"] = version
    return manifest


def build_wheel(tag, system, arch, archives, output, manifest):
    version = tag[1:]
    base = f"{PACKAGE_NAME}-{version}"
    info = f"{base}.dist-info"
    exe = "d2.exe" if system == "windows" else "d2"
    with tarfile.open(archives / f"d2-{tag}-{system}-{arch}.tar.gz", "r:gz") as archive:
        binary = read_member(archive, f"d2-{tag}/bin/{exe}")
        licenses = {name: read_member(archive, f"d2-{tag}/{name}") for name in LICENSES}

    tags = platform_tags(system, arch, binary)
    filename = f"{base}-py3-none-{'.'.join(tags)}.whl"
    metadata = "\n".join([
        "Metadata-Version: 2.4",
        f"Name: {PACKAGE_NAME}",
        f"Version: {version}",
        "Summary: The official D2 diagram scripting language CLI",
        "Description-Content-Type: text/markdown",
        "Requires-Python: >=3.8",
        "License-Expression: MPL-2.0",
        *(f"License-File: {name}" for name in LICENSES),
        "Project-URL: Homepage, https://d2lang.com/",
        "Project-URL: Source, https://github.com/d2lang/d2",
        "",
        "D2 turns text into diagrams. Render a file with `uvx d2lang input.d2 output.svg`.",
        "Install the native `d2` command with `uv tool install d2lang` or `pip install d2lang`.\n",
    ])
    if manifest:
        metadata += f"\nRun the D2 MCP server with `uvx d2lang mcp`.\n\n<!-- mcp-name: {manifest['name']} -->\n"
    wheel_metadata = "\n".join([
        "Wheel-Version: 1.0",
        "Generator: D2 release",
        "Root-Is-Purelib: false",
        *(f"Tag: py3-none-{platform}" for platform in tags),
        "",
    ])
    files = {
        f"{base}.data/scripts/{exe}": binary,
        "d2lang.py": (Path(__file__).parent / "pypi" / "d2lang.py").read_bytes(),
        f"{info}/METADATA": metadata.encode(),
        f"{info}/WHEEL": wheel_metadata.encode(),
        f"{info}/entry_points.txt": b"[console_scripts]\nd2lang = d2lang:main\n",
    }
    files.update({f"{info}/licenses/{name}": content for name, content in licenses.items()})
    record = []
    for path, data in files.items():
        digest = base64.urlsafe_b64encode(sha256(data).digest()).rstrip(b"=").decode()
        record.append(f"{path},sha256={digest},{len(data)}\n")
    record.append(f"{info}/RECORD,,\n")
    files[f"{info}/RECORD"] = "".join(record).encode()

    with zipfile.ZipFile(output / filename, "w") as wheel:
        for path, data in files.items():
            entry = zipfile.ZipInfo(path)  # Fixed ZIP epoch for reproducible wheels.
            entry.create_system = 3
            entry.external_attr = (0o100755 if path.endswith(f"/scripts/{exe}") else 0o100644) << 16
            wheel.writestr(entry, data, compress_type=zipfile.ZIP_DEFLATED)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True, help="stable D2 release tag, e.g. v0.9.0")
    parser.add_argument("--archives", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    if not re.fullmatch(r"v(0|[1-9][0-9]*)(\.(0|[1-9][0-9]*)){2}", args.version):
        parser.error("--version must be a stable vX.Y.Z release tag")
    args.output.mkdir(parents=True, exist_ok=True)
    manifest = mcp_manifest(args.version[1:])
    with tempfile.TemporaryDirectory(dir=args.output) as temporary:
        staging = Path(temporary)
        for system in ("linux", "macos", "windows"):
            for arch in ARCHITECTURES:
                build_wheel(args.version, system, arch, args.archives, staging, manifest)
        if manifest:
            (staging / "server.json").write_text(json.dumps(manifest, indent=2) + "\n")
        for artifact in sorted(staging.iterdir()):
            print(artifact.replace(args.output / artifact.name))


if __name__ == "__main__":
    main()
