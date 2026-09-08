# Security evidence

The `results` directory records reproducible dependency and toolchain checks
used for release review. Results are evidence for the scanned revision and the
vulnerability database timestamp in the transcript; they are not a general
security certification.

The scanner does not execute checkout content, Git hooks, package managers, or
the Git executable. When scanning hostile input, account for these operational limits:

- go-git can spend memory and CPU decoding Git trees and delta chains before
  Dircue's file and tree limits take effect;
- filesystem scans require a stable checkout because preflight and content
  reads are not one atomic snapshot;
- legacy text output preserves control characters in Git-valid filenames, so
  automated consumers should use JSON;
- static vulnerability reachability does not prove that every runtime path is
  safe, and imported-but-unreached advisories remain visible in the transcript.

Use operating-system or container memory, CPU, and wall-clock limits for
untrusted Git object stores. `--source=directory` avoids Git object decoding for
an extracted source tree.
