# go-web-template 常用开发命令
# 用法: make <target>   例如: make dev / make check / make migrate-up

APP_NAME   := go-web-template
BIN        := bin/$(APP_NAME)
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS    := -X main.version=$(VERSION) -X main.commit=$(COMMIT)
GO         ?= go
TOOL_BIN   := $(shell $(GO) env GOPATH)/bin
# 非登录 shell（agent / 本地 CI 风格调用）通常没有 $GOPATH/bin：显式注入，
# 让 make check 在任何 shell 都能找到 pin 版本工具，而不是误报“缺少工具”。
export PATH := $(TOOL_BIN):$(PATH)
# 注意：macOS 自带 make 3.81 的 direct-exec 不会应用上面的 export PATH，
# 所以真正被 make 直接执行的工具必须解析成绝对路径（否则报 No such file or directory）。
GOLANGCI_LINT ?= $(shell [ -x "$(TOOL_BIN)/golangci-lint" ] && echo "$(TOOL_BIN)/golangci-lint" || command -v golangci-lint || echo golangci-lint)
AIR           ?= $(shell [ -x "$(TOOL_BIN)/air" ] && echo "$(TOOL_BIN)/air" || command -v air || echo air)
GOVULNCHECK   ?= $(shell [ -x "$(TOOL_BIN)/govulncheck" ] && echo "$(TOOL_BIN)/govulncheck" || command -v govulncheck || echo govulncheck)
# 覆盖率统计范围：默认 package 归属会让「被集成测试覆盖的 handler/routes」显示 0%，
# 合并 internal/... 才能看到真实覆盖率。
COVERPKG   ?= ./internal/...
# 覆盖率下限（总量，口径 = COVERPKG）：低于它就当回归，不接受「数字看着还行」。
# 抬高时只改这里；不要为了让红灯变绿而降低阈值。
COVER_MIN  ?= 78
# 每包下限 + 关键文件「零覆盖函数」规则见 scripts/cover-check.sh：
# 总量会被集成测试拉高，掩盖单个包整体腐烂；阈值调高同样只改这里。
COVER_PKG_MIN ?= 55
MIGRATE_URL ?= postgres://postgres:postgres@127.0.0.1:5432/go_web_template?sslmode=disable

# 工具版本：Makefile 是唯一来源，升级时只改这里
GOLANGCI_LINT_VERSION ?= v2.15.0
AIR_VERSION           ?= v1.67.4
GOVULNCHECK_VERSION   ?= v1.9.0
MIGRATE_VERSION       ?= v4.20.1

.PHONY: help tools golangci-lint-tool air-tool govulncheck-tool migrate-tool \
        build run dev test test-race coverage coverage-func cover-check vet lint fmt fmt-check tidy \
        check gate openapi tidy-check vuln audit smoke verify-auth seed-admin seed-demo db-up bench fuzz deps-outdated \
        ci web-install web-deps e2e \
        migrate-up migrate-down migrate-create migrate-roundtrip \
        web-check docker-build image-scan sbom jwt-secret docker-up docker-down docker-logs clean

help: ## 显示所有命令
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

## ---- 工具链（版本 pin，缺失时先 make tools） --------------------------------

tools: golangci-lint-tool air-tool govulncheck-tool migrate-tool ## 安装/升级全部 pin 版本工具
	@echo "工具安装目录: $$($(GO) env GOPATH)/bin"

golangci-lint-tool: ## 安装 golangci-lint（v2 配置）
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

air-tool: ## 安装 air（热重载）
	$(GO) install github.com/air-verse/air@$(AIR_VERSION)

govulncheck-tool: ## 安装 govulncheck
	$(GO) install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

migrate-tool: ## 安装 golang-migrate（含 postgres 驱动，供无 docker 环境使用）
	$(GO) install -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@$(MIGRATE_VERSION)

## ---- 构建 / 运行 -----------------------------------------------------------

build: ## 编译二进制到 bin/（server + admin）
	$(GO) build -trimpath -ldflags "-s -w $(LDFLAGS)" -o $(BIN) ./cmd/server
	$(GO) build -trimpath -ldflags "-s -w $(LDFLAGS)" -o bin/$(APP_NAME)-admin ./cmd/admin

# 本地走 http：refresh cookie 不能带 Secure（否则浏览器不回传，刷新页面就掉线）。
# configs/config.yaml 保持 cookie_secure: true 以便镜像进生产时默认安全，
# 本地开发由这里和 scripts/dev.sh 显式注入 false。
LOCAL_HTTP_ENV := APP_AUTH_COOKIE_SECURE=false

