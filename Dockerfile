# ---- Build stage ----------------------------------------------------------
# 必须与 go.mod 的 `toolchain go1.27.3` 一致：stdlib 漏洞（如 GO-2026-6809
# crypto/x509）只能靠工具链修，换依赖没用。GOTOOLCHAIN=local 让「镜像里的 Go
# 比 go.mod 要求更旧」在构建期直接失败，而不是偷偷下载另一个工具链。
FROM golang:1.27.3-alpine AS builder

ENV GOTOOLCHAIN=local

WORKDIR /src

# Cache module downloads.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
ARG COMMIT=unknown
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /out/server ./cmd/server \
 && CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /out/admin ./cmd/admin

# ---- Runtime stage --------------------------------------------------------
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 app

WORKDIR /app
COPY --from=builder /out/server /usr/local/bin/server
# admin 用于容器内 bootstrap 管理员：docker compose exec app /usr/local/bin/admin seed --email ...
COPY --from=builder /out/admin /usr/local/bin/admin
COPY configs/config.yaml /app/configs/config.yaml

USER app
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/server"]
