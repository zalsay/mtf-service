//! `TongDaXin` TCP 协议常量与请求构造
//!
//! 协议说明：
//! - 使用纯二进制协议（非 HTTP）
//! - 请求包包含固定模板字节，路径位置替换为 "hishf/date/YYYYMMDD/shCODE.img"
//! - 分页通过在 `OFFSET_POS1` 写入小端序 u32 偏移量实现
//! - 响应体包含 MAGIC 分隔的块，每块含 zlib 压缩的分时快照数据

use anyhow::{anyhow, Context, Result};

/// MAGIC 字节：数据块起始标志 (0x0074CBB1)
pub const MAGIC: &[u8] = &[0xB1, 0xCB, 0x74, 0x00];

/// Zlib 压缩头（支持所有合法压缩级别）
pub const ZLIB_HEADERS: &[&[u8]] = &[&[0x78, 0x01], &[0x78, 0x5E], &[0x78, 0x9C], &[0x78, 0xDA]];

/// HELLO 请求包（首次握手；含文件路径占位符）
pub const DEFAULT_HELLO: &str = r"
00 00 00 00  00 00 2a 00  2a 00 c5 02  68 69 73 68
66 2f 64 61  74 65 2f 32  30 32 35 30  36 31 32 2f
73 68 36 30  35 35 59 38  2e 69 6d 67  00 00 00 00
00 00 00 00
";

/// 分页请求包模板（路径、偏移量需在发送前替换）
pub const DEFAULT_REQUEST: &str = r"
00 00 00 00  00 00 36 01  36 01 b9 06  00 00 00 00
30 75 00 00  68 69 73 68  66 2f 64 61  74 65 2f 32
30 32 35 30  36 31 32 2f  73 68 36 30  35 35 39 38
2e 69 6d 67  00 00 00 00  00 00 00 00  00 00 00 00
00 00 00 00  00 00 00 00  00 00 00 00  00 00 00 00
00 00 00 00  00 00 00 00  00 00 00 00  00 00 00 00
00 00 00 00  00 00 00 00  00 00 00 00  00 00 00 00
00 00 00 00  00 00 00 00  00 00 00 00  00 00 00 00
00 00 00 00  00 00 00 00  00 00 00 00  00 00 00 00
00 00 00 00  00 00 00 00  00 00 00 00  00 00 00 00
00 00 00 00  00 00 00 00  00 00 00 00  00 00 00 00
00 00 00 00  00 00 00 00  00 00 00 00  00 00 00 00
00 00 00 00  00 00 00 00  00 00 00 00  00 00 00 00
00 00 00 00  00 00 00 00  00 00 00 00  00 00 00 00
00 00 00 00  00 00 00 00  00 00 00 00  00 00 00 00
00 00 00 00  00 00 00 00  00 00 00 00  00 00 00 00
00 00 00 00  00 00 00 00  00 00 00 00  00 00 00 00
00 00 00 00  00 00 00 00  00 00 00 00  00 00 00 00
00 00 00 00  00 00 00 00  00 00 00 00  00 00 00 00
00 00 00 00  00 00 00 00  00 00 00 00  00 00 00 00
";

/// 分页偏移量在请求包中的字节位置（小端序 u32）
pub const OFFSET_POS1: usize = 0x0C;

/// 单次请求步长（字节）
pub const DEFAULT_STEP: u32 = 0x7530; // 30_000

/// 历史分时数据服务器（通达信）
pub const HISTORY_HOST: &str = "129.211.70.79";
/// 历史分时数据服务器端口（通达信）
pub const HISTORY_PORT: u16 = 7709;

/// 路径前缀
const PATH_PREFIX: &[u8] = b"hishf/date/";
/// 路径总长度（"hishf/date/YYYYMMDD/shNNNNNN.img" = 32 字节）
const PATH_LEN: usize = 32;

// ── 请求构造 ────────────────────────────────────────────────────────

