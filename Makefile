# GoCMS Makefile
# 用法：make help

APP      := gocms
CMD      := ./cmd/gocms
PORT     ?= 8090
GOFMT    := $(shell go env GOROOT)/bin/gofmt
LDFLAGS  := -s -w
UNAME    := $(shell uname -s)

# 集成测试需要 MySQL：仓储测试清空该库；后台写路径测试自动派生 <库>_writeflow 库
TEST_DSN ?= root:123456@tcp(127.0.0.1:3306)/gocms_test?parseTime=true&charset=utf8mb4&loc=Local

.PHONY: help run build build-linux assets assets-watch fmt vet tidy clean start stop restart logs check sign test test-db test-cover backup restore

help: ## 显示所有可用命令
	@echo "GoCMS 常用命令："
	@grep -E '^[a-zA-Z_-]+:.*## .*$$' $(MAKEFILE_LIST) | awk -F ':.*## ' '{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

run: ## 开发模式运行（模板热重载）
	go run $(CMD)

build: ## 编译当前平台（macOS 自动补 ad-hoc 签名）
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(APP) $(CMD)
	@if [ "$(UNAME)" = "Darwin" ]; then codesign --force --sign - ./$(APP) 2>/dev/null && echo "✓ 已签名"; fi

build-linux: ## 交叉编译 Linux amd64（用于服务器部署）
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(APP)-linux-amd64 $(CMD)

sign: ## macOS：给已有二进制补 ad-hoc 签名（报 Code Signature Invalid 时用）
	@if [ "$(UNAME)" = "Darwin" ]; then codesign --force --sign - ./$(APP) && echo "✓ 已签名"; \
	else echo "仅 macOS 需要"; fi

assets: ## 构建后台样式（Tailwind → web/admin/static/app.css）
	@test -d node_modules || npm install
	npm run build
	@echo "✓ 样式已构建：web/admin/static/app.css"

assets-watch: ## 监听模板变化并自动重建样式
	@test -d node_modules || npm install
	npm run dev

fmt: ## 格式化代码
	$(GOFMT) -l -w ./cmd ./internal

vet: ## 静态检查
	go vet ./...

tidy: ## 整理依赖
	go mod tidy

check: fmt vet ## 格式化 + 静态检查
	@echo "✓ 检查通过"

test: ## 跑单元测试（仓储集成测试会自动跳过）
	go test ./... -count=1

test-db: ## 跑全部测试，含仓储与后台写路径集成测试（需要 MySQL，测试库会被清空）
	GOCMS_TEST_DSN='$(TEST_DSN)' go test ./... -count=1 -v

test-cover: ## 生成覆盖率报告
	go test ./internal/... -count=1 -coverprofile=coverage.out
	@go tool cover -func=coverage.out | tail -1
	@echo "  明细：go tool cover -html=coverage.out -o coverage.html"

backup: ## 备份数据库与 uploads 到 backups/（凭据读 .env）
	bash scripts/backup.sh

restore: ## 恢复备份（会覆盖！）：make restore F=backups/gocms-<时间戳>.tar.gz
	@test -n "$(F)" || { echo "用法: make restore F=backups/gocms-<时间戳>.tar.gz"; exit 1; }
	bash scripts/restore.sh $(F)

clean: ## 清理编译产物
	rm -f $(APP) $(APP)-linux-amd64

start: build ## 后台启动
	@mkdir -p .run
	@if [ -f .run/$(APP).pid ] && kill -0 $$(cat .run/$(APP).pid) 2>/dev/null; then \
		echo "已在运行，PID=$$(cat .run/$(APP).pid)"; \
	else \
		nohup ./$(APP) > .run/$(APP).log 2>&1 & echo $$! > .run/$(APP).pid; \
		sleep 1; echo "已启动 PID=$$(cat .run/$(APP).pid)  http://localhost:$(PORT)"; \
	fi

stop: ## 停止后台进程
	@if [ -f .run/$(APP).pid ]; then \
		kill $$(cat .run/$(APP).pid) 2>/dev/null && echo "已停止"; \
		rm -f .run/$(APP).pid; \
	else echo "未在运行"; fi

restart: stop start ## 重启

logs: ## 查看运行日志
	@tail -f .run/$(APP).log
