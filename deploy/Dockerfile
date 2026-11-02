FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS build
ARG VERSION=0.0.0-dev
ARG BUF_VERSION=1.75.0
ARG TARGETOS=linux
ARG TARGETARCH=amd64
WORKDIR /src
COPY go.mod go.sum ./
COPY sdk/go/go.mod sdk/go/go.sum ./sdk/go/
RUN go mod download
RUN go install github.com/bufbuild/buf/cmd/buf@v${BUF_VERSION}
COPY buf.yaml buf.gen.yaml buf.gen.sdk.yaml ./
COPY proto ./proto
COPY sdk/go ./sdk/go
RUN buf generate && buf generate --template buf.gen.sdk.yaml
COPY server ./server
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
    -ldflags="-s -w -X github.com/laminara/laminara/server/internal/version.Current=${VERSION}" \
    -o /out/laminara-server ./server/cmd/laminara-server

FROM debian:12-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
RUN useradd --system --uid 10001 --home-dir /var/lib/laminara --shell /usr/sbin/nologin laminara \
    && install -d -o laminara -g laminara /var/lib/laminara /run/laminara
COPY --from=build /out/laminara-server /usr/local/bin/laminara-server
VOLUME ["/var/lib/laminara"]
USER laminara
ENV XDG_RUNTIME_DIR=/run/laminara
WORKDIR /var/lib/laminara
EXPOSE 8099
HEALTHCHECK --interval=30s --timeout=12s --start-period=30s CMD ["laminara-server", "health"]
ENTRYPOINT ["/usr/local/bin/laminara-server"]
CMD ["start", "--config", "/etc/laminara/config.json", "--config-mode", "external"]