/// 将 hexdump 文本解析为字节
pub fn parse_hexdump(text: &str) -> Result<Vec<u8>> {
    let hex: String = text.chars().filter(char::is_ascii_hexdigit).collect();
    if hex.len() % 2 != 0 {
        return Err(anyhow!("hexdump 长度为奇数"));
    }
    (0..hex.len())
        .step_by(2)
        .map(|i| u8::from_str_radix(&hex[i..i + 2], 16).context("hex 字节解析失败"))
        .collect()
}

/// 将请求包中的日期和代码路径替换为新值（原地修改，零分配）
pub fn replace_date_code(payload: &mut [u8], date: &str, code: &str) -> Result<()> {
    let market = if code.starts_with('6') { "sh" } else { "sz" };
    let new_path = format!("hishf/date/{date}/{market}{code}.img");
    debug_assert_eq!(new_path.len(), PATH_LEN, "路径长度应为 32 字节");

    let pos = payload
        .windows(PATH_PREFIX.len())
        .position(|w| w == PATH_PREFIX)
        .ok_or_else(|| anyhow!("在 payload 中未找到路径前缀"))?;

    if pos + PATH_LEN > payload.len() {
        return Err(anyhow!("路径区域超出 payload 范围"));
    }
    payload[pos..pos + PATH_LEN].copy_from_slice(new_path.as_bytes());
    Ok(())
}

/// 在 payload 的指定偏移处写入小端序 u32
pub fn write_u32_le(data: &mut [u8], offset: usize, value: u32) -> Result<()> {
    if offset + 4 > data.len() {
        return Err(anyhow!("write_u32_le: 偏移 {offset} 超出边界"));
    }
    data[offset..offset + 4].copy_from_slice(&value.to_le_bytes());
    Ok(())
}

// ── 响应解析辅助 ────────────────────────────────────────────────────

/// 在字节流中查找所有 needle 出现的位置
#[must_use]
pub fn find_all(haystack: &[u8], needle: &[u8]) -> Vec<usize> {
    let mut positions = Vec::new();
    let mut start = 0;
    while let Some(pos) = haystack[start..]
        .windows(needle.len())
        .position(|w| w == needle)
    {
        positions.push(start + pos);
        start += pos + 1;
    }
    positions
}

/// 按 MAGIC 分割响应为多个块
#[must_use]
pub fn slice_blocks(data: &[u8]) -> Vec<&[u8]> {
    let positions = find_all(data, MAGIC);
    if positions.is_empty() {
        return vec![];
    }
    let mut blocks = Vec::new();
    for (i, &start) in positions.iter().enumerate() {
        let end = positions.get(i + 1).copied().unwrap_or(data.len());
        blocks.push(&data[start..end]);
    }
    blocks
}

/// TDX 二进制协议字段分隔符（解析器使用）
pub mod tdx_binary {
    /// 字段分隔符（token 之间）
    pub const STX: u8 = 0x02;
    /// 记录开始标志
    pub const ETX: u8 = 0x03;
    /// 帧结束标志
    pub const EOT: u8 = 0x04;
    /// MAGIC 常量（同外层，方便 extract 模块引用）
    pub const MAGIC: &[u8] = super::MAGIC;
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_replace_date_code() {
        let mut payload = parse_hexdump(DEFAULT_HELLO).unwrap();
        replace_date_code(&mut payload, "20260506", "600519").unwrap();
        let path_pos = find_all(&payload, b"hishf/date/")[0];
        let path = std::str::from_utf8(&payload[path_pos..path_pos + PATH_LEN]).unwrap();
        assert_eq!(path, "hishf/date/20260506/sh600519.img");
    }

    #[test]
    fn test_write_u32_le() {
        let mut data = vec![0u8; 20];
        write_u32_le(&mut data, 0x0C, 30_000).unwrap();
        assert_eq!(&data[0x0C..0x10], &30_000u32.to_le_bytes());
    }

    #[test]
    fn test_slice_blocks_empty() {
        assert!(slice_blocks(b"no magic here").is_empty());
    }
}
