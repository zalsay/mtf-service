# UZI Service

`uzi-service` 是 fintrack 的研报服务适配层。它对 fintrack-api 暴露稳定的
HTTP/SSE 契约，内部可以选择不同 runtime：

- `uzi_skill`：旧版 Python `UZI-Skill` runtime
- `deepseek_tui`：新版 DeepSeek-TUI HTTP runtime

它负责三件事：

- 通过 HTTP 触发一次个股研报分析
- 暴露已生成的报告目录和报告文件
- 为 `fintrack-api` 提供稳定的容器内调用入口

当前实现是同步执行模型，不带任务队列，不带鉴权，不做数据库持久化。

其中 `POST /analyze` 现在使用 SSE 流式返回，而不是等待整个任务结束后一次性返回 JSON。

## DeepSeek-TUI backend

启用方式：

```bash
export UZI_RUNTIME_BACKEND=deepseek_tui
export DEEPSEEK_TUI_CLI_BIN=deepseek
export DEEPSEEK_TUI_BASE_URL=http://127.0.0.1:7878
export DEEPSEEK_TUI_DEFAULT_MODEL=deepseek-v4-pro

deepseek serve --http --host 127.0.0.1 --port 7878
uvicorn app.main:app --host 0.0.0.0 --port 9011
```

如果 `uzi-service` 跑在 Docker 容器内，而 DeepSeek-TUI 跑在宿主机：

```bash
export DEEPSEEK_TUI_BASE_URL=http://host.docker.internal:7878
```

此模式下：

- 如果请求里带 `ai_model`，`POST /analyze` 会优先调用 DeepSeek-TUI CLI one-shot，并通过环境变量注入该用户的 `api_key/base_url/model_id`
- 如果请求里没有 `ai_model`，`POST /analyze` 会调用 DeepSeek-TUI HTTP runtime 的 `POST /v1/stream`
- 服务会将 DeepSeek-TUI 返回内容整理为 `full-report-standalone.html`
- 报告仍保存到 `UZI_REPORTS_ROOT`
- `/reports-index`、`/reports-entry`、`/reports/*` 保持不变
- fintrack-api 与前端无需改动

注意：DeepSeek-TUI 是本地 agent runtime。它需要独立启动
`deepseek serve --http`，并通过其自身配置或环境变量配置 DeepSeek API Key。
fintrack 用户设置里的模型配置只会被 CLI one-shot 路径使用；HTTP runtime
不支持每次请求传入用户 API Key。

## 目录结构

```text
uzi-service/
├── app/main.py          # FastAPI 服务入口
├── Dockerfile
├── requirements.txt
└── vendor/UZI-Skill     # vendored UZI runtime
```

## 默认运行参数

服务默认监听：

- 容器内端口：`9011`

默认环境变量：

- `SERVICE_PORT=9011`
- `UZI_RUNTIME_BACKEND=uzi_skill`
- `UZI_RUN_TIMEOUT_SECONDS=1800`
- `UZI_RUNTIME_ROOT=/opt/uzi-skill`
- `UZI_REPORTS_ROOT=/opt/uzi-skill/skills/deep-analysis/scripts/reports`
- `UZI_PYTHON_BIN=python3`
- `UZI_NO_UPDATE_CHECK=1`
- `DEEPSEEK_TUI_BASE_URL=http://127.0.0.1:7878`
- `DEEPSEEK_TUI_REQUEST_TIMEOUT_SECONDS=900`
- `DEEPSEEK_TUI_DEFAULT_MODEL=deepseek-v4-pro`
- `DEEPSEEK_TUI_CLI_BIN=deepseek`
- `DEEPSEEK_TUI_CLI_TIMEOUT_SECONDS=900`

## Docker 启动

### 单独构建

```bash
cd /home/cc-dev/data/projects/ai-fin/fintrack/uzi-service
docker build -t ai-functions-uzi:local .
```

### 单独运行

```bash
docker run --rm -p 59011:9011 \
  -e SERVICE_PORT=9011 \
  -e UZI_RUN_TIMEOUT_SECONDS=1800 \
  ai-functions-uzi:local
```

### 通过 compose 运行

本项目实际接入方式见：

- `ai-fucntions/docker-compose.yml`

当前映射：

