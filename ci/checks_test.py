import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

from checks import select_jobs, verify


class SelectionTests(unittest.TestCase):
    def test_pull_request_paths(self):
        cases = [
            (["README.md", "ci/release/changelogs/next.md", "d2layouts/d2talalayout/README.md"], {}),
            (["d2layouts/d2talalayout/internal/placement/place.go"], {"archives": "size", "fuzz": True}),
            (["d2layouts/d2elklayout/layout.go"], {"archives": "size"}),
            (["go.sum"], {"archives": "size", "fuzz": True, "browser": "packed"}),
            (["d2lib/board_reveal.go"], {"browser": "native"}),
            (["d2renderers/d2animate/d2animate.go"], {"browser": "native"}),
            (["d2renderers/internal/svgframe/frame.go"], {"browser": "native"}),
            (["d2js/js/src/worker.browser.js", "d2lib/board_reveal.go"], {"browser": "packed"}),
            (["ci/install-wasm-tools.sh"], {"browser": "packed"}),
            (["ci/sub"], {"archives": "full", "browser": "packed"}),
            ([".gitmodules"], {"archives": "full", "browser": "packed"}),
            (["ci/test-wasm.sh", "d2lib/d2.go"], {"wasm": True}),
            ([".github/workflows/wasm.yml", "d2js/js/src/worker.browser.js"], {"wasm": True}),
            (["d2js/d2wasm/isometric_test.go"], {"wasm": True}),
            (["d2layouts/d2elklayout/wasm_test.go"], {"archives": "size", "wasm": True}),
            (["install.sh"], {"archives": "full", "windows-installer": True}),
            (["lib/localfile/open_unix.go"], {"archives": "full"}),
            (["ci/release/docker/Dockerfile"], {"archives": "full", "docker": True}),
            ([".github/workflows/windows-msi.yml", "d2layouts/d2dagrelayout/layout.go"], {"archives": "full"}),
            (["ci/checks.py"], {"archives": "full", "wasm": True, "docker": True,
                                "fuzz": True, "windows-installer": True}),
        ]
        for paths, expected in cases:
            for order in [paths, list(reversed(paths))]:
                with self.subTest(paths=order):
                    plan = select_jobs("pull_request", paths=order)
                    self.assertEqual({key: mode for key, mode in plan.items() if mode}, expected)

    def test_non_pr_jobs(self):
        def selected(*args, **kwargs):
            return {key: mode for key, mode in select_jobs(*args, **kwargs).items() if mode}
        self.assertEqual(selected("push", tag=True), {"wasm": True})
        self.assertEqual(selected("push", paths=["ci/release/build.sh"]), {})
        self.assertEqual(selected("push", paths=["install.sh"]), {"windows-installer": True})
        for tag in (False, True):
            with self.subTest(tag=tag):
                self.assertEqual(selected("workflow_dispatch", tag=tag), {
                    "archives": "full", "docker": True, "fuzz": True, "wasm": True, "windows-installer": True,
                })

    def test_manifest_changes_and_renames(self):
        script = Path(__file__).with_name("checks.py").resolve()
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            def git(*args):
                return subprocess.check_output(["git", *args], cwd=root, text=True).strip()

            def commit():
                git("add", "-A")
                git("-c", "user.name=CI", "-c", "user.email=d2@example.invalid",
                    "-c", "commit.gpgsign=false", "commit", "-qm", "test")
                return git("rev-parse", "HEAD")

            def plan(base):
                output = root / ".git" / "output"
                output.unlink(missing_ok=True)
                subprocess.run([sys.executable, str(script), "plan"], cwd=root, check=True,
                               stdout=subprocess.PIPE, env=dict(os.environ, GITHUB_EVENT_NAME="pull_request",
                               GITHUB_REF_TYPE="branch", BASE_SHA=base, GITHUB_OUTPUT=str(output)))
                return json.loads(output.read_text().removeprefix("jobs="))

            git("init", "-q")
            worker = root / "d2js" / "worker.js"
            worker.parent.mkdir()
            worker.write_text("// worker\n")
            without_manifest = commit()
            manifest = root / "go.mod"
            manifest.write_text("module example.test/d2\ngo 1.27.4\n")
            base = commit()
            self.assertEqual(plan(without_manifest)["archives"], "full")
            manifest.write_text(manifest.read_text() + "require example.test/lib v1.0.0\n")
            dep = commit()
            self.assertEqual(plan(base)["archives"], "size")

            manifest.write_text(manifest.read_text().replace("1.27.4", "1.27.5"))
            upgraded = commit()
            upgraded_jobs = plan(dep)
            self.assertEqual(upgraded_jobs["archives"], "full")
            self.assertTrue(upgraded_jobs["wasm"])
            self.assertFalse(upgraded_jobs["browser"])

            worker.rename(root / "previous worker.txt")
            renamed = commit()
            self.assertEqual(plan(upgraded)["browser"], "packed")

            manifest.unlink()
            commit()
            self.assertEqual(plan(renamed)["archives"], "full")


class GateTests(unittest.TestCase):
    def setUp(self):
        plan = select_jobs("pull_request", paths=["d2lib/board_reveal.go"])
        self.needs = {
            "select": {"result": "success", "outputs": {"jobs": json.dumps(plan)}},
            **{job: {"result": "success"} for job in ("native", "signed", "windows-imports")},
            **{job: {"result": "success" if mode else "skipped"} for job, mode in plan.items()},
        }

    def test_selected_success_and_intentional_skips(self):
        verify(self.needs)

    def test_missing_or_unexpected_results_fail(self):
        for job in ("select", "native", "windows-imports", "signed", "browser"):
            for result in ("failure", "cancelled", "skipped"):
                with self.subTest(job=job, result=result), self.assertRaises(ValueError):
                    verify({**self.needs, job: {**self.needs[job], "result": result}})
        for result in ("failure", "cancelled", "success"):
            with self.subTest(unselected=result), self.assertRaises(ValueError):
                verify({**self.needs, "fuzz": {"result": result}})
        del self.needs["browser"]
        with self.assertRaisesRegex(ValueError, "dependency list"):
            verify(self.needs)


if __name__ == "__main__":
    unittest.main()
