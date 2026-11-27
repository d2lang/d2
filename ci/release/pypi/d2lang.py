"""The d2lang console-script alias for the adjacent native D2 executable."""

import os
from pathlib import Path
import subprocess
import sys


def main():
    directory = Path(sys.argv[0]).resolve().parent
    if os.name == "nt":
        return subprocess.call([str(directory / "d2.exe"), *sys.argv[1:]])
    program = str(directory / "d2")
    os.execv(program, [program, *sys.argv[1:]])
