# FinTrack MTF Open API 合约草案

生成日期：2026-06-04

本文定义面向外部 skill / agent / service-to-service 调用的统一 Open API。当前 `fintrack-api` 已有 `/api/v1` JWT 路由，但尚未具备独立的 `/api/open/v1` API key 调用面；本文是后续实现目标合约。

## 1. 设计目标

- 给外部 skill 安全调用 FinTrack 的 MTF、ETF、策略、行情和 Agent 能力。
- 使用 API key 鉴权，并把每个 key 明确绑定到一个 FinTrack `user_id` 或受控的用户映射策略。
- 复用现有 service 层权限和会员规则，避免外部调用绕过用户数据隔离。
- 统一响应 envelope、错误码、审计字段和限流语义。
- 将内部写入接口与开放调用接口隔离，避免直接暴露 `/api/v1/save-predictions/*`。

## 2. 路由命名

统一前缀：

```text
/api/open/v1
```

推荐分组：

- `/api/open/v1/etf/*`：ETF 候选、行情、热门 ETF。
- `/api/open/v1/mtf/*`：预测、缓存、best、future、backtest。
- `/api/open/v1/strategy/*`：策略列表、保存、绑定。
- `/api/open/v1/watchlist/*`：自选股读写。
- `/api/open/v1/agent/*`：MTF Agent 会话、消息、skill。
- `/api/open/v1/uzi/*`：UZI 报告索引和查询。

## 3. 鉴权

请求头：

```http
Authorization: Bearer <fintrack_open_api_key>
X-FinTrack-User: <optional external user alias>
X-Request-Id: <optional caller request id>
```

服务端必须：

- 只保存 API key hash，不保存明文 key。
- 从 key 解析 `owner_user_id`、`scopes`、`status`、`expires_at`、`rate_limit`。
- 每次调用都解析出有效 FinTrack `user_id`。
- 若 key 支持多用户代理，必须通过 `X-FinTrack-User` 或请求体 `external_user_id` 映射到已授权的 FinTrack 用户。
- 默认禁止 admin 全局数据访问；只有显式 `admin:*` scope 的内部 key 才可启用。

建议表：

```sql
CREATE TABLE IF NOT EXISTS open_api_keys (
  id BIGSERIAL PRIMARY KEY,
  key_hash TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  owner_user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  scopes TEXT[] NOT NULL DEFAULT '{}',
  status TEXT NOT NULL DEFAULT 'active',
  expires_at TIMESTAMP WITH TIME ZONE,
  rate_limit_per_minute INTEGER NOT NULL DEFAULT 60,
  created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
  last_used_at TIMESTAMP WITH TIME ZONE
);

CREATE TABLE IF NOT EXISTS open_api_user_mappings (
  id BIGSERIAL PRIMARY KEY,
  key_id BIGINT NOT NULL REFERENCES open_api_keys(id) ON DELETE CASCADE,
  external_user_id TEXT NOT NULL,
  fintrack_user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
  UNIQUE (key_id, external_user_id)
);

CREATE TABLE IF NOT EXISTS open_api_audit_logs (
  id BIGSERIAL PRIMARY KEY,
  request_id TEXT NOT NULL,
  key_id BIGINT,
  fintrack_user_id INTEGER,
  method TEXT NOT NULL,
  path TEXT NOT NULL,
  scope TEXT,
  status_code INTEGER,
  latency_ms INTEGER,
  error_code TEXT,
  created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);
```

## 4. Scopes

| Scope | 能力 |
| --- | --- |
| `etf:read` | 读取热门 ETF、ETF 名称、ETF 行情。 |
| `mtf:read` | 读取 MTF best、future、validation、backtest、缓存预测。 |
| `mtf:predict` | 触发 MTF 单次预测或 best 训练。 |
| `mtf:backtest` | 触发 MTF 回测。 |
| `strategy:read` | 读取用户策略和系统策略。 |
| `strategy:write` | 保存策略参数、绑定策略。 |
| `watchlist:read` | 读取自选股。 |
| `watchlist:write` | 添加、更新、删除自选股。 |
| `agent:chat` | 调用 MTF Agent 会话、消息、skill。 |
| `uzi:read` | 查询 UZI 报告索引和摘要。 |
| `admin:*` | 内部管理用途，默认不授予外部 skill。 |

