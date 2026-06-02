#!/bin/bash

# PostgreSQL 股票数据处理服务 - 快速部署脚本
# 使用方法: ./deploy.sh [start|stop|restart|logs|status|clean]

set -e

PROJECT_NAME="fintrack-postgres-handler"
COMPOSE_FILE="docker-compose.yml"

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 打印带颜色的消息
print_message() {
    local color=$1
    local message=$2
    echo -e "${color}[$(date '+%Y-%m-%d %H:%M:%S')] ${message}${NC}"
}

# 检查Docker和docker-compose是否安装
check_dependencies() {
    if ! command -v docker &> /dev/null; then
        print_message $RED "错误: Docker 未安装"
        exit 1
    fi
    
    if ! command -v docker-compose &> /dev/null; then
        print_message $RED "错误: docker-compose 未安装"
        exit 1
    fi
}

# 准备环境配置
prepare_env() {
    if [ ! -f .env ]; then
        print_message $YELLOW "复制环境配置文件..."
        cp .env.docker .env
        print_message $GREEN "环境配置文件已创建: .env"
        print_message $BLUE "提示: 可以编辑 .env 文件来修改配置"
    fi
}

# 启动服务
start_services() {
    print_message $BLUE "启动 PostgreSQL 股票数据处理服务..."
    prepare_env
    
    # 构建镜像
    print_message $YELLOW "构建 Docker 镜像..."
    docker build -t ${PROJECT_NAME}:latest . || {
        print_message $RED "Docker 镜像构建失败"
        exit 1
    }
    
    # 启动服务
    print_message $YELLOW "启动服务容器..."
    docker-compose up -d || {
        print_message $RED "服务启动失败"
        exit 1
    }
    
    print_message $GREEN "服务启动成功！"
    print_message $BLUE "API 服务: http://localhost:8080"
    print_message $BLUE "数据库: localhost:5432"
    print_message $BLUE "使用 './deploy.sh logs' 查看日志"
    print_message $BLUE "使用 './deploy.sh status' 查看状态"
}

# 启动完整服务（包含pgAdmin）
start_full_services() {
    print_message $BLUE "启动完整服务（包含 pgAdmin）..."
    prepare_env
    
    # 构建镜像
    print_message $YELLOW "构建 Docker 镜像..."
    docker build -t ${PROJECT_NAME}:latest .
    
    # 启动完整服务
    print_message $YELLOW "启动完整服务容器..."
    docker-compose --profile admin up -d
    
    print_message $GREEN "完整服务启动成功！"
    print_message $BLUE "API 服务: http://localhost:8080"
    print_message $BLUE "pgAdmin: http://localhost:5050"
    print_message $BLUE "数据库: localhost:5432"
}

# 停止服务
stop_services() {
    print_message $YELLOW "停止服务..."
    docker-compose down
    print_message $GREEN "服务已停止"
}

# 重启服务
restart_services() {
    print_message $BLUE "重启服务..."
    stop_services
    sleep 2
    start_services
}

# 查看日志
show_logs() {
    print_message $BLUE "显示服务日志..."
    docker-compose logs -f
}

# 查看状态
show_status() {
    print_message $BLUE "服务状态:"
    docker-compose ps
    echo ""
    print_message $BLUE "Docker 镜像:"
    docker images | grep -E "(${PROJECT_NAME}|postgres|alpine)" || true
}

# 清理资源
clean_resources() {
    print_message $YELLOW "清理 Docker 资源..."
    docker-compose down -v --rmi local 2>/dev/null || true
    docker system prune -f
    print_message $GREEN "资源清理完成"
}

# 测试API
test_api() {
    print_message $BLUE "测试 API 接口..."
    if [ -f test_api.sh ]; then
        chmod +x test_api.sh
        ./test_api.sh
    else
        print_message $RED "测试脚本 test_api.sh 不存在"
    fi
}

# 显示帮助信息
show_help() {
    echo "PostgreSQL 股票数据处理服务 - 部署脚本"
    echo ""
    echo "使用方法: $0 [命令]"
    echo ""
    echo "可用命令:"
    echo "  start     - 启动服务（API + PostgreSQL）"
    echo "  full      - 启动完整服务（API + PostgreSQL + pgAdmin）"
    echo "  stop      - 停止服务"
    echo "  restart   - 重启服务"
    echo "  logs      - 查看服务日志"
    echo "  status    - 查看服务状态"
    echo "  test      - 测试API接口"
    echo "  clean     - 清理Docker资源"
    echo "  help      - 显示此帮助信息"
    echo ""
    echo "示例:"
    echo "  $0 start    # 启动基础服务"
    echo "  $0 full     # 启动包含pgAdmin的完整服务"
    echo "  $0 logs     # 查看实时日志"
    echo "  $0 test     # 测试API功能"
}

# 主函数
main() {
    check_dependencies
    
    case "${1:-help}" in
        start)
            start_services
            ;;
        full)
            start_full_services
            ;;
        stop)
            stop_services
            ;;
        restart)
            restart_services
            ;;
        logs)
            show_logs
            ;;
        status)
            show_status
            ;;
        test)
            test_api
            ;;
        clean)
            clean_resources
            ;;
        help|*)
            show_help
            ;;
    esac
}

# 执行主函数
main "$@"