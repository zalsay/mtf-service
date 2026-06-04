import json
import hashlib
import logging
import os
import re
import shutil
import subprocess
import time
import urllib.error
import urllib.request
from datetime import datetime, timedelta, timezone
from html import escape
from pathlib import Path
from typing import Generator, Literal

from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse, StreamingResponse
from fastapi.staticfiles import StaticFiles
from pydantic import BaseModel, Field


APP_PORT = int(os.getenv("SERVICE_PORT", "9011"))
RUN_TIMEOUT_SECONDS = int(os.getenv("UZI_RUN_TIMEOUT_SECONDS", "1800"))
LOCAL_RUNTIME_ROOT = Path(__file__).resolve().parents[1] / "vendor" / "UZI-Skill"
DEFAULT_RUNTIME_ROOT = LOCAL_RUNTIME_ROOT if LOCAL_RUNTIME_ROOT.exists() else Path("/opt/uzi-skill")
UZI_RUNTIME_ROOT = Path(os.getenv("UZI_RUNTIME_ROOT", str(DEFAULT_RUNTIME_ROOT))).resolve()
UZI_SCRIPTS_ROOT = UZI_RUNTIME_ROOT / "skills" / "deep-analysis" / "scripts"
REPORTS_ROOT = Path(
    os.getenv(
        "UZI_REPORTS_ROOT",
        str(UZI_SCRIPTS_ROOT / "reports"),
    )
).resolve()
UZI_PYTHON_BIN = os.getenv("UZI_PYTHON_BIN", "python3")
UZI_RUNTIME_BACKEND = os.getenv("UZI_RUNTIME_BACKEND", "uzi_skill").strip().lower()
DEEPSEEK_TUI_BASE_URL = os.getenv("DEEPSEEK_TUI_BASE_URL", "http://127.0.0.1:7878").strip().rstrip("/")
DEEPSEEK_TUI_REQUEST_TIMEOUT_SECONDS = int(os.getenv("DEEPSEEK_TUI_REQUEST_TIMEOUT_SECONDS", "900"))
DEEPSEEK_TUI_DEFAULT_MODEL = os.getenv("DEEPSEEK_TUI_DEFAULT_MODEL", "deepseek-v4-pro")
DEEPSEEK_TUI_CLI_BIN = os.getenv("DEEPSEEK_TUI_CLI_BIN", "deepseek").strip() or "deepseek"
DEEPSEEK_TUI_CLI_TIMEOUT_SECONDS = int(os.getenv("DEEPSEEK_TUI_CLI_TIMEOUT_SECONDS", "900"))
AKSHARE_SERVICE_URL = (
    os.getenv("HISTORY_SERVICE_URL")
    or os.getenv("AKSHARE_SERVICE_URL")
    or "http://ai-functions-postgres-handler:58004"
).strip().rstrip("/")
AKSHARE_SERVICE_TOKEN = (
    os.getenv("MTF_SERVICE_TOKEN")
    or os.getenv("HISTORY_SERVICE_TOKEN")
    or os.getenv("AKSHARE_SERVICE_TOKEN")
    or os.getenv("POSTGRES_HANDLER_TOKEN")
    or ""
).strip()
UZI_MARKET_DATA_TIMEOUT_SECONDS = int(os.getenv("UZI_MARKET_DATA_TIMEOUT_SECONDS", "45"))
UZI_DEEPSEEK_TUI_REPORT_RETRIES = int(os.getenv("UZI_DEEPSEEK_TUI_REPORT_RETRIES", "1"))
UZI_LOG_OUTPUT_SNIPPETS = os.getenv("UZI_LOG_OUTPUT_SNIPPETS", "0").strip().lower() in {"1", "true", "yes", "on"}
UZI_OUTPUT_LOG_CHARS = int(os.getenv("UZI_OUTPUT_LOG_CHARS", "1200"))

REPORT_PATH_PATTERN = re.compile(r"报告路径:\s*(.+)")
ANSI_ESCAPE_PATTERN = re.compile(r"\x1B\[[0-?]*[ -/]*[@-~]")
HTML_FENCE_PATTERN = re.compile(r"```(?:html)?\s*(.*?)\s*```", re.DOTALL | re.IGNORECASE)

logging.basicConfig(
    level=os.getenv("UZI_LOG_LEVEL", "INFO").upper(),
    format="%(asctime)s %(levelname)s [%(name)s] %(message)s",
)
logger = logging.getLogger("uzi_service")
logger.setLevel(os.getenv("UZI_LOG_LEVEL", "INFO").upper())

STAGE_RULES = [
    (
        "bootstrap",
        "准备运行环境与参数校验",
        ("启动", "初始化", "runtime", "start", "initial", "prompt", "依赖安装", "环境"),
    ),
    (
        "market_data",
        "拉取行情、财务和基础数据",
        (
            "fetch_",
            "fetch",
            "数据",
            "行情",
            "财务",
            "akshare",
            "quotes",
            "fundamental",
            "download",
            "采集",
        ),
    ),
    (
        "analysis",
        "执行研究分析与投研技能链",
        (
            "analysis",
            "skill",
            "deep-analysis",
            "研究",
            "估值",
            "模型",
            "memo",
            "valuation",
            "pipeline",
            "score",
            "panel",
            "评委",
            "打分",
        ),
    ),
    (
        "reporting",
        "生成研报内容和可视化产物",
        (
            "assemble_report",
            "report",
            "html",
            "markdown",
            "render",
            "图表",
            "研报",
            "渲染",
            "报告",
        ),
    ),
    (
        "finalizing",
        "收尾并整理报告路径",
        ("完成", "结束", "输出", "写入", "保存", "报告路径", "done", "finish", "saved"),
    ),
]

STAGE_ORDER = {
    "bootstrap": 0,
    "market_data": 1,
    "analysis": 2,
    "reporting": 3,
    "finalizing": 4,
}

app = FastAPI(title="UZI Service", version="1.1.0")
REPORTS_ROOT.mkdir(parents=True, exist_ok=True)
app.mount("/reports", StaticFiles(directory=str(REPORTS_ROOT)), name="reports")


class AIModelConfig(BaseModel):
    provider_name: str | None = None
    base_url: str | None = None
    api_key: str | None = None
    model_id: str | None = None


class AnalyzeRequest(BaseModel):
    ticker: str = Field(..., min_length=1)
    depth: Literal["lite", "medium", "deep"] | None = None
    no_resume: bool = False
    ai_model: AIModelConfig | None = None


DEPTH_VALUES = {"lite", "medium", "deep"}


def normalize_ticker(ticker: str | None) -> str:
    raw = (ticker or "").strip().upper()
    if not raw:
        return ""
    if raw.startswith("SH") and len(raw) > 2:
        return f"{raw[2:]}.SH"
    if raw.startswith("SZ") and len(raw) > 2:
        return f"{raw[2:]}.SZ"
    return raw


def normalize_depth(depth: str | None) -> str:
    value = (depth or "").strip().lower()
    return value if value in DEPTH_VALUES else "medium"


def report_directory_name(ticker: str, date_tag: str, depth: str | None) -> str:
    return f"{normalize_ticker(ticker)}_{date_tag}_{normalize_depth(depth)}"


def parse_report_directory_name(directory_name: str) -> tuple[str, str, str | None]:
    parts = (directory_name or "").split("_")
    if len(parts) >= 3 and parts[-1] in DEPTH_VALUES:
        return "_".join(parts[:-2]), parts[-2], parts[-1]
    if len(parts) >= 2:
        return "_".join(parts[:-1]), parts[-1], None
    return directory_name, "", None


def tail_text(text: str, limit: int = 6000) -> str:
    normalized = (text or "").strip()
    if len(normalized) <= limit:
        return normalized
    return normalized[-limit:]


