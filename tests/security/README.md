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
- legacy text renderers escape control characters in filenames; automated
  consumers should still use JSON to retain exact path values;
- static vulnerability reachability does not prove that every runtime path is
  safe, and imported-but-unreached advisories remain visible in the transcript.

Use operating-system or container memory, CPU, and wall-clock limits for
untrusted Git object stores. `--source=directory` avoids Git object decoding for
an extracted source tree.

The September 7 transcript predates the auragaze-to-dircue rename and is retained as historical evidence. It is not a scan of the current source. Current advisory checks and their database timestamps must be evaluated independently.