## 5. 统一响应

成功：

```json
{
  "request_id": "req_20260604_abcdef",
  "status": "ok",
  "data": {}
}
```

失败：

```json
{
  "request_id": "req_20260604_abcdef",
  "status": "error",
  "error": {
    "code": "scope_denied",
    "message": "scope mtf:predict is required",
    "retryable": false
  }
}
```

推荐错误码：

- `invalid_api_key`
- `api_key_expired`
- `api_key_disabled`
- `scope_denied`
- `user_mapping_required`
- `rate_limited`
- `validation_error`
- `upstream_unavailable`
- `prediction_cache_not_found`
- `job_not_found`

## 6. ETF Open API

### GET `/api/open/v1/etf/hot`

Scope: `etf:read`

能力：返回热门 ETF 雷达结构化列表，对应内部 `/api/v1/finance-news/hot-etf`。

响应 `data`：

```json
{
  "source": "meetlife_hot_etf",
  "count": 1,
  "items": [
    {
      "code": "515220",
      "name": "国泰中证煤炭ETF",
      "risk_rps": 92,
      "radar_priority": 93.1,
      "grade": "A",
      "trend": "...",
      "month": { "score": 1, "text": "..." },
      "week": { "score": 1, "text": "..." },
      "day": { "score": 1, "text": "..." },
      "stop_price": "1.234",
      "stop_distance": "5.6%",
      "total_score": 88.5,
      "status": "关注"
    }
  ]
}
```

### POST `/api/open/v1/etf/quotes`

Scope: `etf:read`

请求：

```json
{
  "symbols": ["510300", "159919"]
}
```

语义：按 `stock_type=2` 查询 ETF 最新行情，复用内部 batch latest quotes。

### GET `/api/open/v1/etf/lookup?symbol=510300`

Scope: `etf:read`

语义：等价查询 `stock_type=2` 的名称。

## 7. MTF Open API

### GET `/api/open/v1/mtf/best`

Scope: `mtf:read`

Query:

- `symbol`：可选，ETF 或 A 股代码。
- `stock_type`：可选，ETF 必须为 `2`；默认由代码推断。
- `horizon_len`：可选。
- `include_validation`：默认 `true`。

语义：返回当前 API key 对应用户可访问的 best prediction 和 validation chunks。不要返回其他用户私有数据。

### GET `/api/open/v1/mtf/best/by-config`

Scope: `mtf:read`

Query:

- `symbol`
- `stock_type`
- `horizon_len`
- `context_len`

语义：返回最新 `mtf_lite_unique_key` 和 `mtf_pro_unique_key`。

### GET `/api/open/v1/mtf/future?unique_key=...`

Scope: `mtf:read`

语义：读取未来预测序列、预测最新价、实际最新价、预测涨跌幅。

### POST `/api/open/v1/mtf/predict-once`

Scope: `mtf:predict`

请求：

```json
{
  "stock_code": "510300",
  "stock_type": 2,
  "prediction_type": "mtf-lite",
  "horizon_len": 7,
  "context_len": 256,
  "best_max_age_days": 180,
  "prefer_cache": true
}
```

语义：

1. 当 `prefer_cache=true` 时先查缓存。
2. 缓存 miss 后再按用户会员权限触发 `/predict_once`。
3. 必须注入解析后的 `user_id`，不能信任外部请求体里的 `user_id`。
4. fintrack-api 默认向推理网关注入 `best_max_age_days=180`、`predict_from_best_val_end=true`、`chunk_until_latest=true`；180 日内 best 均视为有效，并从 best `val_end_date` 续跑到当前可用 chunk。

### POST `/api/open/v1/mtf/predict-best`

Scope: `mtf:predict`

请求：