run: ## 直接运行（需先启动 postgres）
	$(LOCAL_HTTP_ENV) $(GO) run ./cmd/server

dev: ## 开发模式：air 热重载
	@./scripts/check-tools.sh air
	$(LOCAL_HTTP_ENV) $(AIR) -c .air.toml

smoke: ## 本地冒烟：自起自停服务，验证探针 + 登录 + CRUD（DB 不可用时只验 liveness）
	@./scripts/smoke.sh

verify-auth: ## 认证端到端验证：宽限期/登出即时/body 传输/会话上限（需 postgres）
	@./scripts/verify-auth.sh

seed-admin: ## 建/重置管理员: EMAIL=admin@example.com APP_SEED_PASSWORD='...' make seed-admin
	@test -n "$(EMAIL)" || { echo "用法: EMAIL=admin@example.com APP_SEED_PASSWORD='...' make seed-admin"; exit 1; }
	@test -n "$$APP_SEED_PASSWORD" || { echo "缺少 APP_SEED_PASSWORD 环境变量"; exit 1; }
	$(GO) run ./cmd/admin seed --email "$(EMAIL)" --name "$(or $(NAME),Administrator)"

seed-demo: ## 批量创建测试假数据: USERS=20 MESSAGES=10 PREFIX=seed make seed-demo
	$(GO) run ./cmd/seeding --config configs/config.yaml \
		--prefix "$(or $(PREFIX),seed)" \
		--users $(or $(USERS),20) \
		--messages $(or $(MESSAGES),10) \
		--max-recipients $(or $(MAX_RECIPIENTS),5) \
		--admin-every $(or $(ADMIN_EVERY),0)

db-up: ## 起本地 postgres（有 docker 走 compose，否则检查本机 postgres）
	@if command -v docker >/dev/null 2>&1; then \
		docker compose -f deploy/docker-compose.yml up -d postgres; \
		echo "等待 postgres healthy..."; \
		for _ in $$(seq 60); do \
			docker compose -f deploy/docker-compose.yml exec -T postgres pg_isready -U postgres -d go_web_template >/dev/null 2>&1 && break; \
			sleep 1; \
		done; \
		echo "postgres ready"; \
	elif command -v pg_isready >/dev/null 2>&1; then \
		pg_isready -h 127.0.0.1 -p 5432 >/dev/null 2>&1 \
			&& echo "本机 postgres 已就绪" \
			|| { echo "本机有 postgres 但未运行：brew services start postgresql@16"; exit 1; }; \
	else \
		echo "既无 docker 也无本机 postgres：装 Docker Desktop 或 brew install postgresql@16"; exit 1; \
	fi

## ---- 质量门（提交前唯一入口） ----------------------------------------------

check: lint gate ## 提交前唯一门槛 = lint + gate

# 全量门：三层门里除「需要真实 PG」的两项（smoke / verify-auth）之外的全部。
# 存在的理由：check 不包含前端，web-check 与 audit 靠人记，结果就是前端回归
# 只在本地某次手动调用时才被发现。本地提交前 / 接入 CI 都只需调这一个目标。
ci: check web-check audit ## 全量门（本地/CI 唯一入口）= check + web-check + audit

gate: fmt-check vet test-race cover-check build tidy-check ## 本地校验链（不含 golangci-lint）

fmt-check: ## 校验 gofmt（docs/ 含生成物与 Markdown，豁免）
	@unformatted=$$(gofmt -l . | grep -v '^docs/' || true); \
	if [ -n "$$unformatted" ]; then \
		echo "以下文件未格式化（运行 make fmt）:"; echo "$$unformatted"; exit 1; \
	fi
	@echo "fmt-check: ok"

vet: ## 静态检查
	$(GO) vet ./...

lint: ## golangci-lint（配置见 .golangci.yml，v2 格式）
	@./scripts/check-tools.sh golangci-lint
	$(GOLANGCI_LINT) run

fmt: ## 格式化代码（gofmt + goimports，走 golangci-lint 的 formatters）
	@./scripts/check-tools.sh golangci-lint
	$(GOLANGCI_LINT) fmt

tidy: ## 整理依赖
	$(GO) mod tidy

test: ## 运行全部测试
	$(GO) test ./... -count=1

deps-outdated: ## 列出可升级依赖（Go + 前端；只读，不自动升级）
	@echo "== Go =="
	@$(GO) list -m -u -f '{{if and (not .Indirect) .Update}}{{.Path}} {{.Version}} -> {{.Update.Version}}{{end}}' all | sed '/^$$/d'
	@echo "== web/admin =="
	@cd web/admin && pnpm outdated || true

