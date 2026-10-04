#!/usr/bin/env python3
"""Validate manual workflow inputs before passing them to Make as literal values."""

import os
from pathlib import Path
import re
import subprocess


PACKAGE = re.compile(r"(?:\./\.\.\.|\./(?:[A-Za-z0-9_-]+/)*[A-Za-z0-9_-]+(?:/\.\.\.)?)\Z")


def command(environment):
    seconds = environment.get("FUZZ_SECONDS_INPUT", "60")
    if not re.fullmatch(r"[0-9]{1,4}", seconds) or not 1 <= int(seconds) <= 1800:
        raise ValueError("fuzz_time must be an integer from 1 to 1800 seconds")
    packages = environment.get("FUZZ_PACKAGES_INPUT", "./...").split()
    if not packages or any(not PACKAGE.fullmatch(package) for package in packages):
        raise ValueError("packages must contain local Go package paths or ./... patterns")
    cache = Path(environment["RUNNER_TEMP"]).resolve() / "fuzz-cache"
    return ["make", "fuzz-campaign", "FUZZ_TIME=" + seconds,
            "FUZZ_PKG=" + " ".join(packages), "FUZZ_CACHE=" + str(cache)]


def main():
    args = command(os.environ)
    raise SystemExit(subprocess.run(args, check=False).returncode)


if __name__ == "__main__":
    main()
