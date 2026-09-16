# The build fetches pinned dependencies. Running the resulting worker is offline.
FROM rust:1.94.0-slim-bookworm@sha256:a86cada82e36ebd7a9bffed7548792c55a952fdb20718eea9278a936bcb76e62 AS build
RUN apt-get update && apt-get install -y --no-install-recommends gcc libc6-dev && rm -rf /var/lib/apt/lists/*
WORKDIR /build
COPY worker/Cargo.toml worker/Cargo.lock ./
COPY worker/src ./src
RUN cargo build --locked --release
RUN mkdir -p /notices/source && cp Cargo.lock /notices/Cargo.lock && \
    for package in /usr/local/cargo/registry/src/*/*; do \
      for notice in "$package"/LICENSE* "$package"/COPYING* "$package"/NOTICE*; do \
        if [ -f "$notice" ]; then \
          mkdir -p "/notices/$(basename "$package")"; \
          cp "$notice" "/notices/$(basename "$package")/"; \
        fi; \
      done; \
    done && \
    cp /usr/local/cargo/registry/cache/*/big-code-analysis-2.2.0.crate /notices/source/

FROM debian:bookworm-slim@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171
COPY --from=build /build/target/release/dircue-structural-worker /usr/local/bin/dircue-structural-worker
COPY --from=build /notices /usr/share/dircue-structural/notices
COPY LICENSE /usr/share/dircue-structural/LICENSE
USER 65534:65534
ENTRYPOINT ["/usr/local/bin/dircue-structural-worker"]