def text_diagnostics(text: str) -> dict:
    raw = text or ""
    lowered = raw.lower()
    return {
        "chars": len(raw),
        "bytes": len(raw.encode("utf-8", errors="replace")),
        "sha256": hashlib.sha256(raw.encode("utf-8", errors="replace")).hexdigest(),
        "has_doctype": "<!doctype" in lowered,
        "has_html_open": "<html" in lowered,
        "has_html_close": "</html>" in lowered,
    }


def log_text_diagnostics(label: str, text: str, **fields) -> None:
    payload = {
        "label": label,
        **fields,
        **text_diagnostics(text),
    }
    if UZI_LOG_OUTPUT_SNIPPETS:
        limit = max(0, UZI_OUTPUT_LOG_CHARS)
        payload["head"] = (text or "")[:limit]
        payload["tail"] = (text or "")[-limit:] if limit else ""
    logger.info("text_diagnostics %s", json.dumps(payload, ensure_ascii=False, default=str))


def build_base_env() -> dict[str, str]:
    env = os.environ.copy()
    env.setdefault("UZI_NO_UPDATE_CHECK", "1")
    env.setdefault("UZI_NO_AUTO_OPEN", "1")
    env.setdefault("PYTHONUNBUFFERED", "1")
    env.setdefault("PYTHONIOENCODING", "utf-8")
    load_runtime_dotenv(env)
    return env


def load_runtime_dotenv(env: dict[str, str]) -> None:
    env_path = UZI_RUNTIME_ROOT / ".env"
    if not env_path.exists():
        return
    for line in env_path.read_text(encoding="utf-8").splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("#") or "=" not in stripped:
            continue
        key, _, value = stripped.partition("=")
        key = key.strip()
        if key and key not in env:
            env[key] = value.strip().strip("'\"")


def build_analyze_env(payload: AnalyzeRequest) -> dict[str, str]:
    env = build_base_env()
    if payload.depth:
        env["UZI_DEPTH"] = payload.depth
        env["UZI_LITE"] = "1" if payload.depth == "lite" else "0"

    config = payload.ai_model
    if not config:
        return env

    base_url = (config.base_url or "").strip().rstrip("/")
    api_key = (config.api_key or "").strip()
    model_id = (config.model_id or "").strip()
    provider_name = (config.provider_name or "").strip()
    if not base_url or not api_key or not model_id:
        return env

    env["CODEX"] = "1"
    env["OPENAI_BASE_URL"] = base_url
    env["OPENAI_API_BASE"] = base_url
    env["OPENAI_API_BASE_URL"] = base_url
    env["OPENAI_API_KEY"] = api_key
    env["OPENAI_MODEL"] = model_id
    env["OPENAI_DEFAULT_MODEL"] = model_id
    env["CODEX_MODEL"] = model_id
    env["UZI_AI_PROVIDER"] = provider_name
    env["UZI_AI_BASE_URL"] = base_url
    env["UZI_AI_MODEL"] = model_id

    if provider_name.lower() == "deepseek" or "deepseek" in base_url.lower():
        env["DEEPSEEK_BASE_URL"] = base_url
        env["DEEPSEEK_API_KEY"] = api_key
        env["DEEPSEEK_MODEL"] = model_id

    return env


def extract_report_path(stdout: str, stderr: str) -> Path | None:
    for text in (stdout, stderr):
        for line in text.splitlines():
            match = REPORT_PATH_PATTERN.search(line)
            if not match:
                continue
            candidate = Path(match.group(1).strip()).resolve()
            if candidate.exists():
                return candidate
    return None


def resolve_latest_report(ticker: str) -> Path | None:
    patterns = [
        f"{ticker}_*/full-report-standalone.html",
        f"{ticker.upper()}_*/full-report-standalone.html",
        f"{ticker.lower()}_*/full-report-standalone.html",
    ]
    matches: list[Path] = []
    for pattern in patterns:
        matches.extend(REPORTS_ROOT.glob(pattern))
    if not matches:
        return None
    return sorted(matches)[-1].resolve()


def report_url(request: Request, report_path: Path) -> str:
    relative_path = report_path.relative_to(REPORTS_ROOT).as_posix()
    return f"{str(request.base_url).rstrip('/')}/reports/{relative_path}"


def report_entry(request: Request, report_path: Path) -> dict:
    stat = report_path.stat()
    relative_path = report_path.relative_to(REPORTS_ROOT).as_posix()
    parent_name = report_path.parent.name
    ticker, date_tag, depth = parse_report_directory_name(parent_name)
    return {
        "ticker": ticker,
        "depth": depth,
        "directory_name": parent_name,
        "date_tag": date_tag,
        "report_relative_path": relative_path,
        "report_url": report_url(request, report_path),
        "size_bytes": stat.st_size,
        "updated_at": datetime.fromtimestamp(stat.st_mtime, tz=timezone.utc).isoformat(),
    }


def iter_report_files() -> list[Path]:
    if not REPORTS_ROOT.exists():
        return []
    return sorted(
        REPORTS_ROOT.glob("*/full-report-standalone.html"),
        key=lambda path: path.stat().st_mtime,
        reverse=True,
    )


def ensure_depth_report_path(payload: AnalyzeRequest, report_path: Path) -> Path:
    depth = normalize_depth(payload.depth)
    ticker = normalize_ticker(payload.ticker)
    _, date_tag, current_depth = parse_report_directory_name(report_path.parent.name)
    if current_depth == depth:
        return report_path
    if not date_tag:
        date_tag = datetime.now(timezone.utc).strftime("%Y%m%d")
    target_dir = REPORTS_ROOT / report_directory_name(ticker, date_tag, depth)
    target_dir.mkdir(parents=True, exist_ok=True)
    target_path = target_dir / report_path.name
    if report_path.resolve() != target_path.resolve():
        for source in report_path.parent.iterdir():
            destination = target_dir / source.name
            if source.is_dir():
                shutil.copytree(source, destination, dirs_exist_ok=True)
            elif source.is_file():
                shutil.copy2(source, destination)
    return target_path


def resolve_report_target(relative_path: str) -> Path:
    cleaned_path = Path(relative_path.strip("/"))
    target = (REPORTS_ROOT / cleaned_path).resolve()
    if not str(target).startswith(str(REPORTS_ROOT)):
        raise ValueError("invalid report path")
    if target == REPORTS_ROOT or not target.exists():
        raise FileNotFoundError("report path not found")
    return target


def strip_ansi(raw: str) -> str:
    return ANSI_ESCAPE_PATTERN.sub("", raw or "").strip()


def detect_stage(line: str) -> tuple[str, str] | None:
    normalized = strip_ansi(line).lower()
    if not normalized:
        return None
    for stage_key, summary, keywords in STAGE_RULES:
        if any(keyword in normalized for keyword in keywords):
            return stage_key, summary
    return None


def should_advance_stage(current_stage: str, next_stage: str) -> bool:
    return STAGE_ORDER.get(next_stage, -1) > STAGE_ORDER.get(current_stage, -1)


def sse_event(event: str, payload: dict) -> str:
    return f"event: {event}\ndata: {json.dumps(payload, ensure_ascii=False)}\n\n"


def build_analyze_command(payload: AnalyzeRequest) -> list[str]:
    command = [UZI_PYTHON_BIN, "run.py", payload.ticker, "--no-browser"]
    if payload.depth:
        command.extend(["--depth", payload.depth])
    if payload.no_resume:
        command.append("--no-resume")
    return command


def has_ai_model(payload: AnalyzeRequest) -> bool:
    config = payload.ai_model
    if not config:
        return False
    return all(
        (value or "").strip()
        for value in (config.base_url, config.api_key, config.model_id)
    )


def chat_completions_url(base_url: str) -> str:
    normalized = (base_url or "").strip().rstrip("/")
    if normalized.endswith("/chat/completions"):
        return normalized
    if normalized.endswith("/v1"):
        return f"{normalized}/chat/completions"
    return f"{normalized}/v1/chat/completions"


