//! 高性能 TCP 客户端（带连接池 + HELLO 复用）
//!
//! 协议流程：
//! 1. 从连接池获取已认证连接（若无则新建 + HELLO 握手）
//! 2. 循环发送分页 REQUEST 包（每次更新偏移量）
//! 3. 使用精确帧接收：从响应头 bytes[12..14]（u16 LE）读取 body_len，用 read_exact 避免静默超时
//! 4. 检测短页或空响应，停止分页
//! 5. 返回连接到池，由下次调用复用

use anyhow::{Context, Result};
use parking_lot::Mutex;
use std::sync::Arc;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpStream;
use tokio::time::{timeout, Duration};
use tracing::{debug, warn};

use crate::models::MarketData;
use super::extract::extract_payloads;
use super::parser::parse_payload;
use super::protocol::{
    parse_hexdump, replace_date_code, write_u32_le,
    DEFAULT_HELLO, DEFAULT_REQUEST, DEFAULT_STEP, MAGIC, OFFSET_POS1,
};

/// 等待服务器首字节的超时（毫秒）
const FIRST_BYTE_MS: u64 = 1_000;
/// 连接池默认容量
const DEFAULT_POOL_SIZE: usize = 512;

// ── 连接池 ────────────────────────────────────────────────────────────

/// 已完成 HELLO 握手的可复用 TCP 连接
struct AuthStream {
    stream: TcpStream,
}

/// TCP 连接池
struct ConnPool {
    host: String,
    port: u16,
    timeout_secs: u64,
    idle: Mutex<Vec<AuthStream>>,
    max_size: usize,
    hello_bytes: Vec<u8>,
}

impl ConnPool {
    fn new(host: String, port: u16, timeout_secs: u64, max_size: usize) -> Result<Self> {
        let hello_bytes = parse_hexdump(DEFAULT_HELLO).context("解析 HELLO 模板失败")?;
        Ok(Self {
            host,
            port,
            timeout_secs,
            idle: Mutex::new(Vec::with_capacity(max_size)),
            max_size,
            hello_bytes,
        })
    }

    /// 获取空闲连接（若池为空则创建新连接并完成 HELLO 握手）
    async fn get(&self) -> Result<AuthStream> {
        if let Some(conn) = self.idle.lock().pop() {
            return Ok(conn);
        }
        let addr = format!("{}:{}", self.host, self.port);
        let mut stream = timeout(
            Duration::from_secs(self.timeout_secs),
            TcpStream::connect(&addr),
        )
        .await
        .context("连接超时")?
        .with_context(|| format!("无法连接服务器 {addr}"))?;

        stream
            .write_all(&self.hello_bytes)
            .await
            .context("发送 HELLO 失败")?;
        // 排空 HELLO 响应（超时接收）
        recv_data(&mut stream, FIRST_BYTE_MS, 500)
            .await
            .context("接收 HELLO 响应失败")?;
        debug!("新建连接 + HELLO 完成");
        Ok(AuthStream { stream })
    }

    /// 归还连接到池
    fn put(&self, conn: AuthStream) {
        let mut idle = self.idle.lock();
        if idle.len() < self.max_size {
            idle.push(conn);
        }
    }
}

// ── 接收辅助 ─────────────────────────────────────────────────────────

/// 超时接收（仅用于 HELLO 握手响应）：`first_ms` 等待首字节，`quiet_ms` 检测后续静默
async fn recv_data(stream: &mut TcpStream, first_ms: u64, quiet_ms: u64) -> Result<Vec<u8>> {
    let mut buf = Vec::with_capacity(256 * 1024);
    let mut chunk = vec![0u8; 16_384];

    match timeout(Duration::from_millis(first_ms), stream.read(&mut chunk)).await {
        Ok(Ok(n)) if n > 0 => buf.extend_from_slice(&chunk[..n]),
        Ok(Ok(_)) | Err(_) => return Ok(buf),
        Ok(Err(e)) => return Err(e).context("读取 TCP 流失败"),
    }

    loop {
        match timeout(Duration::from_millis(quiet_ms), stream.read(&mut chunk)).await {
            Ok(Ok(n)) if n > 0 => buf.extend_from_slice(&chunk[..n]),
            Ok(Ok(_)) | Err(_) => break,
            Ok(Err(e)) => return Err(e).context("读取 TCP 流失败"),
        }
    }
    Ok(buf)
}

