# syntax=docker/dockerfile:1

ARG WEB_ASSETS_STAGE=web-builder

FROM --platform=$BUILDPLATFORM node:22-bookworm-slim AS web-builder
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY web/ ./
RUN npm run build

FROM scratch AS prebuilt-web
COPY web/dist /src/web/dist

FROM ${WEB_ASSETS_STAGE} AS web-assets

FROM --platform=$BUILDPLATFORM golang:1.24-bookworm AS go-deps
WORKDIR /src
COPY go.mod go.sum ./
# Keep modules in a cached layer so ephemeral CI runners can reuse them.
RUN go mod download

FROM go-deps AS go-source
COPY . .
COPY --from=web-assets /src/web/dist ./web/dist

FROM go-source AS agent-builder
RUN --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p /out/downloads \
    && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/downloads/mizupanel-agent-linux-amd64 ./cmd/agent \
    && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /out/downloads/mizupanel-agent-linux-arm64 ./cmd/agent \
    && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o /out/downloads/mizupanel-agent-windows-amd64.exe ./cmd/agent

FROM go-source AS server-builder
ARG BUILDARCH
ARG TARGETOS
ARG TARGETARCH
# On a native runner BUILDARCH == TARGETARCH, so CGO links with the system gcc
# and no emulation or cross toolchain is needed. The arm64 cross compiler is
# only pulled in for cross/emulated builds (e.g. an arm64 image built on an
# amd64 host).
RUN --mount=type=cache,target=/root/.cache/go-build \
    CC=gcc; \
    if [ "$TARGETARCH" = "arm64" ] && [ "$BUILDARCH" != "arm64" ]; then \
      apt-get update \
      && apt-get install -y --no-install-recommends gcc-aarch64-linux-gnu libc6-dev-arm64-cross \
      && rm -rf /var/lib/apt/lists/*; \
      CC=aarch64-linux-gnu-gcc; \
    fi \
    && CGO_ENABLED=1 GOOS="$TARGETOS" GOARCH="$TARGETARCH" CC="$CC" \
      go build -o /out/mizupanel-server ./cmd/server

FROM debian:bookworm-slim AS runtime
WORKDIR /app
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /app/data
COPY --from=server-builder /out/mizupanel-server /app/mizupanel-server
COPY --from=web-assets /src/web/dist /app/web
COPY --from=agent-builder /out/downloads /app/downloads
COPY VERSION /app/VERSION
COPY docker/server.sqlite.yaml /app/server.yaml
COPY docker/server.mysql.yaml /app/server.mysql.yaml
EXPOSE 8080
VOLUME ["/app/data"]
CMD ["/app/mizupanel-server", "--config", "/app/server.yaml"]