bench: ## 基准测试（-benchmem；Benchmark* 在 internal/... 各包）
	$(GO) test ./internal/... -run '^$$' -bench . -benchmem

FUZZTIME ?= 30s
fuzz: ## 模糊测试（默认 FuzzTokenParse，FUZZTIME=30s 可调；加了新 Fuzz 目标就改这里）
	$(GO) test ./internal/platform/authkit -run '^$$' -fuzz=FuzzTokenParse -fuzztime=$(FUZZTIME)

test-race: ## 带竞态检测运行测试（make check 同款）
	$(GO) test -race ./... -count=1

coverage: ## 生成 HTML 覆盖率报告（会打开浏览器）
	$(GO) test ./... -coverpkg=$(COVERPKG) -coverprofile=coverage.out
	$(GO) tool cover -html=coverage.out

coverage-func: ## 覆盖率文本摘要（agent/人类友好，不开浏览器）
	$(GO) test ./... -coverpkg=$(COVERPKG) -coverprofile=coverage.out >/dev/null
	$(GO) tool cover -func=coverage.out

cover-check: ## 覆盖率门：总量 + 每包下限 + 关键文件零覆盖函数（scripts/cover-check.sh）
	$(GO) test ./... -count=1 -coverpkg=$(COVERPKG) -coverprofile=coverage.out >/dev/null
	@COVER_MIN=$(COVER_MIN) COVER_PKG_MIN=$(COVER_PKG_MIN) ./scripts/cover-check.sh

openapi: ## 重新生成 OpenAPI 契约（改了路由元数据后必须跑）
	$(GO) test ./internal/app -run TestOpenAPIGolden -update

tidy-check: ## 校验 go.mod/go.sum 已 tidy（未 tidy 时自动整理并提示提交）
	@tmp=$$(mktemp -d) && cp go.mod go.sum "$$tmp"/ && \
	$(GO) mod tidy >/dev/null && \
	if cmp -s go.mod "$$tmp/go.mod" && cmp -s go.sum "$$tmp/go.sum"; then \
		rm -rf "$$tmp"; echo "tidy-check: ok"; \
	else \
		rm -rf "$$tmp"; echo "go.mod/go.sum 未 tidy：已自动整理，请提交 go.mod/go.sum"; \
		git diff --stat -- go.mod go.sum; exit 1; \
	fi

vuln: ## 依赖漏洞扫描
	@./scripts/check-tools.sh govulncheck
	$(GOVULNCHECK) ./...

web-install: ## 安装前端依赖（frozen lockfile；新机器/CI 用）
	cd web/admin && pnpm install --frozen-lockfile

web-deps: ## 内部前置：确认前端依赖已安装（否则给出可执行的提示）
	@test -x web/admin/node_modules/.bin/eslint || { \
		echo "web/admin 依赖未安装：先运行 make web-install"; exit 1; \
	}

e2e: web-deps ## 前端端到端冒烟（需后端 :8080 + 已 seed 管理员；首次 pnpm exec playwright install chromium）
	cd web/admin && pnpm test:e2e

web-check: web-deps ## 前端门：eslint + vue-tsc + vitest + API/组件类型未漂移 + 构建（改 web/admin 时跑）
	cd web/admin && pnpm lint && pnpm typecheck && pnpm test:coverage && pnpm api:check && pnpm build
	@git diff --exit-code --quiet -- web/admin/src/components.d.ts web/admin/src/auto-imports.d.ts || { \
		echo "自动导入类型漂移：提交 web/admin/src/{components,auto-imports}.d.ts"; exit 1; \
	}

audit: ## 安全/SQL 静态审计 + 鉴权矩阵回归（auth/迁移改动、发布前跑）
	@./scripts/audit.sh

## ---- 数据库迁移（优先 docker；无 docker 用本地 migrate 二进制） ------------

migrate-up: ## 应用全部迁移
	@if command -v docker >/dev/null 2>&1; then \
		docker run --rm -v $(PWD)/migrations:/migrations --network host migrate/migrate \
			-path=/migrations -database '$(MIGRATE_URL)' up; \
	elif command -v migrate >/dev/null 2>&1; then \
		migrate -path migrations -database '$(MIGRATE_URL)' up; \
	else \
		echo "需要 docker 或本地 migrate 二进制；无 docker 时运行 make migrate-tool"; exit 1; \
	fi

migrate-roundtrip: ## 迁移可逆性：scratch 库 up → down 全部 → up（先 make db-up；不碰开发库）
	@./scripts/migrate-roundtrip.sh

