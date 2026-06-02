# FinTrack

FinTrack is a full-stack finance tracking and AI-assisted analysis platform. The
repository contains the public application surface, API services, data bridge
services, gateway components, and frontend application code.

The private model/runtime service formerly kept under `ai-fucntions/` is not
part of this repository. Keep that service in its own private repository and
configure this project to call it through service URLs and environment
variables.

## Repository Layout

| Path | Purpose |
| --- | --- |
| `fintrack-front/` | React + Vite frontend application. |
| `fintrack-api/` | Go API service for auth, watchlists, portfolios, MTF agent, news, and UZI integration. |
| `gateway/` | Go inference gateway and queueing layer. |
| `deepseek-tui/` | Node.js DeepSeek TUI HTTP wrapper runtime. |
| `postgres-handler/` | Go PostgreSQL data service and schema helpers. |
| `uzi-service/` | UZI report service container and vendored skill runtime. |
| `docs/` | Architecture notes, API contracts, and integration plans. |

## Prerequisites

- Go 1.21+ for `fintrack-api`
- Go 1.22+ for `gateway`
- Go 1.24+ for `postgres-handler`
- Node.js 20+ for `fintrack-front` and `deepseek-tui`
- PostgreSQL for persistent application data
- Redis for gateway queue state when running gateway-backed workflows

## Configuration

Copy the example environment files before local development:

```bash
cp fintrack-api/.env.example fintrack-api/.env
cp fintrack-front/.env.example fintrack-front/.env.development.local
cp postgres-handler/.env.example postgres-handler/.env
```

Do not commit real secrets. Keep API keys, JWT secrets, database passwords, OSS
credentials, and model-provider tokens in local `.env` files or deployment
secret stores.

Important service URLs:

- `PYTHON_SERVICE_URL`: inference gateway URL used by `fintrack-api`
- `POSTGRES_HANDLER_URL`: data service URL used by API and gateway flows
- `UZI_SERVICE_URL`: UZI report service URL
- `MTF_AGENT_RUNTIME_URL`: MTF agent runtime URL when enabled

## Local Development

Frontend:

```bash
cd fintrack-front
npm install
npm run dev
```

API:

```bash
cd fintrack-api
go mod download
go run .
```

Gateway:

```bash
cd gateway
go mod download
go run ./cmd/inference-gateway
```

DeepSeek TUI runtime:

```bash
cd deepseek-tui
node deepseek_tui_runtime.js
```

Postgres handler:

```bash
cd postgres-handler
go mod download
go run .
```

## Tests

Run focused tests from each service directory:

```bash
cd fintrack-api && go test ./...
cd gateway && go test ./...
cd postgres-handler && go test ./...
cd deepseek-tui && node --test deepseek_tui_runtime.test.js
cd fintrack-front && npm run build
```

## Deployment Notes

- Each backend/runtime service owns its own Dockerfile.
- Keep private model/runtime services outside this public repository.
- Use environment variables for cross-service routing instead of hard-coded
  hostnames.
- Prefer deployment-specific secret management over checked-in `.env` files.
- Validate generated compose or deployment manifests before publishing changes.

## Contribution Guidelines

- Keep service boundaries clear; avoid mixing frontend, API, gateway, and model
  runtime concerns in a single change.
- Add tests for API contracts, queue behavior, and data normalization changes.
- Keep generated files, caches, local binaries, and notebooks out of Git unless
  they are intentionally part of the product.
- Update this README or service-level documentation when setup, environment
  variables, or deployment behavior changes.

---

# FinTrack 中文说明

FinTrack 是一个面向金融数据跟踪、组合管理和 AI 辅助分析的全栈项目。本仓库包含
公开应用代码、API 服务、数据桥接服务、推理网关、运行时包装器和前端应用。

原先放在 `ai-fucntions/` 下的私有模型/运行时服务不属于本仓库。该服务应保存在独
立私有仓库中，本项目通过服务 URL 和环境变量与其对接。

## 目录结构

| 路径 | 说明 |
| --- | --- |
| `fintrack-front/` | React + Vite 前端应用。 |
| `fintrack-api/` | Go API 服务，负责认证、自选股、组合、MTF Agent、资讯和 UZI 集成。 |
| `gateway/` | Go 推理网关和队列调度层。 |
| `deepseek-tui/` | Node.js DeepSeek TUI HTTP 包装运行时。 |
| `postgres-handler/` | Go PostgreSQL 数据服务和 schema 辅助逻辑。 |
| `uzi-service/` | UZI 报告服务容器和 vendored skill runtime。 |
| `docs/` | 架构说明、API 契约和集成计划。 |

## 环境要求

- `fintrack-api` 需要 Go 1.21+
- `gateway` 需要 Go 1.22+
- `postgres-handler` 需要 Go 1.24+
- `fintrack-front` 和 `deepseek-tui` 需要 Node.js 20+
- PostgreSQL 用于持久化应用数据
- Redis 用于 gateway 队列状态

## 配置

本地开发前复制环境变量模板：

```bash
cp fintrack-api/.env.example fintrack-api/.env
cp fintrack-front/.env.example fintrack-front/.env.development.local
cp postgres-handler/.env.example postgres-handler/.env
```

不要提交真实密钥。API Key、JWT secret、数据库密码、OSS 凭据和模型供应商 token
应放在本地 `.env` 文件或部署平台的 secret 管理中。

关键服务 URL：

- `PYTHON_SERVICE_URL`：`fintrack-api` 调用的推理网关地址
- `POSTGRES_HANDLER_URL`：API 和 gateway 流程使用的数据服务地址
- `UZI_SERVICE_URL`：UZI 报告服务地址
- `MTF_AGENT_RUNTIME_URL`：启用 MTF Agent 时的运行时地址

## 本地开发

前端：

```bash
cd fintrack-front
npm install
npm run dev
```

API：

```bash
cd fintrack-api
go mod download
go run .
```

Gateway：

```bash
cd gateway
go mod download
go run ./cmd/inference-gateway
```

DeepSeek TUI runtime：

```bash
cd deepseek-tui
node deepseek_tui_runtime.js
```

Postgres handler：

```bash
cd postgres-handler
go mod download
go run .
```

## 测试

在各服务目录运行聚焦测试：

```bash
cd fintrack-api && go test ./...
cd gateway && go test ./...
cd postgres-handler && go test ./...
cd deepseek-tui && node --test deepseek_tui_runtime.test.js
cd fintrack-front && npm run build
```

## 部署说明

- 每个后端/运行时服务维护自己的 Dockerfile。
- 私有模型/运行时服务应保留在本仓库之外。
- 跨服务调用使用环境变量配置，不要硬编码 hostname。
- 真实密钥应通过部署环境的 secret 管理注入。
- 发布 compose 或部署清单变更前，应先做配置校验。

## 贡献约定

- 保持服务边界清晰，不要在单个改动中混合前端、API、gateway 和模型运行时职责。
- API 契约、队列行为和数据归一化变更应补充测试。
- 除非明确是产品资产，不要提交生成文件、缓存、本地二进制和 notebook。
- 启动方式、环境变量或部署行为变化时，同步更新 README 或服务级文档。
