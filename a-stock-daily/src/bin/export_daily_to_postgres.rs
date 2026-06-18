use anyhow::{anyhow, Context, Result};
use chrono::NaiveDate;
use clickhouse::Row;
use flate2::{write::GzEncoder, Compression};
use serde::{Deserialize, Serialize};
use std::env;
use std::io::Write;
use std::time::Duration;
use stock_fetcher::config;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpStream;
use tracing::info;

#[derive(Debug, Row, Deserialize)]
struct DailyRow {
    code: String,
    stock_type: i32,
    open: f64,
    close: f64,
    high: f64,
    low: f64,
    volume: u64,
    amount: f64,
}

#[derive(Debug, Serialize, PartialEq)]
struct StockDataRecord {
    datetime: String,
    date_str: String,
    open: f64,
    close: f64,
    high: f64,
    low: f64,
    volume: u64,
    amount: f64,
    amplitude: f64,
    percentage_change: f64,
    amount_change: f64,
    turnover_rate: f64,
    #[serde(rename = "type")]
    stock_type: i32,
    symbol: String,
}

#[derive(Debug, Clone, PartialEq)]
struct HttpEndpoint {
    host: String,
    port: u16,
    host_header: String,
    base_path: String,
}

#[tokio::main]
async fn main() -> Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(env::var("RUST_LOG").unwrap_or_else(|_| "info".to_string()))
        .init();

    let args: Vec<String> = env::args().collect();
    if args.len() != 2 {
        eprintln!("usage: {} YYYYMMDD", args[0]);
        std::process::exit(2);
    }

    let date = parse_date(&args[1])?;
    let date_str = date.format("%Y-%m-%d").to_string();
    let config_path = env_string("CONFIG_FILE", "config.toml");
    let cfg = config::Config::load(&config_path)
        .with_context(|| format!("failed to load {}", config_path))?;

    let clickhouse_client = clickhouse::Client::default()
        .with_url(&cfg.clickhouse.url)
        .with_user(&cfg.clickhouse.username)
        .with_password(&cfg.clickhouse.password)
        .with_database(&cfg.clickhouse.database);

    ensure_daily_symbol_info_table(&clickhouse_client).await?;

    let rows: Vec<DailyRow> = clickhouse_client
        .query(&daily_sql(&date_str))
        .fetch_all()
        .await
        .with_context(|| format!("failed to query market_data for {}", date_str))?;

    let records: Vec<StockDataRecord> = rows
        .into_iter()
        .map(|row| build_stock_record(row, &date_str))
        .collect();

    if records.is_empty() {
        info!("no rows to export for {}", date_str);
        return Ok(());
    }

    let endpoint = parse_http_endpoint(&env_string(
        "POSTGRES_HANDLER_URL",
        "http://pos-handler:8080",
    ))?;
    let timeout = Duration::from_secs(env_u64("POSTGRES_TIMEOUT", 60)?);
    let batch_size = env_usize("POSTGRES_BATCH_SIZE", 1000)?;
    let mut total = 0usize;

    for batch in records.chunks(batch_size) {
        post_batch(&endpoint, batch, timeout).await?;
        total += batch.len();
        info!(
            "posted {}/{} stock_data rows for {}",
            total,
            records.len(),
            date_str
        );
    }

    Ok(())
}

async fn ensure_daily_symbol_info_table(client: &clickhouse::Client) -> Result<()> {
    client
        .query(
            r#"
CREATE TABLE IF NOT EXISTS daily_symbol_info (
    code String,
    stock_type UInt8,
    updated_at DateTime DEFAULT now()
) ENGINE = ReplacingMergeTree(updated_at)
ORDER BY code
"#,
        )
        .execute()
        .await
        .context("failed to ensure daily_symbol_info table")
}

fn daily_sql(date_str: &str) -> String {
    format!(
        r#"
SELECT
    daily.code AS code,
    toInt32(ifNull(meta.stock_type, multiIf(
        startsWith(daily.code, '5') OR startsWith(daily.code, '15') OR startsWith(daily.code, '16') OR startsWith(daily.code, '18'), 2,
        {default_stock_type}
    ))) AS stock_type,
    open,
    close,
    ifNull(high_raw, greatest(open, close)) AS high,
    ifNull(low_raw, least(open, close)) AS low,
    toUInt64(ifNull(volume_raw, 0)) AS volume,
    toFloat64(ifNull(amount_raw, 0)) AS amount
FROM
(
    SELECT
        code,
        assumeNotNull(argMin(avg_sell_price, time_sec)) AS open,
        assumeNotNull(argMax(avg_sell_price, time_sec)) AS close,
        max(high_price) AS high_raw,
        min(low_price) AS low_raw,
        max(cum_volume) AS volume_raw,
        max(cum_amount) AS amount_raw
    FROM market_data
    WHERE trade_date = toDate('{date_str}')
      AND avg_sell_price IS NOT NULL
    GROUP BY code
) AS daily
LEFT JOIN
(
    SELECT code, anyLast(stock_type) AS stock_type
    FROM daily_symbol_info
    GROUP BY code
) AS meta ON daily.code = meta.code
WHERE open > 0 AND close > 0
ORDER BY daily.code
"#
        ,
        default_stock_type = env_i32("STOCK_TYPE", 1).unwrap_or(1)
    )
}

