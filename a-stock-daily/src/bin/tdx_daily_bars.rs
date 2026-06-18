use anyhow::{anyhow, Context, Result};
use chrono::{NaiveDate, NaiveDateTime, NaiveTime};
use serde::Serialize;
use std::env;
use std::io::{Read, Write};
use std::net::{TcpStream, ToSocketAddrs};
use std::time::Duration;

const DEFAULT_HOSTS: &[(&str, u16)] = &[
    ("110.41.147.114", 7709),
    ("8.129.13.54", 7709),
    ("120.24.149.49", 7709),
    ("47.113.94.204", 7709),
    ("124.70.176.52", 7709),
    ("47.100.236.28", 7709),
    ("101.133.214.242", 7709),
    ("121.36.54.217", 7709),
];

const SETUP_1: &[u8] = &[
    0x0c, 0x02, 0x18, 0x93, 0x00, 0x01, 0x03, 0x00, 0x03, 0x00, 0x0d, 0x00, 0x01,
];
const SETUP_2: &[u8] = &[
    0x0c, 0x02, 0x18, 0x94, 0x00, 0x01, 0x03, 0x00, 0x03, 0x00, 0x0d, 0x00, 0x02,
];
const SETUP_3: &[u8] = &[
    0x0c, 0x03, 0x18, 0x99, 0x00, 0x01, 0x20, 0x00, 0x20, 0x00, 0xdb, 0x0f, 0xd5,
    0xd0, 0xc9, 0xcc, 0xd6, 0xa4, 0xa8, 0xaf, 0x00, 0x00, 0x00, 0x8f, 0xc2, 0x25,
    0x40, 0x13, 0x00, 0x00, 0xd5, 0x00, 0xc9, 0xcc, 0xbd, 0xf0, 0xd7, 0xea, 0x00,
    0x00, 0x00, 0x02,
];

#[derive(Debug, Clone, Serialize)]
struct DailyBar {
    code: String,
    adjust: String,
    datetime: String,
    trade_date: String,
    date_str: String,
    open: f64,
    close: f64,
    high: f64,
    low: f64,
    volume: u64,
    amount: f64,
}

#[derive(Debug)]
struct Options {
    symbol: String,
    start: usize,
    pages: usize,
    count: u16,
    frequency: u16,
    format: String,
    insert_clickhouse: bool,
    start_date: NaiveDate,
}

fn main() -> Result<()> {
    let opts = parse_args()?;
    let (host, port) = choose_server()?;
    let mut client = TdxClient::connect(&host, port)?;
    let market = stock_market(&opts.symbol);
    let mut all = Vec::new();

    for page in 0..opts.pages {
        let start = opts.start + page * opts.count as usize;
        let mut bars = client.security_bars(
            opts.frequency,
            market,
            &opts.symbol,
            start as u16,
            opts.count,
        )?;
        if bars.is_empty() {
            break;
        }
        all.append(&mut bars);
    }

    let min_trade_date = opts.start_date.format("%Y-%m-%d").to_string();
    all.retain(|bar| bar.trade_date.as_str() >= min_trade_date.as_str());
    all.sort_by(|a, b| a.datetime.cmp(&b.datetime));
    all.dedup_by(|a, b| a.date_str == b.date_str && a.code == b.code);

    if opts.insert_clickhouse {
        insert_clickhouse(&all)?;
    }

    match opts.format.as_str() {
        "none" => {}
        "csv" => print_csv(&all),
        "jsonl" => print_jsonl(&all)?,
        other => return Err(anyhow!("unsupported format: {other}; expected none|csv|jsonl")),
    }

    Ok(())
}

