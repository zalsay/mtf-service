# alipay-service

独立 Go 服务，用于承接支付宝 AI 收 / 402 Payment Required 的支付凭证验证。

正确链路：

```text
Agent -> fintrack-api /api/v1/paid/mtf/predict-once
fintrack-api --402 Payment Required--> Agent
Agent -> Alipay AI Pay
Agent -> fintrack-api /api/v1/paid/mtf/predict-once with payment credential
fintrack-api -> alipay-service /api/v1/payments/verify
fintrack-api -> Python/Gateway /predict_once
```

## 能力

- 提供支付凭证验证接口，供 `fintrack-api` 查询是否已支付。
- 提供 local 开发凭证，方便本地测试 402 重试链路。
- 支持两种凭证验证模式：
  - `local`：本地 HMAC 凭证，用于开发和自动化测试。
  - `alipay`：通过可配置 HTTP adapter 调用支付宝凭证查询接口，并可选调用履约确认接口。

> 真实支付宝接口的请求字段、加签和验签规则以签约后官方控制台/SDK 为准。本服务把外部调用隔离在 `internal/payment/AlipayVerifier`，后续替换真实 SDK 时不影响业务 HTTP 层。

## 运行

```bash
cd alipay-service
go run ./cmd/alipay-service
```

默认监听 `:59100`。

## 环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `PORT` | `59100` | HTTP 端口 |
| `ALIPAY_AI_PAY_MODE` | `local` | `local` 或 `alipay` |
| `ALIPAY_MERCHANT_ID` | `dev-merchant` | 收款方 ID |
| `ALIPAY_MERCHANT_NAME` | `FinTrack` | 收款方名称 |
| `ALIPAY_RESOURCE_ID` | `mtf.predict.once` | 资源 ID |
| `ALIPAY_RESOURCE_NAME` | `MTF 单次预测` | 商品/资源名称 |
| `ALIPAY_AI_PAY_AMOUNT_CENTS` | `199` | 金额，单位分 |
| `ALIPAY_AI_PAY_CURRENCY` | `CNY` | 币种 |
| `ALIPAY_AI_PAY_LOCAL_SECRET` | `change-me` | local 模式 HMAC 密钥 |
| `ALIPAY_AI_PAY_LOCAL_TTL_SECONDS` | `600` | local 凭证有效期 |
| `ALIPAY_CREDENTIAL_API_URL` | 空 | alipay 模式凭证查询接口 |
| `ALIPAY_FULFILLMENT_API_URL` | 空 | alipay 模式履约确认接口，可选 |
| `ALIPAY_APP_ID` | 空 | 支付宝应用 ID |
| `ALIPAY_APP_PRIVATE_KEY` | 空 | 预留给真实 SDK 加签 |
| `ALIPAY_PUBLIC_KEY` | 空 | 预留给真实 SDK 验签 |

## 接口

### 健康检查

```bash
curl http://127.0.0.1:59100/health
```

### 验证支付凭证

`fintrack-api` 调用：

```bash
curl -s -X POST http://127.0.0.1:59100/api/v1/payments/verify \
  -H 'Content-Type: application/json' \
  -d '{"credential":"<credential>","resource_id":"mtf.predict.once","order_id":"order-1"}'
```

### local 模式生成开发凭证

```bash
curl -s -X POST http://127.0.0.1:59100/api/v1/dev/credentials \
  -H 'Content-Type: application/json' \
  -d '{"order_id":"order-1","resource_id":"mtf.predict.once"}'
```

Agent 应调用 `fintrack-api` 的 paid once 入口：

```bash
TOKEN='<上一步返回的 credential>'
curl -s -X POST http://127.0.0.1:59000/api/v1/paid/mtf/predict-once \
  -H "Authorization: Alipay-AI-Pay ${TOKEN}" \
  -H "X-Alipay-Order-Id: order-1" \
  -H "Content-Type: application/json" \
  -d '{"stock_code":"000002","stock_type":1,"prediction_type":"mtf-lite","horizon_len":7,"context_len":256}'
```

## 测试

```bash
cd alipay-service
go test ./...
```
