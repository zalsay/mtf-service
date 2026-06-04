# PostgreSQL 股票数据处理服务

这是一个基于Go语言开发的PostgreSQL股票数据处理服务，支持股票历史数据的存储和查询，并基于type字段进行数据库分区优化。

## 功能特性

- ✅ **分区表设计**：基于type字段进行范围分区，提高查询性能
- ✅ **完整字段支持**：支持所有股票数据字段（开盘、收盘、最高、最低、成交量等）
- ✅ **批量操作**：支持单条和批量数据插入
- ✅ **灵活查询**：支持按股票代码、类型、日期范围查询
- ✅ **RESTful API**：提供标准的HTTP API接口
- ✅ **环境配置**：支持环境变量配置数据库连接

## 数据结构

### StockData 结构体
```go
type StockData struct {
    ID               int       `json:"id"`                // 自增ID
    Datetime         time.Time `json:"datetime"`          // 日期
    Open             float64   `json:"open"`              // 开盘价
    Close            float64   `json:"close"`             // 收盘价
    High             float64   `json:"high"`              // 最高价
    Low              float64   `json:"low"`               // 最低价
    Volume           int64     `json:"volume"`            // 成交量
    Amount           float64   `json:"amount"`            // 成交额
    Amplitude        float64   `json:"amplitude"`         // 振幅
    PercentageChange float64   `json:"percentage_change"` // 涨跌幅
    AmountChange     float64   `json:"amount_change"`     // 涨跌额
    TurnoverRate     float64   `json:"turnover_rate"`     // 换手率
    Type             int       `json:"type"`              // 类型（分区字段）
    Symbol           string    `json:"symbol"`            // 股票代码
    CreatedAt        time.Time `json:"created_at"`        // 创建时间
    UpdatedAt        time.Time `json:"updated_at"`        // 更新时间
}
```

### Type 字段说明
- `1`: 股票数据
- `2`: 基金数据（包含ETF）
- `3`: 指数数据
- `4-99`: 其他类型数据

## 数据库分区设计

系统会自动创建以下分区表：

| 分区名称 | Type范围 | 说明 |
|---------|----------|------|
| stock_data_stocks | 1-2 | 股票数据分区 |
| stock_data_funds | 2-3 | 基金数据分区（包含ETF） |
| stock_data_indices | 3-4 | 指数数据分区 |
| stock_data_others | 4-100 | 其他类型数据分区 |

每个分区表都会自动创建以下索引：
- `datetime` 字段索引
- `symbol` 字段索引  
- `symbol + datetime` 复合索引

## 环境配置

设置以下环境变量来配置数据库连接：

```bash
export DB_HOST=localhost          # 数据库主机地址
export DB_PORT=5432              # 数据库端口
export DB_USER=postgres          # 数据库用户名
export DB_PASSWORD=password      # 数据库密码
export DB_NAME=fintrack          # 数据库名称
export PORT=8080                 # 服务端口
```

历史行情接口默认按 `tickflow,daily,eastmoney,baidu` 顺序尝试。`daily` 指 `a-stock-daily` 服务，会在 TickFlow 失败或无数据后作为第二优先级读取 ClickHouse 历史日线：

```bash
export HISTORY_PROVIDER_ORDER=tickflow,daily,eastmoney,baidu
export A_STOCK_DAILY_URL=http://a-stock-daily:8080
export A_STOCK_DAILY_TOKEN=fintrack-dev-token
export EASTMONEY_BASE_URL=http://push2his.eastmoney.com
export BAIDU_FINANCE_BASE_URL=https://finance.pae.baidu.com
export TICKFLOW_BASE_URL=https://api.tickflow.org
export TICKFLOW_API_KEY=your_tickflow_key
```

## 安装和运行

### 方式一：Docker 部署（推荐）

#### 快速开始（使用部署脚本）
```bash
# 1. 克隆项目
git clone <repository-url>
cd postgres-handler

# 2. 启动服务（包含PostgreSQL数据库）
./deploy.sh start

# 3. 测试API
./deploy.sh test

# 4. 查看日志
./deploy.sh logs

# 5. 查看状态
./deploy.sh status

# 6. 停止服务
./deploy.sh stop
```

