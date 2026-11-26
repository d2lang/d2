"""Select CI jobs from the complete Git diff and check their results."""

import json
import os
import re
import subprocess
import sys


def select_jobs(event, tag=False, paths=(), toolchain_changed=False):
    jobs = dict.fromkeys(("archives", "browser", "docker", "fuzz", "wasm", "windows-installer"), False)
    if event == "push" and tag:
        # The release workflow already builds the full archives. WASM includes the browser smoke.
        jobs["wasm"] = True
        return jobs
    if event == "workflow_dispatch":
        jobs.update(archives="full", docker=True, fuzz=True, wasm=True)
        jobs["windows-installer"] = True
        return jobs

    for path in paths:
        installer = path in ("install.sh", ".github/workflows/windows-installer.yml")
        if installer:
            jobs["windows-installer"] = True
        if event != "pull_request":
            continue

        ci_policy = path in (".github/workflows/ci.yml", "ci/checks.py", "ci/checks_test.py")
        if ci_policy:
            jobs["docker"] = jobs["fuzz"] = jobs["windows-installer"] = True

        if path.endswith(".md"):
            continue
        packaging = path.startswith("ci/release/") and not path.startswith("ci/release/changelogs/")
        platform_source = re.search(r"_(?:windows|linux|darwin|unix|portable|amd64|arm64)(?:_[a-z0-9]+)*\.(?:go|s)$", path)
        if ci_policy or packaging or installer or platform_source or path in (
            ".github/workflows/release-archives.yml", ".github/workflows/archive-checks.yml",
            ".github/workflows/windows-msi.yml", "ci/sub", ".gitmodules", "LICENSE.txt", "THIRD_PARTY_NOTICES.txt",
        ):
            jobs["archives"] = "full"
        elif jobs["archives"] != "full" and (path.startswith("d2layouts/") or path in ("go.mod", "go.sum")):
            jobs["archives"] = "size"

        if path.startswith("ci/release/docker/") or path in (
            ".github/workflows/docker-image-smoke.yml", ".github/workflows/docker-publish.yml",
        ):
            jobs["docker"] = True
        if path.startswith("d2layouts/d2talalayout/") or path in (".github/workflows/tala-fuzz.yml", "go.mod", "go.sum"):
            jobs["fuzz"] = True

        wasm_specific = re.search(r"(?:^|[/_])(?:wasm|tinygo)(?:[_.]|$)", path)
        wasm_test = path.endswith("_test.go") and (
            path.startswith("d2js/d2wasm/") or (path.startswith("d2layouts/d2elklayout/") and wasm_specific)
        )
        if ci_policy or wasm_test or path in (".github/workflows/wasm.yml", "ci/test-wasm.sh"):
            jobs["wasm"] = True
        if path.startswith(("d2js/", ".github/actions/browser-smoke/")) or path in (
            ".github/workflows/browser-smoke.yml", ".github/workflows/npm-stage.yml", "ci/sub", ".gitmodules",
            "ci/build-wasm.sh", "ci/install-wasm-tools.sh", "go.mod", "go.sum",
        ) or wasm_specific:
            jobs["browser"] = "packed"
        elif jobs["browser"] != "packed" and path.startswith((
            "d2compiler/", "d2ir/", "d2lib/", "d2renderers/d2svg/", "d2renderers/d2animate/", "d2cli/", "d2target/",
            "d2renderers/internal/svgframe/", "d2layouts/d2sequence/",
            "d2layouts/d2talalayout/board_", "d2layouts/d2talalayout/step_",
        )):
            jobs["browser"] = "native"
    if event == "pull_request" and toolchain_changed:
        jobs["archives"] = "full"
        jobs["wasm"] = True
    if jobs["wasm"]:
        # The full WASM suite already runs the packed browser smoke.
        jobs["browser"] = False
    return jobs


def go_toolchain(revision):
    if not subprocess.check_output(["git", "ls-tree", "--name-only", revision, "--", "go.mod"]):
        return None
    manifest = subprocess.check_output(["git", "show", f"{revision}:go.mod"], text=True)
    return [line.split("//", 1)[0].split() for line in manifest.splitlines()
            if line.split()[:1] in (["go"], ["toolchain"])]


def plan():
    event = os.environ["GITHUB_EVENT_NAME"]
    tag = os.environ["GITHUB_REF_TYPE"] == "tag"
    paths = []
    toolchain_changed = False
    if not tag and event in ("push", "pull_request"):
        base = os.environ["BASE_SHA"]
        if base == "0" * 40:
            files = subprocess.check_output(["git", "ls-files", "-z"])
            toolchain_changed = True
        else:
            # A push can span more commits than the shallow checkout contains.
            if subprocess.run(["git", "cat-file", "-e", f"{base}^{{commit}}"], stderr=subprocess.DEVNULL).returncode:
                subprocess.run(["git", "fetch", "--no-tags", "--depth=1", "origin", base], check=True)
            # Renames must select tests for both the old and new paths.
            files = subprocess.check_output(["git", "diff", "--no-renames", "--name-only", "-z", base, "HEAD"])
        paths = list(map(os.fsdecode, filter(None, files.split(b"\0"))))
        if event == "pull_request" and "go.mod" in paths and not toolchain_changed:
            toolchain_changed = go_toolchain(base) != go_toolchain("HEAD")
    jobs = json.dumps(select_jobs(event, tag, paths, toolchain_changed))
    print(jobs)
    with open(os.environ["GITHUB_OUTPUT"], "a") as output:
        print(f"jobs={jobs}", file=output)


def verify(needs):
    selection = needs["select"]
    if selection["result"] != "success":
        raise ValueError(f"Job selection ended with {selection['result']}")
    jobs = json.loads(selection["outputs"]["jobs"])
    expected = dict.fromkeys(("select", "native", "signed", "windows-imports"), "success")
    expected.update((job, "success" if mode else "skipped") for job, mode in jobs.items())
    if set(expected) != set(needs):
        raise ValueError(f"CI dependency list differs from plan: {sorted(set(expected) ^ set(needs))}")
    errors = [f"{job}: expected {result}, got {needs[job]['result']}"
              for job, result in expected.items() if needs[job]["result"] != result]
    if errors:
        raise ValueError("\n".join(errors))


if __name__ == "__main__":
    if sys.argv[1:] == ["plan"]:
        plan()
    elif sys.argv[1:] == ["verify"]:
        verify(json.loads(os.environ["NEEDS"]))
    else:
        sys.exit("usage: checks.py plan|verify")
