# syntax=docker/dockerfile:1.7

# ---- Build stage ----------------------------------------------------------
# 必须与 go.mod 的 `toolchain go1.27.3` 一致：stdlib 漏洞（如 GO-2026-6809
# crypto/x509）只能靠工具链修，换依赖没用。GOTOOLCHAIN=local 让「镜像里的 Go
# 比 go.mod 要求更旧」在构建期直接失败，而不是偷偷下载另一个工具链。
#
# 基础镜像按 digest 固定（tag 会漂移，digest 不会）。升级时先改 tag，再执行
#   docker buildx imagetools inspect golang:<tag> --format '{{.Manifest.Digest}}'
# 把新 digest 写回来；`make deps-outdated` 的月度批次里一起做。
FROM golang:1.27.3-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS builder

ENV GOTOOLCHAIN=local

WORKDIR /src

# Cache module downloads and the Go build cache across rebuilds (BuildKit).
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
ARG VERSION=dev
ARG COMMIT=unknown
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /out/server ./cmd/server \
 && CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /out/admin ./cmd/admin

# ---- Runtime stage --------------------------------------------------------
FROM alpine:3.21@sha256:ee16d92f96db08d9d2d07da48555a27f3a69d8f0759021dbc601195ed0d2b5bd

RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 app

WORKDIR /app
COPY --from=builder /out/server /usr/local/bin/server
# admin 用于容器内 bootstrap 管理员：docker compose exec app /usr/local/bin/admin seed --email ...
COPY --from=builder /out/admin /usr/local/bin/admin
COPY configs/config.yaml /app/configs/config.yaml

USER app
EXPOSE 8080

# 让 `docker run` 也有健康检查（compose 的 healthcheck 会覆盖它）。
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1
# 让 `docker stop` 走 SIGTERM → http.Server.Shutdown（ADR 14），而不是 SIGKILL。
STOPSIGNAL SIGTERM

ENTRYPOINT ["/usr/local/bin/server"]