def parse_agent_analysis_content(text: str) -> dict:
    content = (text or "").strip()
    fenced = re.search(r"```(?:json)?\s*(\{.*?\})\s*```", content, re.DOTALL | re.IGNORECASE)
    if fenced:
        content = fenced.group(1).strip()

    decoder = json.JSONDecoder()
    start = content.find("{")
    if start < 0:
        raise ValueError("AI response did not contain JSON object")
    parsed, _ = decoder.raw_decode(content[start:])
    if not isinstance(parsed, dict):
        raise ValueError("agent_analysis must be a JSON object")
    return parsed


def build_stage_command(stage: Literal["stage1", "stage2"], ticker: str) -> list[str]:
    scripts_dir = UZI_SCRIPTS_ROOT.as_posix()
    ticker_literal = json.dumps(ticker)
    call = f'{stage}({ticker_literal})'
    if stage == "stage2":
        call = f'print("📄 报告路径: " + str({call}))'
    script = (
        "import os, sys; "
        f"sys.path.insert(0, {json.dumps(scripts_dir)}); "
        f"os.chdir({json.dumps(scripts_dir)}); "
        f"from run_real_test import {stage}; "
        f"{call}"
    )
    return [UZI_PYTHON_BIN, "-c", script]


def cache_file(ticker: str, name: str) -> Path:
    normalized = normalize_ticker(ticker)
    return UZI_SCRIPTS_ROOT / ".cache" / normalized / f"{name}.json"


def read_cache_json(ticker: str, name: str) -> dict:
    path = cache_file(ticker, name)
    if not path.exists():
        raise FileNotFoundError(f"UZI cache missing: {path}")
    return json.loads(path.read_text(encoding="utf-8"))


def write_cache_json(ticker: str, name: str, data: dict) -> Path:
    path = cache_file(ticker, name)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(data, ensure_ascii=False, indent=2), encoding="utf-8")
    return path


def build_agent_analysis_prompt(ticker: str, raw: dict, dims: dict, panel: dict) -> str:
    compact_payload = {
        "ticker": normalize_ticker(ticker),
        "raw_data": raw,
        "dimensions": dims,
        "panel": panel,
    }
    return (
        "你是股票研报的投研复核 agent。请只输出一个 JSON 对象，不要输出解释文字。\n"
        "目标：基于 raw_data、dimensions、panel 生成 agent_analysis.json。\n"
        "必须包含字段：agent_reviewed=true、dim_commentary、panel_insights、"
        "great_divide_override、narrative_override。\n"
        "dim_commentary 使用维度 key 作为键，每条用用户可读中文说明关键判断。\n"
        "panel_insights 用一段中文概括评委分歧。\n"
        "great_divide_override 至少包含 punchline、bull_say_rounds、bear_say_rounds。\n"
        "narrative_override 至少包含 core_conclusion、risks、buy_zones。\n\n"
        f"输入数据 JSON：{json.dumps(compact_payload, ensure_ascii=False, default=str)}"
    )


def generate_agent_analysis(payload: AnalyzeRequest, env: dict[str, str]) -> dict:
    if not payload.ai_model:
        raise ValueError("AI model config is required")

    raw = read_cache_json(payload.ticker, "raw_data")
    dims = read_cache_json(payload.ticker, "dimensions")
    panel = read_cache_json(payload.ticker, "panel")
    request_body = {
        "model": payload.ai_model.model_id,
        "messages": [
            {
                "role": "system",
                "content": "你只返回严格 JSON，不要包含 Markdown 解释。",
            },
            {
                "role": "user",
                "content": build_agent_analysis_prompt(payload.ticker, raw, dims, panel),
            },
        ],
        "temperature": 0.2,
    }
    data = json.dumps(request_body, ensure_ascii=False).encode("utf-8")
    request_url = chat_completions_url(payload.ai_model.base_url or "")
    request_obj = urllib.request.Request(
        request_url,
        data=data,
        headers={
            "Authorization": f"Bearer {env['OPENAI_API_KEY']}",
            "Content-Type": "application/json",
        },
        method="POST",
    )

    try:
        with urllib.request.urlopen(request_obj, timeout=180) as response:
            response_text = response.read().decode("utf-8")
    except urllib.error.HTTPError as exc:
        body = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"AI model request failed: HTTP {exc.code} {tail_text(body, 1000)}") from exc

    response_json = json.loads(response_text)
    content = (
        response_json.get("choices", [{}])[0]
        .get("message", {})
        .get("content", "")
    )
    agent_analysis = parse_agent_analysis_content(content)
    agent_analysis["agent_reviewed"] = True
    for key in ("dim_commentary", "great_divide_override", "narrative_override"):
        if not isinstance(agent_analysis.get(key), dict):
            raise ValueError(f"agent_analysis.{key} must be an object")
    if not isinstance(agent_analysis.get("panel_insights"), str):
        raise ValueError("agent_analysis.panel_insights must be a string")

    write_cache_json(payload.ticker, "agent_analysis", agent_analysis)
    return agent_analysis


def build_final_result(
    *,
    request: Request,
    payload: AnalyzeRequest,
    command: list[str],
    started_at: float,
    returncode: int,
    stdout_text: str,
    stderr_text: str,
) -> tuple[int, dict]:
    elapsed = round(time.time() - started_at, 2)
    report_path = extract_report_path(stdout_text, stderr_text) or resolve_latest_report(payload.ticker)

    if returncode != 0 and report_path is None:
        return 502, {
            "status": "failed",
            "ticker": payload.ticker,
            "exit_code": returncode,
            "duration_seconds": elapsed,
            "command": command,
            "stdout_tail": tail_text(stdout_text),
            "stderr_tail": tail_text(stderr_text),
        }

    if report_path is None or not report_path.exists():
        return 502, {
            "status": "failed",
            "ticker": payload.ticker,
            "exit_code": returncode,
            "duration_seconds": elapsed,
            "command": command,
            "error": "UZI completed without a resolvable report path",
            "stdout_tail": tail_text(stdout_text),
            "stderr_tail": tail_text(stderr_text),
        }

    report_path = ensure_depth_report_path(payload, report_path)
    report_relative_path = report_path.relative_to(REPORTS_ROOT).as_posix()
    status = "succeeded" if returncode == 0 else "partial_success"
    return (200 if returncode == 0 else 207), {
        "status": status,
        "ticker": payload.ticker,
        "exit_code": returncode,
        "duration_seconds": elapsed,
        "command": command,
        "report_path": str(report_path),
        "report_relative_path": report_relative_path,
        "report_url": report_url(request, report_path),
        "report": report_entry(request, report_path),
        "stdout_tail": tail_text(stdout_text),
        "stderr_tail": tail_text(stderr_text),
    }


def deepseek_tui_stream_url() -> str:
    return f"{DEEPSEEK_TUI_BASE_URL}/v1/stream"


def select_deepseek_tui_model(payload: AnalyzeRequest) -> str:
    configured = payload.ai_model.model_id if payload.ai_model else None
    return (configured or DEEPSEEK_TUI_DEFAULT_MODEL).strip() or "deepseek-v4-pro"


def ticker_to_market_symbol(ticker: str) -> str:
    normalized = normalize_ticker(ticker)
    if normalized.endswith(".SH"):
        return f"sh{normalized.removesuffix('.SH')}"
    if normalized.endswith(".SZ"):
        return f"sz{normalized.removesuffix('.SZ')}"
    raw = normalized.replace(".", "")
    if raw.startswith(("5", "6", "9")):
        return f"sh{raw}"
    if raw.startswith(("0", "1", "2", "3")):
        return f"sz{raw}"
    return raw.lower()


def infer_stock_type(ticker: str) -> int:
    code = normalize_ticker(ticker).split(".", 1)[0]
    if code.startswith(("51", "56", "58", "15", "16")):
        return 2
    if code.startswith(("000", "399")):
        return 3
    return 1


