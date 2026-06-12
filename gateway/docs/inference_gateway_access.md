# Inference Gateway 接入文档

## 1. 概览

当前推理服务已经收敛为一个统一入口：

- 外部统一入口：`ai-functions-gateway`
- 结果持久化接口：`ai-functions-postgres-handler`
- XPU 后端：`ai-functions-xpu`
- ROCm 后端：`ai-functions-rocm`
- 队列与任务持久化：`ai-functions-redis`

统一入口负责：

- 接收外部推理请求
- 生成 `job_id`
- 将任务写入 Redis
- 按设备并发上限调度到 XPU / ROCm 后端
- 提供任务状态查询接口

当前并发上限：

- `xpu = 2`
- `rocm = 1`

## 2. 端口约定

Docker Compose 当前对外暴露端口如下：

| 服务 | 容器端口 | 宿主机端口 | 用途 |
| --- | --- | --- | --- |
| `ai-functions-gateway` | `9010` | `59010` | 外部统一推理入口 |
| `ai-functions-postgres-handler` | `58004` | `58004` | 预测结果持久化与查询 |
| `ai-functions-xpu` | `9008` | `59008` | XPU 后端 |
| `ai-functions-rocm` | `9009` | `59009` | ROCm 后端 |
| `ai-functions-redis` | `6379` | 未对外暴露 | 队列与任务持久化 |

推荐外部只访问：

- `http://<host>:59010`

## 3. 服务启动

`ai-functions-postgres-handler` 当前通过挂载宿主机预构建二进制启动，因此在执行 Compose 之前，需要先在宿主机编译：

```bash
cd /root/workers/ai-finance/postgres-handler
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOTOOLCHAIN=local go build -o postgres-handler-linux-amd64 .
```

使用 Docker Compose 启动：

```bash
docker compose up -d --build
```

查看服务状态：

```bash
docker ps --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}' | rg 'ai-functions-(gateway|xpu|rocm|redis)'
```

健康检查：

```bash
curl http://127.0.0.1:59010/health
```

示例返回：

```json
{
  "status": "healthy",
  "timestamp": "2026-04-22T10:52:12Z",
  "scheduler": {
    "queue_depth": 0,
    "backends": [
      {
        "name": "xpu",
        "url": "http://ai-functions-xpu:9008",
        "capacity": 2,
        "in_flight": 0,
        "available": 2
      },
      {
        "name": "rocm",
        "url": "http://ai-functions-rocm:9009",
        "capacity": 1,
        "in_flight": 0,
        "available": 1
      }
    ],
    "jobs": {
      "failed": 1,
      "queued": 0,
      "running": 0,
      "succeeded": 0
    }
  }
}
```

## 4. 推理接口

### 4.1 提交最佳预测任务

- 方法：`POST`
- 路径：`/predict_for_best`
- 地址：`http://<host>:59010/predict_for_best`

请求体示例：

```json
{
  "stock_code": "601766",
  "stock_type": "stock",
  "time_step": 0,
  "years": 15,
  "horizon_len": 7,
  "context_len": 2048,
  "prediction_type": "mtf-lite",
  "user_id": 1
}
```

支持字段：

| 字段 | 类型 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| `stock_code` | `string` | 是 | 无 | 股票代码 |
| `stock_type` | `number|string` | 否 | `1` | 股票类型 |
| `time_step` | `number` | 否 | `0` | 时间步长 |
| `years` | `number` | 否 | `15` | 历史数据年数 |
| `horizon_len` | `number` | 否 | `7` | 预测步长 |
| `context_len` | `number` | 否 | `2048` | 上下文长度 |
| `prediction_type` | `string` | 否 | `mtf-lite` | `mtf-lite` 为轻量 MTF，`mtf-pro` 为市场协变量增强；兼容旧值 `mtf-lite`/`mtf-pro` |
| `user_id` | `number` | 否 | `null` | 用户 ID |

示例：

```bash
curl -X POST http://127.0.0.1:59010/predict_for_best \
  -H 'Content-Type: application/json' \
  -d '{
    "stock_code": "601766",
    "stock_type": "stock",
    "time_step": 0,
    "years": 15,
    "horizon_len": 7,
    "context_len": 2048,
    "prediction_type": "mtf-lite",
    "user_id": 1
  }'
```

去重规则：

