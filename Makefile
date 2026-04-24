# ohmysmsapp Makefile
# 约定：开发机 amd64，目标 aarch64（树莓派 5）
#   - make build            本地编译（amd64，用于本地调试）
#   - make build-arm64      交叉编译 aarch64 二进制
#   - make frontend-build   打包 Vue dist
#   - make deploy           完整流水线：前端 build → 后端交叉编译 → rsync → 重启 systemd
#
# 可覆盖：
#   PI_HOST=kimmy@172.16.84.233  部署目标
#   PI_DEST=/opt/ohmysmsapp       远端目录
#   PI_SERVICE=ohmysmsd            systemd 单元名
#   GOPROXY=https://goproxy.cn,direct

SHELL      := /bin/bash
VERSION    ?= $(shell git describe --tags --dirty --always 2>/dev/null || echo "dev")
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS    := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

PI_HOST    ?= kimmy@172.16.84.233
PI_DEST    ?= /opt/ohmysmsapp
PI_SERVICE ?= ohmysmsd

GO_MODULE_DIR := backend
FE_DIR        := frontend
DIST_DIR      := deploy/build

.PHONY: help
help:
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

# ---------- 后端 ----------

.PHONY: backend-deps
backend-deps: ## 下载 Go 依赖
	cd $(GO_MODULE_DIR) && go mod tidy

.PHONY: build
build: ## 本地编译后端（调试用）
	cd $(GO_MODULE_DIR) && go build -ldflags="$(LDFLAGS)" -o ../$(DIST_DIR)/ohmysmsd-local ./cmd/ohmysmsd

.PHONY: build-arm64
build-arm64: ## 交叉编译 aarch64（树莓派 5）
	@mkdir -p $(DIST_DIR)
	cd $(GO_MODULE_DIR) && \
		CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
		go build -trimpath -ldflags="$(LDFLAGS)" \
		-o ../$(DIST_DIR)/ohmysmsd ./cmd/ohmysmsd
	@ls -la $(DIST_DIR)/ohmysmsd

.PHONY: backend-run-local
backend-run-local: ## 本地跑后端（需存在 backend/config.yaml）
	cd $(GO_MODULE_DIR) && \
		go run ./cmd/ohmysmsd -config config.yaml

.PHONY: test
test: ## 跑后端测试
	cd $(GO_MODULE_DIR) && go test ./... -race -count=1

.PHONY: lint
lint: ## vet
	cd $(GO_MODULE_DIR) && go vet ./...

# ---------- 前端 ----------

.PHONY: frontend-install
frontend-install: ## npm install
	cd $(FE_DIR) && npm install

.PHONY: frontend-dev
frontend-dev: ## Vite dev server (代理到 $(BACKEND_URL))
	cd $(FE_DIR) && npm run dev

.PHONY: frontend-build
frontend-build: ## 打包 Vue dist 到 deploy/build/web/
	cd $(FE_DIR) && npm run build
	@mkdir -p $(DIST_DIR)/web
	@rm -rf $(DIST_DIR)/web/*
	@cp -r $(FE_DIR)/dist/* $(DIST_DIR)/web/
	@ls -la $(DIST_DIR)/web/

# ---------- 部署 ----------

.PHONY: deploy
deploy: build-arm64 frontend-build ## 构建 + rsync 到树莓派 + 重启服务
	@echo "==> deploy to $(PI_HOST):$(PI_DEST)"
	ssh $(PI_HOST) "sudo mkdir -p $(PI_DEST)/bin $(PI_DEST)/web $(PI_DEST)/data && sudo chown -R \$$(whoami):\$$(whoami) $(PI_DEST)"
	rsync -avz --progress \
		$(DIST_DIR)/ohmysmsd \
		$(PI_HOST):$(PI_DEST)/bin/ohmysmsd.new
	rsync -avz --delete --progress \
		$(DIST_DIR)/web/ \
		$(PI_HOST):$(PI_DEST)/web/
	rsync -avz \
		backend/config.example.yaml \
		$(PI_HOST):$(PI_DEST)/config.example.yaml
	rsync -avz \
		deploy/systemd/ohmysmsd.service \
		$(PI_HOST):/tmp/ohmysmsd.service
	ssh $(PI_HOST) '\
		sudo mv $(PI_DEST)/bin/ohmysmsd.new $(PI_DEST)/bin/ohmysmsd && \
		sudo chmod +x $(PI_DEST)/bin/ohmysmsd && \
		[ -f $(PI_DEST)/config.yaml ] || cp $(PI_DEST)/config.example.yaml $(PI_DEST)/config.yaml && \
		sudo mv /tmp/ohmysmsd.service /etc/systemd/system/$(PI_SERVICE).service && \
		sudo systemctl daemon-reload && \
		sudo systemctl enable $(PI_SERVICE) && \
		sudo systemctl restart $(PI_SERVICE) && \
		sleep 1 && \
		sudo systemctl --no-pager status $(PI_SERVICE) | head -15 \
	'

.PHONY: pi-logs
pi-logs: ## tail 树莓派上的服务日志
	ssh $(PI_HOST) "sudo journalctl -u $(PI_SERVICE) -f -n 50"

.PHONY: pi-status
pi-status: ## 查看服务状态
	ssh $(PI_HOST) "sudo systemctl --no-pager status $(PI_SERVICE)"

.PHONY: pi-restart
pi-restart: ## 重启服务
	ssh $(PI_HOST) "sudo systemctl restart $(PI_SERVICE)"

.PHONY: pi-stop
pi-stop: ## 停止服务
	ssh $(PI_HOST) "sudo systemctl stop $(PI_SERVICE)"

# ---------- 清理 ----------

.PHONY: clean
clean:
	rm -rf $(DIST_DIR)
	cd $(GO_MODULE_DIR) && go clean

.DEFAULT_GOAL := help
