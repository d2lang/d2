#!/usr/bin/env python3
"""Promote a D2 release's attested SHA256SUMS into the standalone installer catalog."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile


REPO = "d2lang/d2"
REPO_ID = "533087958"
WORKFLOW = f"https://github.com/{REPO}/.github/workflows/release-archives.yml"
VERSION = re.compile(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?")
TARGETS = ("linux-amd64", "linux-arm64", "macos-amd64", "macos-arm64", "windows-amd64", "windows-arm64")


def gh(*args):
    return subprocess.check_output(["gh", *args], env={**os.environ, "GH_HOST": "github.com"}, text=True)


def tag_commit(version):
    commit = gh("api", f"repos/{REPO}/commits/refs/tags/{version}", "--jq", ".sha").strip()
    if not re.fullmatch(r"[a-f0-9]{40}", commit):
        raise ValueError("release tag has no commit")
    return commit


def archive_checksums(version, data):
    # The release builder sorts by filename. The final LF and exact bytes are signed.
    names = [f"d2-{version}-{target}.tar.gz" for target in TARGETS]
    lines = data.decode("ascii").splitlines(keepends=True)
    if len(lines) != len(names):
        raise ValueError("SHA256SUMS must cover exactly six archives")
    checksums = {}
    for name, line in zip(names, lines):
        match = re.fullmatch(r"([0-9a-f]{64})  " + re.escape(name) + r"\n", line)
        if not match:
            raise ValueError(f"expected canonical SHA256SUMS entry for {name}")
        checksums[name] = "sha256:" + match[1]
    return checksums


def release_snapshot(version, names):
    release = json.loads(gh("release", "view", version, "--repo", REPO, "--json", "databaseId,tagName,assets"))
    if release["tagName"] != version:
        raise ValueError("release tag does not match the requested version")
    assets = {}
    for asset in release["assets"]:
        name = asset["name"]
        if name in names:
            if name in assets or asset["state"] != "uploaded" or not re.fullmatch(r"sha256:[0-9a-f]{64}", asset["digest"] or ""):
                raise ValueError(f"duplicate or incomplete release asset: {name}")
            assets[name] = (asset["id"], asset["digest"])
    if assets.keys() != names:
        raise ValueError("release is missing required archives or SHA256SUMS")
    return release["databaseId"], assets


def verify_manifest(manifest, version, commit, digest):
    ref = f"refs/tags/{version}"
    results = json.loads(gh(
        "attestation", "verify", str(manifest), "--repo", REPO,
        "--cert-identity", f"{WORKFLOW}@{ref}", "--source-ref", ref, "--source-digest", commit,
        "--predicate-type", "https://slsa.dev/provenance/v1", "--format", "json",
    ))
    for result in results:
        verified = result["verificationResult"]
        if verified["signature"]["certificate"].get("sourceRepositoryIdentifier") == REPO_ID and any(
            subject.get("name") == "SHA256SUMS" and subject.get("digest", {}).get("sha256") == digest
            for subject in verified["statement"]["subject"]
        ):
            return
    raise ValueError("no verified SHA256SUMS subject from the D2 release repository")


def promote(version, check, directory):
    if not VERSION.fullmatch(version):
        raise ValueError("expected a v-prefixed release version")
    cli_version = re.match(r"gh version (\d+)\.(\d+)\.(\d+)\s", gh("--version"))
    if not cli_version or tuple(map(int, cli_version.groups())) < (2, 102, 0):
        raise ValueError("gh 2.102.0 or later is required for exact attestation identity matching")
    commit = tag_commit(version)
    target = directory / f"{version}.txt"
    if target.is_symlink() or (check and not target.is_file()):
        raise ValueError(f"catalog entry must be a regular file: {target}")
    with tempfile.TemporaryDirectory() as tmp:
        manifest = Path(tmp) / "SHA256SUMS"
        if check:
            # PR checks only need read access to the public tag and its attestations.
            manifest.write_bytes(target.read_bytes())
        else:
            names = {f"d2-{version}-{platform}.tar.gz" for platform in TARGETS} | {"SHA256SUMS"}
            snapshot = release_snapshot(version, names)
            gh("release", "download", version, "--repo", REPO, "--pattern", "SHA256SUMS", "--output", str(manifest))
        data = manifest.read_bytes()
        checksums = archive_checksums(version, data)
        digest = hashlib.sha256(data).hexdigest()
        verify_manifest(manifest, version, commit, digest)
        if not check:
            checksums["SHA256SUMS"] = "sha256:" + digest
            if any(snapshot[1][name][1] != checksum for name, checksum in checksums.items()):
                raise ValueError("release asset digest differs from signed SHA256SUMS")
            if release_snapshot(version, names) != snapshot:
                raise ValueError("release assets changed during promotion")
        if tag_commit(version) != commit:
            raise ValueError("release tag changed during verification")
    if check:
        return
    # A hard link publishes the entire verified file atomically without replacing an entry.
    directory.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(dir=directory) as tmp:
        staged = Path(tmp) / "SHA256SUMS"
        staged.write_bytes(data)
        try:
            os.link(staged, target)
        except FileExistsError:
            if target.is_symlink() or not target.is_file() or target.read_bytes() != data:
                raise ValueError(f"refusing to replace existing catalog entry: {target}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True)
    parser.add_argument("--check", action="store_true", help="verify the committed entry without writing")
    args = parser.parse_args()
    try:
        promote(args.version, args.check, Path(__file__).resolve().with_name("checksums"))
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        sys.exit(str(error))
    print(f"Verified release checksums for {args.version}")


if __name__ == "__main__":
    main()