fn build_stock_record(row: DailyRow, date_str: &str) -> StockDataRecord {
    StockDataRecord {
        datetime: format!("{date_str}T00:00:00+08:00"),
        date_str: date_str.to_string(),
        open: row.open,
        close: row.close,
        high: row.high,
        low: row.low,
        volume: row.volume,
        amount: row.amount,
        amplitude: 0.0,
        percentage_change: 0.0,
        amount_change: 0.0,
        turnover_rate: 0.0,
        stock_type: row.stock_type,
        symbol: row.code.trim().to_string(),
    }
}

async fn post_batch(
    endpoint: &HttpEndpoint,
    batch: &[StockDataRecord],
    timeout: Duration,
) -> Result<()> {
    tokio::time::timeout(timeout, post_batch_inner(endpoint, batch))
        .await
        .context("postgres-handler request timed out")?
}

async fn post_batch_inner(endpoint: &HttpEndpoint, batch: &[StockDataRecord]) -> Result<()> {
    let mut body = serde_json::to_vec(batch)?;
    let mut extra_headers = String::new();

    if body.len() > 1024 {
        let mut encoder = GzEncoder::new(Vec::new(), Compression::default());
        encoder.write_all(&body)?;
        body = encoder.finish()?;
        extra_headers.push_str("Content-Encoding: gzip\r\n");
    }

    if let Ok(token) = env::var("POSTGRES_HANDLER_TOKEN") {
        let token = token.trim();
        if !token.is_empty() {
            ensure_header_value(token, "POSTGRES_HANDLER_TOKEN")?;
            extra_headers.push_str(&format!("X-Token: {token}\r\n"));
        }
    }

    let mut request = String::new();
    request.push_str(&format!(
        "POST {} HTTP/1.1\r\n",
        endpoint.path("/api/v1/stock-data/batch")
    ));
    request.push_str(&format!("Host: {}\r\n", endpoint.host_header));
    request.push_str("Content-Type: application/json\r\n");
    request.push_str(&format!("Content-Length: {}\r\n", body.len()));
    request.push_str("Connection: close\r\n");
    request.push_str(&extra_headers);
    request.push_str("\r\n");

    let mut stream = TcpStream::connect((endpoint.host.as_str(), endpoint.port))
        .await
        .with_context(|| {
            format!(
                "failed to connect postgres-handler {}:{}",
                endpoint.host, endpoint.port
            )
        })?;
    stream.write_all(request.as_bytes()).await?;
    stream.write_all(&body).await?;
    stream.shutdown().await?;

    let mut response = Vec::new();
    stream.read_to_end(&mut response).await?;
    let (status, response_body) = parse_http_response(&response)?;

    if !(200..300).contains(&status) {
        return Err(anyhow!(
            "postgres-handler returned HTTP {}: {}",
            status,
            response_body
        ));
    }

    Ok(())
}

impl HttpEndpoint {
    fn path(&self, suffix: &str) -> String {
        if self.base_path.is_empty() {
            suffix.to_string()
        } else {
            format!("{}/{}", self.base_path, suffix.trim_start_matches('/'))
        }
    }
}

fn parse_http_endpoint(raw: &str) -> Result<HttpEndpoint> {
    let value = raw.trim().trim_end_matches('/');
    let rest = value
        .strip_prefix("http://")
        .ok_or_else(|| anyhow!("POSTGRES_HANDLER_URL must use http://"))?;
    let (authority, path) = rest.split_once('/').unwrap_or((rest, ""));
    if authority.is_empty() {
        return Err(anyhow!("POSTGRES_HANDLER_URL is missing host"));
    }

    let (host, port) = match authority.rsplit_once(':') {
        Some((host, port)) if !host.is_empty() => {
            let port = port
                .parse::<u16>()
                .with_context(|| "invalid POSTGRES_HANDLER_URL port")?;
            (host.to_string(), port)
        }
        _ => (authority.to_string(), 80),
    };

    Ok(HttpEndpoint {
        host,
        port,
        host_header: authority.to_string(),
        base_path: if path.is_empty() {
            String::new()
        } else {
            format!("/{}", path.trim_matches('/'))
        },
    })
}

