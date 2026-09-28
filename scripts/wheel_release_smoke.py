#!/usr/bin/env python3
"""Install a native release wheel offline and check its installed console command."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import subprocess
import sys
import tempfile

import declarations_release_smoke as declarations
import formats_release_smoke as formats
import wheels

ROOT = Path(__file__).resolve().parents[1]
INSTALLATION = 'isolated venv; bundled ensurepip; pip --isolated --no-index --no-deps'
SCOPE = 'Native installed console launcher; Linux execution uses glibc. No package index, package build or publication.'


def require(condition, message):
    if not condition:
        raise ValueError(message)


def sha(data):
    return hashlib.sha256(data).hexdigest()


def source_inputs():
    return {path.relative_to(ROOT).as_posix(): sha(path.read_bytes()) for path in
            (Path(__file__).resolve(), ROOT / 'scripts/declarations_release_smoke.py', ROOT / 'scripts/wheels.py')}


def validate_receipt(receipt, version, target, core_sha256, wheel_name, wheel_sha256):
    require(isinstance(receipt, dict), 'native wheel smoke receipt must be an object')
    require(receipt.get('schema_version') == '1.0.0' and receipt.get('passed') is True and
            receipt.get('version') == version and receipt.get('platform') == target,
            'native wheel smoke release identity differs')
    require(receipt.get('wheel') == wheel_name and receipt.get('wheel_sha256') == wheel_sha256 and
            receipt.get('installed_core_sha256') == core_sha256 and
            declarations.valid_digest(receipt.get('launcher_sha256')), 'native wheel smoke artifact identity differs')
    require(receipt.get('wheel_tag_executed') == wheels.PLATFORMS[tuple(target.split('-'))][0],
            'native wheel smoke target tag differs')
    require(receipt.get('harness_sha256') == source_inputs() and receipt.get('fixture_sha256') == declarations.fixture_inputs(),
            'native wheel smoke source identity differs')
    required = declarations.declarations_required(version)
    expected_checks = declarations.DEFAULT_CHECKS | (declarations.DECLARATION_CHECKS if required else set())
    require(receipt.get('checks') == sorted(expected_checks), 'native wheel smoke check inventory differs')
    digests = receipt.get('stdout_sha256')
    keys = {'default_languages', 'default_all'} | ({'declarations', 'combined', 'changed_compare', 'identical_compare', 'partial_compare'} if required else set())
    require(isinstance(digests, dict) and set(digests) == keys and all(declarations.valid_digest(value) for value in digests.values()),
            'native wheel smoke output identity missing')
    require(receipt.get('source_removed_before_compare') is required and receipt.get('installation') == INSTALLATION and
            receipt.get('scope') == SCOPE, 'native wheel smoke execution scope differs')
    require(receipt.get('observed_facts') == (declarations.FACTS if required else {}) and
            receipt.get('negative_cases') == (['duplicate-json-key', 'malformed-json'] if required else []),
            'native wheel smoke coverage differs')
    if formats.required(version):
        formats.validate_receipt(receipt.get('formats'), version, receipt['launcher_sha256'])
    return receipt


def native_platform():
    operating_system = {'Linux': 'linux', 'Darwin': 'darwin', 'Windows': 'windows'}.get(platform.system())
    architecture = {'x86_64': 'amd64', 'AMD64': 'amd64', 'aarch64': 'arm64', 'arm64': 'arm64', 'ARM64': 'arm64'}.get(platform.machine())
    require((operating_system, architecture) in wheels.PLATFORMS, 'unsupported native wheel test host')
    return operating_system + '-' + architecture


def environment(inherited=None):
    inherited = os.environ if inherited is None else inherited
    result = {key: value for key, value in inherited.items() if not key.upper().startswith(('PYTHON', 'PIP_'))}
    # pip's explicit config-file override disables global configuration too;
    # --isolated separately ignores user configuration and environment options.
    result['PIP_CONFIG_FILE'] = os.devnull
    return result


def select_wheel(core, directory, target, version):
    provenance, raw, python_version, loaded = wheels.load_release(core)
    require(provenance['version'] == version, 'core version differs')
    selected = [row for row, _, _ in loaded if row['os'] + '-' + row['arch'] == target]
    require(len(selected) == 1, 'native core target is missing or duplicated')
    row = selected[0]
    # Native Linux release jobs run on glibc; the musl wheel is content-verified
    # by assembly, but this execution does not claim a musl runtime test.
    tag = wheels.PLATFORMS[(row['os'], row['arch'])][0]
    name = f'dircue-{python_version}-py3-none-{tag}.whl'
    wheel = directory / name
    require(wheel.is_file() and not wheel.is_symlink(), 'native wheel is not a regular file')
    receipt = json.loads((directory / 'wheel-provenance.json').read_bytes())
    require(receipt['version'] == python_version and receipt['binary_version'] == version and
            receipt['source_revision'] == provenance['git_revision'] and
            receipt['release_provenance_sha256'] == sha(raw), 'wheel source identity differs')
    matches = [entry for entry in receipt['wheels'] if entry['name'] == name]
    require(len(matches) == 1 and matches[0]['platform'] == tag and
            matches[0]['binary_sha256'] == row['binary_sha256'] and
            matches[0]['source_archive'] == row['name'] and
            matches[0]['source_archive_sha256'] == row['sha256'] and
            matches[0]['sha256'] == sha(wheel.read_bytes()), 'wheel payload identity differs')
    return wheel, row, tag


def execute(command, env, cwd, timeout=120):
    result = subprocess.run([str(part) for part in command], env=env, cwd=cwd,
                            capture_output=True, timeout=timeout)
    require(result.returncode == 0, f'wheel smoke subprocess failed: exit={result.returncode}, stdout SHA256={sha(result.stdout)}, stderr SHA256={sha(result.stderr)}')
    return result


def install_wheel(wheel, target, venv, env, area):
    execute([sys.executable, '-I', '-m', 'venv', '--without-pip', venv], env, area)
    commands = venv / ('Scripts' if target.startswith('windows-') else 'bin')
    python = commands / ('python.exe' if target.startswith('windows-') else 'python')
    execute([python, '-I', '-m', 'ensurepip', '--default-pip'], env, area)
    execute([python, '-I', '-m', 'pip', '--isolated', '--disable-pip-version-check',
             '--no-cache-dir', 'install', '--no-index', '--no-deps', '--only-binary=:all:',
             '--no-compile', wheel], env, area)
    return python, commands


def run(core, directory, target, version):
    require(target == native_platform(), 'requested wheel platform differs from the executing host')
    core, directory = core.resolve(), directory.resolve()
    wheel, row, tag = select_wheel(core, directory, target, version)
    wheel_hash = sha(wheel.read_bytes())
    env = environment()
    with tempfile.TemporaryDirectory(prefix='dircue native wheel ') as temporary:
        area = Path(temporary)
        venv = area / 'isolated environment'
        python, commands = install_wheel(wheel, target, venv, env, area)
        installed = execute([python, '-I', '-c',
                             'import dircue,hashlib,json,sys;from pathlib import Path;'
                             'p=Path(dircue.get_binary_path());'
                             'print(json.dumps({"prefix":sys.prefix,"module":dircue.__file__,"binary":str(p),'
                             '"sha256":hashlib.sha256(p.read_bytes()).hexdigest()}))'], env, area)
        identity = json.loads(installed.stdout)
        require(Path(identity['prefix']).resolve() == venv.resolve() and
                Path(identity['module']).resolve().is_relative_to(venv.resolve()) and
                Path(identity['binary']).resolve().is_relative_to(venv.resolve()) and
                identity['sha256'] == row['binary_sha256'], 'installed package is outside the isolated environment or differs from the core')
        launcher = commands / ('dircue.exe' if target.startswith('windows-') else 'dircue')
        require(launcher.is_file() and not launcher.is_symlink(), 'installed console entrypoint is missing')
        launcher_hash = sha(launcher.read_bytes())
        # Verify the dirq alias is also installed as a console script.
        dirq_launcher = commands / ('dirq.exe' if target.startswith('windows-') else 'dirq')
        require(dirq_launcher.is_file() and not dirq_launcher.is_symlink(), 'dirq console entrypoint is missing')
        smoke_file = area / 'launcher-declarations.json'
        # -E/-s ignore caller Python settings while retaining the trusted helper
        # directory for its local imports. All helper subprocesses inherit env.
        execute([python, '-E', '-s', ROOT / 'scripts/declarations_release_smoke.py',
                 '--candidate', launcher, '--version', version, '--output', smoke_file], env, area)
        smoke = json.loads(smoke_file.read_bytes())
        require(smoke['passed'] is True and smoke['candidate_sha256'] == launcher_hash,
                'console entrypoint smoke identity differs')
        # Verify dirq --version exits 0 and embeds the correct version number.
        dirq_version_result = execute([dirq_launcher, '--version'], env, area)
        dirq_version_line = dirq_version_result.stdout.decode('utf-8', errors='replace').strip()
        dircue_version_result = execute([launcher, '--version'], env, area)
        dircue_version_line = dircue_version_result.stdout.decode('utf-8', errors='replace').strip()
        # Extract the version number (last whitespace-delimited token) from each line.
        require(dirq_version_line.split()[-1:] == dircue_version_line.split()[-1:],
                f'dirq --version number differs from dircue --version: {dirq_version_line!r} vs {dircue_version_line!r}')
        require(sha(Path(identity['binary']).read_bytes()) == row['binary_sha256'] and
                sha(wheel.read_bytes()) == wheel_hash, 'wheel or installed binary changed during execution')
        receipt = {'schema_version': '1.0.0', 'passed': True, 'version': version,
                'platform': target, 'wheel': wheel.name, 'wheel_sha256': wheel_hash,
                'wheel_tag_executed': tag, 'installed_core_sha256': row['binary_sha256'],
                'launcher_sha256': launcher_hash, 'installation': INSTALLATION,
                'checks': smoke['checks'], 'stdout_sha256': smoke['stdout_sha256'],
                'source_removed_before_compare': smoke['source_removed_before_compare'],
                'fixture_sha256': smoke['fixture_sha256'], 'observed_facts': smoke['observed_facts'],
                'negative_cases': smoke['negative_cases'], 'harness_sha256': source_inputs(), 'scope': SCOPE}
        if formats.required(version):
            format_file = area / 'launcher-formats.json'
            execute([python, '-E', '-s', ROOT / 'scripts/formats_release_smoke.py',
                     '--candidate', launcher, '--version', version, '--output', format_file], env, area)
            receipt['formats'] = json.loads(format_file.read_bytes())
        return validate_receipt(receipt, version, target, row['binary_sha256'], wheel.name, wheel_hash)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--release-dir', type=Path, required=True)
    parser.add_argument('--wheel-dir', type=Path, required=True)
    parser.add_argument('--platform', required=True)
    parser.add_argument('--version', required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    try:
        require(not os.path.lexists(args.output), 'choose a fresh wheel smoke receipt path')
        receipt = run(args.release_dir, args.wheel_dir, args.platform, args.version)
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(receipt, indent=2) + '\n')
        print(f"PASS: offline installed wheel launcher, {args.platform}, {receipt['wheel_tag_executed']}, {len(receipt['checks'])} checks; core SHA256={receipt['installed_core_sha256']}")
    except (ValueError, KeyError, OSError, subprocess.TimeoutExpired) as error:
        parser.exit(1, 'native wheel smoke: ' + str(error) + '\n')


if __name__ == '__main__':
    main()
