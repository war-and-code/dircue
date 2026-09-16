#!/usr/bin/env python3
"""Smoke-test existing release wheels offline on macOS and cached Linux images."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import subprocess
import sys
import tarfile
import tempfile
import zipfile

LINUX_IMAGES = (
    ("glibc", "ghcr.io/astral-sh/uv:python3.13-bookworm-slim", "manylinux_2_17_aarch64"),
    ("musl", "ghcr.io/astral-sh/uv:python3.13-alpine", "musllinux_1_2_aarch64"),
)
COMMANDS = (
    ["--breakdown", "--json", "."],
    ["analyze", "metrics", "--json", "--files", "."],
    ["analyze", "all", "--metrics", "--json", "."],
)
BINARY_HASH = ("import hashlib,dircue;"
               "print(hashlib.sha256(open(dircue.get_binary_path(),'rb').read()).hexdigest())")


def sha(content):
    return hashlib.sha256(content).hexdigest()


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--release-dir", type=Path, required=True)
    parser.add_argument("--wheel-dir", type=Path, required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    require(platform.system() == "Darwin" and platform.machine() == "arm64",
            "the native portion requires macOS arm64")
    release_dir, wheel_dir = args.release_dir.resolve(), args.wheel_dir.resolve()
    checks, platforms = [], []
    with tempfile.TemporaryDirectory(prefix="dircue wheel smoke ") as directory:
        temporary = Path(directory)
        fixture = temporary / "source with spaces"
        fixture.mkdir(mode=0o755)
        for name, content in {
            "Main.cs": "// C# sample\n\nclass Hello { static void Main() { if (true) {} } }\n",
            "Hello.java": "// Java sample\n\nclass Hello { public static void main(String[] args) {} }\n",
            "App.csproj": '<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup></Project>\n',
            "data.xml": "<events><event>log</event></events>\n",
        }.items():
            (fixture / name).write_text(content)
            (fixture / name).chmod(0o644)
        replacements = sorted({
            str(temporary): "<temporary>", str(release_dir): "<release-dir>",
            str(wheel_dir): "<wheel-dir>", str(Path(__file__).resolve().parents[2]): "<repository>",
            sys.executable: "<python>", str(Path.home()): "<home>",
        }.items(), key=lambda pair: -len(pair[0]))

        def scrub(value):
            if isinstance(value, str):
                for source, replacement in replacements:
                    value = value.replace(source, replacement)
                return value
            if isinstance(value, list):
                return [scrub(item) for item in value]
            if isinstance(value, dict):
                return {key: scrub(item) for key, item in value.items()}
            return value

        environment = {key: value for key, value in os.environ.items() if not key.startswith("UV_")}
        environment.update(UV_CACHE_DIR=str(temporary / "cache"), UV_PYTHON_DOWNLOADS="never",
                           UV_NO_CONFIG="true")

        def run(command, status=0, label="native"):
            command = [str(part) for part in command]
            result = subprocess.run(command, cwd=fixture, env=environment, capture_output=True,
                                    text=True, timeout=120)
            record = scrub({"platform": label, "command": command, "exit_code": result.returncode,
                            "stdout": result.stdout, "stderr": result.stderr})
            checks.append(record)
            require(result.returncode == status, f"unexpected process status: {record}")
            return result

        def artifact(operating_system, tag):
            archive = release_dir / f"dircue_{args.version}_{operating_system}_arm64.tar.gz"
            with tarfile.open(archive, "r:gz") as source:
                member = source.getmember("dircue")
                require(member.isfile(), "archive executable is not a regular file")
                content = source.extractfile(member).read()
            direct = temporary / f"dircue-{operating_system}-arm64"
            direct.write_bytes(content)
            direct.chmod(0o755)
            wheel = wheel_dir / f"dircue-{args.version}-py3-none-{tag}.whl"
            with zipfile.ZipFile(wheel) as source:
                require(source.read("dircue/bin/dircue") == content,
                        f"{wheel.name}: bundled binary differs from release archive")
            return direct, wheel, {"archive": archive.name, "archive_sha256": sha(archive.read_bytes()),
                                   "wheel": wheel.name, "wheel_sha256": sha(wheel.read_bytes()),
                                   "binary_sha256": sha(content)}

        def compare(wrapper, direct, label, binary_hash):
            require(run([*wrapper, "--version"], label=label).stdout == f"dircue {args.version}\n",
                    f"{label}: wrong version")
            for arguments in COMMANDS:
                actual = run([*wrapper, *arguments], label=label)
                expected = run([*direct, *arguments], label=label)
                require(actual.stdout == expected.stdout, f"{label}: JSON differs from archive binary")
                report = json.loads(actual.stdout)
                if arguments[0] == "analyze":
                    metrics = report["metrics"]
                    require(metrics["status"] == "complete" and metrics["totals"]["files"] == 2,
                            f"{label}: incomplete counts or XML entered default source scope")
                    require({row["language"] for row in metrics["languages"]} == {"Java", "C#"},
                            f"{label}: missing Java/C# metrics")
                else:
                    require(set(report) == {"Java", "C#"}, f"{label}: unexpected languages")
            actual = run([*wrapper, "--json", "./missing"], 1, label)
            expected = run([*direct, "--json", "./missing"], 1, label)
            require(actual.stdout == expected.stdout == "" and expected.stderr
                    and actual.stderr.endswith(expected.stderr), f"{label}: error behavior differs")
            installed_hash = run([*wrapper[:-1], "python", "-c", BINARY_HASH], label=label)
            require(installed_hash.stdout.strip() == binary_hash,
                    f"{label}: installed wheel binary differs from archive")

        direct, wheel, record = artifact("darwin", "macosx_12_0_arm64")
        native = ["uvx", "--offline", "--no-index", "--no-config", "--no-python-downloads",
                  "--python", sys.executable, "--from", wheel, "dircue"]
        compare(native, [direct], "native macOS arm64", record["binary_sha256"])
        platforms.append({"platform": "native macOS arm64", "python": platform.python_version(), **record})
        for libc, image_name, tag in LINUX_IMAGES:
            image = json.loads(subprocess.check_output(["docker", "image", "inspect", image_name], text=True))[0]
            require(image["Architecture"] == "arm64" and image["Os"] == "linux",
                    f"{image_name}: expected cached Linux arm64 image")
            direct, wheel, record = artifact("linux", tag)
            docker = ["docker", "run", "--rm", "--pull", "never", "--platform", "linux/arm64",
                      "--network", "none", "--read-only", "--cap-drop", "ALL", "--security-opt",
                      "no-new-privileges", "--user", "65532:65532", "--memory", "512m", "--cpus", "2",
                      "--tmpfs", "/tmp:rw,exec,mode=1777,size=256m", "-e", "HOME=/tmp",
                      "-e", "UV_CACHE_DIR=/tmp/cache", "-e", "UV_PYTHON_DOWNLOADS=never",
                      "-v", f"{wheel_dir}:/wheels:ro", "-v", f"{direct}:/archive/dircue:ro",
                      "-v", f"{fixture}:/source with spaces:ro", "-w", "/source with spaces"]
            wrapper = [*docker, "--entrypoint", "uvx", image["Id"], "--offline", "--no-index",
                       "--no-config", "--no-python-downloads", "--from", f"/wheels/{wheel.name}", "dircue"]
            archive_command = [*docker, "--entrypoint", "/archive/dircue", image["Id"]]
            compare(wrapper, archive_command, f"Linux arm64 {libc}", record["binary_sha256"])
            platforms.append({"platform": f"Linux arm64 {libc}", "image_id": image["Id"],
                              "network": "none", "read_only": True, "uid": 65532, **record})
        result = scrub({"complete": True, "version": args.version, "platforms": platforms, "checks": checks})
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(result, indent=2) + "\n")
    print("PASS: offline macOS, Linux glibc and Linux musl wheels match archive binaries")


if __name__ == "__main__":
    main()
