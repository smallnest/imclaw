# IMClaw Makefile

# 版本信息
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME ?= $(shell date -u '+%Y-%m-%d_%H:%M:%S')
GO_VERSION ?= $(shell go version | awk '{print $$3}')

# 构建参数
BINARY_NAME := imclaw
CLI_NAME := imclaw-cli
MAIN_PATH := ./cmd/imclaw
CLI_PATH := ./cmd/imclaw-cli
BUILD_DIR := bin
LD_FLAGS := -ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

# Go 相关
GOCMD := go
GOBUILD := $(GOCMD) build
GOTEST := $(GOCMD) test
GOGET := $(GOCMD) get
GOMOD := $(GOCMD) mod
GOFLAGS := -v

# 默认目标
.PHONY: all
all: build

# 构建
.PHONY: build
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(GOFLAGS) $(LD_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

# 构建CLI
.PHONY: build-cli
build-cli:
	@echo "Building $(CLI_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(GOFLAGS) $(LD_FLAGS) -o $(BUILD_DIR)/$(CLI_NAME) $(CLI_PATH)
	@echo "Build complete: $(BUILD_DIR)/$(CLI_NAME)"

# 构建所有
.PHONY: build-all-bin
build-all-bin: build build-cli
	@echo "All builds complete"

# 构建所有平台
.PHONY: build-all
build-all:
	@echo "Building for all platforms..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LD_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 $(MAIN_PATH)
	GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LD_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 $(MAIN_PATH)
	GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LD_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 $(MAIN_PATH)
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LD_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe $(MAIN_PATH)
	# CLI builds
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LD_FLAGS) -o $(BUILD_DIR)/$(CLI_NAME)-linux-amd64 $(CLI_PATH)
	GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LD_FLAGS) -o $(BUILD_DIR)/$(CLI_NAME)-darwin-amd64 $(CLI_PATH)
	GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LD_FLAGS) -o $(BUILD_DIR)/$(CLI_NAME)-darwin-arm64 $(CLI_PATH)
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LD_FLAGS) -o $(BUILD_DIR)/$(CLI_NAME)-windows-amd64.exe $(CLI_PATH)
	@echo "Build complete for all platforms"

# 运行
.PHONY: run
run: build
	@echo "Running $(BINARY_NAME)..."
	./$(BUILD_DIR)/$(BINARY_NAME)

# 测试
.PHONY: test
test:
	@echo "Running tests..."
	$(GOTEST) -v ./...

# 测试覆盖率
.PHONY: test-coverage
test-coverage:
	@echo "Running tests with coverage..."
	$(GOTEST) -v -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# 代码检查
.PHONY: lint
lint:
	@echo "Running linter..."
	@which golangci-lint > /dev/null || go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	golangci-lint run ./...

# 格式化代码
.PHONY: fmt
fmt:
	@echo "Formatting code..."
	$(GOCMD) fmt ./...

# 整理依赖
.PHONY: tidy
tidy:
	@echo "Tidying dependencies..."
	$(GOMOD) tidy

# 清理
.PHONY: clean
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@rm -f coverage.out coverage.html

# 安装
.PHONY: install
install:
	@echo "Installing $(BINARY_NAME)..."
	$(GOBUILD) $(LD_FLAGS) -o $(GOPATH)/bin/$(BINARY_NAME) $(MAIN_PATH)

# 安装CLI
.PHONY: install-cli
install-cli:
	@echo "Installing $(CLI_NAME)..."
	$(GOBUILD) $(LD_FLAGS) -o $(GOPATH)/bin/$(CLI_NAME) $(CLI_PATH)

# 开发模式（带热重载，需要安装 air）
.PHONY: dev
dev:
	@which air > /dev/null || go install github.com/cosmtrek/air@latest
	air

# 生成示例配置
.PHONY: config
config:
	@echo "Creating default config..."
	@mkdir -p ~/.imclaw
	@if [ ! -f ~/.imclaw/config.json ]; then \
		cp config.example.json ~/.imclaw/config.json; \
		echo "Config created at ~/.imclaw/config.json"; \
	else \
		echo "Config already exists at ~/.imclaw/config.json"; \
	fi

# 显示帮助
.PHONY: help
help:
	@echo "IMClaw Makefile"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  build          构建项目 (默认)"
	@echo "  build-cli      构建 CLI 工具"
	@echo "  build-all      构建所有平台版本"
	@echo "  run            构建并运行"
	@echo "  test           运行测试"
	@echo "  test-coverage  运行测试并生成覆盖率报告"
	@echo "  lint           代码检查"
	@echo "  fmt            格式化代码"
	@echo "  tidy           整理依赖"
	@echo "  clean          清理构建产物"
	@echo "  install        安装到 GOPATH/bin"
	@echo "  install-cli    安装 CLI 到 GOPATH/bin"
	@echo "  dev            开发模式（热重载）"
	@echo "  config         生成默认配置文件"
	@echo "  help           显示帮助信息"

# 版本信息
.PHONY: version
version:
	@echo "Version: $(VERSION)"
	@echo "Build Time: $(BUILD_TIME)"
	@echo "Go Version: $(GO_VERSION)"
