# OpsMesh Makefile - 一键构建和运维
# 用法：make <target>

# 变量
GOCMD = go
GOBUILD = $(GOCMD) build
GOTEST = $(GOCMD) test
GOVET = $(GOCMD) vet
GOFLAGS = -timeout 120s
BINARY = opsmesh
BINARY_WIN = opsmesh.exe
MAIN = ./cmd/opsmesh

# 环境变量配置（生产环境必须设置 OPSMESH_JWT_SECRET）
# 安全基线（P2 修复）：不再内置 demo 默认密钥。未设置 OPSMESH_JWT_SECRET 时
# 二进制自动生成随机密钥（重启后会话失效），杜绝弱密钥被误带入生产。
ALLOW_PUBLIC_REGISTER ?= false

# 前端
NPM = npm
WEB_ENTERPRISE = web/enterprise

# 默认目标
.PHONY: all
all: build

# 完整构建：前端 + 后端
.PHONY: build
build: frontend backend
	@echo "✓ Build complete: $(BINARY_WIN)"

# 仅构建后端（强制重新编译，确保 go:embed 前端更新）
.PHONY: backend
backend:
	$(GOBUILD) -a -o $(BINARY_WIN) $(MAIN)

# 仅构建前端（企业版 Vue3）
.PHONY: frontend
frontend:
	cd $(WEB_ENTERPRISE) && $(NPM) run build

# 运行测试
.PHONY: test
test:
	$(GOTEST) $(GOFLAGS) ./...

# 运行 vet
.PHONY: vet
vet:
	$(GOVET) ./...

# 构建 services/ 下全部独立子模块（18 个，每个都是独立 go.mod + func main）
.PHONY: services-build
services-build:
	@set -e; for d in services/*/; do \
		echo "== build $$d =="; \
		(cd "$$d" && go build ./...) || exit 1; \
	done
	@echo "✓ services build complete (18 modules)"

# 测试 services/ 下全部独立子模块（依赖环境的用例有 Skip 机制的自行跳过）
.PHONY: services-test
services-test:
	@set -e; for d in services/*/; do \
		echo "== test $$d =="; \
		(cd "$$d" && go test -timeout 300s ./...) || exit 1; \
	done
	@echo "✓ services test complete (18 modules)"

# 构建 + 测试 + vet
.PHONY: ci
ci: vet test build
	@echo "✓ CI checks passed"

# 启动控制面（demo 模式）
.PHONY: run
run: build
	# JWT 密钥从环境变量 OPSMESH_JWT_SECRET 读取（config 内置 env 兜底）；
	# 未设置时自动生成随机密钥（重启后旧 token 失效），不再内置 demo 默认值。
	./$(BINARY_WIN) --mode=controlplane --store=memory --demo --allow-public-register=$(ALLOW_PUBLIC_REGISTER) --http-port=8080 --grpc-port=9090

# 启动 agent（task 97：修正 flag 名称 --controlplane → --control-addr）
.PHONY: run-agent
run-agent: build
	./$(BINARY_WIN) --mode=agent --control-addr=http://127.0.0.1:8080

# 清理构建产物
.PHONY: clean
clean:
	rm -f $(BINARY) $(BINARY_WIN) $(BINARY_WIN)~
	rm -rf $(WEB_ENTERPRISE)/dist/

# Docker 构建
.PHONY: docker
docker:
	docker build -t opsmesh:latest .

# Helm lint
.PHONY: helm
helm:
	helm lint deploy/helm/opsmesh/

# 生成 protobuf（如果需要）
.PHONY: proto
proto:
	cd proto && buf generate

# 帮助
.PHONY: help
help:
	@echo "OpsMesh Makefile targets:"
	@echo "  make build      - 完整构建（前端+后端）"
	@echo "  make backend    - 仅构建后端（go build -a）"
	@echo "  make frontend   - 仅构建前端（npm run build）"
	@echo "  make test       - 运行测试"
	@echo "  make vet        - 运行 vet"
	@echo "  make services-build - 构建 services/ 全部 18 个子模块"
	@echo "  make services-test  - 测试 services/ 全部 18 个子模块"
	@echo "  make ci         - vet + test + build"
	@echo "  make run        - 构建并启动控制面"
	@echo "  make run-agent  - 构建并启动 agent"
	@echo "  make clean      - 清理构建产物"
	@echo "  make docker     - Docker 构建"
	@echo "  make helm       - Helm lint"
	@echo "  make proto      - 生成 protobuf"
	@echo "  make help       - 显示帮助"
