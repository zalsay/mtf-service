#!/bin/bash
# 作者: Claude
# 创建日期: $(date +"%Y-%m-%d")
# 描述: New TaskQueue项目自动化Docker镜像构建、版本更新和部署脚本

# Docker 构建和部署脚本
# 用于自动化构建 New TaskQueue Go API Docker 镜像并更新版本

set -e  # 遇到错误立即退出

# 配置变量
IMAGE_NAME="192.168.9.20:8500/postgres-handler"
DOCKER_COMPOSE_FILE="docker-compose.yml"
DOCKERFILE="Dockerfile"

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 日志函数
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 获取当前版本号
get_current_version() {
    grep "image:" $DOCKER_COMPOSE_FILE | grep -o "v[0-9]\+\.[0-9]\+\.[0-9]\+" | head -1
}

# 生成新版本号
generate_new_version() {
    local current_version=$1
    local version_type=${2:-patch}  # major, minor, patch
    
    # 移除 'v' 前缀
    local version_num=${current_version#v}
    
    # 分割版本号
    IFS='.' read -ra VERSION_PARTS <<< "$version_num"
    local major=${VERSION_PARTS[0]}
    local minor=${VERSION_PARTS[1]}
    local patch=${VERSION_PARTS[2]}
    
    case $version_type in
        "major")
            major=$((major + 1))
            minor=0
            patch=0
            ;;
        "minor")
            minor=$((minor + 1))
            patch=0
            ;;
        "patch")
            patch=$((patch + 1))
            ;;
        *)
            log_error "无效的版本类型: $version_type (支持: major, minor, patch)"
            exit 1
            ;;
    esac
    
    echo "v${major}.${minor}.${patch}"
}

# 更新 docker-compose.yml 中的版本
update_compose_version() {
    local old_version=$1
    local new_version=$2
    
    log_info "更新 docker-compose.yml 中的版本: $old_version -> $new_version"
    
    # 使用 sed 替换版本号
    sed -i "s|${IMAGE_NAME}:${old_version}|${IMAGE_NAME}:${new_version}|g" $DOCKER_COMPOSE_FILE
    
    if [ $? -eq 0 ]; then
        log_success "版本号更新成功"
    else
        log_error "版本号更新失败"
        exit 1
    fi
}

# 检查Go环境
check_go_environment() {
    log_info "检查Go环境..."
    
    if ! command -v go &> /dev/null; then
        log_error "Go未安装或不在PATH中"
        exit 1
    fi
    
    local go_version=$(go version | awk '{print $3}')
    log_info "Go版本: $go_version"
    
    # 检查go.mod文件
    if [ ! -f "go.mod" ]; then
        log_error "go.mod文件不存在"
        exit 1
    fi
    
    log_success "Go环境检查通过"
}

# 运行测试
run_tests() {
    log_info "运行Go测试..."
    
    # 检查是否有测试文件
    if find . -name "*_test.go" -type f | grep -q .; then
        go test ./... -v
        if [ $? -eq 0 ]; then
            log_success "所有测试通过"
        else
            log_error "测试失败"
            exit 1
        fi
    else
        log_warning "未找到测试文件，跳过测试"
    fi
}

# 构建 Docker 镜像
build_image() {
    local version=$1
    local image_tag="${IMAGE_NAME}:${version}"
    local no_cache=${2:-false}
    
    log_info "开始构建 Docker 镜像: $image_tag"
    
    # 构建镜像
    if [ "$no_cache" = true ]; then
        docker build --no-cache -t $image_tag -f $DOCKERFILE .
    else
        docker build -t $image_tag -f $DOCKERFILE .
    fi
    
    if [ $? -eq 0 ]; then
        log_success "镜像构建成功: $image_tag"
        
        # 同时标记为 latest
        docker tag $image_tag "${IMAGE_NAME}:latest"
        log_info "已标记为 latest 版本"
    else
        log_error "镜像构建失败"
        exit 1
    fi
}

# 推送镜像到仓库
push_image() {
    local version=$1
    local image_tag="${IMAGE_NAME}:${version}"
    
    log_info "推送镜像到仓库..."
    
    # 推送版本标签
    local time=$(date +"%Y%m%d%H%M%S")
    log_info "推送版本标签: $image_tag $time"
    docker push $image_tag
    
    if [ $? -eq 0 ]; then
        log_success "版本镜像推送成功"
    else
        log_error "版本镜像推送失败"
        exit 1
    fi
}

# 部署服务
deploy_service() {
    log_info "部署服务..."
    
    # 创建必要的目录
    mkdir -p logs data
    
    # 检查.env文件
    if [ ! -f ".env" ]; then
        if [ -f ".env.example" ]; then
            log_warning ".env文件不存在，从.env.example复制"
            cp .env.example .env
        else
            log_warning ".env文件不存在，请手动创建"
        fi
    fi
    
    # 停止现有服务
    log_info "停止现有服务"
    docker-compose down
    
    # 启动新服务
    log_info "启动新服务"
    docker-compose up -d
    
    if [ $? -eq 0 ]; then
        log_success "服务部署成功"
        
        # 显示服务状态
        log_info "服务状态:"
        docker-compose ps
        
        # 等待服务启动
        log_info "等待服务启动..."
        sleep 10
        
        # 检查健康状态
        check_service_health
    else
        log_error "服务部署失败"
        exit 1
    fi
}