migrate-down: ## 回滚最近 1 个迁移（破坏性，先确认）
	@if command -v docker >/dev/null 2>&1; then \
		docker run --rm -v $(PWD)/migrations:/migrations --network host migrate/migrate \
			-path=/migrations -database '$(MIGRATE_URL)' down 1; \
	elif command -v migrate >/dev/null 2>&1; then \
		migrate -path migrations -database '$(MIGRATE_URL)' down 1; \
	else \
		echo "需要 docker 或本地 migrate 二进制；无 docker 时运行 make migrate-tool"; exit 1; \
	fi

migrate-create: ## 新建迁移: make migrate-create NAME=add_posts_table
	@test -n "$(NAME)" || { echo "用法: make migrate-create NAME=add_posts_table"; exit 1; }
	@if command -v docker >/dev/null 2>&1; then \
		docker run --rm -v $(PWD)/migrations:/migrations migrate/migrate create \
			-dir /migrations -ext sql -seq $(NAME); \
	elif command -v migrate >/dev/null 2>&1; then \
		migrate create -dir migrations -ext sql -seq $(NAME); \
	else \
		echo "需要 docker 或本地 migrate 二进制；无 docker 时运行 make migrate-tool"; exit 1; \
	fi

## ---- 容器 ----------------------------------------------------------------

# 本地编排用的 JWT 密钥：首次生成、之后复用；删除文件即轮换（所有 token 失效）。
# 不在仓库里写死默认值：仓库自带的默认密钥 = 公开密钥，等于没有认证。
JWT_SECRET_FILE ?= .local/jwt-secret

jwt-secret: ## 生成/复用本地编排的 JWT 密钥（.local/jwt-secret，gitignored）
	@mkdir -p $(dir $(JWT_SECRET_FILE))
	@test -s $(JWT_SECRET_FILE) || { umask 077; openssl rand -base64 48 | tr -d '\n' > $(JWT_SECRET_FILE); echo "已生成 $(JWT_SECRET_FILE)（本地编排用，勿提交；删除即轮换）"; }

docker-build: ## 构建镜像
	docker build -t $(APP_NAME):$(VERSION) .

sbom: docker-build ## 生成 CycloneDX SBOM（trivy，需要网络）
	@mkdir -p dist
	docker run --rm -v /var/run/docker.sock:/var/run/docker.sock $(TRIVY_ENV) \
		-v "$(PWD)/dist:/out" $(TRIVY_IMAGE) \
		image --format cyclonedx --output /out/sbom.cdx.json $(APP_NAME):$(VERSION)
	@echo "SBOM: dist/sbom.cdx.json（$$(du -h dist/sbom.cdx.json | cut -f1)）"

# 镜像漏洞扫描：trivy 跑在容器里，不需要本机安装。首次运行会拉镜像（需要网络）。
# 只卡 HIGH/CRITICAL（--exit-code 1）；base 镜像 digest pin 与镜像签名不在本模板范围。
TRIVY_IMAGE ?= aquasec/trivy:0.61.0
# trivy 需要联网拉 vuln DB；-e VAR 形式只在调用方设置了该变量时透传，兼容公司代理。
# 注意容器里的 127.0.0.1 不是宿主机：Docker Desktop 下应传 host.docker.internal:7890。
TRIVY_ENV := -e HTTP_PROXY -e HTTPS_PROXY -e NO_PROXY

image-scan: docker-build ## 扫描镜像 HIGH/CRITICAL 漏洞（trivy，需要网络）
	docker run --rm -v /var/run/docker.sock:/var/run/docker.sock $(TRIVY_ENV) $(TRIVY_IMAGE) \
		image --scanners vuln --severity HIGH,CRITICAL --ignore-unfixed --exit-code 1 $(APP_NAME):$(VERSION)

docker-up: jwt-secret ## 一键启动 postgres + redis + migrate + app（自动注入 JWT 密钥）
	# 本地是 http：compose 默认 Secure=true 会让浏览器不回传 refresh cookie，这里显式关掉。
	APP_AUTH_JWT_SECRET="$$(cat $(JWT_SECRET_FILE))" APP_AUTH_COOKIE_SECURE=false \
		docker compose -f deploy/docker-compose.yml up -d --build

docker-down: ## 停止并移除容器（保留数据卷）
	docker compose -f deploy/docker-compose.yml down

docker-logs: ## 查看应用日志
	docker compose -f deploy/docker-compose.yml logs -f app

clean: ## 清理构建产物
	rm -rf bin coverage.out
