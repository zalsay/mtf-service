# FinTrack

[English](README.md) | [中文](readme_cn.md)

FinTrack is a full-stack finance tracking and AI-assisted analysis platform. The
repository contains the public application surface, API services, data bridge
services, gateway components, and frontend application code.

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

- `INFERENCE_GATEWAY_URL`: inference gateway URL used by `fintrack-api`
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
