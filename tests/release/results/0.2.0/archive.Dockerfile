FROM scratch
COPY --chmod=0555 dircue /usr/local/bin/dircue
USER 65532:65532
WORKDIR /repo
ENTRYPOINT ["/usr/local/bin/dircue"]
