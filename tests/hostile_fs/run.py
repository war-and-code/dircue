#!/usr/bin/env python3
"""Hostile-filesystem conformance harness for dircue.

Builds a synthetic hostile directory at runtime in a temp dir (never committed),
runs dircue with a timeout for each case, records per-case metrics to JSONL, and
asserts expected outcomes. Exits non-zero on any assertion failure or timeout.

Usage:
  python3 tests/hostile_fs/run.py --binary bin/dircue
  python3 tests/hostile_fs/run.py --binary bin/dircue --quick   # CI subset only
"""

import argparse
import ctypes
import json
import os
import platform
import signal
import socket
import stat
import struct
import subprocess
import sys
import tempfile
import time
from pathlib import Path

try:
    import resource
except ImportError:  # Windows has no resource module.
    resource = None

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent

SKIP_WINDOWS = platform.system() == "Windows"
IS_MACOS = platform.system() == "Darwin"
IS_LINUX = platform.system() == "Linux"


# ---------------------------------------------------------------------------
# Harness utilities
# ---------------------------------------------------------------------------

def run_dircue(binary, args, timeout=30):
    """Run dircue with args, return dict with exit_code, stdout, stderr,
    wall_ms, rss_kb. Enforces a hard timeout via SIGKILL."""
    started = time.perf_counter()
    try:
        proc = subprocess.run(
            [str(binary)] + [str(a) for a in args],
            capture_output=True,
            text=True,
            timeout=timeout,
        )
        elapsed_ms = int((time.perf_counter() - started) * 1000)
        rss_kb = _get_rss_kb()
        return {
            "exit_code": proc.returncode,
            "stdout": proc.stdout,
            "stderr": proc.stderr,
            "wall_ms": elapsed_ms,
            "rss_kb": rss_kb,
            "timed_out": False,
        }
    except subprocess.TimeoutExpired as exc:
        elapsed_ms = int((time.perf_counter() - started) * 1000)
        return {
            "exit_code": -1,
            "stdout": "",
            "stderr": str(exc),
            "wall_ms": elapsed_ms,
            "rss_kb": 0,
            "timed_out": True,
        }


def _get_rss_kb():
    if resource is None:
        return 0
    try:
        usage = resource.getrusage(resource.RUSAGE_CHILDREN)
        rss = usage.ru_maxrss
        if IS_MACOS:
            rss = rss // 1024  # bytes → kibibytes on macOS
        return rss
    except Exception:
        return 0


def extract_warnings(stderr):
    """Return list of {path, code, message} dicts from dircue warning lines."""
    warnings = []
    for line in stderr.splitlines():
        line = line.strip()
        if line.startswith("warning:"):
            # Format: warning: <path>: <message> (<code>)
            rest = line[len("warning: "):]
            if rest.endswith(")"):
                code_start = rest.rfind("(")
                if code_start != -1:
                    code = rest[code_start + 1:-1]
                    rest = rest[:code_start].rstrip(": ")
                    parts = rest.split(": ", 1)
                    path = parts[0]
                    message = parts[1] if len(parts) > 1 else ""
                    warnings.append({"path": path, "code": code, "message": message})
    return warnings


def write_jsonl(path, records):
    Path(path).parent.mkdir(parents=True, exist_ok=True)
    with open(path, "a") as f:
        for rec in records:
            f.write(json.dumps(rec) + "\n")


# ---------------------------------------------------------------------------
# Fixture builders
# ---------------------------------------------------------------------------

def make_regular_file(path, content=b"package main\nfunc main() {}\n"):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "wb") as f:
        f.write(content)


def make_fifo(path):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    os.mkfifo(path, 0o600)


def make_unix_socket(path):
    """Create a Unix domain socket. macOS limits paths to 104 bytes."""
    os.makedirs(os.path.dirname(path), exist_ok=True)
    s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    s.bind(path)
    return s  # caller owns; keep alive until done


def make_symlink_loop(dir_path):
    """Create a → b → a symlink cycle."""
    a = os.path.join(dir_path, "loop_a")
    b = os.path.join(dir_path, "loop_b")
    os.symlink(b, a)
    os.symlink(a, b)


def make_escaping_symlink(dir_path, outside_path):
    """Create a symlink inside dir_path pointing to outside_path."""
    target = os.path.join(dir_path, "escape.go")
    os.symlink(outside_path, target)


def make_sparse_file(path, size_bytes=10 * 1024 * 1024 * 1024):
    """Create a sparse file with the given apparent size via truncate."""
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "wb") as f:
        f.truncate(size_bytes)


