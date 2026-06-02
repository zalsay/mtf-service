# FinTrack UZI API 合同草案

## 目标

为 `fintrack-front` 与 `fintrack-api` 之间的 UZI 分析调用定义第一版 API 合同。

本文只定义接口形态与前后端责任，不实现代码。

## 设计原则

- 前端只调用 `fintrack-api`
- 前端不直接感知 `.codex`、Python、worker
- 所有接口默认需要用户登录
- 长任务统一走异步任务模型

## 路由前缀

建议统一挂在：

```text
/api/v1/uzi
```

## 认证

### 要求

- 所有任务接口都要求 `fintrack` 当前登录态
- 后端按 `user_id` 强制隔离任务数据

### 不做的事

- 不允许匿名提交分析任务
- 不允许跨用户读取任务结果

## 枚举约定

### `depth`

- `lite`
- `medium`
- `deep`

### `status`

- `pending`
- `running`
- `succeeded`
- `failed`
- `canceled`

### `mode`

建议首版支持：

- `standard`
- `quick_scan`
- `panel_only`

如果首版不需要区分，可全部固定为 `standard`。

## 接口一：创建任务

### `POST /api/v1/uzi/jobs`

### 请求体

```json
{
  "ticker": "600519.SH",
  "depth": "medium",
  "mode": "standard"
}
```

### 字段说明

- `ticker`
  - 必填
  - 用户输入的股票代码或名称

- `depth`
  - 必填
  - `lite / medium / deep`

- `mode`
  - 可选
  - 默认 `standard`

### 成功响应

```json
{
  "job_id": 123,
  "status": "pending",
  "ticker": "600519.SH",
  "depth": "medium",
  "mode": "standard",
  "created_at": "2026-04-23T04:00:00Z"
}
```

### 失败响应

#### 参数错误

```json
{
  "error": "invalid_request",
  "message": "depth must be one of lite, medium, deep"
}
```

#### 并发超限

```json
{
  "error": "too_many_running_jobs",
  "message": "You already have a running UZI analysis job"
}
```

## 接口二：查询单个任务

### `GET /api/v1/uzi/jobs/:id`

### 成功响应

```json
{
  "id": 123,
  "ticker": "600519.SH",
  "display_ticker": "贵州茅台",
  "normalized_ticker": "600519.SH",
  "depth": "medium",
  "mode": "standard",
  "status": "running",
  "progress": 42,
  "current_stage": "stage1_fetch_financials",
  "result_summary": null,
  "error_code": null,
  "error_message": null,
  "created_at": "2026-04-23T04:00:00Z",
  "started_at": "2026-04-23T04:00:03Z",
  "finished_at": null
}
```

### 完成态响应示例

```json
{
  "id": 123,
  "ticker": "600519.SH",
  "display_ticker": "贵州茅台",
  "normalized_ticker": "600519.SH",
  "depth": "medium",
  "mode": "standard",
  "status": "succeeded",
  "progress": 100,
  "current_stage": "completed",
  "result_summary": {
    "headline": "中高质量消费龙头，但估值不便宜",
    "score": 78,
    "stance": "watch_accumulate"
  },
  "error_code": null,
  "error_message": null,
  "created_at": "2026-04-23T04:00:00Z",
  "started_at": "2026-04-23T04:00:03Z",
  "finished_at": "2026-04-23T04:03:40Z"
}
```

### 未找到响应

```json
{
  "error": "job_not_found",
  "message": "UZI job not found"
}
```

## 接口三：任务列表

### `GET /api/v1/uzi/jobs`

### 查询参数

- `status`
- `ticker`
- `page`
- `page_size`

### 响应示例

```json
{
  "items": [
    {
      "id": 123,
      "ticker": "600519.SH",
      "depth": "medium",
      "status": "succeeded",
      "progress": 100,
      "current_stage": "completed",
      "created_at": "2026-04-23T04:00:00Z",
      "finished_at": "2026-04-23T04:03:40Z"
    }
  ],
  "page": 1,
  "page_size": 20,
  "total": 1
}
```

## 接口四：任务产物列表

### `GET /api/v1/uzi/jobs/:id/artifacts`

### 响应示例

```json
{
  "job_id": 123,
  "artifacts": [
    {
      "artifact_type": "html_report",
      "content_type": "text/html",
      "size_bytes": 583201,
      "download_url": "/api/v1/uzi/jobs/123/report"
    },
    {
      "artifact_type": "summary_json",
      "content_type": "application/json",
      "size_bytes": 9142,
      "download_url": "/api/v1/uzi/jobs/123/artifacts/summary_json"
    }
  ]
}
```

## 接口五：完整报告

### `GET /api/v1/uzi/jobs/:id/report`

### 行为建议

- 成功时返回 HTML 内容
- 或重定向到受控下载地址

### 响应头建议

- `Content-Type: text/html; charset=utf-8`
- `Cache-Control: private, max-age=60`

## 可选接口六：SSE 进度流

### `GET /api/v1/uzi/jobs/:id/events`

### 适用时机

- 第二阶段再加
- 第一阶段前端轮询即可

### 事件示例

```text
event: progress
data: {"progress": 30, "current_stage": "stage1_fetch_basic"}

event: completed
data: {"status": "succeeded", "job_id": 123}
```

## 可选接口七：重试任务

### `POST /api/v1/uzi/jobs/:id/retry`

### 适用时机

- 第二阶段支持
- 只允许 `failed` 状态任务重试

## 可选接口八：取消任务

### `POST /api/v1/uzi/jobs/:id/cancel`

### 适用时机

- 第二阶段支持
- `pending` 和 `running` 任务可取消

## 前端调用约定

### 创建任务

```ts
const { data } = await api.post('/api/v1/uzi/jobs', {
  ticker,
  depth,
  mode: 'standard',
})
```

### 轮询任务状态

```ts
const { data } = await api.get(`/api/v1/uzi/jobs/${jobId}`)
```

### 拉取结果页

```ts
window.open(`/api/v1/uzi/jobs/${jobId}/report`, '_blank')
```

## 前端状态映射建议

### `pending`

- 文案：排队中
- 交互：可返回列表页

### `running`

- 文案：分析中
- 交互：显示进度条和当前阶段

### `succeeded`

- 文案：分析完成
- 交互：显示摘要卡片和“查看完整报告”

### `failed`

- 文案：分析失败
- 交互：显示错误信息和“稍后重试”

### `canceled`

- 文案：已取消
- 交互：允许重新发起

## 错误码建议

- `invalid_request`
- `job_not_found`
- `unauthorized`
- `forbidden`
- `too_many_running_jobs`
- `worker_unavailable`
- `analysis_failed`
- `artifact_not_found`

## 版本兼容建议

首版 API 应尽量稳定：

- 不把底层 UZI 字段直接暴露给前端
- 对前端只输出平台自己的统一字段

这样后续即使：

- UZI 输出目录结构变化
- JSON 结构变化
- worker 从本地磁盘切到对象存储

前端合同也能基本不变。
