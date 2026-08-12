# syntax=docker/dockerfile:1

FROM node:22-bookworm-slim AS web-builder
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.24-bookworm AS go-builder
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web-builder /src/web/dist ./web/dist
RUN mkdir -p /out/downloads /out/web \
    && cp -R web/dist/. /out/web/ \
    && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/downloads/mizupanel-agent-linux-amd64 ./cmd/agent \
    && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /out/downloads/mizupanel-agent-linux-arm64 ./cmd/agent \
    && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o /out/downloads/mizupanel-agent-windows-amd64.exe ./cmd/agent \
    && CGO_ENABLED=1 GOOS=linux GOARCH=${TARGETARCH} go build -o /out/mizupanel-server ./cmd/server

FROM debian:bookworm-slim AS runtime
WORKDIR /app
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /app/data
COPY --from=go-builder /out/mizupanel-server /app/mizupanel-server
COPY --from=go-builder /out/web /app/web
COPY --from=go-builder /out/downloads /app/downloads
COPY VERSION /app/VERSION
COPY docker/server.sqlite.yaml /app/server.yaml
COPY docker/server.mysql.yaml /app/server.mysql.yaml
EXPOSE 8080
VOLUME ["/app/data"]
CMD ["/app/mizupanel-server", "--config", "/app/server.yaml"]
