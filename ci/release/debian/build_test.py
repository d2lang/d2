import gzip
import hashlib
import io
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest


BUILDER = Path(__file__).with_name("build.py")
FILES = {
    "bin/d2": b"D2 binary fixture\x00\xff",
    "man/d2.1": b".TH d2 1\n",
    "LICENSE.txt": b"D2 license\n",
    "THIRD_PARTY_NOTICES.txt": b"third-party licenses\n",
}


class DebianPackageTest(unittest.TestCase):
    def setUp(self):
        work = tempfile.TemporaryDirectory()
        self.addCleanup(work.cleanup)
        self.root = Path(work.name)
        self.output = self.root / "out"

    def archive(self, version, arch, *, symlink=False):
        path = self.root / f"d2-{version}-linux-{arch}.tar.gz"
        with tarfile.open(path, "w:gz") as tar:
            for name, data in FILES.items():
                member = tarfile.TarInfo(f"d2-{version}/{name}")
                member.mtime = 1234567890
                if symlink and name == "bin/d2":
                    member.type = tarfile.SYMTYPE
                    member.linkname = "/bin/sh"
                    tar.addfile(member)
                else:
                    member.size = len(data)
                    tar.addfile(member, io.BytesIO(data))
        digest = hashlib.sha256(path.read_bytes()).hexdigest()
        (self.root / "SHA256SUMS").write_text(f"{digest}  {path.name}\n")
        return path

    def build(self, version, arch, **kwargs):
        return subprocess.run([BUILDER, version, arch, self.root, self.output], capture_output=True, **kwargs)

    def test_control_payload_and_reproducibility(self):
        for tag, arch, deb_version in [
            ("v1.2.3", "amd64", "1.2.3"),
            ("v1.2.3-rc-test.1", "arm64", "1.2.3~rc-test.1"),
        ]:
            with self.subTest(tag=tag):
                self.archive(tag, arch)
                self.build(tag, arch, check=True)
                package = self.output / f"d2_{tag[1:]}_{arch}.deb"
                for field, expected in [("Package", "d2"), ("Architecture", arch), ("Version", deb_version)]:
                    self.assertEqual(subprocess.check_output(["dpkg-deb", "-f", package, field], text=True).strip(), expected)
                if "-" in tag:
                    subprocess.run(["dpkg", "--compare-versions", deb_version, "lt", "1.2.3"], check=True)
                payload = subprocess.check_output(["dpkg-deb", "--fsys-tarfile", package])
                with tarfile.open(fileobj=io.BytesIO(payload)) as tar:
                    files = {member.name.removeprefix("./"): member for member in tar if member.isfile()}
                    for name, member in files.items():
                        self.assertEqual((member.uid, member.gid), (0, 0), name)
                        self.assertEqual(member.mode, 0o755 if name == "usr/bin/d2" else 0o644)
                        self.assertEqual(member.mtime, 1234567890)
                    self.assertEqual(tar.extractfile(files["usr/bin/d2"]).read(), FILES["bin/d2"])
                    copyright_file = files["usr/share/doc/d2/copyright"]
                    upstream = f"Upstream source: https://github.com/d2lang/d2/tree/{tag}\n\n"
                    self.assertEqual(tar.extractfile(copyright_file).read(), upstream.encode() + FILES["LICENSE.txt"])
                    self.assertEqual(gzip.decompress(tar.extractfile(files["usr/share/man/man1/d2.1.gz"]).read()), FILES["man/d2.1"])
                    notices = files["usr/share/doc/d2/THIRD_PARTY_NOTICES.txt.gz"]
                    self.assertEqual(gzip.decompress(tar.extractfile(notices).read()), FILES["THIRD_PARTY_NOTICES.txt"])
                    changelog = files["usr/share/doc/d2/changelog.gz"]
                    self.assertEqual(gzip.decompress(tar.extractfile(changelog).read()).decode(), (
                        f"d2 ({deb_version}) upstream; urgency=medium\n\n"
                        f"  * Upstream release {tag}.\n"
                        f"    Release notes: https://github.com/d2lang/d2/releases/tag/{tag}\n\n"
                        " -- D2 maintainers <hi@d2lang.com>  Fri, 13 Feb 2009 23:31:30 +0000\n"
                    ))
                    self.assertEqual(len(files), 5)
                before = package.read_bytes()
                self.build(tag, arch, check=True, umask=0o077)
                self.assertEqual(before, package.read_bytes())

    def test_rejects_mismatched_or_ambiguous_checksum(self):
        version, arch = "v1.2.3", "amd64"
        archive = self.archive(version, arch)
        manifest = self.root / "SHA256SUMS"
        line = manifest.read_text()
        for contents in ["", line + line, f"{'0' * 64}  {archive.name}\n"]:
            with self.subTest(manifest=contents):
                manifest.write_text(contents)
                result = self.build(version, arch)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(b"SHA-256", result.stderr)
                self.assertEqual(list(self.output.glob("*.deb")), [])

    def test_rejects_link_instead_of_binary(self):
        self.archive("v1.2.3", "amd64", symlink=True)
        result = self.build("v1.2.3", "amd64")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(b"regular archive member", result.stderr)
        self.assertEqual(list(self.output.glob("*.deb")), [])


if __name__ == "__main__":
    unittest.main()
