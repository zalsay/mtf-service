# fintrack-api 能力梳理

生成日期：2026-06-04

本文基于当前仓库的 `fintrack-api` 代码整理，重点覆盖已经注册到路由、handler 和 service 中的能力。服务入口为 Gin HTTP API，默认监听 `SERVER_PORT`/`PORT`，当前默认值为 `59000`；业务接口统一挂载在 `/api/v1` 下，另有 `/health` 健康检查。

## 1. 服务定位

`fintrack-api` 是 FinTrack 的后端 API 聚合层，主要职责包括：

- 管理用户、JWT session、会员等级和邀请码。
- 管理自选股、股票名称/行情查询、策略绑定。
- 代理 MTF 预测、单次预测、最佳分位训练和回测任务。
- 持久化 MTF 最佳预测、验证分块、未来预测、回测和策略参数。
- 提供 MTF Agent 会话、消息、记忆、SSE 流式响应、后台 job 和内置 skill。
- 代理 UZI 研报生成、任务状态、WebSocket 状态推送、报告索引/打开/删除。
- 管理用户 AI 模型配置，并提供通用 LLM chat 能力。
- 提供财经新闻聚合查询，并在财经资讯中提供热门 ETF 结构化列表。
- 提供基于 Alipay AI Pay 402 协议的付费单次 MTF 预测入口。

## 2. 运行依赖

核心依赖：

- PostgreSQL：保存用户、session、自选股、MTF 预测/回测、MTF Agent、UZI 报告、AI 支付记录等数据。
- 推理网关服务：`INFERENCE_GATEWAY_URL`，用于 `/predict_for_best`、`/predict_once`、`/jobs/:id`、`/api/sync-stock`。
- postgres-handler：`POSTGRES_HANDLER_URL`，用于单次预测缓存查询和 LLM token usage 记录。
- DeepSeek TUI runtime：`MTF_AGENT_RUNTIME_URL`，用于 MTF Agent thread/turn 交互。
- UZI service / gateway：`UZI_SERVICE_URL` 和 `UZI_GATEWAY_URL`，用于研报生成、报告文件和队列任务。
- Alipay service：`ALIPAY_SERVICE_URL`，用于付费预测凭证校验。
- 外部行情/数据源：东方财富、腾讯行情、MeetLife Hot ETF 等，主要用于行情兜底、财经资讯和 MTF Agent A 股数据 skill。
- OpenAI-compatible LLM：全局 `OPENAI_*` 或用户级 `user_ai_model_configs`。

主要配置项：

- `DB_HOST`、`DB_PORT`、`DB_USER`、`DB_PASSWORD`、`DB_NAME`、`DB_SSLMODE`
- `JWT_SECRET`、`JWT_EXPIRATION_HOURS`
- `MTF_SERVICE_TOKEN`
- `CORS_ALLOWED_ORIGINS`、`CORS_ALLOWED_METHODS`、`CORS_ALLOWED_HEADERS`
- `INFERENCE_GATEWAY_URL`、`INFERENCE_GATEWAY_TIMEOUT`
- `POSTGRES_HANDLER_URL`、`POSTGRES_HANDLER_TIMEOUT`
- `UZI_ENABLED`、`UZI_SERVICE_URL`、`UZI_GATEWAY_URL`、`UZI_SERVICE_TIMEOUT`、`UZI_OPEN_TOKEN_TTL_SECONDS`
- `MTF_AGENT_ENABLED`、`MTF_AGENT_RUNTIME_URL`、`MTF_AGENT_TIMEOUT`、`MTF_AGENT_MODEL`
- `OPENAI_API_KEY`、`OPENAI_BASE_URL`、`OPENAI_DEFAULT_MODEL`、`OPENAI_MAX_CONTEXT_ROUNDS`、`OPENAI_TIMEOUT`
- `ALIPAY_SERVICE_URL`、`ALIPAY_SERVICE_TOKEN`、`ALIPAY_RESOURCE_ID`、`ALIPAY_RESOURCE_NAME`、`ALIPAY_AI_PAY_AMOUNT_CENTS`
- `OSS_*`：UZI 报告对象存储相关配置。