def format_cny_amount(value: float | int | None) -> str:
    if value is None:
        return "数据待核验"
    amount = float(value)
    if abs(amount) >= 100_000_000:
        return f"{amount / 100_000_000:.2f}亿元"
    if abs(amount) >= 10_000:
        return f"{amount / 10_000:.2f}万元"
    return f"{amount:.2f}元"


def format_decimal(value: float | int | None, digits: int = 2) -> str:
    if value is None:
        return "数据待核验"
    return f"{float(value):.{digits}f}"


def safe_float(value) -> float | None:
    try:
        if value is None or value == "":
            return None
        return float(value)
    except (TypeError, ValueError):
        return None


def build_market_context_from_rows(payload: AnalyzeRequest, rows: list[dict], provider: str) -> dict | None:
    if not rows:
        return None
    sorted_rows = sorted(rows, key=lambda row: str(row.get("trade_date") or row.get("datetime") or ""))
    latest = sorted_rows[-1]
    closes = [safe_float(row.get("close")) for row in sorted_rows]
    closes = [value for value in closes if value is not None]
    if not closes:
        return None
    latest_close = safe_float(latest.get("close"))
    prev_close = closes[-2] if len(closes) >= 2 else None
    change_pct = None
    if latest_close is not None and prev_close:
        change_pct = (latest_close / prev_close - 1) * 100
    pct_20d = None
    if len(closes) >= 21 and closes[-21]:
        pct_20d = (closes[-1] / closes[-21] - 1) * 100
    highs = [safe_float(row.get("high")) for row in sorted_rows]
    lows = [safe_float(row.get("low")) for row in sorted_rows]
    amounts = [safe_float(row.get("amount")) for row in sorted_rows[-5:]]
    high_52w = max(value for value in highs if value is not None) if any(value is not None for value in highs) else None
    low_52w = min(value for value in lows if value is not None) if any(value is not None for value in lows) else None
    amount_values = [value for value in amounts if value is not None]
    avg_amount_5d = sum(amount_values) / len(amount_values) if amount_values else None
    trade_date = str(latest.get("trade_date") or latest.get("datetime") or "").split("T", 1)[0]
    name = str(latest.get("name") or "").strip()
    return {
        "status": "ok",
        "source": f"{AKSHARE_SERVICE_URL}/api/v1/history",
        "provider": provider,
        "ticker": normalize_ticker(payload.ticker),
        "name": name,
        "trade_date": trade_date,
        "rows": len(sorted_rows),
        "latest_price": latest_close,
        "prev_close": prev_close,
        "change_pct": change_pct,
        "pct_20d": pct_20d,
        "high_52w": high_52w,
        "low_52w": low_52w,
        "avg_amount_5d": avg_amount_5d,
        "avg_amount_5d_display": format_cny_amount(avg_amount_5d),
    }


def fetch_market_history_context(payload: AnalyzeRequest) -> dict | None:
    end = datetime.now(timezone.utc).date()
    start = end - timedelta(days=430)
    request_body = {
        "symbol": ticker_to_market_symbol(payload.ticker),
        "stock_type": infer_stock_type(payload.ticker),
        "start_date": start.strftime("%Y%m%d"),
        "end_date": end.strftime("%Y%m%d"),
        "adjust": "forward_additive",
    }
    data = json.dumps(request_body, ensure_ascii=False).encode("utf-8")
    headers = {"Content-Type": "application/json"}
    if AKSHARE_SERVICE_TOKEN:
        headers["Authorization"] = f"Bearer {AKSHARE_SERVICE_TOKEN}"
    request_obj = urllib.request.Request(
        f"{AKSHARE_SERVICE_URL}/api/v1/history",
        data=data,
        headers=headers,
        method="POST",
    )
    try:
        with urllib.request.urlopen(request_obj, timeout=UZI_MARKET_DATA_TIMEOUT_SECONDS) as response:
            response_json = json.loads(response.read().decode("utf-8"))
    except Exception as exc:
        logger.warning(
            "market_history_fetch_failed ticker=%s url=%s error=%s",
            payload.ticker,
            AKSHARE_SERVICE_URL,
            exc,
        )
        return None
    rows = response_json.get("data") if isinstance(response_json, dict) else None
    if not isinstance(rows, list):
        logger.warning("market_history_invalid_response ticker=%s body_keys=%s", payload.ticker, list(response_json)[:20])
        return None
    context = build_market_context_from_rows(payload, rows, str(response_json.get("provider") or "unknown"))
    if context:
        logger.info("market_history_context %s", json.dumps(context, ensure_ascii=False, default=str))
    return context


def load_cached_market_context(payload: AnalyzeRequest) -> dict | None:
    raw_path = UZI_SCRIPTS_ROOT / ".cache" / normalize_ticker(payload.ticker) / "raw_data.json"
    if not raw_path.exists():
        return None
    try:
        raw = json.loads(raw_path.read_text(encoding="utf-8"))
        basic = raw.get("dimensions", {}).get("0_basic", {}).get("data", {})
        kline = raw.get("dimensions", {}).get("2_kline", {}).get("data", {})
        indicators = kline.get("indicators", {})
    except Exception as exc:
        logger.warning("market_cache_read_failed ticker=%s path=%s error=%s", payload.ticker, raw_path, exc)
        return None
    context = {
        "status": "cached",
        "source": str(raw_path),
        "provider": "uzi_cache",
        "ticker": normalize_ticker(payload.ticker),
        "name": basic.get("name"),
        "trade_date": "cache",
        "latest_price": safe_float(basic.get("price") or indicators.get("last_close")),
        "prev_close": safe_float(basic.get("prev_close")),
        "high_52w": safe_float(basic.get("high_52w") or indicators.get("year_high")),
        "low_52w": safe_float(basic.get("low_52w") or indicators.get("year_low")),
        "avg_amount_5d": safe_float(basic.get("turnover")),
        "avg_amount_5d_display": format_cny_amount(safe_float(basic.get("turnover"))),
    }
    logger.info("market_cache_context %s", json.dumps(context, ensure_ascii=False, default=str))
    return context


def build_market_context_prompt(context: dict | None) -> str:
    if not context:
        return (
            "行情数据快照：未获取到结构化行情数据。报告中涉及最新价、涨跌幅、52周区间、成交额时必须明确写“数据待核验”。\n"
        )
    lines = [
        "行情数据快照（优先用于报告的行情观察指标卡；不要用 -- 占位替代这些字段）：",
        f"- 数据状态：{context.get('status')}",
        f"- 数据源：{context.get('provider')}；交易日期：{context.get('trade_date')}",
        f"- 股票：{context.get('ticker')} {context.get('name') or ''}".rstrip(),
        f"- 最新可用价/收盘价：{format_decimal(context.get('latest_price'))} CNY",
        f"- 前收盘价：{format_decimal(context.get('prev_close'))}",
    ]
    if context.get("change_pct") is not None:
        lines.append(f"- 单日涨跌幅：{context.get('change_pct'):.2f}%")
    if context.get("pct_20d") is not None:
        lines.append(f"- 近20个交易日涨跌幅：{context.get('pct_20d'):.2f}%")
    lines.extend(
        [
            f"- 52周区间：{format_decimal(context.get('low_52w'))} / {format_decimal(context.get('high_52w'))} CNY",
            f"- 5日均成交额：{context.get('avg_amount_5d_display')}",
            "报告要求：行情观察区必须使用上述数值，并标注“最新可用交易日”而不是写“最新价（待核验）”“-- / --”或“数据待核验”。",
        ]
    )
    return "\n".join(lines) + "\n"