#### 完整部署（包含pgAdmin）
```bash
# 启动包含pgAdmin的完整服务
./deploy.sh full

# 访问地址：
# - API服务: http://localhost:8080
# - pgAdmin: http://localhost:5050 (admin@fintrack.com / admin123)
# - PostgreSQL: localhost:5432
```

#### 使用Makefile（传统方式）
```bash
# 启动基础服务
make docker-run

# 启动包含pgAdmin的完整服务
make docker-run-admin

# 查看日志
make docker-logs

# 停止服务
make docker-stop
```

#### Docker 配置说明
- 服务会自动创建PostgreSQL数据库和分区表
- 数据持久化存储在Docker卷中
- 支持健康检查和自动重启
- 使用非root用户运行，提高安全性

#### 环境变量配置
复制并修改环境配置：
```bash
cp .env.docker .env
# 根据需要修改 .env 中的配置
```

### 方式二：本地开发

### 1. 安装依赖
```bash
cd /root/workers/finance/postgres-handler
go mod tidy
```

### 2. 配置环境变量
复制环境变量模板：
```bash
cp .env.example .env
```

编辑 `.env` 文件，设置数据库连接信息：
```env
DB_HOST=localhost
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=your_password
DB_NAME=fintrack
PORT=8080
```

### 3. 启动PostgreSQL数据库
确保PostgreSQL数据库已启动并创建了对应的数据库。

### 4. 运行服务
```bash
go run main.go
```

服务将在指定端口启动（默认8080），并自动创建数据库表和分区。

## API 接口

### 1. 健康检查
```bash
GET /health
```

### 2. 插入单条股票数据
```bash
POST /api/v1/stock-data
Content-Type: application/json

{
    "datetime": "2024-01-15T00:00:00Z",
    "date_str": "2024-01-15",
    "open": 10.50,
    "close": 10.80,
    "high": 11.00,
    "low": 10.30,
    "volume": 1000000,
    "amount": 10800000.00,
    "amplitude": 6.67,
    "percentage_change": 2.86,
    "amount_change": 0.30,
    "turnover_rate": 1.25,
    "type": 1,
    "symbol": "000001"
}
```

### 3. 批量插入股票数据
```bash
POST /api/v1/stock-data/batch
Content-Type: application/json

[
    {
        "datetime": "2024-01-15T00:00:00Z",
        "date_str": "2024-01-15",
        "open": 10.50,
        "close": 10.80,
        // ... 其他字段
        "type": 1,
        "symbol": "000001"
    },
    {
        "datetime": "2024-01-16T00:00:00Z",
        "date_str": "2024-01-16",
        "open": 10.80,
        "close": 11.00,
        // ... 其他字段
        "type": 1,
        "symbol": "000001"
    }
]
```

### 4. 查询股票数据
```bash
GET /api/v1/stock-data/000001?type=1&limit=100&offset=0
```

参数说明：
- `type`: 数据类型（默认为1）
- `limit`: 返回记录数（默认100）
- `offset`: 偏移量（默认0）

### 5. 按日期范围查询
```bash
GET /api/v1/stock-data/000001/range?type=1&start_date=2024-01-01&end_date=2024-12-31
```

参数说明：
- `type`: 数据类型（默认为1）
- `start_date`: 开始日期（YYYY-MM-DD格式）
- `end_date`: 结束日期（YYYY-MM-DD格式）

### 6. 拉取外部历史行情
```bash
POST /api/v1/history
Content-Type: application/json

{
    "symbol": "600519",
    "stock_type": 1,
    "start_date": "2026-05-01",
    "end_date": "2026-05-20",
    "adjust": "qfq"
}
```

响应中的 `provider` 表示实际命中的数据源，例如 `eastmoney-push2his`、`baidu-gushitong` 或 `tickflow-go`。

## 性能优化

1. **分区表设计**：基于type字段进行范围分区，查询时只扫描相关分区
2. **索引优化**：为常用查询字段创建索引
3. **批量插入**：支持事务批量插入，提高写入性能
4. **连接池**：使用Go标准库的数据库连接池

## 错误处理

服务包含完善的错误处理机制：
- 数据库连接错误
- SQL执行错误
- JSON解析错误
- 参数验证错误

所有错误都会返回适当的HTTP状态码和错误信息。

## 扩展性

- 可以通过修改分区配置来支持更多数据类型
- 支持水平扩展（通过增加更多分区）
- 可以集成到微服务架构中