## 3. 通用能力

- 请求体 gzip 解压：当请求头 `Content-Encoding: gzip` 时，路由层会尝试解压请求体。
- 响应 gzip 压缩：全局启用自定义 gzip response middleware。
- CORS：按配置允许来源、方法、请求头和 credentials。
- JWT 鉴权：支持 `Authorization: Bearer <token>`，WebSocket 升级请求也支持 query 参数 `token`。
- 可选鉴权：部分公开接口可匿名访问，带有效 token 时会注入用户和管理员上下文。
- 管理员鉴权：`/admin` 路由要求已登录且 `is_admin=true`。

## 4. 认证、会员与管理

### 4.1 认证接口

| 方法 | 路径 | 鉴权 | 能力 |
| --- | --- | --- | --- |
| POST | `/api/v1/auth/register` | 否 | 注册用户，支持 `activation_code` 激活会员邀请码。 |
| POST | `/api/v1/auth/login` | 否 | 登录，签发 JWT，并写入 `user_sessions`。 |
| GET | `/api/v1/auth/status` | 可选 | 返回是否已认证和用户基础状态。 |
| GET | `/api/v1/auth/profile` | 是 | 返回用户 profile、会员和 DSA 绑定字段。 |
| POST | `/api/v1/auth/logout` | 是 | 删除当前 token 对应 session。 |
| PUT | `/api/v1/auth/membership` | 是 | 更新当前用户会员等级和过期时间。 |
| POST | `/api/v1/auth/redeem-invite` | 是 | 兑换会员邀请码。 |

会员等级范围为 `0..3`。当前自选股数量限制为：等级 0 最多 3 只，等级 2 最多 10 只，等级 3 最多 50 只；等级 1 仍按默认 3 只处理。

### 4.2 管理接口

| 方法 | 路径 | 鉴权 | 能力 |
| --- | --- | --- | --- |
| GET | `/api/v1/admin/status` | 管理员 | 返回管理员状态。 |
| GET | `/api/v1/admin/invite-codes` | 管理员 | 列出会员邀请码。 |
| POST | `/api/v1/admin/invite-codes` | 管理员 | 创建邀请码，支持会员等级、有效天数、最大使用次数、备注。 |
| PATCH | `/api/v1/admin/invite-codes/:id/active` | 管理员 | 启用/禁用邀请码。 |
| GET | `/api/v1/admin/system-strategies` | 管理员 | 列出公开系统策略。 |
| POST | `/api/v1/admin/system-strategies` | 管理员 | 新增或更新公开系统策略。 |

## 5. 自选股、行情与策略

| 方法 | 路径 | 鉴权 | 能力 |
| --- | --- | --- | --- |
| POST | `/api/v1/watchlist` | 是 | 添加自选股，校验股票/ETF 是否存在，检查会员数量限制，异步触发行情同步。 |
| GET | `/api/v1/watchlist` | 是 | 返回用户自选股、最新关联 MTF unique key、绑定策略、超限标记。 |
| PUT | `/api/v1/watchlist/:id` | 是 | 更新自选股备注。 |
| DELETE | `/api/v1/watchlist/:id` | 是 | 删除自选股。 |
| POST | `/api/v1/watchlist/bind` | 是 | 将自选股绑定到用户策略或系统策略。 |
| POST | `/api/v1/quotes/batch-latest` | 是 | 批量查询 A 股/ETF 最新价格、涨跌幅、交易日期、换手率。 |
| GET | `/api/v1/stocks/lookup?symbol=&stock_type=` | 否 | 查询股票或 ETF 名称。 |
| GET | `/api/v1/stocks` | 否 | 预留接口，返回 coming soon。 |
| GET | `/api/v1/stocks/:symbol` | 否 | 预留接口，返回 coming soon。 |