def build_deepseek_tui_prompt(payload: AnalyzeRequest, retry_reason: str | None = None) -> str:
    ticker = normalize_ticker(payload.ticker)
    depth = payload.depth or "medium"
    market_context = fetch_market_history_context(payload) or load_cached_market_context(payload)
    retry_note = ""
    if retry_reason:
        retry_note = (
            "\n上一轮输出未通过 HTML 校验，原因："
            f"{retry_reason}。\n"
            "本轮必须继续生成到完整 HTML 结束，优先保证主体内容、</body> 与 </html> 完整。\n"
        )
    return (
        "你是面向普通投资者的中文股票研报生成 agent。\n"
        "请为目标股票生成一份可直接给用户阅读的完整 HTML 研报。\n"
        "要求：\n"
        "1. 只输出 HTML，不要 Markdown 代码围栏，不要解释文字。\n"
        "2. HTML 必须是完整 standalone 页面，包含 <!doctype html>、<html>、<head>、<body>。\n"
        "3. 内容面向用户，不要暴露开发者日志、内部命令、接口名、prompt 或工具细节。\n"
        "4. 结构包含：核心结论、行情观察、基本面要点、资金与情绪、机会、风险、操作参考。\n"
        "5. 如果下方提供了行情数据快照，必须优先使用快照里的精确数值；只有快照缺字段时，才写成“数据待核验”，不要编造精确行情。\n"
        "6. 风格简洁、信息密度高，避免大段空话。\n"
        "7. 页面需要内置 CSS，适配桌面与手机。\n\n"
        "8. CSS 必须极简，不要写长篇 reset、动画或装饰样式。\n"
        "9. 输出前自检：最后一行必须是 </html>，不得在 <style>、<head> 或 <body> 中途停止。\n\n"
        f"目标股票：{ticker}\n"
        f"分析深度：{depth}\n"
        f"生成日期：{datetime.now(timezone.utc).date().isoformat()}\n"
        f"{retry_note}"
        f"{build_market_context_prompt(market_context)}"
    )


def extract_html_document(output: str) -> str:
    text = (output or "").strip()
    fenced = HTML_FENCE_PATTERN.search(text)
    if fenced:
        text = fenced.group(1).strip()

    lowered = text.lower()
    html_start = lowered.find("<!doctype")
    if html_start < 0:
        html_start = lowered.find("<html")
    html_end = lowered.rfind("</html>")
    if html_start >= 0 and html_end >= 0:
        return text[html_start : html_end + len("</html>")].strip()
    if html_start >= 0:
        raise RuntimeError("DeepSeek-TUI returned incomplete HTML: missing </html>")
    return render_fallback_deepseek_report_html(text)


def validate_deepseek_report_html(html: str) -> None:
    normalized = (html or "").strip().lower()
    missing = [
        tag
        for tag in ("<!doctype", "<html", "<head", "<body", "</body>", "</html>")
        if tag not in normalized
    ]
    if missing:
        raise RuntimeError(f"DeepSeek-TUI returned incomplete HTML: missing {', '.join(missing)}")


def is_valid_report_html(path: Path) -> bool:
    try:
        html = path.read_text(encoding="utf-8", errors="replace")
        validate_deepseek_report_html(html)
        return True
    except Exception:
        return False


def resolve_existing_valid_report(payload: AnalyzeRequest) -> Path | None:
    ticker = normalize_ticker(payload.ticker)
    date_tag = datetime.now(timezone.utc).strftime("%Y%m%d")
    report_dir = REPORTS_ROOT / report_directory_name(ticker, date_tag, payload.depth)
    candidates = [
        report_dir / "full-report-standalone.html",
        report_dir / "full-report.html",
    ]
    candidates.extend(
        sorted(
            report_dir.glob("full-report-standalone.bak-*.html"),
            key=lambda path: path.stat().st_mtime,
            reverse=True,
        )
    )
    for candidate in candidates:
        if candidate.exists() and is_valid_report_html(candidate):
            return candidate.resolve()
    return None


def render_fallback_deepseek_report_html(content: str) -> str:
    safe_content = escape((content or "").strip()).replace("\n", "<br />\n")
    if not safe_content:
        safe_content = "DeepSeek-TUI 已返回空内容，请稍后重试。"
    return f"""<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>AI 研报</title>
  <style>
    body {{ margin: 0; background: #111827; color: #f8fafc; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }}
    main {{ max-width: 960px; margin: 0 auto; padding: 32px 20px; }}
    .card {{ border: 1px solid rgba(255,255,255,.12); border-radius: 20px; background: rgba(255,255,255,.06); padding: 24px; line-height: 1.8; }}
    h1 {{ margin: 0 0 16px; font-size: 28px; }}
  </style>
</head>
<body>
  <main>
    <h1>AI 研报</h1>
    <section class="card">{safe_content}</section>
  </main>
</body>
</html>"""


def resolve_deepseek_tui_cli() -> str | None:
    cli = DEEPSEEK_TUI_CLI_BIN
    if Path(cli).is_file():
        return cli
    return shutil.which(cli)


def build_deepseek_tui_cli_env(payload: AnalyzeRequest) -> dict[str, str]:
    env = build_base_env()
    config = payload.ai_model
    if not config:
        return env

    api_key = (config.api_key or "").strip()
    base_url = (config.base_url or "").strip().rstrip("/")
    if api_key:
        env["DEEPSEEK_API_KEY"] = api_key
    if base_url:
        env["DEEPSEEK_BASE_URL"] = base_url
    return env


def build_deepseek_tui_cli_command(
    payload: AnalyzeRequest,
    cli_path: str,
    retry_reason: str | None = None,
) -> list[str]:
    return [
        cli_path,
        "--model",
        select_deepseek_tui_model(payload),
        "--skip-onboarding",
        "-p",
        build_deepseek_tui_prompt(payload, retry_reason=retry_reason),
    ]


def call_deepseek_tui_cli(payload: AnalyzeRequest, retry_reason: str | None = None) -> dict:
    cli_path = resolve_deepseek_tui_cli()
    if not cli_path:
        raise RuntimeError(
            "DeepSeek-TUI CLI not found; install deepseek-tui or set DEEPSEEK_TUI_CLI_BIN"
        )

    command = build_deepseek_tui_cli_command(payload, cli_path, retry_reason=retry_reason)
    started_at = time.time()
    logger.info(
        "deepseek_tui_cli_start %s",
        json.dumps(
            {
                "ticker": payload.ticker,
                "normalized_ticker": normalize_ticker(payload.ticker),
                "depth": payload.depth,
                "cli_path": cli_path,
                "model": select_deepseek_tui_model(payload),
                "timeout_seconds": DEEPSEEK_TUI_CLI_TIMEOUT_SECONDS,
                "has_ai_model": has_ai_model(payload),
                "is_retry": bool(retry_reason),
            },
            ensure_ascii=False,
        ),
    )
    try:
        process = subprocess.run(
            command,
            cwd=str(Path.cwd()),
            env=build_deepseek_tui_cli_env(payload),
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            encoding="utf-8",
            errors="replace",
            timeout=DEEPSEEK_TUI_CLI_TIMEOUT_SECONDS,
            check=False,
        )
    except subprocess.TimeoutExpired as exc:
        stdout = exc.stdout or ""
        stderr = exc.stderr or ""
        log_text_diagnostics("deepseek_tui_cli_timeout_stdout", stdout, ticker=payload.ticker)
        log_text_diagnostics("deepseek_tui_cli_timeout_stderr", stderr, ticker=payload.ticker)
        raise RuntimeError(
            f"DeepSeek-TUI CLI timed out after {DEEPSEEK_TUI_CLI_TIMEOUT_SECONDS}s"
        ) from exc

    stdout = process.stdout.strip()
    stderr = process.stderr.strip()
    logger.info(
        "deepseek_tui_cli_finish %s",
        json.dumps(
            {
                "ticker": payload.ticker,
                "returncode": process.returncode,
                "duration_seconds": round(time.time() - started_at, 2),
                "stdout_chars": len(stdout),
                "stderr_chars": len(stderr),
            },
            ensure_ascii=False,
        ),
    )
    log_text_diagnostics("deepseek_tui_cli_stdout", stdout, ticker=payload.ticker, returncode=process.returncode)
    if stderr:
        log_text_diagnostics("deepseek_tui_cli_stderr", stderr, ticker=payload.ticker, returncode=process.returncode)
    if process.returncode != 0:
        raise RuntimeError(
            f"DeepSeek-TUI CLI failed: exit {process.returncode} {tail_text(stderr or stdout, 1000)}"
        )

    return {
        "output": stdout,
        "model": select_deepseek_tui_model(payload),
        "command": [cli_path, "--model", select_deepseek_tui_model(payload), "--skip-onboarding", "-p", "<prompt>"],
        "events": [],
    }


