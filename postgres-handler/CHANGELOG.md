# PostgreSQL 股票数据处理服务 - 更改日志

## [v1.1.0] - 2024-01-XX

### 新增功能
- **Docker 支持**：添加完整的Docker部署方案
  - 多阶段构建Dockerfile，优化镜像大小
  - docker-compose.yml配置，包含PostgreSQL和pgAdmin
  - 数据库初始化脚本（init.sql）
  - Docker专用环境配置（.env.docker）
  - .dockerignore文件，优化构建过程

### 改进
- **Makefile 增强**：添加Docker相关命令
  - `make docker-build`: 构建Docker镜像
  - `make docker-run`: 启动Docker服务
  - `make docker-run-admin`: 启动包含pgAdmin的完整服务
  - `make docker-logs`: 查看服务日志
  - `make docker-stop`: 停止Docker服务
  - `make docker-clean`: 清理Docker资源

- **文档更新**：README.md添加Docker部署说明
  - 快速开始指南
  - 完整部署方案
  - 配置说明和最佳实践

### 技术细节
- 使用golang:1.21-alpine作为构建镜像
- 使用alpine:latest作为运行镜像，减小镜像体积
- 支持健康检查和自动重启
- 使用非root用户运行，提高安全性
- 数据持久化存储在Docker卷中

## [2024-11-02] 分区设计优化

### 更改内容
- 将数据库分区设计从原来的"股票-ETF-指数-其他"四分区改为"股票-基金（包含ETF）-指数-其他"四分区
- 更新了分区表命名和说明

### 具体修改

#### 1. 分区表设计更新
**修改前：**
- `stock_data_stocks` (Type 1-2): 股票数据分区
- `stock_data_etfs` (Type 2-3): ETF数据分区  
- `stock_data_indices` (Type 3-4): 指数数据分区
- `stock_data_others` (Type 4-100): 其他类型数据分区

**修改后：**
- `stock_data_stocks` (Type 1-2): 股票数据分区
- `stock_data_funds` (Type 2-3): 基金数据分区（包含ETF）
- `stock_data_indices` (Type 3-4): 指数数据分区  
- `stock_data_others` (Type 4-100): 其他类型数据分区

#### 2. Type 字段说明更新
**修改前：**
- `1`: 股票数据
- `2`: ETF数据
- `3`: 指数数据
- `4-99`: 其他类型数据

**修改后：**
- `1`: 股票数据
- `2`: 基金数据（包含ETF）
- `3`: 指数数据
- `4-99`: 其他类型数据

#### 3. 代码注释更新
- 更新了 `StockData` 结构体中 `Type` 字段的注释说明
- 更新了分区表创建逻辑中的注释

#### 4. 文档更新
- 更新了 `README.md` 中的分区设计说明
- 更新了 `test_api.sh` 测试脚本，添加了基金数据测试用例

### 影响范围
- ✅ 向后兼容：现有的Type值含义保持不变
- ✅ 数据库结构：分区逻辑保持一致，只是命名和说明更清晰
- ✅ API接口：无任何变化
- ✅ 测试覆盖：增加了基金类型数据的测试用例

### 使用建议
- Type=2 现在明确表示基金数据（包含ETF、公募基金、私募基金等）
- 建议在数据导入时根据实际数据类型设置正确的Type值
- 新的分区命名更加直观，便于数据库管理和维护