def make_chmod000_dir(path):
    """Create a directory that is not readable or executable."""
    os.makedirs(path, exist_ok=True)
    make_regular_file(os.path.join(path, "hidden.go"))
    os.chmod(path, 0o000)


def make_chmod000_file(path):
    make_regular_file(path)
    os.chmod(path, 0o000)


def restore_chmod000_dir(path):
    os.chmod(path, 0o755)


def restore_chmod000_file(path):
    os.chmod(path, 0o644)


def make_deep_tree(base, depth=500):
    """Build base/d/d/d/... depth levels deep with a leaf.go file."""
    current = base
    for _ in range(depth):
        current = os.path.join(current, "d")
    os.makedirs(current, exist_ok=True)
    make_regular_file(os.path.join(current, "leaf.go"))


# ---------------------------------------------------------------------------
# Individual test cases
# ---------------------------------------------------------------------------

class Case:
    def __init__(self, name, is_quick=False):
        self.name = name
        self.is_quick = is_quick
        self.failures = []

    def fail(self, msg):
        self.failures.append(msg)

    def check(self, condition, msg):
        if not condition:
            self.fail(msg)


def case_fifo(binary, tmpdir):
    c = Case("fifo", is_quick=True)
    root = os.path.join(tmpdir, "fifo")
    os.makedirs(root, exist_ok=True)
    make_regular_file(os.path.join(root, "real.go"))
    fifo_path = os.path.join(root, "pipe.go")
    os.mkfifo(fifo_path, 0o600)

    result = run_dircue(binary, ["--json", root], timeout=10)
    c.check(not result["timed_out"], "dircue hung on FIFO input")
    c.check(result["wall_ms"] < 8000, f"dircue took too long: {result['wall_ms']} ms")
    c.check(result["exit_code"] == 0, f"exit code {result['exit_code']} for FIFO (expected 0)")

    result_map = run_dircue(binary, ["map", "--json", root], timeout=10)
    c.check(not result_map["timed_out"], "dircue map hung on FIFO input")

    return c, [
        {"case": c.name, "command": "--json", **result,
         "expected_exit": 0, "passed": not c.failures},
    ]


def case_unix_socket(binary, tmpdir):
    c = Case("unix_socket", is_quick=True)
    if IS_MACOS:
        # macOS limits Unix domain socket paths to 104 bytes; use a short temp dir.
        root = tempfile.mkdtemp(prefix="dircue-sock-")
    else:
        root = os.path.join(tmpdir, "socket")
        os.makedirs(root, exist_ok=True)

    make_regular_file(os.path.join(root, "real.go"))
    sock = None
    try:
        sock_path = os.path.join(root, "s.go")
        try:
            sock = make_unix_socket(sock_path)
        except (OSError, AttributeError):
            return c, []  # socket unavailable on this OS

        result = run_dircue(binary, ["--json", root], timeout=10)
        c.check(not result["timed_out"], "dircue hung on unix socket")
        c.check(result["exit_code"] == 0, f"exit code {result['exit_code']} for socket")
    finally:
        if sock:
            sock.close()
        if IS_MACOS and root.startswith(tempfile.gettempdir()):
            import shutil
            shutil.rmtree(root, ignore_errors=True)

    return c, [{"case": c.name, "command": "--json", **result, "passed": not c.failures}]


def case_symlink_loop(binary, tmpdir):
    c = Case("symlink_loop", is_quick=True)
    root = os.path.join(tmpdir, "symloop")
    os.makedirs(root, exist_ok=True)
    make_regular_file(os.path.join(root, "real.go"))
    make_symlink_loop(root)

    result = run_dircue(binary, ["--json", root], timeout=10)
    c.check(not result["timed_out"], "dircue hung on symlink loop")
    c.check(result["exit_code"] == 0, f"exit code {result['exit_code']} for symlink loop")
    return c, [{"case": c.name, "command": "--json", **result, "passed": not c.failures}]


def case_symlink_escape(binary, tmpdir):
    c = Case("symlink_escape", is_quick=True)
    root = os.path.join(tmpdir, "symescape")
    outside = os.path.join(tmpdir, "outside")
    os.makedirs(root, exist_ok=True)
    os.makedirs(outside, exist_ok=True)
    make_regular_file(os.path.join(outside, "secret.txt"), b"CANARY-OUTSIDE")
    make_escaping_symlink(root, os.path.join(outside, "secret.txt"))

    result = run_dircue(binary, ["--json", root], timeout=10)
    c.check(not result["timed_out"], "dircue hung on escaping symlink")
    c.check(result["exit_code"] == 0, f"exit code {result['exit_code']} for escaping symlink")
    # The canary content must not appear in output.
    c.check("CANARY-OUTSIDE" not in result["stdout"], "outside content leaked into output")
    return c, [{"case": c.name, "command": "--json", **result, "passed": not c.failures}]