fn parse_args() -> Result<Options> {
    let args: Vec<String> = env::args().collect();
    if args.len() < 2 {
        eprintln!(
            "usage: {} SYMBOL [start=0] [pages=1] [count=800] [format=jsonl]",
            args[0]
        );
        std::process::exit(2);
    }

    Ok(Options {
        symbol: args[1].trim().to_string(),
        start: args.get(2).map_or(Ok(0), |v| v.parse())?,
        pages: args.get(3).map_or(Ok(1), |v| v.parse())?,
        count: args.get(4).map_or(Ok(800), |v| v.parse())?,
        frequency: env::var("TDX_FREQUENCY")
            .ok()
            .map_or(Ok(9), |v| v.parse())?,
        format: args.get(5).cloned().unwrap_or_else(|| "jsonl".to_string()),
        insert_clickhouse: env_bool("TDX_INSERT_CLICKHOUSE", false),
        start_date: parse_start_date()?,
    })
}

fn parse_start_date() -> Result<NaiveDate> {
    let raw = env_string("TDX_START_DATE", "2010-01-01");
    NaiveDate::parse_from_str(&raw, "%Y-%m-%d")
        .or_else(|_| NaiveDate::parse_from_str(&raw, "%Y%m%d"))
        .with_context(|| format!("invalid TDX_START_DATE={raw}; expected YYYY-MM-DD or YYYYMMDD"))
}

fn choose_server() -> Result<(String, u16)> {
    if let Ok(server) = env::var("TDX_SERVER") {
        let (host, port) = server
            .split_once(':')
            .ok_or_else(|| anyhow!("TDX_SERVER must be host:port"))?;
        return Ok((host.to_string(), port.parse()?));
    }
    Ok((DEFAULT_HOSTS[0].0.to_string(), DEFAULT_HOSTS[0].1))
}

fn stock_market(symbol: &str) -> u16 {
    if symbol.starts_with("50")
        || symbol.starts_with("51")
        || symbol.starts_with("60")
        || symbol.starts_with("68")
        || symbol.starts_with("90")
        || symbol.starts_with("110")
        || symbol.starts_with("113")
        || symbol.starts_with("132")
        || symbol.starts_with("204")
    {
        1
    } else {
        0
    }
}

struct TdxClient {
    stream: TcpStream,
}

impl TdxClient {
    fn connect(host: &str, port: u16) -> Result<Self> {
        let timeout = Duration::from_secs(env_u64("TDX_TIMEOUT_SECONDS", 10)?);
        let addr = (host, port)
            .to_socket_addrs()?
            .next()
            .ok_or_else(|| anyhow!("failed to resolve {host}:{port}"))?;
        let stream = TcpStream::connect_timeout(&addr, timeout)
            .with_context(|| format!("failed to connect TDX server {host}:{port}"))?;
        stream.set_read_timeout(Some(timeout))?;
        stream.set_write_timeout(Some(timeout))?;
        let mut client = Self { stream };
        client.call_raw(SETUP_1)?;
        client.call_raw(SETUP_2)?;
        client.call_raw(SETUP_3)?;
        Ok(client)
    }

    fn security_bars(
        &mut self,
        category: u16,
        market: u16,
        code: &str,
        start: u16,
        count: u16,
    ) -> Result<Vec<DailyBar>> {
        let mut pkg = Vec::with_capacity(30);
        write_u16(&mut pkg, 0x010c);
        write_u32(&mut pkg, 0x01016408);
        write_u16(&mut pkg, 0x001c);
        write_u16(&mut pkg, 0x001c);
        write_u16(&mut pkg, 0x052d);
        write_u16(&mut pkg, market);
        write_code(&mut pkg, code)?;
        write_u16(&mut pkg, category);
        write_u16(&mut pkg, 1);
        write_u16(&mut pkg, start);
        write_u16(&mut pkg, count);
        write_u32(&mut pkg, 0);
        write_u32(&mut pkg, 0);
        write_u16(&mut pkg, 0);

        let body = self.call_raw(&pkg)?;
        parse_security_bars(code, category, &body)
    }