- Redis 去重键由 `目标内部推理路径 + stock_code + stock_type + time_step + years + horizon_len + context_len` 组成
- `user_id` 不参与去重
- `/predict_for_best` 与 `/predict_once` 的去重空间彼此独立，不会互相复用
- 如果上述字段完全相同，且已有任务状态为 `queued`、`running` 或 `succeeded`，网关会直接复用已有任务，不会再次入队
- 如果已有任务状态为 `failed`，网关会允许重试，并重新生成新的 `job_id` 入队
- 未传可选字段时，会按当前后端默认值参与去重：`stock_type=1`、`time_step=0`、`years=15`、`horizon_len=7`、`context_len=2048`

成功入队返回示例：

```json
{
  "success": true,
  "message": "job accepted",
  "reused": false,
  "job_id": "job-94482aba47347ea2ad82bd53",
  "status": "queued",
  "stock_code": "601766",
  "request_key": "/internal/predict_for_best_sync:{\"stock_code\":\"601766\",\"stock_type\":\"stock\",\"time_step\":0,\"years\":15,\"horizon_len\":7,\"context_len\":2048}",
  "created_at": "2026-04-22T10:49:44.069094733Z",
  "status_url": "/jobs/job-94482aba47347ea2ad82bd53",
  "target_path": "/internal/predict_for_best_sync",
  "queue_status": {
    "queue_depth": 1,
    "backends": [
      {
        "name": "xpu",
        "url": "http://ai-functions-xpu:9008",
        "capacity": 2,
        "in_flight": 1,
        "available": 1
      },
      {
        "name": "rocm",
        "url": "http://ai-functions-rocm:9009",
        "capacity": 1,
        "in_flight": 0,
        "available": 1
      }
    ],
    "jobs": {
      "failed": 0,
      "queued": 1,
      "running": 0,
      "succeeded": 0
    }
  }
}
```

命中去重返回示例：

```json
{
  "success": true,
  "message": "existing job reused",
  "reused": true,
  "job_id": "job-94482aba47347ea2ad82bd53",
  "status": "running",
  "stock_code": "601766",
  "request_key": "/internal/predict_for_best_sync:{\"stock_code\":\"601766\",\"stock_type\":\"stock\",\"time_step\":0,\"years\":15,\"horizon_len\":7,\"context_len\":2048}",
  "created_at": "2026-04-22T10:49:44.069094733Z",
  "status_url": "/jobs/job-94482aba47347ea2ad82bd53",
  "target_path": "/internal/predict_for_best_sync",
  "queue_status": {
    "queue_depth": 0,
    "backends": [
      {
        "name": "xpu",
        "url": "http://ai-functions-xpu:9008",
        "capacity": 2,
        "in_flight": 1,
        "available": 1
      },
      {
        "name": "rocm",
        "url": "http://ai-functions-rocm:9009",
        "capacity": 1,
        "in_flight": 0,
        "available": 1
      }
    ],
    "jobs": {
      "failed": 0,
      "queued": 0,
      "running": 1,
      "succeeded": 0
    }
  }
}
```

说明：

- 该接口返回 `202 Accepted`
- 返回成功仅表示任务已入队，不代表推理已完成
- 实际结果需要通过 `job_id` 查询

### 4.2 提交单次预测任务

- 方法：`POST`
- 路径：`/predict_once`
- 地址：`http://<host>:59010/predict_once`

请求体使用同一套基础字段，但语义不同：`/predict_once` 只基于已有 best 做最新单 chunk 预测，不会补跑训练 + 验证。

```json
{
  "stock_code": "601766",
  "stock_type": "stock",
  "time_step": 0,
  "years": 15,
  "horizon_len": 7,
  "context_len": 2048,
  "prediction_type": "mtf-lite",
  "user_id": 1
}
```

示例：

```bash
curl -X POST http://127.0.0.1:59010/predict_once \
  -H 'Content-Type: application/json' \
  -d '{
    "stock_code": "601766",
    "stock_type": "stock",
    "time_step": 0,
    "years": 15,
    "horizon_len": 7,
    "context_len": 2048,
    "prediction_type": "mtf-lite",
    "user_id": 1
  }'
```

成功入队返回示例：

