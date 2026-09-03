FROM golang:1.26-bookworm AS build
ARG VERSION=0.0.0-dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w -X github.com/laminara/laminara/server/internal/version.Current=${VERSION}" \
    -o /out/laminara-server ./server/cmd/laminara-server

FROM debian:12-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
RUN useradd --system --uid 10001 --home-dir /var/lib/laminara --shell /usr/sbin/nologin laminara \
    && install -d -o laminara -g laminara /var/lib/laminara /run/laminara
COPY --from=build /out/laminara-server /usr/local/bin/laminara-server
ENV XDG_RUNTIME_DIR=/run/laminara
VOLUME ["/var/lib/laminara"]
USER laminara
ENTRYPOINT ["/usr/local/bin/laminara-server"]
CMD ["start", "--config", "/etc/laminara/config.json"]
