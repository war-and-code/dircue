FROM golang:1.26.6-bookworm@sha256:116d58cbd88c1297624acc6e967a060012422bacf9930927e23fb719189c6f36 AS build
ARG VERSION=0.2.0
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags="-s -w -X dircue/internal/cli.Version=${VERSION}" -o /out/dircue .

FROM scratch
COPY --from=build /out/dircue /usr/local/bin/dircue
COPY --from=build /src/LICENSE /src/THIRD_PARTY_NOTICES.md /licenses/
USER 65532:65532
WORKDIR /repo
ENTRYPOINT ["/usr/local/bin/dircue"]