    fn call_raw(&mut self, pkg: &[u8]) -> Result<Vec<u8>> {
        self.stream.write_all(pkg)?;
        let mut header = [0u8; 16];
        self.stream.read_exact(&mut header)?;

        let zip_size = u16::from_le_bytes([header[12], header[13]]) as usize;
        let unzip_size = u16::from_le_bytes([header[14], header[15]]) as usize;
        let mut body = vec![0u8; zip_size];
        self.stream.read_exact(&mut body)?;

        if zip_size != unzip_size {
            body = flate2::read::ZlibDecoder::new(&body[..])
                .bytes()
                .collect::<std::result::Result<Vec<_>, _>>()?;
        }

        Ok(body)
    }
}

fn parse_security_bars(symbol: &str, category: u16, body: &[u8]) -> Result<Vec<DailyBar>> {
    if body.len() < 2 {
        return Ok(Vec::new());
    }

    let ret_count = u16::from_le_bytes([body[0], body[1]]) as usize;
    let mut pos = 2usize;
    let mut pre_diff_base = 0i64;
    let mut bars = Vec::with_capacity(ret_count);

    for _ in 0..ret_count {
        let (year, month, day, hour, minute) = parse_datetime(category, body, &mut pos)?;
        let price_open_diff = get_price(body, &mut pos)? as i64;
        let price_close_diff = get_price(body, &mut pos)? as i64;
        let price_high_diff = get_price(body, &mut pos)? as i64;
        let price_low_diff = get_price(body, &mut pos)? as i64;
        let vol_raw = read_u32_at(body, &mut pos)?;
        let amount_raw = read_u32_at(body, &mut pos)?;

        let open = cal_price1000(price_open_diff, pre_diff_base);
        let price_open_abs = price_open_diff + pre_diff_base;
        let close = cal_price1000(price_open_abs, price_close_diff);
        let high = cal_price1000(price_open_abs, price_high_diff);
        let low = cal_price1000(price_open_abs, price_low_diff);
        pre_diff_base = price_open_abs + price_close_diff;

        let date = NaiveDate::from_ymd_opt(year as i32, month as u32, day as u32)
            .ok_or_else(|| anyhow!("invalid date decoded: {year}-{month}-{day}"))?;
        let time = NaiveTime::from_hms_opt(hour as u32, minute as u32, 0)
            .ok_or_else(|| anyhow!("invalid time decoded: {hour}:{minute}"))?;
        let datetime = NaiveDateTime::new(date, time);

        bars.push(DailyBar {
            code: symbol.to_string(),
            adjust: "none".to_string(),
            datetime: datetime.format("%Y-%m-%d %H:%M:%S").to_string(),
            trade_date: date.format("%Y-%m-%d").to_string(),
            date_str: date.format("%Y-%m-%d").to_string(),
            open,
            close,
            high,
            low,
            volume: get_volume(vol_raw).round() as u64,
            amount: get_volume(amount_raw),
        });
    }

    Ok(bars)
}

fn parse_datetime(category: u16, body: &[u8], pos: &mut usize) -> Result<(u16, u8, u8, u8, u8)> {
    if *pos + 4 > body.len() {
        return Err(anyhow!("not enough bytes for datetime"));
    }

    if category < 4 || category == 7 || category == 8 {
        let zip_day = u16::from_le_bytes([body[*pos], body[*pos + 1]]);
        let minutes = u16::from_le_bytes([body[*pos + 2], body[*pos + 3]]);
        *pos += 4;
        let month = ((zip_day % 2048) / 100) as u8;
        let year = (zip_day >> 11) + 2004;
        let day = (zip_day % 2048 % 100) as u8;
        let hour = (minutes / 60) as u8;
        let minute = (minutes % 60) as u8;
        Ok((year, month, day, hour, minute))
    } else {
        let zip_day = read_u32_at(body, pos)?;
        let month = ((zip_day % 10000) / 100) as u8;
        let year = (zip_day / 10000) as u16;
        let day = (zip_day % 100) as u8;
        Ok((year, month, day, 15, 0))
    }
}