行情来源优先查询本地 `a_stock_comment_daily` / `etf_daily`，并通过东方财富历史 K 线查询前一交易日行情做兜底更新。

策略接口：

| 方法 | 路径 | 鉴权 | 能力 |
| --- | --- | --- | --- |
| POST | `/api/v1/strategy/params` | 是 | 保存用户策略参数。 |
| GET | `/api/v1/strategy/params/by-unique?unique_key=` | 否 | 按 unique key 查询策略参数。 |
| GET | `/api/v1/strategy/list` | 是 | 查询用户策略和公开系统策略。 |

策略参数包括买入/卖出阈值、初始资金、仓位控制、调仓容忍度、交易费率、止盈阈值和止盈卖出比例。

## 6. MTF 预测、回测与数据落库

### 6.1 推理代理

| 方法 | 路径 | 鉴权 | 能力 |
| --- | --- | --- | --- |
| POST | `/api/v1/mtf/predict` | 否 | 直接代理到推理网关 `/predict_for_best`。 |
| POST | `/api/v1/mtf/predict-best` | 是 | 按会员权限规范化最佳分位训练请求，再代理 `/predict_for_best`。 |
| POST | `/api/v1/mtf/predict-once` | 是 | 按会员权限规范化单次预测请求，再代理 `/predict_once`。 |
| POST | `/api/v1/mtf/predict-once/cached` | 是 | 从 postgres-handler 查询单次预测缓存。 |
| GET | `/api/v1/mtf/jobs/:jobID` | 是 | 查询推理网关 job 状态。 |
| POST | `/api/v1/mtf/backtest` | 是 | 代理 MTF 回测请求。 |
| GET | `/api/v1/mtf/backtest/by-unique?unique_key=` | 是 | 按 unique key 查询已落库回测结果。 |

会员权限约束：

- 等级 0：仅 `mtf-lite`，`context_len=256`，`horizon_len=7`。
- 等级 1：`mtf-lite`/`mtf-pro`，`context_len=256/512`，`horizon_len=7/14/28`。
- 等级 2：`mtf-lite`/`mtf-pro`，`context_len=256/512/1024`，`horizon_len=7/14/28`。
- 等级 3：`mtf-lite`/`mtf-pro`，`context_len=256/512/1024/2048`，`horizon_len=7/14/28`。
- 管理员会自动设置 `force_enqueue`，单次预测还会设置 `force_requeue`。

`mtf-pro` 预测会默认补充 `market_cov_v1` 和 `xreg + mtf` 协变量配置。

### 6.2 预测结果查询

| 方法 | 路径 | 鉴权 | 能力 |
| --- | --- | --- | --- |
| GET | `/api/v1/get-predictions/mtf-best` | 是 | 查询当前用户关联的 MTF best 列表。 |
| GET | `/api/v1/get-predictions/mtf-best/public` | 可选 | 匿名只查公开 best；管理员 token 可包含私有数据。支持 `horizon_len`、`symbol`。 |
| GET | `/api/v1/get-predictions/mtf-best/accessible` | 是 | 查询当前用户可访问 best 和 validation chunks；管理员可看全部。支持 `horizon_len`、`symbol`。 |
| GET | `/api/v1/get-predictions/mtf-best/future?unique_key=` | 否 | 查询未来预测日期、预测值、预测最新价、实际最新价和预测涨跌幅。 |

`public` 和 `accessible` 返回时会联查验证分块，并过滤实际值或预测值为 0/NaN/Inf 的点，计算 `max_deviation_percent`。

`accessible` 查询还会对超过 180 天未更新的 MTF best 触发后台刷新入队。

### 6.3 预测结果写入

