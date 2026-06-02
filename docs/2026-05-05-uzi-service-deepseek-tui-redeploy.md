# UZI Service 切换 DeepSeek-TUI 重新部署文档

## 目标

将 `uzi-service` 从旧 `UZI-Skill` Python pipeline 切换为 DeepSeek-TUI backend，并保持 `fintrack-api` 与前端现有接口不变。

切换后链路为：

```text
fintrack-front
  -> fintrack-api /api/v1/uzi/analyze
  -> uzi-service /analyze
  -> DeepSeek-TUI CLI one-shot
  -> HTML report
  -> OSS / uzi_reports
```

## 关键变更

`uzi-service` 新增 `UZI_RUNTIME_BACKEND=deepseek_tui` 模式。

当 `fintrack-api` 传入用户设置里的 `ai_model` 时，`uzi-service` 会优先走 DeepSeek-TUI CLI one-shot：

- `api_key` 注入为 `DEEPSEEK_API_KEY`
- `base_url` 注入为 `DEEPSEEK_BASE_URL`
- `model_id` 通过 `--model` 传给 `deepseek`
- API Key 不出现在命令行参数、日志或返回体中

如果没有 `ai_model`，才回退到 DeepSeek-TUI HTTP runtime 的 `/v1/stream`。线上 fintrack 用户路径应始终走 CLI one-shot，因为每个用户的模型配置存在数据库里。

## 部署前检查

确认代码包含这些文件改动：

```bash
git status --short -- \
  uzi-service/app/main.py \
  uzi-service/tests/test_main.py \
  uzi-service/README.md \
  uzi-service/Dockerfile
```

确认 `uzi-service` 单测通过：

```bash
python -m py_compile uzi-service/app/main.py
python -m pytest uzi-service/tests/test_main.py
git diff --check -- \
  uzi-service/app/main.py \
  uzi-service/tests/test_main.py \
  uzi-service/README.md \
  uzi-service/Dockerfile
```

当前验证结果：

```text
8 passed
```

## 镜像要求

容器里必须能执行 DeepSeek-TUI CLI：

```bash
deepseek --version
```

推荐版本：

```text
deepseek 0.8.11
```

如果镜像内没有 `deepseek`，需要在构建镜像时安装 `deepseek-tui` npm 包，或把预编译二进制放入镜像并设置：

```env
DEEPSEEK_TUI_CLI_BIN=/path/to/deepseek
```

注意：只启动 `deepseek serve --http` 不够。HTTP runtime 不支持每次请求传入用户 API Key，不能满足 fintrack 的“用户独立模型配置”需求。

## Compose 环境变量

在 `ai-fucntions/docker-compose.yml` 的 `ai-functions-uzi.environment` 中增加：

```yaml
      - UZI_RUNTIME_BACKEND=deepseek_tui
      - DEEPSEEK_TUI_CLI_BIN=deepseek
      - DEEPSEEK_TUI_DEFAULT_MODEL=deepseek-v4-pro
      - DEEPSEEK_TUI_CLI_TIMEOUT_SECONDS=900
```

保留原有配置：

```yaml
      - SERVICE_PORT=9011
      - UZI_RUN_TIMEOUT_SECONDS=1800
      - UZI_NO_UPDATE_CHECK=1
```

`DEEPSEEK_API_KEY` 不要写入 compose。它来自 `fintrack-api` 传入的当前登录用户 AI 模型配置。

## 重新构建镜像

在 `fintrack` 根目录执行：

```bash
docker build -t ai-functions-uzi:playwright-local ./uzi-service
```

如果构建环境需要代理，按宿主机实际情况追加 `--build-arg` 或提前配置 Docker daemon 代理。

构建后检查镜像里是否有 DeepSeek-TUI CLI：

```bash
docker run --rm ai-functions-uzi:playwright-local deepseek --version
```

预期：

```text
deepseek 0.8.11
```

## 重启 uzi-service

在 `ai-fucntions` 目录执行：

```bash
docker compose up -d --no-deps --force-recreate ai-functions-uzi
```

确认容器健康：

```bash
docker compose ps ai-functions-uzi
curl -fsS http://127.0.0.1:59011/health
```

预期 health 返回包含：

```json
{
  "status": "ok",
  "service": "uzi",
  "backend": "deepseek_tui"
}
```

如果没有 `backend=deepseek_tui`，说明仍在跑旧镜像或 compose 未注入 `UZI_RUNTIME_BACKEND`。

## 重启 fintrack-api

如果 `fintrack-api` 已经指向 `UZI_SERVICE_URL=http://i.meetlife.com.cn:59011` 或 `http://host.docker.internal:59011`，通常只需要重启 API 进程。

本地直接运行示例：

```bash
cd fintrack-api
./run
```

确认 API 代理到新 UZI backend：

```bash
curl -fsS http://127.0.0.1:59000/api/v1/uzi/health
```

预期同样包含：