fn get_price(body: &[u8], pos: &mut usize) -> Result<i32> {
    if *pos >= body.len() {
        return Err(anyhow!("not enough bytes for encoded price"));
    }
    let mut pos_byte = 6u32;
    let mut b = body[*pos];
    let mut value = (b & 0x3f) as i32;
    let sign = b & 0x40 != 0;

    if b & 0x80 != 0 {
        loop {
            *pos += 1;
            if *pos >= body.len() {
                return Err(anyhow!("unterminated encoded price"));
            }
            b = body[*pos];
            value += ((b & 0x7f) as i32) << pos_byte;
            pos_byte += 7;
            if b & 0x80 == 0 {
                break;
            }
        }
    }
    *pos += 1;

    if sign {
        value = -value;
    }
    Ok(value)
}

fn get_volume(vol: u32) -> f64 {
    let logpoint = vol >> 24;
    let hleax = (vol >> 16) & 0xff;
    let lheax = (vol >> 8) & 0xff;
    let lleax = vol & 0xff;

    let dw_ecx = logpoint as i32 * 2 - 0x7f;
    let dw_edx = logpoint as i32 * 2 - 0x86;
    let dw_esi = logpoint as i32 * 2 - 0x8e;
    let dw_eax = logpoint as i32 * 2 - 0x96;

    let mut xmm6 = 2_f64.powi(dw_ecx.abs());
    if dw_ecx < 0 {
        xmm6 = 1.0 / xmm6;
    }

    let xmm4 = if hleax > 0x80 {
        2_f64.powi(dw_edx) * 128.0 + ((hleax & 0x7f) as f64) * 2_f64.powi(dw_edx + 1)
    } else if dw_edx >= 0 {
        2_f64.powi(dw_edx) * hleax as f64
    } else {
        (1.0 / 2_f64.powi(dw_edx)) * hleax as f64
    };

    let mut xmm3 = 2_f64.powi(dw_esi) * lheax as f64;
    let mut xmm1 = 2_f64.powi(dw_eax) * lleax as f64;
    if hleax & 0x80 != 0 {
        xmm3 *= 2.0;
        xmm1 *= 2.0;
    }

    xmm6 + xmm4 + xmm3 + xmm1
}

fn read_u32_at(body: &[u8], pos: &mut usize) -> Result<u32> {
    if *pos + 4 > body.len() {
        return Err(anyhow!("not enough bytes for u32"));
    }
    let value = u32::from_le_bytes([body[*pos], body[*pos + 1], body[*pos + 2], body[*pos + 3]]);
    *pos += 4;
    Ok(value)
}

fn cal_price1000(base: i64, diff: i64) -> f64 {
    (base + diff) as f64 / 1000.0
}

fn write_u16(buf: &mut Vec<u8>, value: u16) {
    buf.extend_from_slice(&value.to_le_bytes());
}

fn write_u32(buf: &mut Vec<u8>, value: u32) {
    buf.extend_from_slice(&value.to_le_bytes());
}

fn write_code(buf: &mut Vec<u8>, code: &str) -> Result<()> {
    let bytes = code.as_bytes();
    if bytes.len() > 6 {
        return Err(anyhow!("symbol too long: {code}"));
    }
    buf.extend_from_slice(bytes);
    for _ in bytes.len()..6 {
        buf.push(0);
    }
    Ok(())
}

fn print_jsonl(bars: &[DailyBar]) -> Result<()> {
    for bar in bars {
        println!("{}", serde_json::to_string(bar)?);
    }
    Ok(())
}

fn print_csv(bars: &[DailyBar]) {
    println!("code,adjust,datetime,trade_date,date_str,open,close,high,low,volume,amount");
    for bar in bars {
        println!(
            "{},{},{},{},{},{:.3},{:.3},{:.3},{:.3},{},{}",
            bar.code,
            bar.adjust,
            bar.datetime,
            bar.trade_date,
            bar.date_str,
            bar.open,
            bar.close,
            bar.high,
            bar.low,
            bar.volume,
            bar.amount
        );
    }
}

