# FinTrack 容器化 UZI 分析架构设计

## 目标

为 `fintrack` 设计一套可上线的 UZI 分析调用链，满足以下约束：

- `fintrack-front` 只通过后端 API 调用分析能力
- UZI 在容器内运行，不依赖 Codex 开发环境
- 分析任务可异步执行，避免前端和 API 请求长时间阻塞
- 分析结果可持久化、可追踪、可重试
- 不污染 `fintrack-api` 现有 Go 运行时和 `fintrack-front` 现有前端构建链

## 非目标

- 不让浏览器直接调用 `.codex` skill
- 不让 `fintrack-front` 直接执行 Python 或 shell
- 不在第一阶段把 UZI 改造成独立公网服务
- 不在本文中实现代码

## 核心结论

生产环境里，前端**不直接调用 skill**。

正确链路应为：

```text
fintrack-front -> fintrack-api -> uzi job queue -> uzi-worker -> artifact storage
```

说明：

- `SKILL.md`、`AGENTS.md`、`.codex` 只服务开发态 Codex
- 线上真正可运行的能力来自容器中的 Python worker
- 前端只认任务 API，不认 skill 本身

## 方案对比

### 方案 A：API 同步直调 UZI

做法：

- 前端调用 `fintrack-api`
- Go API 直接同步执行 `run.py`
- 等待分析完成后一次性返回结果

优点：

- 实现最少

缺点：

- 分析常常是几十秒到几分钟
- API 请求超时风险高
- worker 资源占用时间长
- 无法优雅做排队、取消、重试
- 不适合容器化生产环境

结论：

- 仅适合临时联调，不建议上线

### 方案 B：异步任务表 + UZI Worker

做法：

- 前端创建分析任务
- `fintrack-api` 只负责建任务、查任务、查结果
- `uzi-worker` 异步消费任务并执行 UZI

优点：

- 最适合长任务
- 便于重试、超时控制、并发限制
- 前端体验清晰，可展示排队和进度
- 容器扩容简单

缺点：

- 需要补任务表、状态机、artifact 管理

结论：

- 推荐作为正式上线方案

### 方案 C：独立 `uzi-service`

做法：

- 将 UZI 封装成单独 Python API 服务
- `fintrack-api` 通过 HTTP 或队列调用它

优点：

- 边界最干净
- 后续可复用于其他系统

缺点：

- 首版部署复杂度更高
- 鉴权、限流、可观测性都要再补一层

结论：

- 适合作为第二阶段演进，不作为首版方案

## 推荐架构

采用 **方案 B：异步任务表 + UZI Worker**。

### 组件划分

#### `fintrack-front`

负责：

- 提交分析任务
- 展示任务状态
- 轮询或订阅进度
- 展示摘要和完整报告入口

不负责：

- 直接调用 Python
- 直接调用 shell
- 解析 UZI 本地产物目录

#### `fintrack-api`

负责：

- 用户鉴权
- 参数校验
- 创建任务
- 查询任务状态
- 查询分析产物
- 对前端输出统一合同

不负责：

- 在 HTTP 请求生命周期内同步跑完整分析

#### `uzi-worker`

负责：

- 消费待执行任务
- 调用 UZI 命令行
- 收集产物
- 更新任务状态
- 写错误日志与失败原因

#### `artifact storage`

负责：

- 保存 HTML 报告
- 保存 JSON 摘要
- 保存 share card / war report / logs

第一阶段建议：

- 使用容器挂载卷或共享目录

后续可演进到：

- S3 / OSS / MinIO 等对象存储

## 运行时模型

### 容器建议

建议至少拆成两个运行单元：

1. `fintrack-api`
2. `uzi-worker`

共享依赖：

- PostgreSQL
- 共享存储目录（如 `/data/uzi-reports`）

UZI 的 Python 依赖只放进 `uzi-worker` 镜像，不放进 Go API 镜像。

### 任务执行命令

Worker 在容器中执行：

```bash
cd /app/.codex/vendor/UZI-Skill
.venv/bin/python run.py 600519.SH --depth medium --no-browser
```

如果保留包装脚本，也可执行：

```bash
/app/.codex/bin/uzi-run.sh 600519.SH --depth medium --no-browser
```

## 数据库设计

### 表一：`uzi_analysis_jobs`

用途：

- 记录每一次分析任务的生命周期

