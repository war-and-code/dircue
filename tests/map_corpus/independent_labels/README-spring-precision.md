# Independent labels: Spring Boot Docker `complete/`

`gs-spring-boot-docker-complete.json` labels the `complete/` subtree of
`spring-guides/gs-spring-boot-docker` at commit
`8f42f5812e8b62bc31b092a8767a5073bbc786e0`. The oracle root is that subtree,
so paths in the JSON are relative to `complete/`.

The source boundary is the full set of 18 tracked files in that subtree. The
JSON records each path and SHA256 of its raw bytes. Every labeled fact gives
its source path and line in `why`. Text source and configuration files were
read directly; wrapper artifacts and scripts are included in the file census
so the source boundary is explicit.

The labels support exhaustive precision denominators for **components** and
**deployables** within this subtree. It contains two overlapping declarations
for the same Java source tree (Maven and Gradle), and one Dockerfile. There are
no other supported deployment declarations in the enumerated subtree. The
other categories are `targeted_recall_only`: interface ownership is ambiguous
because both projects share a root, the Maven wrapper contains a second Java
`main`, and capability/relationship recognizers are bounded. Their absence is
not a negative label.

`application.yml` sets port 8080, but the public MAP vocabulary names declared
ports from Dockerfile `EXPOSE`, Compose, and Kubernetes. It is therefore
documented as source context, not labeled as a supported declared-port node.
Likewise, no HTTP operation label is asserted because MAP.md does not list
Spring controller mappings among its supported interface forms.

These labels were committed before any dircue execution. No implementation,
test, existing label, receipt, or map output was inspected.