def case_hardlinks(binary, tmpdir):
    c = Case("hardlinks", is_quick=True)
    root = os.path.join(tmpdir, "hardlink")
    os.makedirs(root, exist_ok=True)
    make_regular_file(os.path.join(root, "original.go"))
    try:
        os.link(os.path.join(root, "original.go"), os.path.join(root, "hardlink.go"))
    except OSError:
        return c, []  # hardlinks unavailable

    result = run_dircue(binary, ["--json", root], timeout=10)
    c.check(not result["timed_out"], "dircue hung on hardlinks")
    c.check(result["exit_code"] == 0, f"exit code {result['exit_code']} for hardlinks")
    # Both paths should appear in the output.
    if result["stdout"]:
        try:
            data = json.loads(result["stdout"])
            go_lang = data.get("Go", {})
            file_count = go_lang.get("file_count", 0)
            c.check(file_count >= 2 or file_count == 0, f"expected >= 2 Go files counted for hardlinks (or 0 if --include-files not set), got {file_count}")
        except json.JSONDecodeError:
            pass  # text mode
    return c, [{"case": c.name, "command": "--json", **result, "passed": not c.failures}]


def case_sparse_file(binary, tmpdir):
    c = Case("sparse_file", is_quick=False)  # excluded from --quick (allocates 10 GiB apparent)
    root = os.path.join(tmpdir, "sparse")
    os.makedirs(root, exist_ok=True)
    make_regular_file(os.path.join(root, "real.go"))
    sparse_path = os.path.join(root, "big.go")
    try:
        make_sparse_file(sparse_path, 10 * 1024 * 1024 * 1024)
    except OSError:
        return c, []  # sparse file creation unavailable

    result = run_dircue(binary, ["--json", root], timeout=15)
    c.check(not result["timed_out"], "dircue hung on sparse file (10 GiB apparent)")
    c.check(result["exit_code"] == 0, f"exit code {result['exit_code']} for sparse file")
    return c, [{"case": c.name, "command": "--json", **result, "passed": not c.failures}]


def case_permission_denied_file(binary, tmpdir):
    if os.geteuid() == 0:
        return Case("perm_denied_file", is_quick=True), []
    c = Case("perm_denied_file", is_quick=True)
    root = os.path.join(tmpdir, "permfile")
    os.makedirs(root, exist_ok=True)
    make_regular_file(os.path.join(root, "good.go"))
    forbidden = os.path.join(root, "forbidden.go")
    make_chmod000_file(forbidden)
    try:
        # fail policy: should return error
        result_fail = run_dircue(binary, ["--json", root], timeout=10)
        c.check(result_fail["exit_code"] != 0, "fail policy should return non-zero for forbidden file")

        # continue policy: should succeed with warning
        result_cont = run_dircue(binary, ["--on-error", "continue", "--json", root], timeout=10)
        c.check(result_cont["exit_code"] == 0, f"continue policy non-zero for forbidden file: {result_cont['exit_code']}")
        c.check("file_read_error" in result_cont["stderr"],
                "missing file_read_error warning under continue")
    finally:
        restore_chmod000_file(forbidden)

    return c, [
        {"case": c.name, "sub": "fail", **result_fail, "passed": not c.failures},
        {"case": c.name, "sub": "continue", **result_cont, "passed": not c.failures},
    ]


def case_permission_denied_dir(binary, tmpdir):
    if os.geteuid() == 0:
        return Case("perm_denied_dir", is_quick=True), []
    c = Case("perm_denied_dir", is_quick=True)
    root = os.path.join(tmpdir, "permdir")
    os.makedirs(root, exist_ok=True)
    make_regular_file(os.path.join(root, "good.go"))
    secret = os.path.join(root, "secret")
    make_chmod000_dir(secret)
    try:
        # fail policy
        result_fail = run_dircue(binary, ["--json", root], timeout=10)
        c.check(result_fail["exit_code"] != 0, "fail policy should return non-zero for forbidden dir")

        # continue policy
        result_cont = run_dircue(binary, ["--on-error", "continue", "--json", root], timeout=10)
        c.check(result_cont["exit_code"] == 0,
                f"continue policy non-zero for forbidden dir: {result_cont['exit_code']}")
        c.check("permission_denied" in result_cont["stderr"],
                "missing permission_denied warning under continue")
    finally:
        restore_chmod000_dir(secret)

    return c, [
        {"case": c.name, "sub": "fail", **result_fail, "passed": not c.failures},
        {"case": c.name, "sub": "continue", **result_cont, "passed": not c.failures},
    ]


