ARG A_STOCK_REPO_URL=https://github.com/map-A/A-stock-level1-dump.git
ARG A_STOCK_ARCHIVE_URL=https://github.com/map-A/A-stock-level1-dump/archive
ARG A_STOCK_REF=c173115b55c39d439ff91566bd608992edd5b2c7
ARG CARGO_REGISTRY_MIRROR=
ARG RUST_IMAGE=docker.xuanyuan.run/rust:1.94-bookworm
ARG PYTHON_IMAGE=python:3.12-slim

FROM ${RUST_IMAGE} AS builder

ARG A_STOCK_REPO_URL
ARG A_STOCK_ARCHIVE_URL
ARG A_STOCK_REF
ARG CARGO_REGISTRY_MIRROR

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY src/bin/export_daily_to_postgres.rs /tmp/export_daily_to_postgres.rs
COPY src/bin/tdx_daily_bars.rs /tmp/tdx_daily_bars.rs
COPY src/bin/eastmoney_adjusted_bars.rs /tmp/eastmoney_adjusted_bars.rs
RUN if [ -n "${CARGO_REGISTRY_MIRROR}" ]; then \
      mkdir -p /usr/local/cargo \
      && printf '[source.crates-io]\nreplace-with = "mirror"\n\n[source.mirror]\nregistry = "%s"\n\n[net]\nretry = 5\n' "${CARGO_REGISTRY_MIRROR}" > /usr/local/cargo/config.toml; \
    fi \
    && curl -L --retry 5 --retry-delay 2 -o /tmp/a-stock-level1-dump.tar.gz "${A_STOCK_ARCHIVE_URL}/${A_STOCK_REF}.tar.gz" \
    && mkdir a-stock-level1-dump \
    && tar -xzf /tmp/a-stock-level1-dump.tar.gz -C a-stock-level1-dump --strip-components=1 \
    && cd a-stock-level1-dump \
    && if ! grep -q '^ureq =' Cargo.toml; then sed -i '/^parking_lot =/a ureq = { version = "2.12", features = ["tls"] }' Cargo.toml; fi \
    && cp /tmp/export_daily_to_postgres.rs src/bin/export_daily_to_postgres.rs \
    && cp /tmp/tdx_daily_bars.rs src/bin/tdx_daily_bars.rs \
    && cp /tmp/eastmoney_adjusted_bars.rs src/bin/eastmoney_adjusted_bars.rs \
    && cargo build --release --bin bulk_download --bin export_daily_to_postgres --bin tdx_daily_bars --bin eastmoney_adjusted_bars

FROM ${PYTHON_IMAGE}

RUN apt-get update \
    && apt-get install -y --no-install-recommends bash ca-certificates curl postgresql-client tzdata \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /src/a-stock-level1-dump/target/release/bulk_download /usr/local/bin/bulk_download
COPY --from=builder /src/a-stock-level1-dump/target/release/export_daily_to_postgres /usr/local/bin/export_daily_to_postgres
COPY --from=builder /src/a-stock-level1-dump/target/release/tdx_daily_bars /usr/local/bin/tdx_daily_bars
COPY --from=builder /src/a-stock-level1-dump/target/release/eastmoney_adjusted_bars /usr/local/bin/eastmoney_adjusted_bars
COPY --from=builder /src/a-stock-level1-dump/config.toml /app/config.toml
COPY --from=builder /src/a-stock-level1-dump/data /app/data
COPY scripts /app/scripts

ENV TZ=Asia/Shanghai \
    CLICKHOUSE_URL=http://a-stock-clickhouse:8123 \
    CLICKHOUSE_DATABASE=stock_db \
    CLICKHOUSE_USER=stock_user \
    CLICKHOUSE_PASSWORD=stock_pass \
    POSTGRES_HANDLER_URL=http://pos-handler:8080 \
    POSTGRES_HANDLER_TOKEN=fintrack-dev-token \
    SERVICE_PORT=8080 \
    TRIGGER_TOKEN=fintrack-dev-token \
    TRIGGER_TIMEOUT_SECONDS=7200 \
    AUTO_DAILY_SYNC_ENABLED=true \
    DAILY_RUN_HOUR=22 \
    DAILY_RUN_MINUTE=0 \
    STOCK_UNIVERSE=postgres \
    EXPORT_TO_POSTGRES=true \
    DOWNLOAD_CONCURRENT=50 \
    POSTGRES_BATCH_SIZE=1000 \
    STOCK_TYPE=1

RUN chmod +x /app/scripts/*.sh

EXPOSE 8080

ENTRYPOINT ["/app/scripts/entrypoint.sh"]
CMD ["server"]
