#!/usr/bin/env python3
"""Warm the pinned Go module cache with bounded retries and useful diagnostics."""

import os
import re
import subprocess
import sys
import tempfile
import time
from urllib.parse import urlsplit

MAX_DIAGNOSTIC_BYTES = 64 << 10
PUBLIC_MODULE_HOSTS = {"proxy.golang.org", "sum.golang.org", "github.com", "go.dev", "golang.org"}
URL = re.compile(r"(?:https?|ssh|git)://[^\s<>\"']+")


def safe_diagnostics(text):
    """Keep public module paths; withhold credentials and private URL paths."""
    def redact(match):
        raw = match.group()
        try:
            parsed = urlsplit(raw)
            host = parsed.hostname
            if not host:
                return "[redacted URL]"
            # Queries and fragments may carry tokens even on public hosts.
            if host in PUBLIC_MODULE_HOSTS and not parsed.username and not parsed.password:
                return f"{parsed.scheme}://{host}{parsed.path}"
            return f"{parsed.scheme}://{host}/[redacted]"
        except ValueError:
            return "[redacted URL]"
    text = URL.sub(redact, text)
    return "".join(ch if ch in "\n\t" or ch.isprintable() else f"\\x{ord(ch):02x}" for ch in text)


def show_diagnostics(stream):
    stream.seek(0)
    data = stream.read(MAX_DIAGNOSTIC_BYTES + 1)
    truncated = len(data) > MAX_DIAGNOSTIC_BYTES
    # Don't print a partial URL at the cutoff: its credential delimiter may
    # occur beyond the retained prefix. Withhold the whole trailing token.
    if truncated:
        data = data[:MAX_DIAGNOSTIC_BYTES].rsplit(b"\n", 1)[0] if b"\n" in data[:MAX_DIAGNOSTIC_BYTES] else b""
    text = safe_diagnostics(data.decode("utf-8", errors="replace"))
    if text:
        print(text.rstrip("\n"), file=sys.stderr, flush=True)
    if truncated:
        print("Go download diagnostics truncated at 64 KiB", file=sys.stderr, flush=True)


def download(*, run=subprocess.run, sleep=time.sleep):
    env = dict(os.environ)
    env["GOFLAGS"] = "-mod=readonly"
    for attempt in range(1, 4):
        print(f"Go module download attempt {attempt}/3", file=sys.stderr, flush=True)
        # Spool child output to a temporary file rather than retain it all in
        # memory. Only failed attempts display a sanitized, bounded excerpt.
        with tempfile.TemporaryFile() as diagnostics:
            try:
                result = run(["go", "mod", "download"], env=env, timeout=180,
                             check=False, stdout=diagnostics, stderr=subprocess.STDOUT)
                code = result.returncode
            except subprocess.TimeoutExpired:
                print("Go module download exceeded 180 seconds", file=sys.stderr, flush=True)
                code = 1
            except OSError:
                print("Cannot start go mod download; check that Go is installed and executable", file=sys.stderr, flush=True)
                code = 1
            if code == 0:
                return 0
            print(f"Go module download failed (exit {code})", file=sys.stderr, flush=True)
            show_diagnostics(diagnostics)
        if attempt < 3:
            sleep(2 ** attempt)
    print("Go module downloads failed after three attempts", file=sys.stderr, flush=True)
    return 1


if __name__ == "__main__":
    sys.exit(download())
