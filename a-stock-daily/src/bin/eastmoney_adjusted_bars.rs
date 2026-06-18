use anyhow::{anyhow, Context, Result};
use chrono::{Datelike, Local};
use serde::{Deserialize, Serialize};
use std::env;
use std::io::{Read, Write};
use std::net::TcpStream;
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::Duration;

#[derive(Debug, Clone, Serialize)]
struct DailyBar {
    code: String,
    adjust: String,
    trade_date: String,
    datetime: String,
    date_str: String,
    open: f64,
    close: f64,
    high: f64,
    low: f64,
    volume: u64,
    amount: f64,
    source: String,
}

#[derive(Debug, Deserialize)]
struct EastmoneyResponse {
    rc: i64,
    data: Option<EastmoneyData>,
}

#[derive(Debug, Deserialize)]
struct EastmoneyData {
    klines: Option<Vec<String>>,
}

fn main() -> Result<()> {
    let args: Vec<String> = env::args().collect();
    let mode = args.get(1).map(String::as_str).unwrap_or("history");
    let default_start;
    let start_raw = if let Some(value) = args.get(2) {
        value.as_str()
    } else {
        default_start = if mode == "daily" {
            today_ymd_string()
        } else {
            env_string("TDX_START_DATE", "2010-01-01")
        };
        default_start.as_str()
    };
    let start_date = normalize_date(start_raw)?;
    let default_end;
    let end_raw = if let Some(value) = args.get(3) {
        value.as_str()
    } else {
        default_end = if mode == "daily" {
            today_ymd_string()
        } else {
            env_string("TDX_END_DATE", "20500101")
        };
        default_end.as_str()
    };
    let end_date = normalize_date(end_raw)?;
    let adjusts = env_string("ADJUSTED_BARS_ADJUSTS", "qfq,hfq")
        .split(',')
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .map(str::to_string)
        .collect::<Vec<_>>();
    let workers = env_usize("ADJUSTED_BARS_WORKERS", env_usize("TDX_BACKFILL_WORKERS", 8)? as u64)?;
    let retries = env_usize("TDX_RETRIES", 3)?;

    ensure_table()?;
    let symbols = load_symbols()?;
    if symbols.is_empty() {
        return Err(anyhow!("no symbols found in stock_info"));
    }

    println!(
        "eastmoney {mode} start={start_date} end={end_date} adjusts={} symbols={} workers={workers}",
        adjusts.join(","),
        symbols.len()
    );
    let mut pending = symbols;
    let mut total_ok = 0usize;
    let mut failed = Vec::new();

    for attempt in 0..=retries {
        if attempt > 0 {
            println!("retry attempt {attempt}/{retries} symbols={}", pending.len());
            thread::sleep(Duration::from_secs(env_u64("TDX_RETRY_SLEEP_SECONDS", 5)?));
        }
        let result = run_batch(&pending, &adjusts, &start_date, &end_date, workers)?;
        total_ok += result.ok;
        failed = result.failed;
        if failed.is_empty() {
            println!("eastmoney {mode} finished ok={total_ok}");
            return Ok(());
        }
        pending = failed.iter().map(|(symbol, _)| symbol.clone()).collect();
    }

    eprintln!("eastmoney {mode} finished with failures: {}", failed.len());
    Err(anyhow!("failed symbols remain: {}", failed.len()))
}

struct BatchResult {
    ok: usize,
    failed: Vec<(String, String)>,
}

fn run_batch(symbols: &[String], adjusts: &[String], start_date: &str, end_date: &str, workers: usize) -> Result<BatchResult> {
    let queue = Arc::new(Mutex::new(symbols.to_vec()));
    let ok = Arc::new(Mutex::new(0usize));
    let done = Arc::new(Mutex::new(0usize));
    let failed = Arc::new(Mutex::new(Vec::<(String, String)>::new()));
    let total = symbols.len();

    let mut handles = Vec::new();
    for _ in 0..workers.max(1) {
        let queue = Arc::clone(&queue);
        let ok = Arc::clone(&ok);
        let done = Arc::clone(&done);
        let failed = Arc::clone(&failed);
        let adjusts = adjusts.to_vec();
        let start_date = start_date.to_string();
        let end_date = end_date.to_string();
        handles.push(thread::spawn(move || loop {
            let symbol = {
                let mut queue = queue.lock().expect("queue lock");
                queue.pop()
            };
            let Some(symbol) = symbol else {
                break;
            };

            match run_symbol(&symbol, &adjusts, &start_date, &end_date) {
                Ok(()) => *ok.lock().expect("ok lock") += 1,
                Err(exc) => {
                    eprintln!("failed {symbol}: {exc:#}");
                    failed.lock().expect("failed lock").push((symbol.clone(), format!("{exc:#}")));
                }
            }

            let current_done = {
                let mut done = done.lock().expect("done lock");
                *done += 1;
                *done
            };
            if current_done % 50 == 0 || current_done == total {
                let current_ok = *ok.lock().expect("ok lock");
                let current_failed = failed.lock().expect("failed lock").len();
                println!("progress {current_done}/{total} ok={current_ok} failed={current_failed}");
            }
        }));
    }

    for handle in handles {
        handle.join().map_err(|_| anyhow!("worker thread panicked"))?;
    }

    let ok_count = *ok.lock().expect("ok lock");
    let failed_items = failed.lock().expect("failed lock").clone();
    Ok(BatchResult {
        ok: ok_count,
        failed: failed_items,
    })
}