```json
{
  "stock_code": "510300",
  "stock_type": 2,
  "prediction_type": "mtf-pro",
  "horizon_len": 7,
  "context_len": 512,
  "years": 15
}
```

语义：复用现有会员权限规范化逻辑。ETF 必须以 `stock_type=2` 进入下游。

### POST `/api/open/v1/mtf/backtest`

Scope: `mtf:backtest`

请求字段复用内部 MTF 回测请求结构，但服务端必须覆盖 `user_id` 为 API key 解析出的用户。

### GET `/api/open/v1/mtf/jobs/:job_id`

Scope: `mtf:read`

语义：查询 MTF 推理 job 状态。

## 8. Strategy Open API

### GET `/api/open/v1/strategy/list`

Scope: `strategy:read`

语义：返回当前用户策略和公开系统策略。

### POST `/api/open/v1/strategy/params`

Scope: `strategy:write`

请求复用 `SaveStrategyParamsRequest`。服务端必须：

- 覆盖 `user_id` 为当前 key 对应用户，除非内部 key 具备 admin scope。
- 校验 `unique_key`、阈值、仓位、费用等字段。

### POST `/api/open/v1/watchlist/bind-strategy`

Scope: `watchlist:write` + `strategy:read`

请求：

```json
{
  "symbol": "510300",
  "stock_type": 2,
  "strategy_unique_key": "strategy_..."
}
```

语义：给当前用户自选 ETF 绑定策略。

## 9. Watchlist Open API

### GET `/api/open/v1/watchlist`

Scope: `watchlist:read`

语义：返回当前用户自选股/ETF。

### POST `/api/open/v1/watchlist`

Scope: `watchlist:write`

请求：

```json
{
  "symbol": "510300",
  "stock_type": 2,
  "notes": "沪深300 ETF 核心宽基观察"
}
```

语义：添加 ETF 到当前用户自选，复用会员数量限制。

## 10. Agent Open API

### POST `/api/open/v1/agent/messages`

Scope: `agent:chat`

请求：

```json
{
  "message": "从热门ETF中筛选3只适合7日MTF预测的标的，并给出策略规则"
}
```

语义：复用 MTF Agent 非流式消息能力，用户上下文为 API key 对应用户。

### GET `/api/open/v1/agent/skills/history-trends`

Scope: `agent:chat` + `mtf:read`

Query 支持 `symbol`、`unique_key`、`prediction_type`、`horizon_len`、`limit`、`chunk_limit`、`point_limit`。

### GET `/api/open/v1/agent/skills/uzi-reports`

Scope: `agent:chat` + `uzi:read`

Query 支持 `ticker`、`limit`。

## 11. A 股 ETF 助手推荐编排

外部 skill 的 ETF 选择 + 预测 + 策略推荐建议按以下顺序调用：

1. `GET /etf/hot` 获取候选池。
2. `POST /etf/quotes` 补行情。
3. `GET /mtf/best?stock_type=2&include_validation=true` 查已有预测和验证。
4. 对缺失或过期标的调用 `POST /mtf/predict-once` 或 `POST /mtf/predict-best`。
5. `POST /mtf/backtest` 验证策略参数。
6. `POST /strategy/params` 保存策略。
7. `POST /watchlist` 与 `/watchlist/bind-strategy` 更新用户工作台。

任何一步数据不足都应返回明确缺口，不应编造 ETF 实时数值、预测结果或回测收益。

## 12. 实现注意事项

- Open API middleware 应在进入 handler 前设置 `user_id`、`user`、`is_admin=false`、`open_api_key_id`、`request_id`。
- 不要复用 JWT token 校验作为 API key 校验。
- 不要允许外部请求体直接指定 `user_id` 生效。
- 对预测触发、回测、Agent chat 做更低限流。
- 对所有 Open API 响应记录审计日志。
- API key 创建/撤销应只允许用户本人或管理员操作；明文 key 只在创建时显示一次。
- 推荐先实现 read-only scopes，再开放预测和写入 scopes。
