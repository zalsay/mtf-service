//! 简单 TCP 客户端（无连接池，每次抓取新建连接）

use anyhow::{Context, Result};
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpStream;
use tokio::time::{timeout, Duration};
use tracing::{debug, info};

use crate::models::MarketData;
use super::extract::extract_payloads;
use super::parser::parse_payload;
use super::protocol::{
    parse_hexdump, replace_date_code, write_u32_le,
    DEFAULT_HELLO, DEFAULT_REQUEST, DEFAULT_STEP, MAGIC, OFFSET_POS1,
};

/// 等待服务器首字节的超时（毫秒）
const FIRST_BYTE_MS: u64 = 1_000;

pub struct NativeTcpClient {
    host: String,
    port: u16,
    timeout_secs: u64,
}

impl NativeTcpClient {
    pub fn new(host: String, port: u16, timeout_secs: u64) -> Self {
        Self { host, port, timeout_secs }
    }

    /// 超时接收（仅用于 HELLO 握手响应）
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
    async fn recv_page_exact(
        stream: &mut TcpStream,
        first_byte_ms: u64,
    ) -> Result<Option<Vec<u8>>> {
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

    /// 抓取数据
    pub async fn fetch(&self, code: &str, date: u32) -> Result<Vec<MarketData>> {
        let date_str = date.to_string();
        info!("正在抓取: {} {}", code, date_str);

        let addr = format!("{}:{}", self.host, self.port);
        let mut stream = timeout(
            Duration::from_secs(self.timeout_secs),
            TcpStream::connect(&addr),
        )
        .await
        .context("连接超时")?
        .context("无法连接服务器")?;

        // HELLO 握手
        let mut hello = parse_hexdump(DEFAULT_HELLO).context("解析 HELLO 失败")?;
        replace_date_code(&mut hello, &date_str, code).context("HELLO 替换路径失败")?;
        stream.write_all(&hello).await.context("发送 HELLO 失败")?;
        let _hello_resp = Self::recv_data(&mut stream, FIRST_BYTE_MS, 500)
            .await
            .context("接收 HELLO 响应失败")?;
        debug!("HELLO response: {} bytes", _hello_resp.len());

        // 准备请求模板
        let mut template = parse_hexdump(DEFAULT_REQUEST).context("解析 REQUEST 模板失败")?;
        replace_date_code(&mut template, &date_str, code).context("REQUEST 替换路径失败")?;

        let mut all_pages = Vec::new();
        let mut baseline_size: Option<usize> = None;

        for page in 0..99u32 {
            write_u32_le(&mut template, OFFSET_POS1, DEFAULT_STEP * page)?;
            stream.write_all(&template).await.context("发送请求失败")?;

            let resp = match Self::recv_page_exact(&mut stream, FIRST_BYTE_MS).await {
                Ok(Some(r)) => r,
                Ok(None) => {
                    debug!("连接无响应，停止分页");
                    break;
                }
                Err(e) => {
                    debug!("接收 page={} 失败: {}", page, e);
                    break;
                }
            };
            let got = resp.len();
            debug!("page={}: {} 字节", page, got);

            if got == 0 {
                break;
            }
            if !resp.windows(MAGIC.len()).any(|w| w == MAGIC) {
                debug!("响应中无 MAGIC，停止分页");
                break;
            }

            if let Some(baseline) = baseline_size {
                let threshold =
                    (baseline.saturating_mul(3) / 5).max(baseline.saturating_sub(4096));
                if got < threshold {
                    all_pages.push(resp);
                    break;
                }
            } else {
                if got <= 20 {
                    break;
                }
                baseline_size = Some(got);
            }
            all_pages.push(resp);
        }

        if all_pages.is_empty() {
            return Ok(Vec::new());
        }

        // 直接拼接所有页（保留完整 MAGIC 头），由 extract_payloads 按 MAGIC 分块解压
        let total: usize = all_pages.iter().map(Vec::len).sum();
        let mut combined = Vec::with_capacity(total);
        for page_data in &all_pages {
            combined.extend_from_slice(page_data);
        }

        let payloads = extract_payloads(&combined).context("提取 payload 失败")?;
        let mut all_records = Vec::new();
        for payload in &payloads {
            all_records.extend(parse_payload(payload, date));
        }

        info!("抓取完成: {} {} - {} 条记录", code, date_str, all_records.len());
        Ok(all_records)
    }
}

#[cfg(test)]
mod tests {
    #[tokio::test]
    async fn test_native_client() {
        // 需要真实服务器连接，仅用于手动测试
    }
}