建议字段：

- `id`
- `user_id`
- `ticker`
- `depth`
- `mode`
- `status`
- `progress`
- `current_stage`
- `request_payload_json`
- `result_summary_json`
- `error_message`
- `artifact_dir`
- `retry_count`
- `started_at`
- `finished_at`
- `created_at`
- `updated_at`

状态建议：

- `pending`
- `running`
- `succeeded`
- `failed`
- `canceled`

索引建议：

- `(user_id, created_at desc)`
- `(status, created_at asc)`
- `(ticker, created_at desc)`

### 表二：`uzi_analysis_artifacts`

用途：

- 记录任务产物

建议字段：

- `id`
- `job_id`
- `artifact_type`
- `storage_path`
- `content_type`
- `size_bytes`
- `meta_json`
- `created_at`

`artifact_type` 建议枚举：

- `html_report`
- `summary_json`
- `panel_json`
- `share_card`
- `war_report`
- `stdout_log`
- `stderr_log`

## API 设计要点

建议最小接口集合：

- `POST /api/v1/uzi/jobs`
- `GET /api/v1/uzi/jobs/:id`
- `GET /api/v1/uzi/jobs`
- `GET /api/v1/uzi/jobs/:id/artifacts`
- `GET /api/v1/uzi/jobs/:id/report`

说明：

- 前端只依赖任务状态与产物查询接口
- 不直接调用 worker

## 前端交互建议

### 提交分析

用户在 `fintrack-front` 中输入：

- `ticker`
- `depth`

提交后：

- API 返回 `job_id`
- 前端跳转到任务详情页

### 展示进度

任务详情页展示：

- `status`
- `progress`
- `current_stage`
- `error_message`

首版建议：

- 轮询，不强依赖 SSE

## 观测与运维

### 日志

至少保留：

- 创建任务日志
- worker 开始/结束日志
- UZI stdout/stderr
- 失败原因

### 指标

建议暴露：

- pending 队列长度
- running 任务数量
- 最近失败任务列表

## 产物存储策略

### 第一阶段

- HTML 报告、JSON 摘要、日志保存在共享卷
- 数据库仅保存路径和摘要

### 第二阶段

- 迁移到对象存储
- API 返回签名 URL 或受控下载接口

## 与开发态 `.codex` 的关系

开发态：

- `.codex/skills/uzi-stock-analysis/SKILL.md`
- `.codex/bin/uzi-run.sh`
- `.codex/vendor/UZI-Skill`

生产态：

- 只保留“运行时和包装脚本”的思想
- 真正被前端调用的是 `fintrack-api` 暴露的任务接口
- 线上不依赖 Codex 自动读取 `SKILL.md`

换句话说：

- `.codex` 是开发时帮助 agent 正确调用 UZI 的外壳
- `uzi-worker` 才是上线时真正跑分析的执行体

## 分阶段落地建议

### Phase 1

- 新增 `uzi_analysis_jobs`
- 新增 `POST /api/v1/uzi/jobs`
- 新增 `GET /api/v1/uzi/jobs/:id`
- 增加单 worker 异步执行
- 前端用轮询展示状态

### Phase 2

- 新增 `uzi_analysis_artifacts`
- 增加报告预览和下载
- 增加重试 / 取消
- 增加 SSE 推送

### Phase 3

- 如请求量上升，再拆独立 `uzi-service`
- 切对象存储
- 增加更细的队列和调度策略

## 最终结论

`fintrack` 上线容器环境后，前端调用 UZI 的正确方式是：

- 前端调用 `fintrack-api`
- `fintrack-api` 创建异步任务
- `uzi-worker` 在容器中运行 UZI
- 结果落库并存储产物
- 前端再查询任务与报告

这比“前端直接调用 skill”更稳、更安全，也更符合容器化生产环境的边界。

## 配套文档

为便于后续实施，本文继续拆分为三份草案：

- [2026-04-23-fintrack-uzi-schema-draft.md](./2026-04-23-fintrack-uzi-schema-draft.md)
- [2026-04-23-fintrack-uzi-api-contract-draft.md](./2026-04-23-fintrack-uzi-api-contract-draft.md)
- [2026-04-23-fintrack-uzi-deployment-and-frontend-draft.md](./2026-04-23-fintrack-uzi-deployment-and-frontend-draft.md)
