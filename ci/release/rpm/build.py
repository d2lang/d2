#!/usr/bin/env python3
"""Build an RPM package from a checksummed native D2 Linux release archive."""

import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from linux_package import RPM_ARCH, parse_args, read_payload, rpm_version_release, write_gzip


SPEC = Path(__file__).with_name("d2.spec").resolve()


def build(tag, arch, archives, output):
    version, release = rpm_version_release(tag)
    target = RPM_ARCH[arch]
    filename = f"d2-{version}-{release}.{target}.rpm"
    files, epoch = read_payload(tag, arch, archives)

    with tempfile.TemporaryDirectory() as temporary:
        top = Path(temporary)
        sources = top / "SOURCES"
        sources.mkdir()
        for name, contents in files.items():
            destination = sources / Path(name).name
            if name == "man/d2.1":
                destination = sources / "d2.1.gz"
                write_gzip(destination, contents)
            else:
                destination.write_bytes(contents)
            os.utime(destination, (epoch, epoch))

        subprocess.run(
            ["rpmbuild", "-bb", "--target", target,
             "--define", f"_topdir {top}", "--define", f"d2_version {version}",
             "--define", f"d2_release {release}", SPEC],
            env={**os.environ, "SOURCE_DATE_EPOCH": str(epoch)}, check=True,
        )
        # The output directory may be on a different filesystem. Publish it
        # atomically after building, including when release jobs get retried.
        output.mkdir(parents=True, exist_ok=True)
        with tempfile.TemporaryDirectory(dir=output) as pending:
            package = Path(pending) / filename
            shutil.copyfile(top / "RPMS" / target / filename, package)
            package.chmod(0o644)
            package.replace(output / filename)


if __name__ == "__main__":
    args = parse_args(__doc__)
    build(args.version, args.arch, args.archives, args.output)
