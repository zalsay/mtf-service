#!/bin/bash

# PostgreSQL 股票数据处理服务 API 测试脚本

BASE_URL="http://localhost:8080"

echo "=== PostgreSQL 股票数据处理服务 API 测试 ==="
echo

# 1. 健康检查
echo "1. 测试健康检查接口..."
curl -s "$BASE_URL/health" | jq '.' || echo "健康检查失败"
echo -e "\n"

# 2. 插入单条股票数据
echo "2. 测试插入单条股票数据..."
curl -s -X POST "$BASE_URL/api/v1/stock-data" \
  -H "Content-Type: application/json" \
  -d '{
    "datetime": "2024-01-15T00:00:00Z",
    "open": 10.50,
    "close": 10.80,
    "high": 11.00,
    "low": 10.30,
    "volume": 1000000,
    "amount": 10800000.00,
    "amplitude": 6.67,
    "percentage_change": 2.86,
    "amount_change": 0.30,
    "turnover_rate": 1.25,
    "type": 1,
    "symbol": "000001"
  }' | jq '.' || echo "插入单条数据失败"
echo -e "\n"

# 3. 批量插入股票数据
echo "3. 测试批量插入股票数据..."
curl -s -X POST "$BASE_URL/api/v1/stock-data/batch" \
  -H "Content-Type: application/json" \
  -d '[
    {
      "datetime": "2024-01-16T00:00:00Z",
      "open": 10.80,
      "close": 11.00,
      "high": 11.20,
      "low": 10.70,
      "volume": 1200000,
      "amount": 13200000.00,
      "amplitude": 4.63,
      "percentage_change": 1.85,
      "amount_change": 0.20,
      "turnover_rate": 1.50,
      "type": 1,
      "symbol": "000001"
    },
    {
      "datetime": "2024-01-17T00:00:00Z",
      "open": 11.00,
      "close": 10.90,
      "high": 11.10,
      "low": 10.85,
      "volume": 800000,
      "amount": 8720000.00,
      "amplitude": 2.27,
      "percentage_change": -0.91,
      "amount_change": -0.10,
      "turnover_rate": 1.00,
      "type": 1,
      "symbol": "000001"
    }
  ]' | jq '.' || echo "批量插入数据失败"
echo -e "\n"

# 3.5. 插入基金数据测试
echo "3.5. 测试插入基金数据（type=2）..."
curl -s -X POST "$BASE_URL/api/v1/stock-data" \
  -H "Content-Type: application/json" \
  -d '{
    "datetime": "2024-01-15T00:00:00Z",
    "open": 1.250,
    "close": 1.280,
    "high": 1.290,
    "low": 1.240,
    "volume": 500000,
    "amount": 640000.00,
    "amplitude": 4.00,
    "percentage_change": 2.40,
    "amount_change": 0.030,
    "turnover_rate": 0.80,
    "type": 2,
    "symbol": "510300"
  }' | jq '.' || echo "插入基金数据失败"
echo -e "\n"

# 4. 查询股票数据
echo "4. 测试查询股票数据..."
curl -s "$BASE_URL/api/v1/stock-data/000001?type=1&limit=10&offset=0" | jq '.' || echo "查询数据失败"
echo -e "\n"

# 5. 按日期范围查询
echo "5. 测试按日期范围查询..."
curl -s "$BASE_URL/api/v1/stock-data/000001/range?type=1&start_date=2024-01-01&end_date=2024-12-31" | jq '.' || echo "日期范围查询失败"
echo -e "\n"

# 4. TimesFM 验证分块列表查询（按chunk_index升序）
echo "4. 查询TimesFM验证分块列表..."
curl -s -G "$BASE_URL/api/v1/save-predictions/mtf-best/val-chunk/list" \
  -H "Authorization: Bearer fintrack-dev-token" \
  --data-urlencode "unique_key=sh510050_best_hlen_7_clen_2048_v_2.5" | jq '.' || echo "查询验证分块列表失败"
echo -e "\n"

echo "=== API 测试完成 ==="