| 方法 | 路径 | 鉴权 | 能力 |
| --- | --- | --- | --- |
| POST | `/api/v1/save-predictions/mtf-best` | 否 | UPSERT MTF best prediction。当前保存时统一写为公开。 |
| GET | `/api/v1/save-predictions/mtf-best/by-unique?unique_key=` | 否 | 按 unique key 查询 best prediction。 |
| GET | `/api/v1/save-predictions/mtf-best/by-config?symbol=&horizon_len=&context_len=` | 否 | 按配置查询最新 mtf-lite/mtf-pro unique key。 |
| POST | `/api/v1/save-predictions/mtf-best/val-chunk` | 否 | UPSERT validation chunk，包括预测、实际值、日期、涨跌幅和协变量信息。 |
| GET | `/api/v1/save-predictions/mtf-best/val-chunk/latest?unique_key=` | 否 | 查询最新 validation chunk 元信息。 |
| POST | `/api/v1/save-predictions/backtest` | 否 | UPSERT MTF 回测结果。 |

落库数据支持 `prediction_type`、`covariate_config`、`covariate_signature`、`covariate_analysis`，回测支持收益、基准、验证区间、仓位控制、每段信号、权益曲线、交易明细等结构化 JSON 字段。

## 7. 付费单次预测

| 方法 | 路径 | 鉴权 | 能力 |
| --- | --- | --- | --- |
| POST | `/api/v1/paid/mtf/predict-once` | 支付凭证 | 使用 Alipay service 校验支付凭证，通过后执行 MTF 单次预测。 |

请求通过 `Authorization` 携带凭证，支持 `Alipay-AI-Pay <credential>`、`Bearer <credential>` 或裸凭证。可选请求头 `X-Alipay-Order-Id`。未提供或校验失败时返回 HTTP 402，并带 `alipay-ai-pay-402` 支付信息。

服务会按 `resource_id + order_id + request_signature + period_key` 写入 `ai_payment_records`：

- 首次请求进入 `processing` 并实际执行预测。
- 同一日同订单同请求已 fulfilled 时直接返回缓存响应。
- 同一记录仍在 processing 时返回 409。

## 8. MTF Agent

### 8.1 外部 API

所有 `/api/v1/mtf-agent` 接口均要求登录。

| 方法 | 路径 | 能力 |
| --- | --- | --- |
| GET | `/session` | 返回当前 thread、model、runtime 可用性、记忆数量、用户 AI 模型配置状态。 |
| GET | `/messages` | 返回当前 thread 最近最多 100 条消息。 |
| POST | `/messages` | 非流式发送消息，返回 assistant 回复。 |
| POST | `/messages/stream` | SSE 发送消息，事件包括 `start`、`heartbeat`、`delta`、`done`、`error`。 |
| POST | `/messages/jobs` | 创建后台消息 job，返回 `job_id` 和 `queued`。 |
| GET | `/messages/jobs/:jobID` | 查询后台 job 状态：`queued`、`running`、`completed`、`failed`。 |
| POST | `/reset` | 创建新 DeepSeek thread 并清空本地消息历史。 |
| GET | `/memory` | 查询长期记忆。 |
| DELETE | `/memory` | 清空长期记忆。 |
| GET | `/skills/history-trends` | 直接调用历史走势/预测趋势 skill。 |
| GET | `/skills/uzi-reports` | 直接调用 UZI 研报 skill。 |

### 8.2 Agent 上下文

发送消息时，服务会构造中文投资研究辅助 prompt，包含：

- 安全边界：不承诺收益，不替代用户决策。
- 回答格式：先结论，再关键依据和风险。
- 用户长期偏好：来自 `mtf_agent_memories`。
- 当前上下文：最近 20 个自选股、最近 10 个相关 MTF 预测、最近 10 个 UZI 研报。
- 流式接口额外加入最近 8 条本地消息历史。
- 当问题命中股票代码和关键词时，自动预取内置 skill 结果。

### 8.3 Agent runtime 与工具

MTF Agent 依赖 DeepSeek TUI runtime：