fn run_symbol(symbol: &str, adjusts: &[String], start_date: &str, end_date: &str) -> Result<()> {
    let mut total = 0usize;
    for adjust in adjusts {
        let rows = fetch_symbol(symbol, adjust, start_date, end_date)?;
        total += rows.len();
        insert_rows(&rows)?;
    }
    eprintln!("inserted {total} rows for {symbol}");
    Ok(())
}

fn fetch_symbol(symbol: &str, adjust: &str, start_date: &str, end_date: &str) -> Result<Vec<DailyBar>> {
    let url = format!(
        "https://push2his.eastmoney.com/api/qt/stock/kline/get?fields1=f1%2Cf2%2Cf3%2Cf4%2Cf5%2Cf6&fields2=f51%2Cf52%2Cf53%2Cf54%2Cf55%2Cf56%2Cf57%2Cf58%2Cf59%2Cf60%2Cf61&klt=101&fqt={}&beg={}&end={}&secid={}",
        fqt(adjust)?,
        start_date,
        end_date,
        secid(symbol)
    );
    let body = ureq::get(&url)
        .set("User-Agent", "Mozilla/5.0")
        .timeout(Duration::from_secs(env_u64("EASTMONEY_TIMEOUT_SECONDS", 30)?))
        .call()
        .with_context(|| format!("eastmoney request failed for {symbol} {adjust}"))?
        .into_string()?;
    let response: EastmoneyResponse = serde_json::from_str(&body)?;
    if response.rc != 0 {
        return Err(anyhow!("eastmoney rc={} for {symbol} {adjust}", response.rc));
    }

    let mut rows = Vec::new();
    for line in response.data.and_then(|data| data.klines).unwrap_or_default() {
        let parts = line.split(',').collect::<Vec<_>>();
        if parts.len() < 7 {
            continue;
        }
        let trade_date = parts[0].to_string();
        rows.push(DailyBar {
            code: symbol.to_string(),
            adjust: adjust.to_string(),
            trade_date: trade_date.clone(),
            datetime: format!("{trade_date} 15:00:00"),
            date_str: trade_date,
            open: parse_f64(parts[1])?,
            close: parse_f64(parts[2])?,
            high: parse_f64(parts[3])?,
            low: parse_f64(parts[4])?,
            volume: parse_f64(parts[5])?.round() as u64,
            amount: parse_f64(parts[6])?,
            source: "eastmoney".to_string(),
        });
    }
    Ok(rows)
}

fn ensure_table() -> Result<()> {
    clickhouse_post(
        r#"
CREATE TABLE IF NOT EXISTS tdx_daily_bars
(
    code String,
    adjust String DEFAULT 'none',
    trade_date Date,
    datetime String,
    date_str String,
    open Float64,
    close Float64,
    high Float64,
    low Float64,
    volume UInt64,
    amount Float64,
    source String DEFAULT 'tdx',
    updated_at DateTime DEFAULT now()
)
ENGINE = ReplacingMergeTree(updated_at)
ORDER BY (code, adjust, trade_date)
"#,
    )?;
    Ok(())
}

fn load_symbols() -> Result<Vec<String>> {
    let text = clickhouse_post("SELECT code FROM stock_info ORDER BY code FORMAT JSONEachRow")?;
    let mut symbols = Vec::new();
    for line in text.lines().filter(|line| !line.trim().is_empty()) {
        let value: serde_json::Value = serde_json::from_str(line)?;
        if let Some(code) = value.get("code").and_then(|code| code.as_str()) {
            symbols.push(code.to_string());
        }
    }
    Ok(symbols)
}

fn insert_rows(rows: &[DailyBar]) -> Result<()> {
    if rows.is_empty() {
        return Ok(());
    }
    let mut body = String::from("INSERT INTO tdx_daily_bars FORMAT JSONEachRow\n");
    for row in rows {
        body.push_str(&serde_json::to_string(row)?);
        body.push('\n');
    }
    clickhouse_post(&body)?;
    Ok(())
}