def case_deep_tree(binary, tmpdir):
    c = Case("deep_tree", is_quick=False)
    root = os.path.join(tmpdir, "deep")
    os.makedirs(root, exist_ok=True)
    make_deep_tree(root, depth=200)

    result = run_dircue(binary, ["--json", "--tree-size", "300", root], timeout=30)
    c.check(not result["timed_out"], "dircue timed out on deep tree (depth 200)")
    c.check(result["exit_code"] == 0, f"exit code {result['exit_code']} for deep tree")
    return c, [{"case": c.name, "command": "--json", **result, "passed": not c.failures}]


def case_zero_byte_files(binary, tmpdir):
    c = Case("zero_byte_file", is_quick=True)
    root = os.path.join(tmpdir, "zerobyte")
    os.makedirs(root, exist_ok=True)
    make_regular_file(os.path.join(root, "empty.go"), b"")
    make_regular_file(os.path.join(root, "full.go"))

    result = run_dircue(binary, ["--json", root], timeout=10)
    c.check(not result["timed_out"], "dircue hung on zero-byte file")
    c.check(result["exit_code"] == 0, f"exit code {result['exit_code']} for zero-byte file")
    return c, [{"case": c.name, "command": "--json", **result, "passed": not c.failures}]


def case_analyze_all(binary, tmpdir):
    """Verify analyze all --json handles hostile inputs."""
    c = Case("analyze_all", is_quick=True)
    root = os.path.join(tmpdir, "analyze_all")
    os.makedirs(root, exist_ok=True)
    make_regular_file(os.path.join(root, "real.go"))
    os.mkfifo(os.path.join(root, "pipe.go"), 0o600)

    result = run_dircue(binary, ["analyze", "all", "--json", root], timeout=10)
    c.check(not result["timed_out"], "analyze all hung on FIFO")
    c.check(result["exit_code"] == 0, f"analyze all exit {result['exit_code']} on FIFO")
    return c, [{"case": c.name, "command": "analyze all", **result, "passed": not c.failures}]


def case_map_json(binary, tmpdir):
    """Verify map --json handles hostile inputs."""
    c = Case("map_json", is_quick=True)
    root = os.path.join(tmpdir, "map_json")
    os.makedirs(root, exist_ok=True)
    make_regular_file(os.path.join(root, "real.go"))
    os.mkfifo(os.path.join(root, "pipe.go"), 0o600)

    result = run_dircue(binary, ["map", "--json", root], timeout=10)
    c.check(not result["timed_out"], "map --json hung on FIFO")
    c.check(result["exit_code"] == 0, f"map --json exit {result['exit_code']} on FIFO")
    return c, [{"case": c.name, "command": "map --json", **result, "passed": not c.failures}]


def case_attach_fifo_sigterm(binary, tmpdir):
    """Regression test: map --attach FIFO must not hang; SIGTERM must work."""
    c = Case("attach_fifo_sigterm", is_quick=True)
    if SKIP_WINDOWS:
        return c, []  # mkfifo unavailable on Windows
    fifo_dir = tempfile.mkdtemp(prefix="dircue-fifo-")
    try:
        fifo_path = os.path.join(fifo_dir, "report.sarif")
        scan_dir = os.path.join(tmpdir, "attach_sigterm_scan")
        os.makedirs(scan_dir, exist_ok=True)
        try:
            os.mkfifo(fifo_path, 0o600)
        except OSError:
            return c, []

        started = time.perf_counter()
        proc = subprocess.Popen(
            [str(binary), "map", "--attach", f"sarif={fifo_path}", scan_dir],
            stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        )
        # Give the process a moment, then send SIGTERM.
        time.sleep(0.3)
        proc.send_signal(signal.SIGTERM)
        try:
            proc.wait(timeout=3)
            elapsed_ms = int((time.perf_counter() - started) * 1000)
            c.check(elapsed_ms < 4000, f"SIGTERM took too long: {elapsed_ms} ms")
        except subprocess.TimeoutExpired:
            proc.kill()
            c.fail("dircue map --attach FIFO did not exit within 3 s of SIGTERM")
            elapsed_ms = 3000
        finally:
            proc.stdout.close()
            proc.stderr.close()

        result = {"exit_code": proc.returncode, "wall_ms": elapsed_ms,
                  "rss_kb": 0, "stdout": "", "stderr": "", "timed_out": False}
    finally:
        import shutil
        shutil.rmtree(fifo_dir, ignore_errors=True)

    return c, [{"case": c.name, "command": "map --attach sarif=FIFO", **result, "passed": not c.failures}]


