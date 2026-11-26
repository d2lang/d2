import io
import json
from email import message_from_bytes
from pathlib import Path
import struct
import subprocess
import sys
import tarfile
import tempfile
import unittest
import zipfile


RELEASE = Path(__file__).resolve().parent


def write_test_archives(root, tag):
    # A single LC_BUILD_VERSION with deployment target macOS 12.
    binary = bytearray(56)
    struct.pack_into("<I", binary, 16, 1)
    struct.pack_into("<IIII", binary, 32, 0x32, 24, 1, 12 << 16)
    for system in ("linux", "macos", "windows"):
        for arch in ("amd64", "arm64"):
            exe = "d2.exe" if system == "windows" else "d2"
            files = {f"bin/{exe}": binary, "LICENSE.txt": b"license", "THIRD_PARTY_NOTICES.txt": b"notices"}
            with tarfile.open(root / f"d2-{tag}-{system}-{arch}.tar.gz", "w:gz") as archive:
                for name, data in files.items():
                    member = tarfile.TarInfo(f"d2-{tag}/{name}")
                    member.size = len(data)
                    archive.addfile(member, io.BytesIO(data))


class WheelTest(unittest.TestCase):
    def test_mcp_listing_and_ownership(self):
        template = json.loads((RELEASE.parents[1] / "server.json").read_text())
        self.assertEqual(template["version"], template["packages"][0]["version"])
        for version in ("0.9.9", "0.10.0", "1.2.3"):
            with self.subTest(version=version), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                tag = f"v{version}"
                write_test_archives(root, tag)
                output = root / "dist"
                subprocess.run(
                    [sys.executable, RELEASE / "build-wheels.py", "--version", tag, "--archives", root, "--output", output],
                    check=True, stdout=subprocess.DEVNULL,
                )
                listing = output / "server.json"
                has_mcp = version != "0.9.9"
                self.assertEqual(listing.exists(), has_mcp)
                if has_mcp:
                    manifest = json.loads(listing.read_text())
                    package, = manifest["packages"]
                    self.assertEqual(manifest["version"], version)
                    self.assertEqual(package["version"], version)
                    # Let uvx use its default /simple index when a client constructs the command.
                    self.assertNotIn("registryBaseUrl", package)
                    self.assertEqual(package["transport"], {"type": "stdio"})
                    self.assertEqual(package["packageArguments"], [{"type": "positional", "value": "mcp"}])

                wheels = list(output.glob("*.whl"))
                self.assertEqual(len(wheels), 6)
                for path in wheels:
                    with zipfile.ZipFile(path) as wheel:
                        metadata = message_from_bytes(wheel.read(f"d2lang-{version}.dist-info/METADATA"))
                    self.assertEqual(metadata["Version"], version)
                    if has_mcp:
                        self.assertEqual(metadata["Name"], package["identifier"])
                        self.assertIn(f"<!-- mcp-name: {manifest['name']} -->", metadata.get_payload().splitlines())
                    else:
                        self.assertNotIn("mcp-name:", metadata.get_payload())


if __name__ == "__main__":
    unittest.main()
