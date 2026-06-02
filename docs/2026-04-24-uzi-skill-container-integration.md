# UZI-Skill 安装与容器接入

## 已完成的安装动作

按 `https://raw.githubusercontent.com/wbh604/UZI-Skill/main/.codex/INSTALL.md` 的做法，已经完成：

1. 克隆 `wbh604/UZI-Skill`
2. 将 `skills/deep-analysis` 安装到 `~/.codex/skills/deep-analysis`

说明：

- Codex 侧技能已安装完成
- 需要重启 Codex 才会自动识别新安装的 skill

## 容器服务

新增目录：

- `uzi-service/`

职责：

- 拉取并固定 `UZI-Skill` 代码版本
- 提供 HTTP 接口给 `fintrack-api`
- 挂载报告目录与缓存目录

镜像内默认固定的 UZI 提交：

- `4be2c34672d6530ecdbe878a396a20563b85bc25`

## Docker Compose

已在 `ai-fucntions/docker-compose.yml` 增加：

- `ai-functions-uzi`

端口：

- 容器内：`9011`
- 宿主机：`59011`

挂载卷：

- `uzi-reports-data`
- `uzi-cache-data`

## UZI 服务接口

### 健康检查

`GET http://127.0.0.1:59011/health`

### 发起分析

`POST http://127.0.0.1:59011/analyze`

请求示例：

```json
{
  "ticker": "600519.SH",
  "depth": "medium",
  "no_resume": false
}
```

返回字段重点：

- `status`
- `report_path`
- `report_relative_path`
- `report_url`
- `stdout_tail`
- `stderr_tail`

### 访问报告

UZI 容器会直接静态暴露报告目录：

`GET http://127.0.0.1:59011/reports/{report_relative_path}`

## FinTrack API 接口

已新增 `fintrack-api` 代理配置与路由。

环境变量：

```env
UZI_ENABLED=true
UZI_SERVICE_URL=http://host.docker.internal:59011
UZI_SERVICE_TIMEOUT=1800
```

接口：

### 健康检查

`GET /api/v1/uzi/health`

### 发起分析

`POST /api/v1/uzi/analyze`

请求示例：

```json
{
  "ticker": "600519.SH",
  "depth": "medium",
  "no_resume": false
}
```

说明：

- 该接口走 `fintrack-api` 代理到 UZI 容器
- 当前已加鉴权
- 成功生成后会把报告元数据写入 PG 的 `uzi_reports` 表

### 代理读取报告

`GET /api/v1/uzi/reports/{report_relative_path}`

示例：

`/api/v1/uzi/reports/600519.SH_20260424/full-report-standalone.html`

### 报告列表

`GET /api/v1/uzi/reports-index`

说明：

- 数据来源改为 PG，不再直接枚举 UZI 容器文件目录
- 只返回当前登录用户自己的报告
- 支持可选参数 `ticker`

### 删除报告

`DELETE /api/v1/uzi/reports-entry?relative_path=600519.SH_20260424/full-report-standalone.html`

说明：

- 会先校验该报告是否属于当前登录用户
- 然后删除 UZI 容器内文件目录
- 最后将 PG 中对应记录标记为软删除

## PostgreSQL 持久化

已新增表：

- `uzi_reports`

用途：

- 保存 UZI 报告路径与目录元数据
- 保存生成参数中的 `ticker`、`depth`
- 保存执行状态与运行摘要，如 `status`、`exit_code`、`duration_seconds`
- 通过 `user_id` 做用户隔离

关键字段：

- `user_id`
- `ticker`
- `depth`
- `status`
- `directory_name`
- `date_tag`
- `report_relative_path`
- `report_url`
- `size_bytes`
- `stdout_tail`
- `stderr_tail`
- `created_at`
- `updated_at`
- `deleted_at`

关键约束：

- 活跃记录唯一键：`(user_id, report_relative_path)`，仅对 `deleted_at IS NULL` 生效
- 删除采用软删除，便于审计和避免误删后主键复用问题

## 启动方式

### 启 UZI 容器

```bash
cd /home/cc-dev/data/projects/ai-fin/fintrack/ai-fucntions
docker compose up -d --build ai-functions-uzi
```

### 本地验证

```bash
curl http://127.0.0.1:59011/health
curl -X POST http://127.0.0.1:59011/analyze \
  -H 'Content-Type: application/json' \
  -d '{"ticker":"600519.SH","depth":"medium","no_resume":false}'
```

## 设计约束

- 当前实现是同步调用，单次请求会阻塞到 UZI 报告生成完成
- 没有复用 TimesFM 的任务队列
- 报告元数据已经持久化进 PG，但 UZI 原始产物本体仍保存在挂载卷里
- 如果后续要前端正式接入，建议补任务表与异步 worker