fn clickhouse_post(sql: &str) -> Result<String> {
    let endpoint = env_string("CLICKHOUSE_URL", "http://a-stock-clickhouse:8123");
    let endpoint = endpoint.trim_end_matches('/').strip_prefix("http://").ok_or_else(|| anyhow!("only http clickhouse url is supported"))?;
    let (authority, path) = endpoint.split_once('/').unwrap_or((endpoint, ""));
    let (host, port) = if let Some((host, port)) = authority.rsplit_once(':') {
        (host.to_string(), port.parse::<u16>()?)
    } else {
        (authority.to_string(), 80)
    };
    let path = if path.is_empty() { "/".to_string() } else { format!("/{path}") };
    let query = format!(
        "database={}&user={}&password={}",
        url_component(&env_string("CLICKHOUSE_DATABASE", "stock_db")),
        url_component(&env_string("CLICKHOUSE_USER", "stock_user")),
        url_component(&env_string("CLICKHOUSE_PASSWORD", "stock_pass"))
    );
    let full_path = format!("{path}?{query}");
    let request = format!(
        "POST {full_path} HTTP/1.1\r\nHost: {authority}\r\nContent-Type: text/plain\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
        sql.len()
    );
    let mut stream = TcpStream::connect((host.as_str(), port))?;
    stream.write_all(request.as_bytes())?;
    stream.write_all(sql.as_bytes())?;
    stream.shutdown(std::net::Shutdown::Write)?;
    let mut response = Vec::new();
    stream.read_to_end(&mut response)?;
    let (status, body) = parse_http_response(&response)?;
    if !(200..300).contains(&status) {
        return Err(anyhow!("ClickHouse returned HTTP {status}: {body}"));
    }
    Ok(body)
}

fn parse_http_response(response: &[u8]) -> Result<(u16, String)> {
    let header_end = response.windows(4).position(|window| window == b"\r\n\r\n").ok_or_else(|| anyhow!("invalid HTTP response"))?;
    let header_text = String::from_utf8_lossy(&response[..header_end]);
    let status = header_text.lines().next().and_then(|line| line.split_whitespace().nth(1)).ok_or_else(|| anyhow!("missing HTTP status"))?.parse::<u16>()?;
    let raw_body = &response[(header_end + 4)..];
    let is_chunked = header_text
        .lines()
        .any(|line| line.to_ascii_lowercase().starts_with("transfer-encoding:") && line.to_ascii_lowercase().contains("chunked"));
    let body_bytes = if is_chunked {
        decode_chunked(raw_body)?
    } else {
        raw_body.to_vec()
    };
    let body = String::from_utf8_lossy(&body_bytes).to_string();
    Ok((status, body))
}

fn decode_chunked(mut body: &[u8]) -> Result<Vec<u8>> {
    let mut out = Vec::new();
    loop {
        let line_end = body
            .windows(2)
            .position(|window| window == b"\r\n")
            .ok_or_else(|| anyhow!("invalid chunked response: missing size line"))?;
        let size_text = String::from_utf8_lossy(&body[..line_end]);
        let size_hex = size_text.split(';').next().unwrap_or("").trim();
        let size = usize::from_str_radix(size_hex, 16)
            .with_context(|| format!("invalid chunk size: {size_text}"))?;
        body = &body[(line_end + 2)..];
        if size == 0 {
            break;
        }
        if body.len() < size + 2 {
            return Err(anyhow!("invalid chunked response: incomplete chunk"));
        }
        out.extend_from_slice(&body[..size]);
        body = &body[(size + 2)..];
    }
    Ok(out)
}

fn fqt(adjust: &str) -> Result<&'static str> {
    match adjust {
        "qfq" | "forward" | "forward_additive" => Ok("1"),
        "hfq" | "backward" | "backward_additive" => Ok("2"),
        "none" | "raw" => Ok("0"),
        other => Err(anyhow!("unsupported adjust: {other}; expected qfq|hfq|none")),
    }
}

fn secid(symbol: &str) -> String {
    let market = if symbol.starts_with('6') || symbol.starts_with('5') { "1" } else { "0" };
    format!("{market}.{symbol}")
}

fn normalize_date(raw: &str) -> Result<String> {
    let value = raw.replace('-', "");
    if value.len() != 8 || !value.chars().all(|ch| ch.is_ascii_digit()) {
        return Err(anyhow!("invalid date: {raw}"));
    }
    Ok(value)
}

fn today_ymd_string() -> String {
    let today = Local::now();
    format!("{:04}{:02}{:02}", today.year(), today.month(), today.day())
}

fn parse_f64(value: &str) -> Result<f64> {
    Ok(value.parse::<f64>().unwrap_or(0.0))
}

fn url_component(value: &str) -> String {
    value
        .bytes()
        .flat_map(|byte| match byte {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => vec![byte as char],
            _ => format!("%{byte:02X}").chars().collect(),
        })
        .collect()
}

fn env_string(name: &str, default: &str) -> String {
    env::var(name).ok().map(|value| value.trim().to_string()).filter(|value| !value.is_empty()).unwrap_or_else(|| default.to_string())
}

fn env_u64(name: &str, default: u64) -> Result<u64> {
    match env::var(name) {
        Ok(value) if !value.trim().is_empty() => Ok(value.parse()?),
        _ => Ok(default),
    }
}

fn env_usize(name: &str, default: u64) -> Result<usize> {
    Ok(env_u64(name, default)? as usize)
}
