//! 从 TDX 响应中提取并解压 zlib 块
//!
//! 流程：
//! 1. `slice_blocks` 按 MAGIC 分割原始响应为数据块
//! 2. 对每个块，扫描 zlib 头（0x78xx），猜测压缩长度，精确解压
//! 3. 返回所有解压后的明文 payload（每个对应一段分时快照）
//!
//! 支持三种 TDX 多页块格式：
//! - **标准单页** `[0x0c]`：Block[44:] 为完整 zlib 流
//! - **0x1c 首块** `[0x1c, 0x0c, ...]`：Block0 外层解压后含 28 字节子头 + 内层 zlib 开头，
//!   续包 Block1-N[20:] 为内层 zlib 续流
//! - **0x1c 尾块** `[0x0c, ..., 0x1c]`：Block0 至 BlockN-1 为完整 zlib 流（标准多页格式），
//!   最后的 0x1c 块为服务端元数据，忽略即可

use anyhow::{anyhow, Result};
use flate2::{Decompress, FlushDecompress, Status};

use super::protocol::{slice_blocks, ZLIB_HEADERS};

// ── 精确 zlib 解压 ────────────────────────────────────────────────────

/// 从 `data[offset..]` 开始解压一个 zlib 流，返回（明文, 消耗字节数）
#[allow(clippy::cast_possible_truncation)]
fn decompress_exact(data: &[u8]) -> Result<(Vec<u8>, usize)> {
    let mut dc = Decompress::new(true);
    let mut out = Vec::with_capacity(data.len() * 4);
    let mut buf = vec![0u8; 8192];

    loop {
        let before_out = dc.total_out();
        let status = dc.decompress(
            &data[dc.total_in() as usize..],
            &mut buf,
            FlushDecompress::None,
        )?;
        let produced = (dc.total_out() - before_out) as usize;
        out.extend_from_slice(&buf[..produced]);
        match status {
            Status::Ok => {}
            Status::StreamEnd => break,
            Status::BufError => {
                if produced == 0 {
                    break;
                }
            }
        }
    }
    Ok((out, dc.total_in() as usize))
}

/// 在块内猜测 zlib 段前方的 (`compressed_len`, `uncompressed_len`) 对
fn guess_len_pair(block: &[u8], zlib_pos: usize) -> Option<(u32, u32)> {
    for back in &[8usize, 12usize] {
        if zlib_pos < *back {
            continue;
        }
        let pos = zlib_pos - back;
        if pos + 8 > block.len() {
            continue;
        }
        let a = u32::from_le_bytes(block[pos..pos + 4].try_into().unwrap());
        let c = u32::from_le_bytes(block[pos + 4..pos + 8].try_into().unwrap());
        let total_len = block.len() - zlib_pos;

        // [comp_len][uncomp_len]
        if a >= 8 && a as usize <= total_len && (16..=100_000_000).contains(&c) && a < c {
            return Some((a, c));
        }
        // [uncomp_len][comp_len]
        if c >= 8 && c as usize <= total_len && (16..=100_000_000).contains(&a) && c < a {
            return Some((c, a));
        }
    }
    None
}

/// TDX 分时帧标记：ETX(0x03) 和 EOT(0x04)
const ETX: u8 = 0x03;
const EOT: u8 = 0x04;

/// 检查数据是否包含 TDX 分时帧分隔符 [EOT, ETX]
fn has_tick_frames(data: &[u8]) -> bool {
    data.windows(2).any(|w| w[0] == EOT && w[1] == ETX)
}

/// 扫描一个 MAGIC 块，提取所有 zlib 段（解压明文），支持嵌套压缩
fn extract_from_block(block: &[u8]) -> Vec<Vec<u8>> {
    extract_recursive(block, 0)
}

