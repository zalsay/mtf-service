# A 股 Level1 日线同步适配器

这个目录基于 `map-A/A-stock-level1-dump` 构建采集容器：

- `bulk_download YYYYMMDD 并发数` 抓取全 A Level1 成交明细到 ClickHouse。
- `export_daily_to_postgres` 是注入同一 Rust crate 编译出的导出工具，从 ClickHouse `market_data` 聚合出 `stock_data` 所需日线字段。
- 容器默认启动完整 HTTP 服务，并在北京时间每天 22:00 自动同步当日全 A 股 Level1 数据。
- 提供 history 查询接口，从 ClickHouse 聚合返回某只股票所有已下载历史日线。

只写入当前 Postgres 的 `stock_data` 日线结构：`datetime/date_str/open/close/high/low/volume/amount/type/symbol`。Level1 数据不能可靠提供昨收、换手率、涨跌幅，所以 `amplitude/percentage_change/amount_change/turnover_rate` 固定写 `0`。

## 单独运行

```bash
cd /projects/ai-fin/mtf-service/a-stock-daily
docker compose up -d --build
```

默认会调用宿主机 `http://host.docker.internal:58004` 的 `postgres-handler`。如端口或 token 不同：

```bash
POSTGRES_HANDLER_URL=http://host.docker.internal:58004 \
POSTGRES_HANDLER_TOKEN=fintrack-dev-token \
docker compose up -d --build
```

## HTTP 服务

容器默认监听 `:8080`，compose 映射到宿主机 `59088`。手动触发某日同步：

```bash
curl -X POST http://127.0.0.1:59088/daily \
  -H 'X-Token: fintrack-dev-token' \
  -H 'Content-Type: application/json' \
  -d '{"date":"20260602"}'
```

不传 `date` 时按容器时区当天日期执行。

查询某只股票所有已下载历史：

```bash
curl 'http://127.0.0.1:59088/api/v1/history?symbol=600186' \
  -H 'X-Token: fintrack-dev-token'
```

可选日期范围：

```bash
curl -X POST http://127.0.0.1:59088/api/v1/history \
  -H 'X-Token: fintrack-dev-token' \
  -H 'Content-Type: application/json' \
  -d '{"symbol":"600186","start_date":"2026-05-01","end_date":"2026-05-31"}'
```

## 股票池 / ETF

默认 `STOCK_UNIVERSE=postgres` 且 `SYMBOL_SOURCE=watchlist`，每次 daily/range 前会从 Postgres `user_watchlist` 同步股票池，只下载用户 watchlist 中 `stock_type IN (1, 2)` 的 A 股股票和 ETF。

ETF 支持沪市 `5xxxxx` 和深市 `15xxxx/16xxxx/18xxxx` 等 6 位代码。同步脚本会写入 ClickHouse:

- `stock_info`: 兼容上游下载工具需要的代码列表。
- `daily_symbol_info`: 保存 `code -> stock_type`，导出到 Postgres 时 ETF 会按 `type=2` 写入，不会混成股票 `type=1`。

如需下载当前 Postgres `stock_data` 中已有的股票/ETF 代码，而不是 watchlist：

```bash
STOCK_UNIVERSE=postgres SYMBOL_SOURCE=postgres
```

如需下载全 A 股：

```bash
STOCK_UNIVERSE=all
```

如需沿用 ClickHouse 现有 `stock_info`：

```bash
STOCK_UNIVERSE=keep
```

## Gateway 兼容

gateway 侧仍可启用 Level1 trigger 调度：

```bash
DAILY_STOCK_SYNC_ENABLED=true
DAILY_STOCK_SYNC_MODE=level1
DAILY_STOCK_SYNC_HOUR=22
DAILY_STOCK_SYNC_MINUTE=0
A_STOCK_DAILY_URL=http://a-stock-daily:8080
A_STOCK_DAILY_CONCURRENT=50
GATEWAY_API_TOKEN=fintrack-dev-token
```

如果 `DAILY_STOCK_SYNC_MODE` 留空但配置了 `A_STOCK_DAILY_URL`，gateway 会自动使用 Level1 trigger；否则保留旧的 history API 逐股票同步模式。完整服务模式下也可以只使用容器内部 22:00 自动任务，不再依赖 gateway 调度。

## 手动补全历史区间

```bash
docker compose run --rm a-stock-daily range 20240401 20250401 50
```

## 全 A 历史回填到 ClickHouse

从 `2010-01-01` 开始跑全 A 股历史 Level1 数据，默认只写 ClickHouse，不导出到 Postgres：

```bash
./scripts/backfill_all_a_from_2010.sh
```

上游 `A-stock-level1-dump` 当前容器内交易日历从 `2015-01-05` 开始；脚本保留 `2010-01-01` 作为请求起点，但会自动从可执行的最早日期开始实际下载。脚本会使用 compose project `a-stock-level1-daily`，并替换同名后台容器 `a-stock-daily-history-backfill`。可覆盖参数：

```bash
START_DATE=20100101 MIN_SUPPORTED_DATE=20150105 END_DATE=20260602 CONCURRENT=50 ./scripts/backfill_all_a_from_2010.sh
```

## 手动同步单日

```bash
docker compose run --rm a-stock-daily daily 20260602 50
```

## 通达信日 K 历史补全

`bulk_download` 抓的是 Level1 日内快照，上游目前有效数据从 2023 年附近开始。容器内另有 Rust 实现的通达信日 K 工具，可用于补 `2010` 起的 OHLCV 日线历史，写入 ClickHouse `tdx_daily_bars` 表：

```bash
docker compose run --rm a-stock-daily tdx-backfill-symbol 000001 0 8 800
```

只查看结果，不写库：

```bash
docker compose run --rm a-stock-daily tdx-bars 000001 3200 1 10 csv
```

说明：

- `start` 是通达信 K 线偏移，`0` 表示最近数据，`800/1600/...` 逐页向历史回溯。
- `count` 单次最大 `800`。
- 这是日 K 线，不是 Level1 快照；字段为 `open/close/high/low/volume/amount`。

## 只导出已在 ClickHouse 的某日数据到 Postgres

```bash
docker compose run --rm a-stock-daily export 20260602
```