- 宿主机：`59011`
- 容器内：`9011`

## 接口概览

服务不带鉴权，所有接口默认可直接访问。

### 1. 健康检查

`GET /health`

用途：

- 检查服务进程是否在线
- 检查 UZI runtime 路径是否存在
- 返回当前 reports 根目录和端口

示例：

```bash
curl http://127.0.0.1:59011/health
```

典型返回：

```json
{
  "status": "ok",
  "service": "uzi",
  "runtime_root": "/opt/uzi-skill",
  "reports_root": "/opt/uzi-skill/skills/deep-analysis/scripts/reports",
  "run_py_exists": true,
  "timeout_seconds": 1800,
  "port": 9011
}
```

### 2. 发起分析

`POST /analyze`

用途：

- 同步执行一次 `UZI-Skill` 分析
- 通过 `text/event-stream` 持续返回运行进度
- 在结束时发送完整的结构化结果

请求体：

```json
{
  "ticker": "601766.SH",
  "depth": "medium",
  "no_resume": false
}
```

字段说明：

- `ticker`
  - 必填
  - 推荐使用 `600519.SH`、`000001.SZ`
  - 也兼容 `sh600519`、`sz000001` 这类格式，但建议调用方先标准化
- `depth`
  - 可选
  - 允许值：`lite`、`medium`、`deep`
- `no_resume`
  - 可选，默认 `false`
  - 为 `true` 时会在命令行追加 `--no-resume`

服务内部实际执行的命令形态：

```bash
python3 run.py 601766.SH --no-browser --depth medium
```

示例：

```bash
curl -N -X POST http://127.0.0.1:59011/analyze \
  -H 'Content-Type: application/json' \
  -d '{"ticker":"601766.SH","depth":"medium","no_resume":false}'
```

返回类型：

- `Content-Type: text/event-stream`

事件类型：

- `start`
  - 请求已接收，UZI 开始初始化
- `stage`
  - 服务根据当前输出推断出的阶段性总结
- `log`
  - 原始输出日志行
- `complete`
  - 最终结构化结果
- `error`
  - 执行异常或超时

SSE 输出示例：

```text
event: start
data: {"status":"running","ticker":"601766.SH","summary":"已接收分析请求，开始初始化 UZI 运行环境"}

event: stage
data: {"stage":"market_data","summary":"拉取行情、财务和基础数据"}

event: log
data: {"stream":"stdout","line":"开始拉取行情数据..."}

event: stage
data: {"stage":"reporting","summary":"生成研报内容和可视化产物"}

event: complete
data: {"status":"succeeded","ticker":"601766.SH","report_relative_path":"601766.SH_20260424/full-report-standalone.html"}
```

`complete` 事件中的最终 payload 示例：

```json
{
  "status": "succeeded",
  "ticker": "601766.SH",
  "exit_code": 0,
  "duration_seconds": 42.31,
  "command": ["python3", "run.py", "601766.SH", "--no-browser", "--depth", "medium"],
  "report_path": "/opt/uzi-skill/skills/deep-analysis/scripts/reports/601766.SH_20260424/full-report-standalone.html",
  "report_relative_path": "601766.SH_20260424/full-report-standalone.html",
  "report_url": "http://127.0.0.1:59011/reports/601766.SH_20260424/full-report-standalone.html",
  "report": {
    "ticker": "601766.SH",
    "directory_name": "601766.SH_20260424",
    "date_tag": "20260424",
    "report_relative_path": "601766.SH_20260424/full-report-standalone.html",
    "report_url": "http://127.0.0.1:59011/reports/601766.SH_20260424/full-report-standalone.html",
    "size_bytes": 630362,
    "updated_at": "2026-04-24T08:02:09.268901+00:00"
  },
  "stdout_tail": "...",
  "stderr_tail": ""
}
```

状态码说明：

- `200`
  - SSE 连接建立成功
- `500`
  - runtime 缺失，例如 `run.py` 不存在

最终分析结果成功与否不通过 HTTP 状态码区分，而通过最后的 `complete` / `error` 事件区分。

阶段总结说明：

- 这是服务端根据 UZI 输出行做的阶段归类
- 当前会归并为这些阶段：
  - `bootstrap`
  - `market_data`
  - `analysis`
  - `reporting`
  - `finalizing`