- 创建 thread：`POST /v1/threads`
- 发送 turn：`POST /v1/threads/:threadID/turns`
- 查询 thread/turn 状态：`GET /v1/threads/:threadID`
- 请求会透传用户 AI 模型配置到 runtime，并通过 header 传递 API key 和 base URL。

内置标准 tools：

- `history_trends`：查询 MTF 历史走势、历史预测趋势和验证分段数据。参数支持 `symbol`、`unique_key`、`prediction_type`、`horizon_len`、`limit`、`chunk_limit`、`point_limit`。
- `uzi_reports`：查询用户已有 UZI 研报索引和摘要。参数支持 `ticker`、`limit`。
- `a_stock_data`：处理 A 股数据问题，意图包括估值、题材归因、研报检索、北向资金、概念板块、资金流向、龙虎榜、限售解禁、行业轮动、融资融券、大宗交易、股东户数、分红送转、新闻公告、批量对比。

当前 `a_stock_data` 已接入的实时 provider 包括：

- `valuation`：腾讯行情。
- `money_flow`：东方财富资金流。
- `lhb`、`market_lhb`：东方财富龙虎榜。
- `sector_rotation`：东方财富行业/板块列表。
- `news_announcements`：财经新闻服务。

其他 A 股意图会返回数据源限制、所需字段和回答指导，不编造实时数值。

### 8.4 Skill 限制

- 标准 tool 调用最多 2 轮。
- `history_trends` 默认最多 3 条记录，最大 5 条；默认 3 个 chunk，最大 6 个；默认 30 个点，最大 80 个点。
- `uzi_reports` 默认最多 5 条，最大 10 条。
- skill 结果 prompt 注入最长约 12000 字符。

## 9. UZI 研报

| 方法 | 路径 | 鉴权 | 能力 |
| --- | --- | --- | --- |
| GET | `/api/v1/uzi/health` | 否 | 代理 UZI service 健康检查。 |
| GET | `/api/v1/uzi/report-open` | open token | 使用短期 open token 打开报告。 |
| POST | `/api/v1/uzi/analyze` | 是 | 入队生成 UZI 研报，要求用户 AI 模型配置可用。 |
| GET | `/api/v1/uzi/jobs/:jobID` | 是 | 查询 UZI 队列 job，并在成功时持久化报告记录。 |
| GET | `/api/v1/uzi/status` | 是 | 查询当前用户内存态研报任务状态。 |
| GET | `/api/v1/uzi/status/ws` | 是 | WebSocket 推送当前用户研报任务状态。 |
| GET | `/api/v1/uzi/reports-index?ticker=` | 是 | 查询当前用户报告列表。 |
| POST | `/api/v1/uzi/reports-open-token` | 是 | 创建短期报告打开 token。 |
| DELETE | `/api/v1/uzi/reports-entry?relative_path=` | 是 | 删除报告，支持 OSS 对象删除和 DB 软删除。 |
| GET | `/api/v1/uzi/reports/*path` | 是 | 代理读取报告内容。 |

研报生成走 UZI gateway 队列。成功后服务会落库 `uzi_reports`，并维护用户级内存状态，供 HTTP 和 WebSocket 查询。

## 10. LLM 与用户 AI 模型配置

| 方法 | 路径 | 鉴权 | 能力 |
| --- | --- | --- | --- |
| POST | `/api/v1/llm/chat` | 是 | OpenAI-compatible chat completion。优先使用用户 AI 模型配置，缺省回退全局 OpenAI 配置。 |
| GET | `/api/v1/llm/models` | 是 | 从全局 OpenAI client 拉取可用 GPT chat 模型列表。 |
| GET | `/api/v1/settings/ai-model` | 是 | 查询用户 AI 模型配置；无配置时返回推荐 DeepSeek 配置和 masked 状态。 |
| PUT | `/api/v1/settings/ai-model` | 是 | 保存用户 AI 模型配置，校验 base URL、API key、model ID。 |