```json
{
  "success": true,
  "message": "job accepted",
  "reused": false,
  "job_id": "job-3efd0603a55e1b4e2e2b3308",
  "status": "queued",
  "stock_code": "601766",
  "request_key": "/internal/predict_once_sync:{\"stock_code\":\"601766\",\"stock_type\":\"stock\",\"time_step\":0,\"years\":15,\"horizon_len\":7,\"context_len\":2048}",
  "created_at": "2026-04-23T05:17:04.178828441Z",
  "status_url": "/jobs/job-3efd0603a55e1b4e2e2b3308",
  "target_path": "/internal/predict_once_sync"
}
```

完成后，`GET /jobs/{job_id}` 的 `result.data` 会直接返回：

- `future_dates`
- `best_prediction_item`
- `best_prediction_values`
- `predictions.mtf*`
- `latest_data_date`
- `request_end_date`

Best 命中规则：

- best 的可用性由最新 once 接口自动处理，fintrack-api 不再传入 `best_max_age_days`。
- `predict_from_best_val_end=true` 时，单次预测应从命中的 best `val_end_date` 开始续跑，而不是要求 best 已覆盖到当前最新数据日。
- `chunk_until_latest=true` 时，后端应生成从 best 结束日期到当前最新可用数据对应 chunk 的预测。

单次预测结果会落库到 PostgreSQL，对应表为：

- `mtf_direct_predictions`

重复命中规则为：

- `stock_code`
- `stock_type`
- `horizon_len`
- `context_len`
- `future_dates`

当以上字段一致时，后端会直接返回数据库中的已有结果，不再重复执行推理；返回体中会带有 `cache_hit=true`。

如果没有找到对应的 fresh best，或者命中的单次预测记录里没有可用的 `best_prediction_item`，后端会直接失败：

- 错误信息：`未生成预测模型，请先训练 MTF 模型后再发起单次预测。`
- 调用方需要先调用 `/predict_for_best` 生成 best，再调用 `/predict_once`
- `force_enqueue=true` 只会强制重跑单次预测，不会触发 best 训练

`mtf_best_predictions` 的落库唯一键则进一步收敛为。对外请求使用 `prediction_type=mtf-lite` / `mtf-pro`，落库 key 后缀使用 MTF 命名：

- `mtf-lite`
  - `{stock}_best_hlen_{h}_clen_{c}_v_{ver}`
- `mtf-pro`
  - `{stock}_best_hlen_{h}_clen_{c}_v_{ver}_mtf-pro`

也就是说，同一股票在 `mtf-pro` 模式下只保留一套 best；`covariate_config` / `covariate_signature` 单独存字段，不再拆成多套按 signature 区分的 best 方案。数据库迁移会将历史旧 key 规范化为 MTF 命名。。

单次预测结果只写入 `mtf_direct_predictions`；best 仍只由 `/predict_for_best` 写入。

### 4.3 查询任务状态

- 方法：`GET`
- 路径：`/jobs/{job_id}`
- 地址：`http://<host>:59010/jobs/<job_id>`

示例：

```bash
curl http://127.0.0.1:59010/jobs/job-94482aba47347ea2ad82bd53
```

返回示例：

```json
{
  "job_id": "job-94482aba47347ea2ad82bd53",
  "status": "failed",
  "stock_code": "601766",
  "target_path": "/internal/predict_for_best_sync",
  "request_key": "/internal/predict_for_best_sync:{\"stock_code\":\"601766\",\"stock_type\":\"stock\",\"time_step\":0,\"years\":15,\"horizon_len\":7,\"context_len\":2048}",
  "backend": "xpu",
  "upstream_status": 500,
  "error": "upstream returned status 500",
  "created_at": "2026-04-22T10:49:44.069094733Z",
  "started_at": "2026-04-22T10:49:44.070332053Z",
  "finished_at": "2026-04-22T10:49:44.28958007Z",
  "result": {
    "success": false,
    "stock_code": "601766",
    "gpu_id": "0",
    "message": "推理失败",
    "error": "Data preprocessing failed",
    "data": {
      "stock_code": "601766",
      "total_chunks": 0,
      "horizon_len": 7,
      "context_len": 512,
      "chunk_results": [],
      "overall_metrics": {
        "avg_mse": null,
        "avg_mae": null,
        "error": "Data preprocessing failed"
      },
      "processing_time": 0.2173151969909668,
      "concatenated_predictions": null,
      "concatenated_actual": null,
      "concatenated_dates": null,
      "validation_chunk_results": null
    }
  }
}
```

额外字段说明：

- `target_path`：本任务实际调用的内部推理接口
- `backend`：实际执行任务的后端，例如 `xpu` 或 `rocm`
- `result`：上游推理服务的原始返回结果

