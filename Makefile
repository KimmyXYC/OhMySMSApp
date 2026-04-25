# ohmysmsapp Makefile
#
# 可通过变量覆盖目标平台与部署信息：
#   make build-linux-arm64 TARGET_GOOS=linux TARGET_GOARCH=arm64
#   make deploy DEPLOY_HOST=user@example-host DEPLOY_DIR=/srv/ohmysmsapp DEPLOY_SERVICE=ohmysmsd

SHELL := /bin/bash

VERSION ?= $(shell git describe --tags --dirty --always 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

GO_MODULE_DIR := backend
FE_DIR        := frontend
DIST_DIR      := deploy/build

TARGET_GOOS   ?= linux
TARGET_GOARCH ?= arm64

DEPLOY_HOST    ?=
DEPLOY_DIR     ?= /srv/ohmysmsapp
DEPLOY_SERVICE ?= ohmysmsd

.PHONY: help
help:
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z0-9_.-]+:.*?##/ { printf "  \033[36m%-24s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

.PHONY: backend-deps
backend-deps: ## 下载并整理 Go 依赖
	cd $(GO_MODULE_DIR) && go mod tidy

.PHONY: build
build: ## 构建当前平台后端二进制
	@mkdir -p $(DIST_DIR)
	cd $(GO_MODULE_DIR) && go build -trimpath -ldflags="$(LDFLAGS)" -o ../$(DIST_DIR)/ohmysmsd-local ./cmd/ohmysmsd

.PHONY: build-linux-arm64
build-linux-arm64: ## 构建指定 TARGET_GOOS/TARGET_GOARCH 的后端二进制
	@mkdir -p $(DIST_DIR)
	cd $(GO_MODULE_DIR) && \
		CGO_ENABLED=0 GOOS=$(TARGET_GOOS) GOARCH=$(TARGET_GOARCH) \
		go build -trimpath -ldflags="$(LDFLAGS)" -o ../$(DIST_DIR)/ohmysmsd ./cmd/ohmysmsd

.PHONY: backend-run-local
backend-run-local: ## 本地运行后端（需准备 backend/config.yaml）
	cd $(GO_MODULE_DIR) && go run ./cmd/ohmysmsd -config config.yaml

.PHONY: test
test: ## 运行后端测试
	cd $(GO_MODULE_DIR) && go test ./... -count=1

.PHONY: lint
lint: ## 运行 go vet
	cd $(GO_MODULE_DIR) && go vet ./...

.PHONY: frontend-install
frontend-install: ## 安装前端依赖
	cd $(FE_DIR) && npm install

.PHONY: frontend-dev
frontend-dev: ## 启动前端开发服务器
	cd $(FE_DIR) && npm run dev

.PHONY: frontend-build
frontend-build: ## 构建前端并复制到 deploy/build/web/
	cd $(FE_DIR) && npm run build
	@mkdir -p $(DIST_DIR)/web
	@rm -rf $(DIST_DIR)/web/*
	@cp -r $(FE_DIR)/dist/* $(DIST_DIR)/web/

.PHONY: deploy-check
deploy-check:
	@if [ -z "$(DEPLOY_HOST)" ]; then \
		echo "DEPLOY_HOST is required, e.g. make deploy DEPLOY_HOST=user@example-host"; \
		exit 1; \
	fi

.PHONY: deploy
deploy: deploy-check build-linux-arm64 frontend-build ## 部署后端与前端到远端主机
	@echo "==> deploy to $(DEPLOY_HOST):$(DEPLOY_DIR)"
	ssh $(DEPLOY_HOST) "sudo mkdir -p $(DEPLOY_DIR)/bin $(DEPLOY_DIR)/web $(DEPLOY_DIR)/data && sudo chown -R \$$(id -un):\$$(id -gn) $(DEPLOY_DIR)"
	rsync -avz --progress $(DIST_DIR)/ohmysmsd $(DEPLOY_HOST):$(DEPLOY_DIR)/bin/ohmysmsd.new
	rsync -avz --delete --progress $(DIST_DIR)/web/ $(DEPLOY_HOST):$(DEPLOY_DIR)/web/
	rsync -avz backend/config.example.yaml $(DEPLOY_HOST):$(DEPLOY_DIR)/config.example.yaml
	ssh $(DEPLOY_HOST) '
		sudo mv $(DEPLOY_DIR)/bin/ohmysmsd.new $(DEPLOY_DIR)/bin/ohmysmsd && \
		sudo chmod +x $(DEPLOY_DIR)/bin/ohmysmsd && \
		[ -f $(DEPLOY_DIR)/config.yaml ] || cp $(DEPLOY_DIR)/config.example.yaml $(DEPLOY_DIR)/config.yaml && \
		sudo systemctl restart $(DEPLOY_SERVICE) && \
		sleep 1 && sudo systemctl --no-pager status $(DEPLOY_SERVICE) | sed -n "1,15p"'

.PHONY: remote-status
remote-status: deploy-check ## 查看远端服务状态
	ssh $(DEPLOY_HOST) "sudo systemctl --no-pager status $(DEPLOY_SERVICE)"

.PHONY: remote-logs
remote-logs: deploy-check ## tail 远端服务日志
	ssh $(DEPLOY_HOST) "sudo journalctl -u $(DEPLOY_SERVICE) -f -n 50"

.PHONY: remote-restart
remote-restart: deploy-check ## 重启远端服务
	ssh $(DEPLOY_HOST) "sudo systemctl restart $(DEPLOY_SERVICE)"

.PHONY: clean
clean: ## 清理构建产物
	rm -rf $(DIST_DIR)
	cd $(GO_MODULE_DIR) && go clean

.DEFAULT_GOAL := help