fn insert_clickhouse(bars: &[DailyBar]) -> Result<()> {
    if bars.is_empty() {
        return Ok(());
    }

    let table = env_string("TDX_DAILY_TABLE", "tdx_daily_bars");
    let create_sql = format!(
        r#"
CREATE TABLE IF NOT EXISTS {table}
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
"#
    );
    clickhouse_post(&create_sql)?;

    let mut body = format!("INSERT INTO {table} FORMAT JSONEachRow\n");
    for bar in bars {
        body.push_str(&serde_json::to_string(bar)?);
        body.push('\n');
    }
    clickhouse_post(&body)?;
    eprintln!("inserted {} rows into {}", bars.len(), table);
    Ok(())
}

fn clickhouse_post(sql: &str) -> Result<String> {
    let endpoint = parse_http_endpoint(&env_string(
        "CLICKHOUSE_URL",
        "http://a-stock-clickhouse:8123",
    ))?;
    let database = env_string("CLICKHOUSE_DATABASE", "stock_db");
    let user = env_string("CLICKHOUSE_USER", "stock_user");
    let password = env_string("CLICKHOUSE_PASSWORD", "stock_pass");
    let path = format!(
        "{}?database={}&user={}&password={}",
        endpoint.base_path,
        url_component(&database),
        url_component(&user),
        url_component(&password)
    );

    let request = format!(
        "POST {path} HTTP/1.1\r\nHost: {}\r\nContent-Type: text/plain\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
        endpoint.host_header,
        sql.len()
    );
    let mut stream = TcpStream::connect((endpoint.host.as_str(), endpoint.port))
        .with_context(|| format!("failed to connect ClickHouse {}:{}", endpoint.host, endpoint.port))?;
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

#[derive(Debug)]
struct HttpEndpoint {
    host: String,
    port: u16,
    host_header: String,
    base_path: String,
}

fn parse_http_endpoint(raw: &str) -> Result<HttpEndpoint> {
    let value = raw.trim().trim_end_matches('/');
    let rest = value
        .strip_prefix("http://")
        .ok_or_else(|| anyhow!("only http:// endpoints are supported: {raw}"))?;
    let (authority, path) = rest.split_once('/').unwrap_or((rest, ""));
    let (host, port) = match authority.rsplit_once(':') {
        Some((host, port)) if !host.is_empty() => (host.to_string(), port.parse()?),
        _ => (authority.to_string(), 80),
    };
    Ok(HttpEndpoint {
        host,
        port,
        host_header: authority.to_string(),
        base_path: if path.is_empty() {
            "/".to_string()
        } else {
            format!("/{}", path.trim_matches('/'))
        },
    })
}

fn parse_http_response(response: &[u8]) -> Result<(u16, String)> {
    let header_end = response
        .windows(4)
        .position(|window| window == b"\r\n\r\n")
        .ok_or_else(|| anyhow!("invalid HTTP response"))?;
    let header_text = String::from_utf8_lossy(&response[..header_end]);
    let status = header_text
        .lines()
        .next()
        .and_then(|line| line.split_whitespace().nth(1))
        .ok_or_else(|| anyhow!("missing HTTP status"))?
        .parse::<u16>()?;
    let body = String::from_utf8_lossy(&response[(header_end + 4)..]).to_string();
    Ok((status, body))
}

fn url_component(value: &str) -> String {
    value
        .bytes()
        .flat_map(|byte| match byte {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => {
                vec![byte as char]
            }
            _ => format!("%{byte:02X}").chars().collect(),
        })
        .collect()
}

fn env_bool(name: &str, default: bool) -> bool {
    match env::var(name) {
        Ok(value) => matches!(value.trim().to_ascii_lowercase().as_str(), "1" | "true" | "yes" | "on"),
        Err(_) => default,
    }
}

fn env_string(name: &str, default: &str) -> String {
    env::var(name)
        .ok()
        .map(|value| value.trim().to_string())
        .filter(|value| !value.is_empty())
        .unwrap_or_else(|| default.to_string())
}

fn env_u64(name: &str, default: u64) -> Result<u64> {
    match env::var(name) {
        Ok(value) if !value.trim().is_empty() => Ok(value.parse()?),
        _ => Ok(default),
    }
}