推荐配置为 DeepSeek：`https://api.deepseek.com` + `deepseek-v4-pro`。API key 返回时只返回 masked 值。LLM chat 完成后会异步向 postgres-handler 写 token usage。

## 11. 财经新闻

| 方法 | 路径 | 鉴权 | 能力 |
| --- | --- | --- | --- |
| GET | `/api/v1/finance-news?category=&symbol=&keyword=&limit=&page=` | 否 | 查询财经新闻列表。 |
| GET | `/api/v1/finance-news/hot-etf` | 否 | 查询热门 ETF 标的评分明细列表，供财经资讯的“热门 ETF” tab 展示。 |

该能力由 `FinanceNewsService` 实现，并被 MTF Agent 的 `a_stock_data.news_announcements` 复用。常规新闻分类支持 `market`、`global`、`stock`、`announcements`、`lhb`；热门 ETF 是财经资讯内的独立 tab，不作为侧边栏独立菜单。

热门 ETF 数据来源为 `https://ai.meetlife.top/hot-etf/latest`。服务优先读取本地 HTML 缓存 `fintrack-api/data/hot-etf/latest.html`；当本地文件不存在或为空时才请求远端，并将 HTML 写回本地路径，避免每次接口调用都拉取远端页面。

后端不会把远端 HTML 直接透传给前端，而是解析页面中的 `table#radarTable` 标的评分明细列表，返回结构化 JSON：

```json
{
  "status": "ok",
  "source": "meetlife_hot_etf",
  "count": 1,
  "items": [
    {
      "code": "515220",
      "name": "国泰中证煤炭ETF",
      "risk_rps": 92,
      "radar_priority": 93.1,
      "grade": "A",
      "trend": "2026-05-19~2026-06-03: +7.1→+19.6 (+2...)",
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

前端 `FinanceNews` 在 `hot_etf` tab 下调用 `/api/v1/finance-news/hot-etf`，展示标的、风险 RPS、雷达优先级、月/周/日信号、止损、总分、状态和趋势。趋势列允许换行，避免较长的评分变化文本被截断。

## 12. 数据模型与表

当前 schema 涉及的主要表：

- 用户与权限：`users`、`user_sessions`、`membership_invite_codes`
- AI 配置：`user_ai_model_configs`
- MTF Agent：`mtf_agent_sessions`、`mtf_agent_memories`、`mtf_agent_messages`
- 股票与自选股：`stocks`、`stock_prices`、`user_watchlist`
- MTF：`mtf_strategy_params`、`mtf_best_predictions`、`mtf_best_validation_chunks`、`mtf_backtests`
- UZI：`uzi_reports`
- 支付：`ai_payment_records`

此外，行情和名称查询依赖外部/同步数据表 `a_stock_comment_daily` 与 `etf_daily`。

热门 ETF 的远端 HTML 快照保存在本地文件 `fintrack-api/data/hot-etf/latest.html`，用于解析财经资讯中的热门 ETF 列表，不落数据库。

## 13. 需要注意的边界

- `fintrack-api` 本身不执行 MTF 模型推理；它负责权限、参数规范化、代理调用和结果落库。
- `/api/v1/save-predictions/*` 当前未加鉴权，适合作为内部写入接口使用，公网暴露时需要额外网关或鉴权保护。
- `/api/v1/mtf/predict` 当前未加鉴权，是直通推理网关 `/predict_for_best` 的代理入口。
- MTF Agent 的后台 job 存在内存 map 中，服务重启会丢失 job 状态。
- `MTF Agent` 的 SSE 接口当前会先返回整段 assistant 文本作为一次 `delta`，不是逐 token 流式输出。
- UZI 状态 hub 是进程内状态，服务重启后只保留数据库中的报告记录，不保留运行中状态。
- 数据库 schema 初始化在 `main.go` 中被禁用，生产/部署应手动执行 `database/schema.sql` 或安全迁移脚本。
- 仓库当前存在未提交改动；本文档反映的是当前工作区代码状态，而非某个已提交版本。
