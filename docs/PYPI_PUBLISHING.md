# Publishing the release wheels to PyPI

PyPI publication is a separate, manual step after a GitHub Release is public. The
`publish-pypi.yml` workflow downloads that release's seven wheels and publishes
those exact files. It never rebuilds the Go executable or uploads a source
distribution. No workflow runs on a schedule or automatically on a tag push.
The wheel packager points relative README links and images at the matching
GitHub tag so that the PyPI description remains navigable.

[1.0.1](https://pypi.org/project/dircue/1.0.1/) was the first successful PyPI
publication. Its [workflow run](https://github.com/war-and-code/dircue/actions/runs/36602597368)
verified the upload and installed the package with the same lockfile on all
five native platforms. The pending publisher becomes a normal publisher
after its first successful upload; future versions reuse that configuration.

## One-time setup

The initial setup below is already complete for dircue. To review or replace
its publisher, use the existing PyPI project's **Publishing** settings.

1. Create a PyPI account with two-factor authentication. Confirm that the
   `dircue` project name is available. An absent project page does not reserve
   the name.
2. Check the repository's `pypi` GitHub environment before dispatch. It is
   configured to allow only `main` and to require `gingeleski` to approve
   deployment. Keep those protections in place: an environment name in YAML
   alone does not establish an approval rule.
3. In PyPI account **Publishing** settings, create a pending Trusted Publisher:
   owner `war-and-code`, repository `dircue`, workflow
   `publish-pypi.yml`, environment `pypi`, project `dircue`. The values must
   match exactly. A pending publisher does not claim the name until the first
   successful upload. No PyPI API token or GitHub secret is needed.

The PyPI account owner must do the PyPI setup. A repository maintainer can
prepare the GitHub environment. Keep the approval rule and branch restriction
in place for subsequent releases.

The 1.0.0 wheels embed a README that says no PyPI package exists. The verifier
rejects that stale description; those wheels were not uploaded to PyPI.
Version 1.0.1 uses durable README wording.

## Before each publication

- Review the published GitHub Release and its release-candidate receipt.
- Confirm the version has not already been published to PyPI. PyPI files cannot
  be replaced with different bytes under the same filename.
- Confirm the seven wheel tags cover the intended hosts: macOS 12+ on Intel
  and Apple Silicon, Windows x64, and Linux x64/ARM64 on glibc or musl.
  Python 3.10+ is needed to run the wheel launcher. There is no source
  distribution fallback for an unsupported platform.

From the repository's Actions page, dispatch **Publish verified wheels to
PyPI** from `main` and enter the new GitHub Release version without `v`.
Replace `<new-version>` below with that version; 1.0.1 is already published:

```sh
gh workflow run publish-pypi.yml --repo war-and-code/dircue --ref main \
  -f version='<new-version>'
```

The read-only verification job rejects a missing, draft, or prerelease GitHub
Release; a non-annotated tag; a tag outside `main` history; a mismatched
release-candidate commit; missing or extra wheels; altered wheel contents or
`RECORD`; and wheel binaries that differ from the release's core checksums. It
also checks each downloaded wheel and the manifest against the release
workflow's GitHub attestation, bound to the tag commit, and verifies the
Sigstore signature on `SHA256SUMS`. A Linux wheel is installed and executed
before publication.

The separate `publish` job waits for approval in the `pypi` environment. It
receives only the verified files, rechecks their hashes, and obtains a
short-lived PyPI credential through Trusted Publishing. Only this job has
`id-token: write`; it has no checkout or repository write permission. After
publication, a single universal `uv.lock` is created and fresh installations
using that same lockfile run on Linux x64/ARM64, both Mac architectures, and
Windows x64. A failure in these post-publication checks
requires investigation and usually a new patch version; do not replace the
published wheel files.

## Confirm use from a separate project

After PyPI publication, pin the version in the project that invokes dircue:

```sh
uv add 'dircue==1.0.1'
uv sync --locked
uv run dircue --version
```

Put the requirement in `[project].dependencies` when deployment environments
also need the executable. A development-only dependency group is omitted by
`uv sync --no-dev`. The lockfile should be committed and tested on each
supported operating system before relying on it in deployment.

For future versions, confirm a fresh download from PyPI before updating the
installation docs to claim availability. The matching
[GitHub Release wheels](DISTRIBUTION.md#use-a-wheel-from-github) also remain
available for direct installation.
