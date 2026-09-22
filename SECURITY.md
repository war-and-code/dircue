# Security policy

Dircue profiles unfamiliar directories, including hostile checkouts.
Reporting a defect that lets one of those inputs escape our documented
boundaries is welcome and taken seriously.

## Supported versions

Only the current 1.0.x line receives security fixes. Older 0.x releases
are historical and will not be patched.

| Version | Status |
| --- | --- |
| 1.0.x | Supported |
| < 1.0 | Unsupported (historical) |

## How to report a vulnerability

Please report privately, not in a public issue.

- **Preferred:** open a private report through GitHub's
  "Report a vulnerability" button on the repository's
  [Security tab](https://github.com/war-and-code/dircue/security/advisories/new).
  This uses GitHub Private Vulnerability Reporting (PVR) and keeps the
  discussion visible to maintainers only until a fix is coordinated.
- **Fallback:** if PVR is not available to you, contact the maintainers
  through the address published on the repository's public profile at
  <https://github.com/war-and-code>. Do not post exploit details in
  public issues, discussions, pull requests, or social media before a
  fix ships.

Please include: dircue version (`dircue --version`), platform, the
exact command and its inputs (a minimal repository or archive if you
can share one), what you observed, and what you expected. If you are
willing, tell us the earliest coordinated-disclosure date that works
for you.

We aim to acknowledge new reports within five working days and to keep
you informed about triage and remediation on a rolling basis. A typical
coordinated-disclosure window is 90 days from the acknowledgement; we
will negotiate a shorter or longer window when the situation calls for
one.

## Scope

Dircue is a self-contained profiler. In its documented use it does not
execute inspected repository content, run project build scripts,
contact the network, or read files outside the selected source. A
report that shows any of the following is in scope:

- **Boundary escape.** Reading, writing, or influencing files outside
  the selected Git tree or directory (for example, through symlinks,
  Git alternates, `.gitattributes`, worker inputs, or saved-report
  references).
- **Code or command execution** triggered by inspected content,
  a crafted saved report, a schema export, a spelling suggestion, or
  any other CLI surface. Dircue never intentionally executes inspected
  content; a defect that causes it to do so is a vulnerability.
- **Credential or environment leakage** into a written report, log, or
  diagnostic. Saved reports and warnings are meant to be safe to
  attach to a pull request; a report that quotes filesystem paths,
  tokens, or environment values that were not user-supplied on the
  command line is a defect.
- **Denial of service through a crafted repository or saved report**:
  memory exhaustion, unbounded CPU or disk use, deadlocks, worker
  hangs, and similar effects that ordinary bounded inputs should not
  cause. Bounds we already document (tree-size limits, packfile
  descriptor caps, structural worker deadlines, saved-report size
  caps) are contract, not vulnerabilities.
- **Supply-chain integrity issues** in released archives, wheels, the
  Docker image, or the checksum manifests attached to a GitHub release.

Out of scope, unless a specific chain leads to one of the above:

- Missing hardening flags on binaries the release scripts produce
  in isolation.
- Best-practice recommendations without a reproducible impact.
- Behavior of unsupported third-party workers a user installs. The
  worker is invoked only when the caller supplies an explicit
  `--structural-worker` path; verifying the worker binary's origin is
  the caller's responsibility.
- Cosmetic issues in help text, diagnostics, or documentation.
- Public issue-tracker abuse or non-technical concerns.

## Coordination

We will credit reporters in the release notes for the fix unless you
ask us not to. Please do not run automated scanners against services
we do not operate; the repository, releases, and wheels are the only
public surfaces to report against.