```json
{
  "backend": "deepseek_tui"
}
```

## 用户配置验证

使用已有账号登录后检查 AI 模型配置。不要在日志或文档中记录密码、Token、API Key。

示例脚本：

```bash
python - <<'PY'
import json
import getpass
import urllib.request

base = "http://127.0.0.1:59000/api/v1"
email = "zalsay@qq.com"
password = getpass.getpass("password: ")

login = urllib.request.Request(
    base + "/auth/login",
    data=json.dumps({"email": email, "password": password}).encode(),
    headers={"Content-Type": "application/json"},
    method="POST",
)
with urllib.request.urlopen(login, timeout=10) as r:
    token = json.loads(r.read().decode())["token"]

req = urllib.request.Request(
    base + "/settings/ai-model",
    headers={"Authorization": "Bearer " + token},
)
with urllib.request.urlopen(req, timeout=10) as r:
    data = json.loads(r.read().decode())
    print(json.dumps({
        "provider_name": data.get("provider_name"),
        "base_url": data.get("base_url"),
        "model_id": data.get("model_id"),
        "has_api_key": data.get("has_api_key"),
        "display_name": data.get("display_name"),
    }, ensure_ascii=False, indent=2))
PY
```

预期：

```json
{
  "provider_name": "DeepSeek",
  "base_url": "https://api.deepseek.com/v1",
  "model_id": "deepseek-v4-pro",
  "has_api_key": true
}
```

## 真实研报验证

用同一账号发起一只股票的 lite 研报：

```bash
python - <<'PY'
import json
import getpass
import urllib.request

base = "http://127.0.0.1:59000/api/v1"
email = "zalsay@qq.com"
password = getpass.getpass("password: ")

login = urllib.request.Request(
    base + "/auth/login",
    data=json.dumps({"email": email, "password": password}).encode(),
    headers={"Content-Type": "application/json"},
    method="POST",
)
with urllib.request.urlopen(login, timeout=10) as r:
    token = json.loads(r.read().decode())["token"]

req = urllib.request.Request(
    base + "/uzi/analyze",
    data=json.dumps({"ticker": "000001.SZ", "depth": "lite"}).encode(),
    headers={
        "Content-Type": "application/json",
        "Authorization": "Bearer " + token,
    },
    method="POST",
)

with urllib.request.urlopen(req, timeout=240) as r:
    current_event = None
    for raw in r:
        line = raw.decode(errors="replace").rstrip("\n")
        if line.startswith("event:"):
            current_event = line.removeprefix("event:").strip()
        elif line.startswith("data:") and current_event in ("complete", "error"):
            print(current_event, line.removeprefix("data:").strip())
            break
PY
```

成功时应看到：

```text
complete {"status":"succeeded", ... "backend":"deepseek_tui", ...}
```

已验证过的临时链路结果：

```text
ticker: 000001.SZ
depth: lite
backend: deepseek_tui
model: deepseek-v4-pro
status: succeeded
duration_seconds: 125.52
report id: 24
```

## 常见问题

### health 没有 backend 字段

说明当前 `fintrack-api` 指向的仍是旧 `uzi-service` 镜像或旧容器。

检查：

```bash
curl -fsS http://127.0.0.1:59011/health
docker compose ps ai-functions-uzi
docker compose logs --tail=100 ai-functions-uzi
```

### 提示 DeepSeek-TUI CLI not found

说明容器内没有 `deepseek`，或 `DEEPSEEK_TUI_CLI_BIN` 指向错误。

检查：

```bash
docker exec -it ai-functions-uzi sh -lc 'which deepseek && deepseek --version'
```

### 提示 DeepSeek API key not found

说明请求没有走 CLI one-shot，或 `fintrack-api` 没有把当前用户的 `ai_model` 传给 `uzi-service`。

检查：

```bash
curl -fsS http://127.0.0.1:59000/api/v1/uzi/health
```

确认返回 `backend=deepseek_tui` 后，再检查用户设置：

```text
provider_name=DeepSeek
has_api_key=true
model_id=deepseek-v4-pro
```

### 请求仍输出旧 UZI-Skill 日志

如果 SSE 中出现：

```text
python3 run.py
游资（UZI）Skills v3.2.0
fetchers
评委
```

说明仍在旧 `UZI-Skill` backend，不是 DeepSeek-TUI backend。

需要重新部署 `ai-functions-uzi` 并确认：

```json
{
  "backend": "deepseek_tui"
}
```

## 回滚方式

如果需要回滚到旧 UZI-Skill：

1. 删除或改回 compose 中的 `UZI_RUNTIME_BACKEND=uzi_skill`
2. 重启容器：

```bash
cd ai-fucntions
docker compose up -d --no-deps --force-recreate ai-functions-uzi
```

3. 确认 health 不再返回 `backend=deepseek_tui`，或返回 `backend=uzi_skill`

回滚不会删除已经生成的报告文件和 `uzi_reports` 记录。