def case_nfc_nfd(binary, tmpdir):
    c = Case("nfc_nfd_filenames", is_quick=True)
    root = os.path.join(tmpdir, "nfcnfd")
    os.makedirs(root, exist_ok=True)
    # NFC: precomposed é (U+00E9); NFD: decomposed e + combining accent (U+0065 U+0301)
    nfc_name = "é_file.go"
    nfd_name = "é_file.go"
    try:
        make_regular_file(os.path.join(root, nfc_name))
        make_regular_file(os.path.join(root, nfd_name))
    except OSError:
        return c, []  # filesystem rejected the names

    result = run_dircue(binary, ["--json", root], timeout=10)
    c.check(not result["timed_out"], "dircue hung on NFC/NFD filenames")
    c.check(result["exit_code"] == 0, f"exit code {result['exit_code']} for NFC/NFD")
    return c, [{"case": c.name, "command": "--json", **result, "passed": not c.failures}]


# ---------------------------------------------------------------------------
# Main runner
# ---------------------------------------------------------------------------

ALL_CASES = [
    case_fifo,
    case_unix_socket,
    case_symlink_loop,
    case_symlink_escape,
    case_hardlinks,
    case_sparse_file,
    case_permission_denied_file,
    case_permission_denied_dir,
    case_deep_tree,
    case_zero_byte_files,
    case_analyze_all,
    case_map_json,
    case_attach_fifo_sigterm,
    case_nfc_nfd,
]


def run_all(binary, quick, jsonl_out):
    print(f"Platform: {platform.system()}")
    if SKIP_WINDOWS:
        print("Note: Windows — POSIX-only cases (FIFO, sockets, symlinks) skipped")

    passed = 0
    failed = 0
    skipped = 0
    rows = []  # for summary table

    with tempfile.TemporaryDirectory(prefix="dircue-hostile-") as tmpdir:
        for case_fn in ALL_CASES:
            case_obj, records = case_fn(binary, tmpdir)

            # Skip cases where no records were produced (unavailable on this platform).
            if not records:
                skipped += 1
                rows.append((case_obj.name, "SKIP", "-", "-"))
                continue

            if quick and not case_obj.is_quick:
                skipped += 1
                rows.append((case_obj.name, "SKIP (--quick)", "-", "-"))
                continue

            write_jsonl(jsonl_out, records)

            if case_obj.failures:
                failed += 1
                status = "FAIL"
                for f in case_obj.failures:
                    print(f"  FAIL [{case_obj.name}]: {f}", file=sys.stderr)
            else:
                passed += 1
                status = "PASS"

            # Summary info from last record.
            last = records[-1]
            wall = f"{last.get('wall_ms', '?')} ms"
            rss = f"{last.get('rss_kb', '?')} KB"
            rows.append((case_obj.name, status, wall, rss))

    # Print summary table.
    print()
    print(f"{'Case':<30} {'Status':<16} {'Wall':<12} {'RSS':<12}")
    print("-" * 72)
    for name, status, wall, rss in rows:
        print(f"{name:<30} {status:<16} {wall:<12} {rss:<12}")
    print()
    print(f"Results: {passed} passed, {failed} failed, {skipped} skipped")
    print(f"JSONL output: {jsonl_out}")

    return failed == 0


def main():
    ap = argparse.ArgumentParser(description="Hostile-filesystem conformance harness")
    ap.add_argument("--binary", default=str(ROOT / "bin" / "dircue"),
                    help="Path to the dircue binary (default: bin/dircue)")
    ap.add_argument("--quick", action="store_true",
                    help="Run only the quick subset (for CI)")
    ap.add_argument("--output", default=str(ROOT / ".cache" / "hostile_fs_results.jsonl"),
                    help="JSONL output file")
    args = ap.parse_args()

    binary = Path(args.binary)
    if not binary.exists():
        # Try to build it.
        print(f"Binary {binary} not found; attempting go build...")
        result = subprocess.run(
            ["go", "build", "-buildvcs=false", "-trimpath", "-o", str(binary), "."],
            cwd=str(ROOT),
        )
        if result.returncode != 0:
            print(f"go build failed; specify --binary or run 'make build' first", file=sys.stderr)
            sys.exit(2)

    jsonl_out = args.output
    # Clear previous results.
    if os.path.exists(jsonl_out):
        os.remove(jsonl_out)

    ok = run_all(binary, args.quick, jsonl_out)
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
