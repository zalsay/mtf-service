# FinTrack UZI 数据模型与状态机草案

## 目标

为 `fintrack` 的 UZI 容器化分析子系统定义第一版数据库模型，满足：

- 支持异步任务
- 支持任务追踪
- 支持产物记录
- 支持失败重试
- 支持用户隔离

本文只定义数据模型和状态机，不定义具体迁移代码。

## 设计原则

- 任务表保存“生命周期真相”
- 产物表保存“结果索引”
- 真正的大文件不放数据库正文
- 所有查询默认以 `user_id` 为边界
- 状态流转要有限且可审计

## 表一：`uzi_analysis_jobs`

### 用途

- 表示一次完整的 UZI 分析请求

### 字段建议

- `id`
  - 类型：`BIGSERIAL PRIMARY KEY`
  - 说明：任务主键

- `user_id`
  - 类型：`INTEGER NOT NULL`
  - 说明：归属用户
  - 约束：`REFERENCES users(id) ON DELETE CASCADE`

- `ticker`
  - 类型：`VARCHAR(32) NOT NULL`
  - 说明：用户请求的股票代码或归一化代码

- `display_ticker`
  - 类型：`VARCHAR(64)`
  - 说明：保留原始输入展示值，如“贵州茅台”

- `normalized_ticker`
  - 类型：`VARCHAR(32)`
  - 说明：归一化后代码，如 `600519.SH`

- `depth`
  - 类型：`VARCHAR(16) NOT NULL`
  - 说明：`lite / medium / deep`

- `mode`
  - 类型：`VARCHAR(32) NOT NULL DEFAULT 'standard'`
  - 说明：调用模式，如 `standard / quick_scan / initiate / panel_only`

- `status`
  - 类型：`VARCHAR(16) NOT NULL`
  - 说明：任务状态

- `progress`
  - 类型：`INTEGER NOT NULL DEFAULT 0`
  - 说明：0-100 的粗粒度进度

- `current_stage`
  - 类型：`VARCHAR(64)`
  - 说明：当前阶段，如 `queued / stage1 / stage2 / report_ready`

- `queue_name`
  - 类型：`VARCHAR(32) NOT NULL DEFAULT 'default'`
  - 说明：预留多队列扩展

- `request_payload_json`
  - 类型：`JSONB`
  - 说明：创建任务时的原始请求参数

- `result_summary_json`
  - 类型：`JSONB`
  - 说明：给前端展示的摘要信息

- `error_code`
  - 类型：`VARCHAR(64)`
  - 说明：失败分类码

- `error_message`
  - 类型：`TEXT`
  - 说明：失败信息

- `artifact_dir`
  - 类型：`TEXT`
  - 说明：产物目录根路径或逻辑目录

- `retry_count`
  - 类型：`INTEGER NOT NULL DEFAULT 0`
  - 说明：当前已重试次数

- `max_retries`
  - 类型：`INTEGER NOT NULL DEFAULT 1`
  - 说明：允许的最大重试次数

- `worker_id`
  - 类型：`VARCHAR(128)`
  - 说明：当前领取该任务的 worker 标识

- `locked_at`
  - 类型：`TIMESTAMP WITH TIME ZONE`
  - 说明：worker 领取时间

- `started_at`
  - 类型：`TIMESTAMP WITH TIME ZONE`
  - 说明：开始执行时间

- `finished_at`
  - 类型：`TIMESTAMP WITH TIME ZONE`
  - 说明：结束时间

- `created_at`
  - 类型：`TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP`

- `updated_at`
  - 类型：`TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP`

### 状态枚举

- `pending`
  - 已创建，尚未被 worker 领取

- `running`
  - 已被 worker 领取并执行中

- `succeeded`
  - 已完成，产物可读

- `failed`
  - 本轮执行失败，且不再自动重试

- `canceled`
  - 被用户或系统取消

### 索引建议

- `idx_uzi_jobs_user_created`
  - `(user_id, created_at DESC)`

- `idx_uzi_jobs_status_created`
  - `(status, created_at ASC)`

- `idx_uzi_jobs_user_status_created`
  - `(user_id, status, created_at DESC)`

- `idx_uzi_jobs_normalized_ticker_created`
  - `(normalized_ticker, created_at DESC)`

### 检查约束建议

- `progress BETWEEN 0 AND 100`
- `depth IN ('lite', 'medium', 'deep')`
- `status IN ('pending', 'running', 'succeeded', 'failed', 'canceled')`

## 表二：`uzi_analysis_artifacts`

### 用途

- 索引任务关联的产物文件和结构化结果

### 字段建议

