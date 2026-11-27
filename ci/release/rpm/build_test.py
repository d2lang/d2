import gzip
import hashlib
import io
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest


BUILDER = Path(__file__).with_name("build.py")
EPOCH = 1234567890
FILES = {
    "bin/d2": b"D2 binary fixture\x00\xff",
    "man/d2.1": b".TH d2 1\n",
    "LICENSE.txt": b"D2 license\n",
    "THIRD_PARTY_NOTICES.txt": b"Third-party notices\n",
}


class RPMPackageTest(unittest.TestCase):
    def run_build(self, command, **kwargs):
        result = subprocess.run(command, capture_output=True, text=True, **kwargs)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_release_packages(self):
        for tag, arch, release, target in [
            ("v1.2.3", "amd64", "1", "x86_64"),
            ("v1.2.3-rc-test.1", "arm64", "0.rc.test.1", "aarch64"),
        ]:
            with self.subTest(tag=tag), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                archive = root / f"d2-{tag}-linux-{arch}.tar.gz"
                with tarfile.open(archive, "w:gz") as tar:
                    for name, data in FILES.items():
                        member = tarfile.TarInfo(f"d2-{tag}/{name}")
                        member.size = len(data)
                        member.mtime = EPOCH
                        tar.addfile(member, io.BytesIO(data))
                manifest = root / "SHA256SUMS"
                manifest.write_text(f"{hashlib.sha256(archive.read_bytes()).hexdigest()}  {archive.name}\n")
                output = root / "release assets"
                command = [sys.executable, BUILDER, tag, arch, root, output]
                self.run_build(command)
                package = output / f"d2-1.2.3-{release}.{target}.rpm"
                query = ["rpm", "-qp"]
                fields = "%{NAME} %{VERSION} %{RELEASE} %{ARCH} %{BUILDTIME}"
                metadata = subprocess.check_output([*query, "--queryformat", fields, package], text=True)
                self.assertEqual(metadata, f"d2 1.2.3 {release} {target} {EPOCH}")
                self.assertEqual(subprocess.check_output([*query, "--scripts", package]), b"")
                requires = subprocess.check_output([*query, "--requires", package], text=True)
                self.assertTrue(all(dep.startswith("rpmlib(") for dep in requires.splitlines()), requires)
                file_fields = "[%{FILENAMES} %{FILEUSERNAME} %{FILEGROUPNAME} %{FILEMTIMES} %{FILEMODES:perms}\n]"
                files = subprocess.check_output([*query, "--queryformat", file_fields, package], text=True)
                licenses = "/usr/share/licenses/d2"
                expected = {
                    "/usr/bin/d2": "-rwxr-xr-x",
                    "/usr/share/man/man1/d2.1.gz": "-rw-r--r--",
                    licenses: "drwxr-xr-x",
                    f"{licenses}/LICENSE.txt": "-rw-r--r--",
                    f"{licenses}/THIRD_PARTY_NOTICES.txt": "-rw-r--r--",
                }
                entries = [f"{name} root root {EPOCH} {mode}" for name, mode in expected.items()]
                self.assertCountEqual(files.splitlines(), entries)
                license_files = subprocess.check_output([*query, "--licensefiles", package], text=True)
                self.assertCountEqual(license_files.splitlines(), [f"{licenses}/LICENSE.txt", f"{licenses}/THIRD_PARTY_NOTICES.txt"])

                payload = subprocess.check_output(["rpm2cpio", package])
                subprocess.run(["cpio", "-idm", "--quiet"], cwd=root, input=payload, check=True)
                self.assertEqual((root / "usr/bin/d2").read_bytes(), FILES["bin/d2"])
                self.assertEqual(gzip.decompress((root / "usr/share/man/man1/d2.1.gz").read_bytes()), FILES["man/d2.1"])
                for name in ("LICENSE.txt", "THIRD_PARTY_NOTICES.txt"):
                    self.assertEqual((root / "usr/share/licenses/d2" / name).read_bytes(), FILES[name])

                before = package.read_bytes()
                self.run_build(command, umask=0o077)
                self.assertEqual(before, package.read_bytes())
                manifest.write_text("")
                result = subprocess.run(command, capture_output=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(b"SHA-256", result.stderr)
                self.assertEqual(before, package.read_bytes())


if __name__ == "__main__":
    unittest.main()
