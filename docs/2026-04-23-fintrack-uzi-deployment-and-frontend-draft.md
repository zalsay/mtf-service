# FinTrack UZI 部署与前端交互草案

## 目标

把 UZI 容器化运行与前端接入方式写清楚，作为后续实施时的部署说明和交互草稿。

## 部署结论

第一阶段推荐部署为两个主要运行单元：

1. `fintrack-api`
2. `uzi-worker`

共享：

- PostgreSQL
- 报告产物目录

## 容器职责

### `fintrack-api`

负责：

- 鉴权
- 创建任务
- 查询任务
- 读取产物

依赖：

- Go 运行时
- PostgreSQL 连接

不负责：

- 安装整套 UZI Python 依赖
- 长时间同步阻塞执行分析

### `uzi-worker`

负责：

- 运行 UZI Python 环境
- 消费任务
- 执行分析
- 写任务状态
- 写产物

依赖：

- Python
- UZI 代码
- UZI `.venv`
- 可选 Playwright 浏览器

## 推荐目录约定

容器内建议统一如下：

```text
/app/
  fintrack-api/
  .codex/
    vendor/
      UZI-Skill/
  data/
    uzi-reports/
```

### 说明

- `/app/.codex/vendor/UZI-Skill`：运行时代码
- `/app/data/uzi-reports`：HTML / JSON / 图片产物

## 环境变量建议

### `fintrack-api`

- `UZI_ENABLED=true`
- `UZI_JOBS_QUEUE=default`
- `UZI_REPORTS_ROOT=/app/data/uzi-reports`
- `UZI_MAX_RUNNING_PER_USER=1`

### `uzi-worker`

- `UZI_ENABLED=true`
- `UZI_QUEUE=default`
- `UZI_REPORTS_ROOT=/app/data/uzi-reports`
- `UZI_RUNTIME_ROOT=/app/.codex/vendor/UZI-Skill`
- `UZI_PYTHON_BIN=/app/.codex/vendor/UZI-Skill/.venv/bin/python`
- `UZI_PLAYWRIGHT_ENABLED=false`

如果后续启用 Playwright：

- `UZI_PLAYWRIGHT_ENABLED=true`

## Worker 启动方式

### 方式 A：轮询数据库

Worker 启动后持续执行：

1. 查询一条 `pending`
2. 原子更新为 `running`
3. 执行 UZI
4. 写结果

优点：

- 首版最简单

缺点：

- 吞吐提升有限

### 方式 B：队列系统

后续如引入 Redis / RabbitMQ / NATS，可改为真正消息队列。

第一阶段不强制。

## 产物落盘约定

建议每个任务使用独立目录：

```text
/app/data/uzi-reports/job-123/
  full-report-standalone.html
  summary.json
  panel.json
  stdout.log
  stderr.log
```

### 命名建议

- 以 `job-{id}` 为目录主键
- 不依赖 ticker 当目录唯一键

原因：

- 同一 ticker 可能多次分析
- 避免重名覆盖

## 前端页面草案

### 页面一：发起分析弹窗或表单

字段建议：

- `ticker`
- `depth`
- 可选 `mode`

按钮：

- `开始分析`

交互：

- 提交后立刻跳转到任务详情页

### 页面二：任务详情页

信息块建议：

- 股票信息
- 当前状态
- 进度条
- 当前阶段
- 创建时间
- 完成时间
- 错误信息

操作建议：

- `查看完整报告`
- `返回任务列表`
- 第二阶段可加 `重试`

### 页面三：任务列表页

列表字段建议：

- `ticker`
- `depth`
- `status`
- `progress`
- `created_at`
- `finished_at`

筛选建议：

- 按状态筛选
- 按 ticker 搜索

## 前端轮询策略

第一阶段建议：

- `pending/running` 状态每 3 到 5 秒轮询一次
- `succeeded/failed/canceled` 后停止轮询

示例策略：

- 列表页：5 秒轮询
- 详情页：3 秒轮询

## 前端文案建议

### 排队中

- 标题：`分析任务已提交`
- 副文案：`当前正在排队，系统会自动开始处理`

### 运行中

- 标题：`正在分析`
- 副文案：`UZI 正在抓取数据并生成报告，请稍候`

### 完成

- 标题：`分析完成`
- 副文案：`可以查看完整报告和关键摘要`

### 失败

- 标题：`分析失败`
- 副文案：`请检查股票代码或稍后再试`

## 失败场景与前端处理

### 股票代码无效

后端行为：

- 任务进入 `failed`
- 返回明确 `error_code`

前端行为：

- 显示“股票代码无法识别”

### Worker 不可用

后端行为：

- 创建任务时可直接拒绝
- 或允许进入 `pending` 但长期无 worker 消费

前端行为：

- 显示“分析服务暂不可用”

### 分析超时

后端行为：

- 任务改为 `failed`
- `error_code=analysis_timeout`

前端行为：

- 显示超时提示

## 资源与容量建议

### 第一阶段

- 单 worker 串行执行
- 每用户并发限制 `1`
- 总并发限制 `1-2`

### 第二阶段

- 横向扩 worker
- 引入更细粒度的队列管理

## Docker Compose 草案

仅作结构示意：

```yaml
services:
  fintrack-api:
    image: fintrack-api:latest
    depends_on:
      - postgres
    volumes:
      - uzi_reports:/app/data/uzi-reports

  uzi-worker:
    image: fintrack-uzi-worker:latest
    depends_on:
      - postgres
    volumes:
      - uzi_reports:/app/data/uzi-reports

  postgres:
    image: postgres:16

volumes:
  uzi_reports:
```

## 镜像职责建议

### `fintrack-api` 镜像

- 尽量保持轻量
- 不打入 Playwright 和大量 Python 科学计算依赖

### `uzi-worker` 镜像

- 预装 UZI 依赖
- 可选预装 Chromium
- 负责真正的分析运行时

## 可观测性建议

至少记录：

- job 创建数量
- running 数量
- succeeded / failed 数量
- 平均耗时
- 超时数量

前端层面至少展示：

- 当前任务状态
- 失败原因
- 最后更新时间

## 第二阶段演进方向

- 前端从轮询升级到 SSE
- 产物从本地卷迁移到对象存储
- worker 从 DB 轮询升级到消息队列
- 如 UZI 需求扩大，再拆独立 `uzi-service`

## 最终建议

首版上线时不要追求一次做满：

1. 先用 `fintrack-api + uzi-worker + PostgreSQL + 共享卷`
2. 前端先做任务页和轮询
3. 后续再补 SSE、重试、对象存储

这样能以最小复杂度把 UZI 能力稳定接进 `fintrack`。