fn ensure_header_value(value: &str, name: &str) -> Result<()> {
    if value.contains('\r') || value.contains('\n') {
        return Err(anyhow!("{} contains a newline", name));
    }
    Ok(())
}

fn parse_http_response(response: &[u8]) -> Result<(u16, String)> {
    let header_end = response
        .windows(4)
        .position(|window| window == b"\r\n\r\n")
        .ok_or_else(|| anyhow!("invalid HTTP response"))?;
    let header_text = String::from_utf8_lossy(&response[..header_end]);
    let status_line = header_text
        .lines()
        .next()
        .ok_or_else(|| anyhow!("missing HTTP status line"))?;
    let status = status_line
        .split_whitespace()
        .nth(1)
        .ok_or_else(|| anyhow!("missing HTTP status code"))?
        .parse::<u16>()
        .context("invalid HTTP status code")?;
    let body = String::from_utf8_lossy(&response[(header_end + 4)..]).to_string();

    Ok((status, body))
}

fn parse_date(raw: &str) -> Result<NaiveDate> {
    let compact = raw.replace('-', "");
    if compact.len() != 8 || !compact.chars().all(|ch| ch.is_ascii_digit()) {
        return Err(anyhow!("invalid date: {}", raw));
    }

    NaiveDate::parse_from_str(&compact, "%Y%m%d").with_context(|| format!("invalid date: {}", raw))
}

fn env_string(name: &str, default: &str) -> String {
    env::var(name)
        .ok()
        .map(|value| value.trim().to_string())
        .filter(|value| !value.is_empty())
        .unwrap_or_else(|| default.to_string())
}

fn env_i32(name: &str, default: i32) -> Result<i32> {
    env_string(name, &default.to_string())
        .parse()
        .with_context(|| format!("invalid {}", name))
}

fn env_u64(name: &str, default: u64) -> Result<u64> {
    env_string(name, &default.to_string())
        .parse()
        .with_context(|| format!("invalid {}", name))
}

fn env_usize(name: &str, default: usize) -> Result<usize> {
    let value: usize = env_string(name, &default.to_string())
        .parse()
        .with_context(|| format!("invalid {}", name))?;

    if value == 0 {
        return Err(anyhow!("{} must be greater than zero", name));
    }

    Ok(value)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_date_accepts_compact_and_dash_format() {
        assert_eq!(parse_date("20260602").unwrap().to_string(), "2026-06-02");
        assert_eq!(parse_date("2026-06-02").unwrap().to_string(), "2026-06-02");
    }

    #[test]
    fn build_stock_record_matches_postgres_stock_data_shape() {
        let row = DailyRow {
            code: " 600519 ".to_string(),
            stock_type: 1,
            open: 1680.12,
            close: 1699.5,
            high: 1700.0,
            low: 1679.0,
            volume: 123456,
            amount: 78901234.56,
        };

        let record = build_stock_record(row, "2026-06-02");

        assert_eq!(record.symbol, "600519");
        assert_eq!(record.date_str, "2026-06-02");
        assert_eq!(record.datetime, "2026-06-02T00:00:00+08:00");
        assert_eq!(record.open, 1680.12);
        assert_eq!(record.close, 1699.5);
        assert_eq!(record.high, 1700.0);
        assert_eq!(record.low, 1679.0);
        assert_eq!(record.volume, 123456);
        assert_eq!(record.amount, 78901234.56);
        assert_eq!(record.stock_type, 1);
    }

    #[test]
    fn build_stock_record_preserves_etf_type() {
        let row = DailyRow {
            code: "159915".to_string(),
            stock_type: 2,
            open: 2.1,
            close: 2.2,
            high: 2.3,
            low: 2.0,
            volume: 1000,
            amount: 2200.0,
        };

        let record = build_stock_record(row, "2026-06-02");

        assert_eq!(record.symbol, "159915");
        assert_eq!(record.stock_type, 2);
    }

    #[test]
    fn parse_http_endpoint_accepts_host_port_and_base_path() {
        let endpoint = parse_http_endpoint("http://host.docker.internal:58005/base/").unwrap();

        assert_eq!(
            endpoint,
            HttpEndpoint {
                host: "host.docker.internal".to_string(),
                port: 58005,
                host_header: "host.docker.internal:58005".to_string(),
                base_path: "/base".to_string(),
            }
        );
        assert_eq!(
            endpoint.path("/api/v1/stock-data/batch"),
            "/base/api/v1/stock-data/batch"
        );
    }
}