/// 递归解压，最多 3 层（应对双重压缩的 一字板/涨停 股票）
fn extract_recursive(block: &[u8], depth: u8) -> Vec<Vec<u8>> {
    if depth > 3 {
        return vec![];
    }
    let mut payloads = Vec::new();
    let mut search = 0usize;

    while search + 2 <= block.len() {
        // 找最近的 zlib 头
        let zlib_pos = ZLIB_HEADERS
            .iter()
            .filter_map(|h| block[search..].windows(h.len()).position(|w| w == *h))
            .min();

        let Some(rel) = zlib_pos else { break };
        let abs = search + rel;

        // 先尝试有长度提示的精确解压
        if let Some((comp_len, _)) = guess_len_pair(block, abs) {
            let end = abs + comp_len as usize;
            if end <= block.len() {
                if let Ok((data, used)) = decompress_exact(&block[abs..end]) {
                    if !data.is_empty() {
                        push_or_recurse(&mut payloads, data, depth);
                        search = abs + used;
                        continue;
                    }
                }
            }
        }

        // 无长度信息时直接解压（让 flate2 自动停止）
        if let Ok((data, used)) = decompress_exact(&block[abs..]) {
            if used > 0 && !data.is_empty() {
                push_or_recurse(&mut payloads, data, depth);
                search = abs + used;
                continue;
            }
        }

        search = abs + 1;
    }

    payloads
}

/// 若解压内容含分时帧则直接入队，否则递归再次提取（处理双重压缩）
fn push_or_recurse(payloads: &mut Vec<Vec<u8>>, data: Vec<u8>, depth: u8) {
    if has_tick_frames(&data) {
        payloads.push(data);
    } else {
        // 双层压缩（如部分涨停/一字板股票）：继续解包内层 zlib
        let nested = extract_recursive(&data, depth + 1);
        if nested.is_empty() {
            // 解包失败也保留原始内容，让上层决定
            payloads.push(data);
        } else {
            payloads.extend(nested);
        }
    }
}

// ── 公开 API ──────────────────────────────────────────────────────────

/// 检查块的 type 字段（偏移 4-7）是否为指定值
fn block_type(block: &[u8]) -> u32 {
    if block.len() < 8 {
        return 0;
    }
    u32::from_le_bytes([block[4], block[5], block[6], block[7]])
}

