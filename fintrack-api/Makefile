# PostgreSQL 股票数据处理服务 Makefile

.PHONY: build run clean test deps help docker-build docker-run docker-stop docker-logs docker-clean

# 默认目标
all: build

# 构建 PostgreSQL 股票数据处理服务（容器兼容版本）
build:
	@echo "构建 PostgreSQL 股票数据处理服务（Linux 静态可执行文件）..."
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -ldflags '-extldflags "-static"' -o run .
	@chmod +x run
	@echo "构建完成！生成的二进制文件可在 Alpine 容器中直接运行。"

# 安装依赖
deps:
	@echo "安装 Go 依赖..."
	go mod tidy
	go mod download
	@echo "依赖安装完成！"

# 运行服务（需要先设置环境变量）
run: build
	@echo "启动 PostgreSQL 股票数据处理服务..."
	@echo "请确保已设置以下环境变量："
	@echo "  DB_HOST, DB_PORT, DB_USER, DB_PASSWORD, DB_NAME, PORT"
	@echo "或者复制 .env.example 为 .env 并修改配置"
	@echo ""
	./postgres-handler

# 运行测试（需要服务已启动）
test:
	@echo "运行 API 测试..."
	@echo "请确保服务已在 http://localhost:8080 启动"
	./test_api.sh

# 清理构建文件
clean:
	@echo "清理构建文件..."
	rm -f postgres-handler
	@echo "清理完成！"

# Docker 相关命令

# 构建 Docker 镜像
docker-build:
	@echo "构建 Docker 镜像..."
	docker build -t fintrack-postgres-handler:latest .
	@echo "Docker 镜像构建完成！"

# 使用 docker-compose 启动服务
docker-run:
	@echo "启动 Docker 服务..."
	@if [ ! -f .env ]; then \
		echo "复制 Docker 环境配置..."; \
		cp .env.docker .env; \
	fi
	docker-compose up -d
	@echo "Docker 服务启动完成！"
	@echo "服务地址: http://localhost:8080"
	@echo "数据库地址: localhost:5432"

# 启动包含 pgAdmin 的完整服务
docker-run-admin:
	@echo "启动包含 pgAdmin 的完整服务..."
	@if [ ! -f .env ]; then \
		echo "复制 Docker 环境配置..."; \
		cp .env.docker .env; \
	fi
	docker-compose --profile admin up -d
	@echo "完整服务启动完成！"
	@echo "API 服务: http://localhost:8080"
	@echo "pgAdmin: http://localhost:5050"
	@echo "数据库: localhost:5432"

# 停止 Docker 服务
docker-stop:
	@echo "停止 Docker 服务..."
	docker-compose down
	@echo "Docker 服务已停止！"

# 查看 Docker 服务日志
docker-logs:
	@echo "查看服务日志..."
	docker-compose logs -f

# 清理 Docker 资源
docker-clean:
	@echo "清理 Docker 资源..."
	docker-compose down -v --rmi local
	docker system prune -f
	@echo "Docker 资源清理完成！"

# 显示帮助信息
help:
	@echo "PostgreSQL 股票数据处理服务 - 可用命令："
	@echo ""
	@echo "本地开发："
	@echo "  make build       - 构建可执行文件"
	@echo "  make deps        - 安装 Go 依赖"
	@echo "  make run         - 构建并运行服务"
	@echo "  make test        - 运行 API 测试"
	@echo "  make clean       - 清理构建文件"
	@echo ""
	@echo "Docker 部署："
	@echo "  make docker-build      - 构建 Docker 镜像"
	@echo "  make docker-run        - 启动 Docker 服务"
	@echo "  make docker-run-admin  - 启动包含 pgAdmin 的完整服务"
	@echo "  make docker-stop       - 停止 Docker 服务"
	@echo "  make docker-logs       - 查看服务日志"
	@echo "  make docker-clean      - 清理 Docker 资源"
	@echo "  make help              - 显示此帮助信息"
	@echo ""
	@echo "快速开始（Docker）："
	@echo "  1. make docker-run        # 启动服务"
	@echo "  2. make test              # 测试 API"
	@echo "  3. make docker-logs       # 查看日志"
	@echo "  4. make docker-stop       # 停止服务"
