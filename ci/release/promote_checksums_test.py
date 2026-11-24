import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("promotion", Path(__file__).with_name("promote-checksums.py"))
promotion = importlib.util.module_from_spec(spec)
spec.loader.exec_module(promotion)


class PromotionTests(unittest.TestCase):
    version = "v1.2.3-rc.1"
    commit = "a" * 40

    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.directory = Path(temp.name)
        self.target = self.directory / f"{self.version}.txt"
        names = [f"d2-{self.version}-{platform}.tar.gz" for platform in promotion.TARGETS]
        hashes = {name: hashlib.sha256(name.encode()).hexdigest() for name in names}
        self.data = "".join(f"{digest}  {name}\n" for name, digest in hashes.items()).encode()
        self.digest = hashes["SHA256SUMS"] = hashlib.sha256(self.data).hexdigest()
        self.release = {"databaseId": 42, "tagName": self.version, "assets": [
            {"id": str(index), "state": "uploaded", "name": name, "digest": "sha256:" + digest}
            for index, (name, digest) in enumerate(hashes.items())
        ]}
        self.statement = {"subject": [{"name": "SHA256SUMS", "digest": {"sha256": self.digest}}]}
        self.certificate = {"sourceRepositoryIdentifier": "533087958"}
        self.calls = []
        mock = patch.object(promotion, "gh", self.gh)
        mock.start()
        self.addCleanup(mock.stop)

    def gh(self, *args):
        self.calls.append(args)
        if args == ("--version",):
            return "gh version 2.102.0 (test)\n"
        if args[0] == "api":
            self.assertEqual(args, ("api", f"repos/d2lang/d2/commits/refs/tags/{self.version}", "--jq", ".sha"))
            return self.commit
        if args[:2] == ("release", "view"):
            self.assertEqual(args[2:5], (self.version, "--repo", "d2lang/d2"))
            return json.dumps(self.release)
        if args[:2] == ("release", "download"):
            self.assertEqual(args[2:-1], (self.version, "--repo", "d2lang/d2", "--pattern", "SHA256SUMS", "--output"))
            Path(args[-1]).write_bytes(self.data)
            return ""
        if args[:2] == ("attestation", "verify"):
            manifest = Path(args[2])
            self.assertEqual(manifest.name, "SHA256SUMS")
            self.assertEqual(manifest.read_bytes(), self.data)
            self.assertEqual(args[3:], (
                "--repo", "d2lang/d2", "--cert-identity",
                f"https://github.com/d2lang/d2/.github/workflows/release-archives.yml@refs/tags/{self.version}",
                "--source-ref", f"refs/tags/{self.version}", "--source-digest", self.commit,
                "--predicate-type", "https://slsa.dev/provenance/v1", "--format", "json",
            ))
            return json.dumps([{"verificationResult": {
                "signature": {"certificate": self.certificate}, "statement": self.statement,
            }}])
        self.fail(args)

    def promote(self, check=False):
        promotion.promote(self.version, check, self.directory)

    def test_promote_retry_and_check(self):
        self.promote()
        self.assertEqual(self.target.read_bytes(), self.data)
        stat = self.target.stat()
        self.promote()
        self.calls.clear()
        self.promote(check=True)
        self.assertFalse(any(args[0] == "release" for args in self.calls))
        self.assertEqual(self.target.stat().st_mtime_ns, stat.st_mtime_ns)

    def test_catalog_is_append_only(self):
        self.target.write_bytes(b"existing entry")
        with self.assertRaisesRegex(ValueError, "refusing to replace"):
            self.promote()
        self.assertEqual(self.target.read_bytes(), b"existing entry")

    def test_missing_or_wrong_signed_subject_or_identity(self):
        for field, bad in (("repo", "42"), ("name", "other"), ("sha256", "b" * 64)):
            with self.subTest(field=field):
                certificate = copy.deepcopy(self.certificate)
                subject = copy.deepcopy(self.statement)
                if field == "repo":
                    certificate["sourceRepositoryIdentifier"] = bad
                elif field == "name":
                    subject["subject"][0]["name"] = bad
                else:
                    subject["subject"][0]["digest"]["sha256"] = bad
                with patch.object(self, "certificate", certificate), patch.object(self, "statement", subject):
                    with self.assertRaisesRegex(ValueError, "no verified SHA256SUMS"):
                        self.promote()
                self.assertFalse(self.target.exists())

    def test_release_digests_must_match_archive_and_manifest(self):
        for asset in (self.release["assets"][0], self.release["assets"][-1]):
            with self.subTest(asset=asset["name"]), patch.dict(asset, digest="sha256:" + "0" * 64):
                with self.assertRaisesRegex(ValueError, "digest differs"):
                    self.promote()
                self.assertFalse(self.target.exists())

    def test_moving_release_or_tag_aborts_promotion(self):
        with patch.object(promotion, "tag_commit", side_effect=[self.commit, "b" * 40]):
            with self.assertRaisesRegex(ValueError, "tag changed"):
                self.promote()
        names = {asset["name"] for asset in self.release["assets"]}
        release_id, assets = promotion.release_snapshot(self.version, names)
        with patch.object(promotion, "release_snapshot", side_effect=[(release_id, assets), (release_id + 1, assets)]):
            with self.assertRaisesRegex(ValueError, "assets changed"):
                self.promote()
        self.assertFalse(self.target.exists())

    def test_rejects_noncanonical_bytes_even_if_attested(self):
        lines = self.data.splitlines(keepends=True)
        for data in (self.data.rstrip(b"\n"), self.data.replace(b"\n", b"\r\n"), b"".join(reversed(lines)),
                     b"".join(lines[:-1] + lines[:1]), self.data.replace(b"windows", b".."), self.data + lines[0]):
            with self.subTest(data=data):
                self.target.write_bytes(data)
                with self.assertRaisesRegex(ValueError, "SHA256SUMS"):
                    self.promote(check=True)
                self.assertEqual(self.target.read_bytes(), data)

    def test_rejects_old_verifier_and_unsafe_version(self):
        with patch.object(promotion, "gh", return_value="gh version 2.101.99 (test)"):
            with self.assertRaisesRegex(ValueError, "2.102.0 or later"):
                self.promote()
        with self.assertRaisesRegex(ValueError, "release version"):
            promotion.promote("../test", True, self.directory)
        self.assertFalse(self.target.exists())


if __name__ == "__main__":
    unittest.main()
