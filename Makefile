# go-web-template 常用开发命令
# 用法: make <target>   例如: make dev / make test / make migrate-up

APP_NAME   := go-web-template
BIN        := bin/$(APP_NAME)
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS    := -X main.version=$(VERSION) -X main.commit=$(COMMIT)
GO         ?= go
MIGRATE_URL ?= postgres://postgres:postgres@127.0.0.1:5432/go_web_template?sslmode=disable

.PHONY: help build run dev test test-race coverage vet lint fmt tidy \
        swagger migrate-up migrate-down migrate-create \
        docker-build docker-up docker-down docker-logs clean

help: ## 显示所有命令
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

build: ## 编译二进制到 bin/
	$(GO) build -trimpath -ldflags "-s -w $(LDFLAGS)" -o $(BIN) ./cmd/server

run: ## 直接运行（需先启动 postgres）
	$(GO) run ./cmd/server

dev: ## 开发模式：air 热重载（需先安装 air: go install github.com/air-verse/air@latest）
	air -c .air.toml

test: ## 运行全部测试
	$(GO) test ./... -count=1

test-race: ## 带竞态检测运行测试
	$(GO) test -race ./... -count=1

coverage: ## 生成覆盖率报告并打开
	$(GO) test ./... -coverprofile=coverage.out
	$(GO) tool cover -html=coverage.out

vet: ## 静态检查
	$(GO) vet ./...

lint: ## golangci-lint（需先安装: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest）
	golangci-lint run

fmt: ## 格式化代码
	gofmt -w .
	goimports -w .

tidy: ## 整理依赖
	$(GO) mod tidy

swagger: ## 重新生成 Swagger 文档（需先安装: go install github.com/swaggo/swag/cmd/swag@latest）
	swag init -g cmd/server/main.go -o docs --parseDependency --parseInternal

migrate-up: ## 执行全部迁移（需本地起 postgres，或用 docker compose run --rm migrate）
	docker run --rm -v $(PWD)/migrations:/migrations --network host migrate/migrate \
		-path=/migrations -database '$(MIGRATE_URL)' up

migrate-down: ## 回滚一个迁移
	docker run --rm -v $(PWD)/migrations:/migrations --network host migrate/migrate \
		-path=/migrations -database '$(MIGRATE_URL)' down 1

migrate-create: ## 新建迁移: make migrate-create NAME=add_posts_table
	docker run --rm -v $(PWD)/migrations:/migrations migrate/migrate create \
		-dir /migrations -ext sql -seq $(NAME)

docker-build: ## 构建镜像
	docker build -t $(APP_NAME):$(VERSION) .

docker-up: ## 一键启动 postgres + migrate + app
	docker compose -f deploy/docker-compose.yml up -d --build

docker-down: ## 停止并移除容器（保留数据卷）
	docker compose -f deploy/docker-compose.yml down

docker-logs: ## 查看应用日志
	docker compose -f deploy/docker-compose.yml logs -f app

clean: ## 清理构建产物
	rm -rf bin coverage.out