- 如果 UZI 上游输出格式变化，阶段归类规则也需要同步调整

错误返回示例：

```json
{
  "status": "failed",
  "ticker": "601766.SH",
  "exit_code": 1,
  "duration_seconds": 5.12,
  "stdout_tail": "...",
  "stderr_tail": "..."
}
```

### 3. 获取报告索引

`GET /reports-index`

用途：

- 扫描 `UZI_REPORTS_ROOT`
- 返回当前已存在的 `full-report-standalone.html` 列表

可选查询参数：

- `ticker`
  - 仅返回指定股票的报告

示例：

```bash
curl http://127.0.0.1:59011/reports-index
curl 'http://127.0.0.1:59011/reports-index?ticker=601766.SH'
```

返回示例：

```json
{
  "items": [
    {
      "ticker": "601766.SH",
      "directory_name": "601766.SH_20260424",
      "date_tag": "20260424",
      "report_relative_path": "601766.SH_20260424/full-report-standalone.html",
      "report_url": "http://127.0.0.1:59011/reports/601766.SH_20260424/full-report-standalone.html",
      "size_bytes": 630362,
      "updated_at": "2026-04-24T08:02:09.268901+00:00"
    }
  ],
  "count": 1
}
```

### 4. 删除报告

`DELETE /reports-entry?relative_path=<report_relative_path>`

用途：

- 删除指定报告文件所在目录
- 不是只删单个 html，而是删整个报告目录

示例：

```bash
curl -X DELETE \
  'http://127.0.0.1:59011/reports-entry?relative_path=601766.SH_20260424/full-report-standalone.html'
```

成功返回：

```json
{
  "success": true,
  "deleted_path": "601766.SH_20260424/full-report-standalone.html",
  "deleted_directory": "601766.SH_20260424"
}
```

常见错误：

- `404`
  - 报告路径不存在
- `400`
  - 报告路径非法，例如越权路径

### 5. 直接访问报告文件

`GET /reports/{report_relative_path}`

用途：

- 直接静态读取报告 HTML
- 一般用于浏览器打开报告

示例：

```bash
curl -I \
  'http://127.0.0.1:59011/reports/601766.SH_20260424/full-report-standalone.html'
```

## 报告目录约定

当前扫描规则固定为：

- 每个报告目录下存在 `full-report-standalone.html`

典型目录结构：

```text
reports/
└── 601766.SH_20260424/
    ├── full-report-standalone.html
    ├── assets/...
    └── ...
```

目录名会被解析为：

- `ticker=601766.SH`
- `date_tag=20260424`

## 与 fintrack-api 的关系

`uzi-service` 本身不关心：

- 用户身份
- 数据库持久化
- 访问权限

这些能力应由上游 `fintrack-api` 负责。

当前接入方式是：

```text
front-end -> fintrack-api -> uzi-service
```

建议：

- 前端不要直接调用 `uzi-service`
- 正式链路统一通过 `fintrack-api /api/v1/uzi/*`

## 限制与注意事项

- 当前是同步执行，请求会一直阻塞到分析完成或超时
- 没有任务队列，不适合高并发直接暴露公网
- 没有鉴权，不应直接作为公网业务接口暴露
- `reports-index` 是文件系统扫描，不是数据库索引
- `delete` 会删除整个报告目录，调用前应由上游确认权限

## 排障

### `GET /health` 正常，但 `POST /analyze` 返回 `500`

优先检查：

- `UZI_RUNTIME_ROOT` 是否正确
- `/opt/uzi-skill/run.py` 是否存在

### `POST /analyze` 返回 `502`

说明：

- UZI 执行失败
- 或执行结束后没有找到可解析的报告路径

排查重点：

- 查看 `stdout_tail`
- 查看 `stderr_tail`
- 检查 `reports` 目录下是否实际生成了报告

### `POST /analyze` 返回 `504`

说明：

- 分析执行超过 `UZI_RUN_TIMEOUT_SECONDS`

处理方式：

- 增大超时时间
- 缩短分析深度
- 检查 UZI runtime 是否卡住

## 相关文件

- `uzi-service/app/main.py`
- `uzi-service/Dockerfile`
- `ai-fucntions/docker-compose.yml`
- `docs/2026-04-24-uzi-skill-container-integration.md`