## 5. 任务状态流转

任务状态只有四种：

- `queued`
- `running`
- `succeeded`
- `failed`

状态流转：

```text
queued -> running -> succeeded
queued -> running -> failed
```

补充说明：

- `queued`：任务已经进入 Redis 队列，等待调度
- `running`：任务已分配给某个后端，`backend` 字段会显示 `xpu` 或 `rocm`
- `succeeded`：后端同步推理完成，`result` 中包含完整返回
- `failed`：后端异常、参数错误、数据预处理失败或网关异常

## 6. 调度规则

调度器当前使用固定优先顺序：

1. 优先占用 `xpu`
2. `xpu` 满载后再使用 `rocm`
3. 全部满载后，新任务进入 Redis 队列等待

当前限制：

- XPU 同时最多执行 `2` 个任务
- ROCm 同时最多执行 `1` 个任务

## 7. Redis 持久化设计

Redis 用于保存：

- 任务详情
- 排队顺序

当前 Key 设计：

- `ai-functions:jobs`
  - 类型：`hash`
  - 内容：`job_id -> job json`
- `ai-functions:queue`
  - 类型：`list`
  - 内容：排队中的 `job_id`

Compose 中 Redis 已开启 AOF：

```text
redis-server --appendonly yes --save 60 1000
```

因此：

- 网关重启后，任务记录仍可查询
- 队列中的未执行任务可恢复

恢复策略：

- `queued` 任务：网关重启后重新进入队列
- `running` 任务：不会自动重放，启动时会被标记为失败

这样处理是为了避免网关重启后重复提交同一条推理任务。

## 8. 与后端服务的关系

外部调用方不应直接访问：

- `59008`
- `59009`

这两个端口对应设备后端，主要供统一网关内部调度使用。

后端内部同步接口：

- XPU：`http://ai-functions-xpu:9008/internal/predict_for_best_sync`
- XPU：`http://ai-functions-xpu:9008/internal/predict_once_sync`
- ROCm：`http://ai-functions-rocm:9009/internal/predict_for_best_sync`
- ROCm：`http://ai-functions-rocm:9009/internal/predict_once_sync`

这两个接口是内部接口，不建议对外暴露给业务方直接接入。

## 9. 常见问题

### 9.1 为什么提交任务成功，但结果是失败

`POST /predict_for_best` 或 `POST /predict_once` 成功只代表：

- 请求格式合法
- 任务已经入队

不代表：

- 数据预处理一定成功
- 模型一定能完成推理

最终结果请看 `GET /jobs/{job_id}`。

### 9.2 为什么某些任务会很快失败

常见原因：

- `years` 太小，历史数据不足
- 股票代码无效
- 后端数据源返回异常
- 模型推理异常

例如：

- `years=1`
- `context_len=512`

这类测试参数可能导致历史数据不足，后端返回 `Data preprocessing failed`。

### 9.3 网关重启后运行中的任务为什么丢了

当前策略不是“断点续跑”，而是“保守恢复”：

- 排队中的任务会恢复
- 正在执行中的任务会标记失败

原因是无法准确确认重启前的上游任务是否已在设备侧执行到一半，自动重放有重复推理风险。

### 9.4 为什么相同请求没有再次入队

这是网关的 Redis 去重策略在生效。

以下字段加上目标接口路径相同，就会命中同一个任务：

- `target_path`
- `stock_code`
- `stock_type`
- `time_step`
- `years`
- `horizon_len`
- `context_len`

以下字段不会参与去重：

- `user_id`

因此，同一只股票、同一组推理参数，即使换了 `user_id`，也会直接返回已有任务的当前状态。

唯一例外是该任务已经 `failed`，这时网关会允许重新入队重试。

## 10. 推荐接入方式

业务方推荐按以下流程接入：

1. 调用 `POST /predict_for_best`
2. 记录返回的 `job_id`
3. 轮询 `GET /jobs/{job_id}`
4. 当状态变为 `succeeded` 或 `failed` 时结束

轮询建议：

- 间隔：`2s ~ 5s`
- 高并发场景下建议从 `5s` 起步

## 11. 后续可扩展项

当前文档对应的是已实现能力。后续可继续扩展：

- 任务取消接口
- 失败重试接口
- 历史任务清理策略
- 任务 TTL
- 回调通知机制
- WebSocket 推送任务状态
