#!/usr/bin/env python3
"""Warm the pinned Go module cache with bounded download retries."""

import os
import subprocess
import sys
import time


def download(*, run=subprocess.run, sleep=time.sleep):
    env = dict(os.environ)
    env["GOFLAGS"] = "-mod=readonly"
    for attempt in range(1, 4):
        print(f"Go module download attempt {attempt}/3", file=sys.stderr, flush=True)
        try:
            result = run(["go", "mod", "download"], env=env, timeout=180, check=False)
            code = result.returncode
        except subprocess.TimeoutExpired:
            print("Go module download exceeded 180 seconds", file=sys.stderr, flush=True)
            code = 1
        if code == 0:
            return 0
        if attempt < 3:
            sleep(2 ** attempt)
    print("Go module downloads failed after three attempts", file=sys.stderr, flush=True)
    return 1


if __name__ == "__main__":
    sys.exit(download())