/// 从合并后的原始 TCP 响应中提取所有解压 payload
///
/// TDX 多页块有四种格式：
///
/// **format A：单页 `[0x0c]`**
///   Block0[44:] 为独立 zlib 流，直接解压
///
/// **format B：多页标准 `[0x0c, 0x0c, ...]`**（绝大多数股票）
///   Block0[44:] + Block1[20:] + Block2[20:] + ... → 单一 zlib 流 → 帧数据
///
/// **format C：0x1c 首块 `[0x1c, 0x0c, ...]`**（一字板/全天涨停）
///   Block0[16:] → 外层 zlib → [28字节子头 + 内层 zlib 开头]
///   Block1-N[20:] 为内层 zlib 续流；完整内层流 decompress → 帧数据
///
/// **format D：含 0x1c 中间/尾块 `[0x0c, ..., 0x1c, ...]`**（午后涨停封板股票）
///   找最后一个 0x1c 块的位置，忽略它及之后所有块，以标准多页格式解压前 N 块
pub fn extract_payloads(response: &[u8]) -> Result<Vec<Vec<u8>>> {
    let blocks = slice_blocks(response);
    if blocks.is_empty() {
        return Err(anyhow!("响应中未找到 MAGIC 数据块"));
    }

    // ── format C：0x1c 首块（一字板/涨停多页）────────────────────────────
    if blocks.len() > 1 && block_type(blocks[0]) == 0x1c {
        const OUTER_HDR: usize = 16;
        const INNER_SUB_HDR: usize = 28;
        const CONT_HDR: usize = 20;

        if blocks[0].len() > OUTER_HDR {
            if let Ok((outer, _)) = decompress_exact(&blocks[0][OUTER_HDR..]) {
                if outer.len() > INNER_SUB_HDR {
                    // 拼接内层 zlib 流：Block0外层解压结果[28:] + 各续包[20:]
                    let mut inner_stream = Vec::with_capacity(response.len());
                    inner_stream.extend_from_slice(&outer[INNER_SUB_HDR..]);
                    for block in &blocks[1..] {
                        if block.len() > CONT_HDR {
                            inner_stream.extend_from_slice(&block[CONT_HDR..]);
                        }
                    }
                    if let Ok((full, _)) = decompress_exact(&inner_stream) {
                        if has_tick_frames(&full) {
                            return Ok(vec![full]);
                        }
                    }
                }
            }
        }
    }

    // ── format D：含 0x1c 中间/尾块（午后涨停封板，0x1c 及其之后的块为元数据）───────
    if let Some(last_1c) = blocks.iter().rposition(|b| block_type(b) == 0x1c) {
        if last_1c > 0 {
            const FIRST_HDR: usize = 44;
            const CONT_HDR: usize = 20;

            let data_blocks = &blocks[..last_1c];
            if data_blocks[0].len() > FIRST_HDR {
                let mut merged = Vec::with_capacity(response.len());
                merged.extend_from_slice(&data_blocks[0][FIRST_HDR..]);
                for block in &data_blocks[1..] {
                    if block.len() > CONT_HDR {
                        merged.extend_from_slice(&block[CONT_HDR..]);
                    }
                }
                if let Ok((data, _)) = decompress_exact(&merged) {
                    if has_tick_frames(&data) {
                        return Ok(vec![data]);
                    }
                }
            }
        }
    }

    // ── format B：标准多页 `[0x0c, 0x0c, ...]` ───────────────────────────
    if blocks.len() > 1 {
        const FIRST_HDR: usize = 44;
        const CONT_HDR: usize = 20;

        if blocks[0].len() > FIRST_HDR {
            let mut merged = Vec::with_capacity(response.len());
            merged.extend_from_slice(&blocks[0][FIRST_HDR..]);
            for block in &blocks[1..] {
                if block.len() > CONT_HDR {
                    merged.extend_from_slice(&block[CONT_HDR..]);
                }
            }
            if let Ok((data, _)) = decompress_exact(&merged) {
                if has_tick_frames(&data) {
                    return Ok(vec![data]);
                }
            }
        }
    }

    // ── 回退到单块模式（每块独立解压）────────────────────────────────────
    let payloads: Vec<Vec<u8>> = blocks
        .into_iter()
        .flat_map(extract_from_block)
        .filter(|p| !p.is_empty())
        .collect();

    Ok(payloads)
}

#[cfg(test)]
mod tests {
    use super::*;
    use flate2::write::ZlibEncoder;
    use flate2::Compression;
    use std::io::Write;

    fn make_zlib(data: &[u8]) -> Vec<u8> {
        let mut enc = ZlibEncoder::new(Vec::new(), Compression::default());
        enc.write_all(data).unwrap();
        enc.finish().unwrap()
    }

    #[test]
    fn test_decompress_roundtrip() {
        let original = b"Hello Level1 TDX data";
        let compressed = make_zlib(original);
        let (decompressed, used) = decompress_exact(&compressed).unwrap();
        assert_eq!(&decompressed, original);
        assert_eq!(used, compressed.len());
    }

    #[test]
    fn test_extract_payloads_with_magic() {
        use super::super::protocol::MAGIC;
        // 使用重复数据，确保压缩后比原始更小
        let data: Vec<u8> = b"test level1 payload frame "
            .iter()
            .cycle()
            .take(200)
            .copied()
            .collect();
        let compressed = make_zlib(&data);

        assert!(
            compressed.len() < data.len(),
            "测试数据应能压缩：comp={} uncomp={}",
            compressed.len(),
            data.len()
        );

        // 构造：MAGIC + padding(4) + comp_len_le4 + uncomp_len_le4 + zlib_data
        let comp_len = compressed.len() as u32;
        let uncomp_len = data.len() as u32;

        let mut block = Vec::new();
        block.extend_from_slice(MAGIC);
        block.extend_from_slice(&[0u8; 4]);
        block.extend_from_slice(&comp_len.to_le_bytes());
        block.extend_from_slice(&uncomp_len.to_le_bytes());
        block.extend_from_slice(&compressed);

        let payloads = extract_payloads(&block).unwrap();
        assert!(!payloads.is_empty(), "应提取到至少 1 个 payload");
        assert_eq!(payloads[0], data);
    }
}