def parse_sse_events(text: str) -> list[tuple[str, dict]]:
    events: list[tuple[str, dict]] = []
    normalized = (text or "").replace("\r\n", "\n")
    for frame in normalized.split("\n\n"):
        event_name = "message"
        data_lines: list[str] = []
        for line in frame.splitlines():
            if line.startswith("event:"):
                event_name = line.removeprefix("event:").strip()
            elif line.startswith("data:"):
                data_lines.append(line.removeprefix("data:").lstrip())
        if not data_lines:
            continue
        data_text = "\n".join(data_lines)
        try:
            payload = json.loads(data_text)
        except json.JSONDecodeError:
            payload = {"raw": data_text}
        events.append((event_name, payload if isinstance(payload, dict) else {"data": payload}))
    return events


def call_deepseek_tui_stream(payload: AnalyzeRequest, retry_reason: str | None = None) -> dict:
    body = json.dumps(
        {
            "prompt": build_deepseek_tui_prompt(payload, retry_reason=retry_reason),
            "model": select_deepseek_tui_model(payload),
            "mode": "agent",
            "allow_shell": False,
            "trust_mode": False,
            "auto_approve": False,
        },
        ensure_ascii=False,
    ).encode("utf-8")
    request_obj = urllib.request.Request(
        deepseek_tui_stream_url(),
        data=body,
        headers={"Content-Type": "application/json", "Accept": "text/event-stream"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(request_obj, timeout=DEEPSEEK_TUI_REQUEST_TIMEOUT_SECONDS) as response:
            response_text = response.read().decode("utf-8")
    except urllib.error.HTTPError as exc:
        error_body = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"DeepSeek-TUI stream failed: HTTP {exc.code} {tail_text(error_body, 1000)}") from exc
    except urllib.error.URLError as exc:
        raise RuntimeError(f"DeepSeek-TUI runtime unavailable at {DEEPSEEK_TUI_BASE_URL}: {exc.reason}") from exc

    events = parse_sse_events(response_text)
    logger.info(
        "deepseek_tui_http_stream_finish %s",
        json.dumps(
            {
                "ticker": payload.ticker,
                "url": deepseek_tui_stream_url(),
                "events": len(events),
                "response_chars": len(response_text),
            },
            ensure_ascii=False,
        ),
    )
    log_text_diagnostics("deepseek_tui_http_raw_sse", response_text, ticker=payload.ticker, events=len(events))
    output_parts: list[str] = []
    for event_name, event_payload in events:
        if event_name == "message.delta":
            output_parts.append(str(event_payload.get("content") or ""))
        elif event_name == "error":
            message = event_payload.get("message") or event_payload.get("raw") or "unknown error"
            raise RuntimeError(f"DeepSeek-TUI stream error: {message}")

    return {
        "output": "".join(output_parts),
        "model": select_deepseek_tui_model(payload),
        "events": [{"event": name, "payload": body} for name, body in events],
    }


def call_deepseek_tui(payload: AnalyzeRequest, retry_reason: str | None = None) -> dict:
    if has_ai_model(payload):
        return call_deepseek_tui_cli(payload, retry_reason=retry_reason)
    return call_deepseek_tui_stream(payload, retry_reason=retry_reason)


def should_use_deepseek_tui_report(payload: AnalyzeRequest) -> bool:
    # 标准研报固定走轻量 HTML 生成；深入研报保留 UZI-Skill deep 模板链路。
    return normalize_depth(payload.depth) != "deep"


def write_deepseek_tui_report(payload: AnalyzeRequest, output: str) -> Path:
    ticker = normalize_ticker(payload.ticker)
    date_tag = datetime.now(timezone.utc).strftime("%Y%m%d")
    report_dir = REPORTS_ROOT / report_directory_name(ticker, date_tag, payload.depth)
    report_dir.mkdir(parents=True, exist_ok=True)
    report_path = report_dir / "full-report-standalone.html"
    html = extract_html_document(output)
    log_text_diagnostics("deepseek_tui_report_input", output, ticker=payload.ticker)
    log_text_diagnostics("deepseek_tui_report_html", html, ticker=payload.ticker, report_path=str(report_path))
    validate_deepseek_report_html(html)
    if report_path.exists():
        backup_path = report_path.with_name(
            f"{report_path.stem}.bak-{datetime.now(timezone.utc).strftime('%Y%m%d%H%M%S')}{report_path.suffix}"
        )
        shutil.copy2(report_path, backup_path)
        logger.info(
            "deepseek_tui_report_backup %s",
            json.dumps(
                {
                    "ticker": payload.ticker,
                    "report_path": str(report_path),
                    "backup_path": str(backup_path),
                    "backup_size_bytes": backup_path.stat().st_size,
                },
                ensure_ascii=False,
            ),
        )
    temp_path = report_path.with_suffix(".html.tmp")
    temp_path.write_text(html, encoding="utf-8")
    os.replace(temp_path, report_path)
    logger.info(
        "deepseek_tui_report_written %s",
        json.dumps(
            {
                "ticker": payload.ticker,
                "report_path": str(report_path),
                "size_bytes": report_path.stat().st_size,
            },
            ensure_ascii=False,
        ),
    )
    return report_path.resolve()


def should_retry_deepseek_report_error(exc: Exception) -> bool:
    message = str(exc)
    return (
        "incomplete HTML" in message
        or "missing </html>" in message
        or "missing </body>" in message
    )


def build_deepseek_tui_result(
    *,
    request: Request,
    payload: AnalyzeRequest,
    started_at: float,
    prompt_response: dict,
) -> dict:
    output = str(prompt_response.get("output") or "")
    fallback_reason = ""
    fallback_used = False
    report_path: Path | None = None
    max_attempts = max(1, UZI_DEEPSEEK_TUI_REPORT_RETRIES + 1)
    try:
        for attempt in range(max_attempts):
            try:
                report_path = write_deepseek_tui_report(payload, output)
                break
            except Exception as exc:
                fallback_reason = str(exc)
                if attempt >= max_attempts - 1 or not should_retry_deepseek_report_error(exc):
                    raise
                logger.warning(
                    "deepseek_tui_report_retry %s",
                    json.dumps(
                        {
                            "ticker": payload.ticker,
                            "attempt": attempt + 1,
                            "max_attempts": max_attempts,
                            "reason": fallback_reason,
                        },
                        ensure_ascii=False,
                    ),
                )
                prompt_response = call_deepseek_tui(payload, retry_reason=fallback_reason)
                output = str(prompt_response.get("output") or "")
    except Exception as exc:
        fallback_reason = str(exc)
        report_path = resolve_existing_valid_report(payload)
        if report_path is None:
            raise
        fallback_used = True
        logger.warning(
            "deepseek_tui_report_fallback %s",
            json.dumps(
                {
                    "ticker": payload.ticker,
                    "reason": fallback_reason,
                    "report_path": str(report_path),
                    "size_bytes": report_path.stat().st_size,
                },
                ensure_ascii=False,
            ),
        )
    if report_path is None:
        raise RuntimeError("DeepSeek-TUI report was not written")
    report_text = report_path.read_text(encoding="utf-8", errors="replace")
    elapsed = round(time.time() - started_at, 2)
    return {
        "success": True,
        "status": "partial_success" if fallback_used else "succeeded",
        "ticker": payload.ticker,
        "exit_code": 0,
        "duration_seconds": elapsed,
        "command": prompt_response.get("command") or ["deepseek", "serve", "--http", deepseek_tui_stream_url()],
        "report_path": str(report_path),
        "report_relative_path": report_path.relative_to(REPORTS_ROOT).as_posix(),
        "report_url": report_url(request, report_path),
        "report": report_entry(request, report_path),
        "stdout_tail": tail_text(output),
        "stderr_tail": fallback_reason,
        "backend": "deepseek_tui",
        "model": str(prompt_response.get("model") or select_deepseek_tui_model(payload)),
        "output_chars": len(output),
        "report_chars": len(report_text),
        "report_size_bytes": report_path.stat().st_size,
        "fallback_report_used": fallback_used,
    }


@app.get("/health")
def health() -> dict:
    return {
        "status": "ok",
        "service": "uzi",
        "backend": UZI_RUNTIME_BACKEND,
        "runtime_root": str(UZI_RUNTIME_ROOT),
        "reports_root": str(REPORTS_ROOT),
        "run_py_exists": (UZI_RUNTIME_ROOT / "run.py").exists(),
        "deepseek_tui_base_url": DEEPSEEK_TUI_BASE_URL,
        "timeout_seconds": RUN_TIMEOUT_SECONDS,
        "port": APP_PORT,
    }


@app.post("/internal/analyze_sync", response_model=None)
def analyze_sync(payload: AnalyzeRequest, request: Request):
    run_py = UZI_RUNTIME_ROOT / "run.py"
    use_deepseek_tui_report = should_use_deepseek_tui_report(payload)
    if not use_deepseek_tui_report:
        return JSONResponse(
            status_code=400,
            content={
                "status": "failed",
                "error": "queued UZI analyze currently supports standard report only",
            },
        )
    if not run_py.exists() and not use_deepseek_tui_report:
        return JSONResponse(
            status_code=500,
            content={
                "status": "failed",
                "error": f"UZI runtime missing: {run_py}",
            },
        )

    started_at = time.time()
    try:
        logger.info(
            "deepseek_tui_analyze_sync_start %s",
            json.dumps(
                {
                    "ticker": payload.ticker,
                    "normalized_ticker": normalize_ticker(payload.ticker),
                    "depth": payload.depth,
                    "has_ai_model": has_ai_model(payload),
                    "backend": "deepseek_tui",
                },
                ensure_ascii=False,
            ),
        )
        prompt_response = call_deepseek_tui(payload)
        final_payload = build_deepseek_tui_result(
            request=request,
            payload=payload,
            started_at=started_at,
            prompt_response=prompt_response,
        )
        logger.info(
            "deepseek_tui_analyze_sync_complete %s",
            json.dumps(
                {
                    "ticker": payload.ticker,
                    "duration_seconds": final_payload.get("duration_seconds"),
                    "output_chars": final_payload.get("output_chars"),
                    "report_chars": final_payload.get("report_chars"),
                    "report_size_bytes": final_payload.get("report_size_bytes"),
                    "report_relative_path": final_payload.get("report_relative_path"),
                },
                ensure_ascii=False,
            ),
        )
        return JSONResponse(status_code=200, content=final_payload)
    except Exception as exc:
        logger.exception(
            "deepseek_tui_analyze_sync_failed ticker=%s duration_seconds=%.2f",
            payload.ticker,
            time.time() - started_at,
        )
        return JSONResponse(
            status_code=500,
            content={
                "status": "failed",
                "ticker": payload.ticker,
                "error": str(exc),
                "duration_seconds": round(time.time() - started_at, 2),
            },
        )


@app.post("/analyze", response_model=None)
def analyze(payload: AnalyzeRequest, request: Request):
    run_py = UZI_RUNTIME_ROOT / "run.py"
    use_deepseek_tui_report = should_use_deepseek_tui_report(payload)
    if not use_deepseek_tui_report and not run_py.exists():
        return JSONResponse(
            status_code=500,
            content={
                "status": "failed",
                "error": f"UZI runtime missing: {run_py}",
            },
        )

    if use_deepseek_tui_report:
        def deepseek_event_stream():
            started_at = time.time()
            try:
                logger.info(
                    "deepseek_tui_analyze_start %s",
                    json.dumps(
                        {
                            "ticker": payload.ticker,
                            "normalized_ticker": normalize_ticker(payload.ticker),
                            "depth": payload.depth,
                            "has_ai_model": has_ai_model(payload),
                            "backend": "deepseek_tui",
                        },
                        ensure_ascii=False,
                    ),
                )
                yield sse_event(
                    "start",
                    {
                        "status": "running",
                        "ticker": payload.ticker,
                        "backend": "deepseek_tui",
                        "summary": "已接收分析请求，开始调用 DeepSeek-TUI 研报引擎",
                        "started_at": datetime.now(timezone.utc).isoformat(),
                    },
                )
                yield sse_event(
                    "stage",
                    {
                        "stage": "analysis",
                        "summary": "DeepSeek-TUI 正在生成研报内容",
                    },
                )
                prompt_response = call_deepseek_tui(payload)
                output = str(prompt_response.get("output") or "")
                yield sse_event(
                    "log",
                    {
                        "stream": "stdout",
                        "line": f"DeepSeek-TUI 已返回研报内容 · {len(output)} chars",
                    },
                )
                yield sse_event(
                    "stage",
                    {
                        "stage": "reporting",
                        "summary": "写入 standalone HTML 研报文件",
                    },
                )
                final_payload = build_deepseek_tui_result(
                    request=request,
                    payload=payload,
                    started_at=started_at,
                    prompt_response=prompt_response,
                )
                logger.info(
                    "deepseek_tui_analyze_complete %s",
                    json.dumps(
                        {
                            "ticker": payload.ticker,
                            "duration_seconds": final_payload.get("duration_seconds"),
                            "output_chars": final_payload.get("output_chars"),
                            "report_chars": final_payload.get("report_chars"),
                            "report_size_bytes": final_payload.get("report_size_bytes"),
                            "report_relative_path": final_payload.get("report_relative_path"),
                        },
                        ensure_ascii=False,
                    ),
                )
                yield sse_event(
                    "log",
                    {
                        "stream": "stdout",
                        "line": (
                            "DeepSeek-TUI 报告写入完成 · "
                            f"output={final_payload.get('output_chars')} chars · "
                            f"html={final_payload.get('report_chars')} chars · "
                            f"file={final_payload.get('report_size_bytes')} bytes"
                        ),
                    },
                )
                yield sse_event(
                    "stage",
                    {
                        "stage": "finalizing",
                        "summary": "DeepSeek-TUI 已完成分析并生成报告",
                    },
                )
                yield sse_event("complete", final_payload)
            except Exception as exc:
                fallback_report = resolve_existing_valid_report(payload)
                if fallback_report is not None:
                    elapsed = round(time.time() - started_at, 2)
                    logger.warning(
                        "deepseek_tui_analyze_error_fallback %s",
                        json.dumps(
                            {
                                "ticker": payload.ticker,
                                "error": str(exc),
                                "report_path": str(fallback_report),
                                "size_bytes": fallback_report.stat().st_size,
                                "duration_seconds": elapsed,
                            },
                            ensure_ascii=False,
                        ),
                    )
                    final_payload = {
                        "status": "partial_success",
                        "ticker": payload.ticker,
                        "exit_code": 0,
                        "duration_seconds": elapsed,
                        "command": ["deepseek", "<fallback-existing-report>"],
                        "report_path": str(fallback_report),
                        "report_relative_path": fallback_report.relative_to(REPORTS_ROOT).as_posix(),
                        "report_url": report_url(request, fallback_report),
                        "report": report_entry(request, fallback_report),
                        "stdout_tail": "",
                        "stderr_tail": str(exc),
                        "backend": "deepseek_tui",
                        "model": select_deepseek_tui_model(payload),
                        "report_chars": len(fallback_report.read_text(encoding="utf-8", errors="replace")),
                        "report_size_bytes": fallback_report.stat().st_size,
                        "fallback_report_used": True,
                    }
                    yield sse_event(
                        "log",
                        {
                            "stream": "stderr",
                            "line": f"DeepSeek-TUI 生成失败，已回退到现有完整报告：{exc}",
                        },
                    )
                    yield sse_event("complete", final_payload)
                    return
                logger.exception(
                    "deepseek_tui_analyze_failed ticker=%s duration_seconds=%s",
                    payload.ticker,
                    round(time.time() - started_at, 2),
                )
                yield sse_event(
                    "error",
                    {
                        "status": "failed",
                        "ticker": payload.ticker,
                        "backend": "deepseek_tui",
                        "error": str(exc),
                        "duration_seconds": round(time.time() - started_at, 2),
                        "stdout_tail": "",
                        "stderr_tail": str(exc),
                    },
                )

        return StreamingResponse(
            deepseek_event_stream(),
            media_type="text/event-stream",
            headers={
                "Cache-Control": "no-cache",
                "Connection": "keep-alive",
                "X-Accel-Buffering": "no",
            },
        )

    use_ai_review = has_ai_model(payload)
    command = build_stage_command("stage1", payload.ticker) if use_ai_review else build_analyze_command(payload)

    def event_stream():
        started_at = time.time()
        stage_key = "bootstrap"
        final_command = command
        active_process: subprocess.Popen | None = None
        stdout_lines: list[str] = []
        stderr_lines: list[str] = []
        env = build_analyze_env(payload)

        yield sse_event(
            "start",
            {
                "status": "running",
                "ticker": payload.ticker,
                "command": command,
                "summary": "已接收分析请求，开始初始化 UZI 运行环境",
                "started_at": datetime.now(timezone.utc).isoformat(),
            },
        )
        yield sse_event(
            "stage",
            {
                "stage": stage_key,
                "summary": "准备运行环境与参数校验",
            },
        )

        def run_command(current_command: list[str]) -> Generator[str, None, int | None]:
            nonlocal active_process, stage_key
            active_process = subprocess.Popen(
                current_command,
                cwd=str(UZI_RUNTIME_ROOT),
                env=env,
                stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT,
                text=True,
                encoding="utf-8",
                errors="replace",
                bufsize=1,
            )
            assert active_process.stdout is not None
            for raw_line in active_process.stdout:
                if time.time() - started_at > RUN_TIMEOUT_SECONDS:
                    active_process.kill()
                    active_process.wait(timeout=5)
                    timed_out_payload = {
                        "status": "failed",
                        "ticker": payload.ticker,
                        "error": f"UZI analyze timed out after {RUN_TIMEOUT_SECONDS}s",
                        "duration_seconds": round(time.time() - started_at, 2),
                        "command": current_command,
                        "stdout_tail": tail_text("\n".join(stdout_lines)),
                        "stderr_tail": tail_text("\n".join(stderr_lines)),
                    }
                    yield sse_event("error", timed_out_payload)
                    return None

                clean_line = strip_ansi(raw_line)
                if not clean_line:
                    continue

                stdout_lines.append(clean_line)
                yield sse_event(
                    "log",
                    {
                        "stream": "stdout",
                        "line": clean_line,
                    },
                )

                detected = detect_stage(clean_line)
                if detected and should_advance_stage(stage_key, detected[0]):
                    stage_key = detected[0]
                    yield sse_event(
                        "stage",
                        {
                            "stage": stage_key,
                            "summary": detected[1],
                            "line": clean_line,
                        },
                    )

            return active_process.wait(timeout=5)

        try:
            if use_ai_review:
                stage1_command = command
                stage1_returncode = yield from run_command(stage1_command)
                if stage1_returncode is None:
                    return
                if stage1_returncode != 0:
                    final_payload = {
                        "status": "failed",
                        "ticker": payload.ticker,
                        "exit_code": stage1_returncode,
                        "duration_seconds": round(time.time() - started_at, 2),
                        "command": stage1_command,
                        "stdout_tail": tail_text("\n".join(stdout_lines)),
                        "stderr_tail": tail_text("\n".join(stderr_lines)),
                    }
                    yield sse_event("error", final_payload)
                    return

                stage_key = "analysis"
                yield sse_event(
                    "stage",
                    {
                        "stage": "analysis",
                        "summary": "调用已配置 AI 模型生成研报复核内容",
                    },
                )
                agent_analysis = generate_agent_analysis(payload, env)
                stdout_lines.append(
                    f"AI agent_analysis.json 已写入 · dim_commentary="
                    f"{len(agent_analysis.get('dim_commentary') or {})}"
                )
                yield sse_event(
                    "log",
                    {
                        "stream": "stdout",
                        "line": "AI 研报复核完成，继续生成最终 HTML 报告",
                    },
                )

                final_command = build_stage_command("stage2", payload.ticker)
                returncode = yield from run_command(final_command)
                if returncode is None:
                    return
            else:
                returncode = yield from run_command(final_command)
                if returncode is None:
                    return

            final_status_code, final_payload = build_final_result(
                request=request,
                payload=payload,
                command=final_command,
                started_at=started_at,
                returncode=returncode,
                stdout_text="\n".join(stdout_lines),
                stderr_text="\n".join(stderr_lines),
            )
            stage_summary = (
                "UZI 已完成分析并生成报告"
                if final_status_code in (200, 207)
                else "UZI 分析执行结束，但未成功生成有效报告"
            )
            yield sse_event(
                "stage",
                {
                    "stage": "finalizing",
                    "summary": stage_summary,
                },
            )
            final_event = "complete" if final_status_code in (200, 207) else "error"
            yield sse_event(final_event, final_payload)
        except Exception as exc:
            if active_process is not None and active_process.poll() is None:
                active_process.kill()
                active_process.wait(timeout=5)
            error_payload = {
                "status": "failed",
                "ticker": payload.ticker,
                "error": str(exc),
                "duration_seconds": round(time.time() - started_at, 2),
                "command": final_command,
                "stdout_tail": tail_text("\n".join(stdout_lines)),
                "stderr_tail": tail_text("\n".join(stderr_lines)),
            }
            yield sse_event("error", error_payload)

    return StreamingResponse(
        event_stream(),
        media_type="text/event-stream",
        headers={
            "Cache-Control": "no-cache",
            "Connection": "keep-alive",
            "X-Accel-Buffering": "no",
        },
    )


@app.get("/reports-index")
def list_reports(request: Request, ticker: str | None = None) -> dict:
    normalized_filter = normalize_ticker(ticker)
    items = []
    for report_path in iter_report_files():
        entry = report_entry(request, report_path)
        if normalized_filter and normalize_ticker(entry["ticker"]) != normalized_filter:
            continue
        items.append(entry)
    return {
        "items": items,
        "count": len(items),
    }


@app.delete("/reports-entry")
def delete_report(relative_path: str) -> JSONResponse:
    try:
        target = resolve_report_target(relative_path)
    except FileNotFoundError as exc:
        return JSONResponse(status_code=404, content={"error": str(exc)})
    except ValueError as exc:
        return JSONResponse(status_code=400, content={"error": str(exc)})

    delete_root = target.parent if target.is_file() else target
    for child in sorted(delete_root.rglob("*"), reverse=True):
        if child.is_file() or child.is_symlink():
            child.unlink(missing_ok=True)
        elif child.is_dir():
            child.rmdir()
    delete_root.rmdir()
    return JSONResponse(
        status_code=200,
        content={
            "success": True,
            "deleted_path": relative_path,
            "deleted_directory": delete_root.relative_to(REPORTS_ROOT).as_posix(),
        },
    )
