# Security policy

Dircue profiles unfamiliar directories, which could contain hostile inputs.

Please report any defects that let inspected content escape the documented analysis boundaries, expose credentials, or exhaust resources unexpectedly.

## Reporting a vulnerability

- Preferably, use GitHub's [private vulnerability reporting](https://github.com/war-and-code/dircue/security/advisories/new).
- Otherwise, please open an issue requesting a private contact channel without including vulnerability details.
    - Do not attach credentials, private source code, or sensitive reports to a public issue.
- Include the dircue version, operating system, exact command, expected and observed behavior, and a minimal reproduction if it can be shared.
- Maintainers will coordinate a fix and disclosure with the reporter.
    - Response times and backports are not guaranteed; fixes normally target the latest release.

## Analysis boundaries

The built-in profilers work offline and do not execute inspected project code. Directory reads use containment checks; Git profiling reads the selected tree and the repository metadata and object storage needed to resolve it. Explicit caller inputs such as saved reports, rules, and worker executables may be outside the profiled directory. Those paths are not implicitly trusted because they happen to sit inside a repository.

Optional structural analysis executes the worker selected by the caller. It is not an operating-system sandbox: the worker inherits process environment and working directory. Use a trusted worker and enforce process limits externally when inspecting untrusted content. Application read bounds and worker timeouts are not hard operating-system memory or CPU limits.

Reports can contain source paths, project names, package coordinates, and other repository metadata. Supported credential fields are redacted, but reports are not guaranteed free of confidential information. Review them before sharing.

Useful reports include:

- Reads that escape documented directory containment or resolve references from a saved report into unintended filesystem or network access.
- Execution of inspected content, including build scripts or commands inferred from project declarations.
- Credential disclosure through a supported parser or diagnostic.
- Unexpected unbounded resource use, deadlocks, or worker hangs.
- Incorrect checksums, executable identities, or dependency notices in released archives and wheels.

Ordinary classification differences, missing ecosystem support, and cosmetic issues belong in GitHub Issues unless they enable a concrete boundary violation.

Third-party worker behavior is outside dircue's guarantees; defects in how dircue validates and handles worker responses are within scope.