/// 精确帧接收：读取 16 字节头，再按 body_len 精确读取体
///
/// 协议：每页响应起始 bytes[12..14] = body_len（u16 LE），总页大小 = 16 + body_len。
///
/// - 返回 `Ok(None)`：首字节 EOF 或超时，说明连接已关闭或不可用
/// - 返回 `Ok(Some(bytes))`：完整帧（含头）
/// - 返回 `Err`：IO 错误或超时，连接不可用
async fn recv_page_exact(stream: &mut TcpStream, first_byte_ms: u64) -> Result<Option<Vec<u8>>> {
    let mut header = [0u8; 16];
    match timeout(
        Duration::from_millis(first_byte_ms),
        stream.read(&mut header[..1]),
    )
    .await
    {
        Ok(Ok(1)) => {}
        Ok(Ok(_)) | Err(_) => return Ok(None),
        Ok(Err(e)) => return Err(e).context("读取页首字节失败"),
    }
    // 读取剩余 15 字节头部
    timeout(Duration::from_secs(2), stream.read_exact(&mut header[1..]))
        .await
        .map_err(|_| anyhow::anyhow!("读取页头超时（15字节）"))?
        .context("读取页头失败")?;

    if &header[0..4] != MAGIC {
        return Ok(Some(header.to_vec()));
    }

    let body_len = u16::from_le_bytes([header[12], header[13]]) as usize;
    if body_len == 0 {
        return Ok(Some(header.to_vec()));
    }

    let mut body = vec![0u8; body_len];
    timeout(Duration::from_secs(10), stream.read_exact(&mut body))
        .await
        .map_err(|_| anyhow::anyhow!("读取页体超时（body_len={body_len}）"))?
        .context("读取页体失败")?;

    let mut page = Vec::with_capacity(16 + body_len);
    page.extend_from_slice(&header);
    page.extend_from_slice(&body);
    Ok(Some(page))
}

// ── 公开 API ─────────────────────────────────────────────────────────

/// 高性能 TCP 客户端（带连接池 + HELLO 复用）
pub struct HighPerfTcpClient {
    pool: Arc<ConnPool>,
    request_template: Vec<u8>,
}

impl HighPerfTcpClient {
    pub fn new(host: String, port: u16, timeout_secs: u64, pool_size: usize) -> Result<Self> {
        let request_template =
            parse_hexdump(DEFAULT_REQUEST).context("解析 REQUEST 模板失败")?;
        let pool = Arc::new(ConnPool::new(
            host,
            port,
            timeout_secs,
            pool_size.max(DEFAULT_POOL_SIZE),
        )?);
        Ok(Self {
            pool,
            request_template,
        })
    }

    /// 抓取单只股票某天的分时数据
    pub async fn fetch(&self, code: &str, date: u32) -> Result<Vec<MarketData>> {
        let date_str = date.to_string();
        debug!("开始抓取 {} {}", code, date_str);

        let mut conn = self.pool.get().await?;

        let mut req = self.request_template.clone();
        replace_date_code(&mut req, &date_str, code).context("REQUEST 替换路径失败")?;

        let mut all_pages: Vec<Vec<u8>> = Vec::new();
        let mut baseline_size: Option<usize> = None;
        let mut fetch_ok = true;

        // ── 分页循环 ─────────────────────────────────────────────────
        for page in 0..99u32 {
            if let Err(e) = write_u32_le(&mut req, OFFSET_POS1, DEFAULT_STEP * page) {
                warn!("写入偏移量失败: {}", e);
                fetch_ok = false;
                break;
            }

            if let Err(e) = conn.stream.write_all(&req).await {
                warn!("发送 page={} 失败: {}", page, e);
                fetch_ok = false;
                break;
            }

            let resp = match recv_page_exact(&mut conn.stream, FIRST_BYTE_MS).await {
                Ok(Some(r)) => r,
                Ok(None) => {
                    debug!("连接无响应或已关闭，停止分页");
                    fetch_ok = false;
                    break;
                }
                Err(e) => {
                    warn!("接收 page={} 失败: {}", page, e);
                    fetch_ok = false;
                    break;
                }
            };
            let got = resp.len();
            debug!("page={}: {} 字节", page, got);

            if got == 0 {
                break;
            }

            // 无 MAGIC 数据块 = 服务器已无有效数据
            if !resp.windows(MAGIC.len()).any(|w| w == MAGIC) {
                debug!("响应中无 MAGIC，停止分页");
                fetch_ok = false;
                break;
            }

            // 短页判断
            if let Some(baseline) = baseline_size {
                let threshold =
                    (baseline.saturating_mul(3) / 5).max(baseline.saturating_sub(4096));
                if got < threshold {
                    debug!("短页检测 got={} threshold={}，停止", got, threshold);
                    all_pages.push(resp);
                    break;
                }
            } else {
                // 首页：若 ≤ 20 字节则为停牌空响应
                if got <= 20 {
                    debug!("空页响应（{}字节），股票可能停牌，停止分页", got);
                    break;
                }
                baseline_size = Some(got);
            }

            all_pages.push(resp);

            if all_pages.len() >= 200 {
                warn!("{}_{} 达到页数上限 200，强制停止", code, date_str);
                break;
            }
        }

        if fetch_ok {
            self.pool.put(conn);
        }

        debug!("{}_{} 抓取完成，共 {} 页", code, date_str, all_pages.len());

        if all_pages.is_empty() {
            return Ok(Vec::new());
        }

        // 直接拼接所有页（保留每页完整 MAGIC 头），由 extract_payloads 按 MAGIC 分块解压
        let total: usize = all_pages.iter().map(Vec::len).sum();
        let mut combined = Vec::with_capacity(total);
        for page_data in &all_pages {
            combined.extend_from_slice(page_data);
        }

        let payloads = extract_payloads(&combined).context("提取 payload 失败")?;

        let mut all_records = Vec::with_capacity(payloads.len() * 100);
        for payload in &payloads {
            let records = parse_payload(payload, date);
            all_records.extend(records);
        }

        Ok(all_records)
    }
}