# 检查服务健康状态
check_service_health() {
    log_info "检查服务健康状态..."
    
    local max_attempts=30
    local attempt=1
    
    while [ $attempt -le $max_attempts ]; do
        if curl -f http://localhost:8800/health > /dev/null 2>&1; then
            log_success "服务健康检查通过"
            return 0
        fi
        
        log_info "健康检查尝试 $attempt/$max_attempts..."
        sleep 2
        attempt=$((attempt + 1))
    done
    
    log_error "服务健康检查失败"
    docker-compose logs new-taskqueue
    exit 1
}

# 清理旧镜像
cleanup_old_images() {
    log_info "清理旧的 Docker 镜像..."
    
    # 删除悬空镜像
    docker image prune -f
    
    # 删除旧版本镜像（保留最新的3个版本）
    local old_images=$(docker images $IMAGE_NAME --format "table {{.Repository}}:{{.Tag}}" | grep -v "TAG" | grep -v "latest" | tail -n +4)
    
    if [ ! -z "$old_images" ]; then
        echo "$old_images" | xargs docker rmi -f
        log_success "旧镜像清理完成"
    else
        log_info "没有需要清理的旧镜像"
    fi
}

# 显示帮助信息
show_help() {
    echo "New TaskQueue Docker 构建和部署脚本"
    echo ""
    echo "用法: $0 [选项]"
    echo ""
    echo "选项:"
    echo "  -h, --help              显示帮助信息"
    echo "  -v, --version TYPE      指定版本更新类型 (major|minor|patch，默认: patch)"
    echo "  -b, --build-only        仅构建镜像，不推送和部署"
    echo "  -p, --push-only         构建并推送镜像，不部署"
    echo "  -d, --deploy-only       仅部署服务，不构建和推送"
    echo "  -c, --cleanup           清理旧镜像"
    echo "  -t, --test              运行测试"
    echo "  --no-cache              构建时不使用缓存"
    echo "  --skip-tests            跳过测试"
    echo ""
    echo "示例:"
    echo "  $0                      # 默认构建、推送和部署 (patch 版本)"
    echo "  $0 -v minor             # 更新 minor 版本"
    echo "  $0 -b                   # 仅构建镜像"
    echo "  $0 -d                   # 仅部署服务"
    echo "  $0 -c                   # 清理旧镜像"
    echo "  $0 -t                   # 运行测试"
}

# 主函数
main() {
    local version_type="patch"
    local build_only=false
    local push_only=false
    local deploy_only=false
    local cleanup_only=false
    local test_only=false
    local no_cache=false
    local skip_tests=false
    
    # 解析命令行参数
    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--help)
                show_help
                exit 0
                ;;
            -v|--version)
                version_type="$2"
                shift 2
                ;;
            -b|--build-only)
                build_only=true
                shift
                ;;
            -p|--push-only)
                push_only=true
                shift
                ;;
            -d|--deploy-only)
                deploy_only=true
                shift
                ;;
            -c|--cleanup)
                cleanup_only=true
                shift
                ;;
            -t|--test)
                test_only=true
                shift
                ;;
            --no-cache)
                no_cache=true
                shift
                ;;
            --skip-tests)
                skip_tests=true
                shift
                ;;
            *)
                log_error "未知选项: $1"
                show_help
                exit 1
                ;;
        esac
    done
    
    # 检查 Docker 是否运行
    if ! docker info > /dev/null 2>&1; then
        log_error "Docker 未运行，请启动 Docker"
        exit 1
    fi
    
    # 检查必要文件
    if [ ! -f "$DOCKERFILE" ]; then
        log_error "Dockerfile 不存在"
        exit 1
    fi
    
    if [ ! -f "$DOCKER_COMPOSE_FILE" ]; then
        log_error "docker-compose.yml 不存在"
        exit 1
    fi
    
    # 仅清理模式
    if [ "$cleanup_only" = true ]; then
        cleanup_old_images
        exit 0
    fi
    
    # 仅测试模式
    if [ "$test_only" = true ]; then
        check_go_environment
        run_tests
        exit 0
    fi
    
    # 仅部署模式
    if [ "$deploy_only" = true ]; then
        deploy_service
        exit 0
    fi
    
    # 检查Go环境
    # check_go_environment
    
    # 运行测试（除非跳过）
    if [ "$skip_tests" != true ]; then
        run_tests
    fi
    
    # 获取当前版本
    local current_version=$(get_current_version)
    if [ -z "$current_version" ]; then
        log_warning "无法获取当前版本，使用默认版本 v1.0.0"
        current_version="v1.0.0"
    fi
    
    # 生成新版本
    local new_version=$(generate_new_version $current_version $version_type)
    
    log_info "当前版本: $current_version"
    log_info "新版本: $new_version"
    
    # 更新版本号
    update_compose_version $current_version $new_version
    
    # 构建镜像（除非是仅部署模式）
    if [ "$deploy_only" != true ]; then
        build_image $new_version $no_cache
    fi
    
    # 推送镜像（除非是仅构建模式）
    if [ "$build_only" != true ] && [ "$deploy_only" != true ]; then
        push_image $new_version
    fi
    
    # 部署服务（除非是仅构建或仅推送模式）
    if [ "$build_only" != true ] && [ "$push_only" != true ]; then
        deploy_service
    fi
    
    log_success "所有操作完成！"
    log_info "新版本: $new_version"
    log_info "服务地址: http://localhost:8800"
    log_info "健康检查: http://localhost:8800/health"
}

# 执行主函数
main "$@"