# 多阶段构建 Dockerfile for PostgreSQL 股票数据处理服务
# 第一阶段：构建阶段
FROM ubuntu:24.04 AS builder

# 设置工作目录
WORKDIR /app
ENV GO111MODULE=on
ENV GOPROXY=https://goproxy.cn,direct
ENV GOSUMDB=off
ENV DEBIAN_FRONTEND=noninteractive

# 安装必要的工具
RUN apt-get update \
    && apt-get install -y --no-install-recommends golang-go git ca-certificates tzdata curl \
    && rm -rf /var/lib/apt/lists/*

# 复制 go mod 文件
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 构建应用
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags='-w -s -extldflags "-static"' \
    -a -installsuffix cgo \
    -o postgres-handler main.go

# 第二阶段：运行阶段
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

# 安装必要的运行时依赖
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata curl \
    && rm -rf /var/lib/apt/lists/*

# 设置时区
ENV TZ=Asia/Shanghai
RUN ln -snf /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone

# 创建非root用户
RUN groupadd -g 1001 appgroup && \
    useradd -u 1001 -g appgroup -m -s /usr/sbin/nologin appuser

# 设置工作目录
WORKDIR /app

# 从构建阶段复制二进制文件
COPY --from=builder /app/postgres-handler .

# 复制配置文件示例
COPY --from=builder /app/.env.example .

# 创建必要的目录
RUN mkdir -p logs data && \
    chown -R appuser:appgroup /app

# 切换到非root用户
USER appuser

# 暴露端口（默认8080，可通过环境变量PORT修改）
EXPOSE 8080

# 健康检查
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:${PORT:-8080}/health || exit 1

# 启动应用
CMD ["./postgres-handler"]