- `id`
  - 类型：`BIGSERIAL PRIMARY KEY`

- `job_id`
  - 类型：`BIGINT NOT NULL`
  - 约束：`REFERENCES uzi_analysis_jobs(id) ON DELETE CASCADE`

- `artifact_type`
  - 类型：`VARCHAR(32) NOT NULL`

- `storage_backend`
  - 类型：`VARCHAR(16) NOT NULL DEFAULT 'local'`
  - 说明：`local / s3 / oss / minio`

- `storage_path`
  - 类型：`TEXT NOT NULL`
  - 说明：逻辑路径或对象 key

- `content_type`
  - 类型：`VARCHAR(128)`

- `size_bytes`
  - 类型：`BIGINT`

- `checksum`
  - 类型：`VARCHAR(128)`
  - 说明：用于校验和幂等

- `meta_json`
  - 类型：`JSONB`
  - 说明：附加元信息，如标题、stage、生成时间

- `created_at`
  - 类型：`TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP`

### `artifact_type` 枚举建议

- `html_report`
- `summary_json`
- `panel_json`
- `agent_analysis_json`
- `share_card`
- `war_report`
- `stdout_log`
- `stderr_log`

### 索引建议

- `idx_uzi_artifacts_job_type`
  - `(job_id, artifact_type)`

- `idx_uzi_artifacts_job_created`
  - `(job_id, created_at DESC)`

## 可选表三：`uzi_analysis_events`

### 是否需要

第一阶段不是必须。

如果后续需要：

- 更细粒度的前端进度时间线
- 更完整的排障回放
- 与 SSE 事件一一对应

则可新增事件表。

### 字段建议

- `id`
- `job_id`
- `event_type`
- `stage`
- `message`
- `payload_json`
- `created_at`

### 适用场景

- 前端想显示“已完成 stage1 / 正在抓财报 / 正在生成报告”
- 需要做后台排障

## 状态机

### 正常路径

```text
pending -> running -> succeeded
```

### 失败路径

```text
pending -> running -> failed
```

### 取消路径

```text
pending -> canceled
running -> canceled
```

### 重试路径

逻辑上仍维持单行任务记录，不新增子任务行：

```text
failed -> pending -> running -> succeeded|failed
```

要求：

- `retry_count` 自增
- 旧错误保留在 `error_message` 历史日志或外部日志系统中

## 状态流转约束

### 允许的流转

- `pending -> running`
- `pending -> canceled`
- `running -> succeeded`
- `running -> failed`
- `running -> canceled`
- `failed -> pending`

### 禁止的流转

- `succeeded -> running`
- `succeeded -> pending`
- `canceled -> running`

### 说明

对已经成功的任务，如需重新分析，建议新建任务，而不是复用成功记录。

## 任务领取策略

建议 worker 通过数据库原子更新领取任务：

- 条件：`status = 'pending'`
- 排序：`created_at ASC`
- 领取后写：
  - `status = 'running'`
  - `worker_id`
  - `locked_at`
  - `started_at`

避免多个 worker 重复消费同一条任务。

## 幂等性建议

### 创建任务

同一用户短时间重复点按钮时，建议后端做如下处理之一：

1. 允许重复建任务
2. 在一定时间窗口内按 `user_id + normalized_ticker + depth + status in (pending,running)` 去重

推荐：

- 首版允许重复建任务，避免额外复杂度
- 前端自己做按钮节流

### 产物写入

建议同一 `job_id + artifact_type` 允许更新覆盖或只保留最新一条。

## 生命周期清理建议

### 热数据

- 最近 30-90 天任务保留在主表中可直接查询

### 冷数据

- 旧日志和大产物可迁移到对象存储或归档目录

### 删除策略

- 删除用户时，级联删除其任务记录
- 若法规或审计要求保留，可改为软删除

## 和前端的关系

前端最常需要的字段是：

- `id`
- `ticker`
- `depth`
- `status`
- `progress`
- `current_stage`
- `result_summary_json`
- `error_message`
- `created_at`
- `finished_at`

不要让前端直接依赖底层产物文件结构。

## 和部署的关系

如果第一阶段使用本地磁盘：

- `artifact_dir` 可记录类似 `/data/uzi-reports/job-123`

如果后续切对象存储：

- `artifact_dir` 可改为逻辑前缀，如 `uzi/job-123/`

这样数据模型不需要大改。

## 最终建议

首版最小闭环使用两张表即可：

1. `uzi_analysis_jobs`
2. `uzi_analysis_artifacts`

等真正需要更细时间线和消息推送时，再补 `uzi_analysis_events`。
