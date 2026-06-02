from __future__ import annotations

import importlib.util
import json
import os
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path

import pytest


APP_MAIN = Path(__file__).resolve().parents[1] / "app" / "main.py"


def load_main():
    spec = importlib.util.spec_from_file_location("uzi_app_main", APP_MAIN)
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def test_chat_completions_url_normalizes_deepseek_base_url():
    main = load_main()

    assert (
        main.chat_completions_url("https://api.deepseek.com")
        == "https://api.deepseek.com/v1/chat/completions"
    )
    assert (
        main.chat_completions_url("https://api.deepseek.com/v1")
        == "https://api.deepseek.com/v1/chat/completions"
    )
    assert (
        main.chat_completions_url("https://example.com/openai/chat/completions")
        == "https://example.com/openai/chat/completions"
    )


def test_parse_agent_analysis_content_extracts_fenced_json():
    main = load_main()

    parsed = main.parse_agent_analysis_content(
        """
        ```json
        {
          "agent_reviewed": true,
          "dim_commentary": {"0_basic": "基础面稳定"},
          "panel_insights": "价值派与趋势派分歧明显",
          "great_divide_override": {"punchline": "分歧来自估值"},
          "narrative_override": {"core_conclusion": "观望"}
        }
        ```
        """
    )

    assert parsed["agent_reviewed"] is True
    assert parsed["dim_commentary"]["0_basic"] == "基础面稳定"
    assert parsed["panel_insights"] == "价值派与趋势派分歧明显"


def test_build_stage_command_uses_direct_stage_calls():
    main = load_main()

    stage1 = main.build_stage_command("stage1", "000001.SZ")
    stage2 = main.build_stage_command("stage2", "000001.SZ")

    assert stage1[:2] == [main.UZI_PYTHON_BIN, "-c"]
    assert "from run_real_test import stage1" in stage1[2]
    assert "stage1(\"000001.SZ\")" in stage1[2]
    assert "from run_real_test import stage2" in stage2[2]
    assert "stage2(\"000001.SZ\")" in stage2[2]


def test_extract_html_document_unwraps_fenced_html():
    main = load_main()

    html = main.extract_html_document(
        """
        ```html
        <!doctype html>
        <html><body><h1>测试研报</h1></body></html>
        ```
        """
    )

    assert html.startswith("<!doctype html>")
    assert "<h1>测试研报</h1>" in html
    assert "```" not in html


def test_extract_html_document_rejects_incomplete_html():
    main = load_main()

    with pytest.raises(RuntimeError, match="missing </html>"):
        main.extract_html_document("<!doctype html><html><head></head><body><h1>半截")


def test_write_deepseek_tui_report_keeps_existing_file_on_incomplete_html(tmp_path, monkeypatch):
    reports_root = tmp_path / "reports"
    report_dir = reports_root / "600246.SH_20260505_medium"
    report_dir.mkdir(parents=True)
    report_path = report_dir / "full-report-standalone.html"
    report_path.write_text(
        "<!doctype html><html><head><title>old</title></head><body>old report</body></html>",
        encoding="utf-8",
    )
    monkeypatch.setenv("UZI_REPORTS_ROOT", str(reports_root))
    main = load_main()
    payload = main.AnalyzeRequest(ticker="600246.SH")

    with pytest.raises(RuntimeError, match="missing </html>"):
        main.write_deepseek_tui_report(payload, "<!doctype html><html><body>broken")

    assert report_path.read_text(encoding="utf-8").endswith("</html>")
    assert "old report" in report_path.read_text(encoding="utf-8")


def test_build_deepseek_tui_result_retries_incomplete_html(tmp_path, monkeypatch):
    reports_root = tmp_path / "reports"
    monkeypatch.setenv("UZI_REPORTS_ROOT", str(reports_root))
    monkeypatch.setenv("UZI_DEEPSEEK_TUI_REPORT_RETRIES", "1")
    main = load_main()
    payload = main.AnalyzeRequest(ticker="601166.SH", depth="lite")
    retry_calls = []

    def fake_call_deepseek_tui(payload_arg, retry_reason=None):
        retry_calls.append(retry_reason)
        return {
            "output": "<!doctype html><html><head><title>ok</title></head><body><h1>兴业银行</h1></body></html>",
            "model": "deepseek-v4-pro",
            "command": ["deepseek", "<retry>"],
        }

    class Request:
        base_url = "http://127.0.0.1:59011/"

    monkeypatch.setattr(main, "call_deepseek_tui", fake_call_deepseek_tui)
    result = main.build_deepseek_tui_result(
        request=Request(),
        payload=payload,
        started_at=0,
        prompt_response={
            "output": "```html\n<!doctype html><html><head><style>.broken {",
            "model": "deepseek-v4-pro",
            "command": ["deepseek", "<initial>"],
        },
    )

    assert result["status"] == "succeeded"
    assert result["success"] is True
    assert retry_calls == ["DeepSeek-TUI returned incomplete HTML: missing </html>"]
    assert result["report_relative_path"].endswith("_lite/full-report-standalone.html")
    assert "兴业银行" in Path(result["report_path"]).read_text(encoding="utf-8")


def test_build_deepseek_tui_result_accepts_complete_html_without_size_retry(tmp_path, monkeypatch):
    reports_root = tmp_path / "reports"
    monkeypatch.setenv("UZI_REPORTS_ROOT", str(reports_root))
    monkeypatch.setenv("UZI_DEEPSEEK_TUI_REPORT_RETRIES", "1")
    main = load_main()
    payload = main.AnalyzeRequest(ticker="600186.SH", depth="lite")
    short_html = (
        "<!doctype html><html><head><title>莲花控股</title></head>"
        "<body><main><h1>莲花控股研报</h1><section>结构完整但内容较短。</section></main></body></html>"
    )
    retry_calls = []

    def fake_call_deepseek_tui(payload_arg, retry_reason=None):
        retry_calls.append(retry_reason)
        return {
            "output": short_html,
            "model": "deepseek-v4-pro",
            "command": ["deepseek", "<retry>"],
        }

    class Request:
        base_url = "http://127.0.0.1:59011/"

    monkeypatch.setattr(main, "call_deepseek_tui", fake_call_deepseek_tui)
    result = main.build_deepseek_tui_result(
        request=Request(),
        payload=payload,
        started_at=0,
        prompt_response={
            "output": short_html,
            "model": "deepseek-v4-pro",
            "command": ["deepseek", "<initial>"],
        },
    )

    assert result["status"] == "succeeded"
    assert retry_calls == []
    assert result["report_size_bytes"] < 8000
    assert "莲花控股研报" in Path(result["report_path"]).read_text(encoding="utf-8")


def test_build_deepseek_tui_prompt_has_no_byte_limit(monkeypatch):
    main = load_main()
    monkeypatch.setattr(main, "fetch_market_history_context", lambda payload: None)
    monkeypatch.setattr(main, "load_cached_market_context", lambda payload: None)

    prompt = main.build_deepseek_tui_prompt(
        main.AnalyzeRequest(ticker="600186.SH", depth="lite"),
        retry_reason="DeepSeek-TUI returned incomplete HTML: missing </html>",
    )

    assert "字节" not in prompt
    assert "总输出控制" not in prompt
    assert "最后一行必须是 </html>" in prompt


def test_parse_sse_events_collects_message_delta():
    main = load_main()

    events = main.parse_sse_events(
        """
event: turn.started
data: {"thread_id":"thr_1"}

event: message.delta
data: {"content":"<!doctype html>"}

event: message.delta
data: {"content":"<html></html>"}

event: done
data: {}

"""
    )

    assert events[0][0] == "turn.started"
    assert "".join(event[1].get("content", "") for event in events) == "<!doctype html><html></html>"


def test_fetch_market_history_context_sends_bearer_token(tmp_path, monkeypatch):
    class Handler(BaseHTTPRequestHandler):
        auth_header = ""

        def do_POST(self):
            Handler.auth_header = self.headers.get("Authorization", "")
            length = int(self.headers.get("Content-Length", "0"))
            self.rfile.read(length)
            body = json.dumps(
                {
                    "provider": "test",
                    "data": [
                        {
                            "trade_date": "20260515",
                            "close": 10.0,
                            "high": 10.5,
                            "low": 9.8,
                            "amount": 1000000,
                        },
                        {
                            "trade_date": "20260518",
                            "close": 10.5,
                            "high": 10.8,
                            "low": 10.1,
                            "amount": 1200000,
                        },
                    ],
                }
            ).encode("utf-8")
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def log_message(self, format, *args):
            return

    server = HTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    monkeypatch.setenv("HISTORY_SERVICE_URL", f"http://127.0.0.1:{server.server_port}")
    monkeypatch.setenv("HISTORY_SERVICE_TOKEN", "fintrack-dev-token")
    main = load_main()

    try:
        context = main.fetch_market_history_context(main.AnalyzeRequest(ticker="601166.SH"))
    finally:
        server.shutdown()

    assert Handler.auth_header == "Bearer fintrack-dev-token"
    assert context and context["latest_price"] == 10.5


def test_deepseek_tui_stream_and_report_write(tmp_path, monkeypatch):
    reports_root = tmp_path / "reports"

    class Handler(BaseHTTPRequestHandler):
        seen_path = ""

        def do_POST(self):
            Handler.seen_path = self.path
            length = int(self.headers.get("Content-Length", "0"))
            self.rfile.read(length)
            body = (
                'event: message.delta\n'
                'data: {"content":"<!doctype html><html><head><title>test</title></head><body>"}\n\n'
                'event: message.delta\n'
                'data: {"content":"<h1>000001.SZ 研报</h1></body></html>"}\n\n'
                'event: done\n'
                'data: {}\n\n'
            ).encode("utf-8")
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def log_message(self, format, *args):
            return

    server = HTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    monkeypatch.setenv("UZI_REPORTS_ROOT", str(reports_root))
    monkeypatch.setenv("DEEPSEEK_TUI_BASE_URL", f"http://127.0.0.1:{server.server_port}")
    main = load_main()
    payload = main.AnalyzeRequest(ticker="000001.SZ", depth="deep")

    try:
        response = main.call_deepseek_tui_stream(payload)
        report_path = main.write_deepseek_tui_report(payload, response["output"])
    finally:
        server.shutdown()

    relative_path = report_path.relative_to(reports_root).as_posix()
    date_tag = report_path.parent.name.split("_")[1]
    assert Handler.seen_path == "/v1/stream"
    assert relative_path == f"000001.SZ_{date_tag}_deep/full-report-standalone.html"
    assert "<h1>000001.SZ 研报</h1>" in report_path.read_text(encoding="utf-8")


def test_report_entry_parses_depth_specific_directory(tmp_path, monkeypatch):
    reports_root = tmp_path / "reports"
    report_dir = reports_root / "000001.SZ_20260505_lite"
    report_dir.mkdir(parents=True)
    report_path = report_dir / "full-report-standalone.html"
    report_path.write_text("<!doctype html><html><body>lite</body></html>", encoding="utf-8")
    monkeypatch.setenv("UZI_REPORTS_ROOT", str(reports_root))
    main = load_main()

    class Request:
        base_url = "http://127.0.0.1:59011/"

    entry = main.report_entry(Request(), report_path)

    assert entry["ticker"] == "000001.SZ"
    assert entry["date_tag"] == "20260505"
    assert entry["depth"] == "lite"
    assert entry["directory_name"] == "000001.SZ_20260505_lite"


def test_ensure_depth_report_path_copies_report_directory_assets(tmp_path, monkeypatch):
    reports_root = tmp_path / "reports"
    report_dir = reports_root / "000001.SZ_20260505"
    assets_dir = report_dir / "assets"
    assets_dir.mkdir(parents=True)
    report_path = report_dir / "full-report-standalone.html"
    report_path.write_text("<!doctype html><html><body><img src='assets/a.png'></body></html>", encoding="utf-8")
    (assets_dir / "a.png").write_bytes(b"png")
    monkeypatch.setenv("UZI_REPORTS_ROOT", str(reports_root))
    main = load_main()
    payload = main.AnalyzeRequest(ticker="000001.SZ", depth="lite")

    copied = main.ensure_depth_report_path(payload, report_path)

    assert copied.relative_to(reports_root).as_posix() == "000001.SZ_20260505_lite/full-report-standalone.html"
    assert (reports_root / "000001.SZ_20260505_lite/assets/a.png").read_bytes() == b"png"


def test_deepseek_tui_cli_uses_ai_model_env_without_exposing_key(tmp_path, monkeypatch):
    fake_cli = tmp_path / "deepseek"
    env_capture = tmp_path / "env.json"
    fake_cli.write_text(
        """#!/usr/bin/env python3
import json
import os
import sys

capture_path = os.environ["DEEPSEEK_TUI_TEST_ENV_CAPTURE"]
with open(capture_path, "w", encoding="utf-8") as fh:
    json.dump(
        {
            "argv": sys.argv,
            "api_key": os.environ.get("DEEPSEEK_API_KEY"),
            "base_url": os.environ.get("DEEPSEEK_BASE_URL"),
        },
        fh,
        ensure_ascii=False,
    )
print("<!doctype html><html><body><h1>CLI 研报</h1></body></html>")
""",
        encoding="utf-8",
    )
    fake_cli.chmod(fake_cli.stat().st_mode | 0o111)

    monkeypatch.setenv("DEEPSEEK_TUI_CLI_BIN", str(fake_cli))
    monkeypatch.setenv("DEEPSEEK_TUI_TEST_ENV_CAPTURE", str(env_capture))
    main = load_main()
    payload = main.AnalyzeRequest(
        ticker="000001.SZ",
        ai_model=main.AIModelConfig(
            provider_name="DeepSeek",
            base_url="https://api.deepseek.com/v1",
            api_key="sk-secret-from-user-settings",
            model_id="deepseek-v4-pro",
        ),
    )

    result = main.call_deepseek_tui_cli(payload)
    captured = json.loads(env_capture.read_text(encoding="utf-8"))

    assert "<h1>CLI 研报</h1>" in result["output"]
    assert captured["api_key"] == "sk-secret-from-user-settings"
    assert captured["base_url"] == "https://api.deepseek.com/v1"
    assert "--model" in captured["argv"]
    assert "deepseek-v4-pro" in captured["argv"]
    assert "sk-secret-from-user-settings" not in captured["argv"]
    assert "sk-secret-from-user-settings" not in result["command"]


def test_generate_agent_analysis_calls_chat_api_and_writes_cache(tmp_path, monkeypatch):
    runtime_root = tmp_path / "uzi"
    scripts_root = runtime_root / "skills" / "deep-analysis" / "scripts"
    cache_root = scripts_root / ".cache" / "000001.SZ"
    cache_root.mkdir(parents=True)
    for name in ("raw_data", "dimensions", "panel"):
        (cache_root / f"{name}.json").write_text(
            json.dumps({"ticker": "000001.SZ", "name": name}, ensure_ascii=False),
            encoding="utf-8",
        )

    class Handler(BaseHTTPRequestHandler):
        seen_path = ""

        def do_POST(self):
            Handler.seen_path = self.path
            length = int(self.headers.get("Content-Length", "0"))
            self.rfile.read(length)
            content = {
                "choices": [
                    {
                        "message": {
                            "content": json.dumps(
                                {
                                    "agent_reviewed": True,
                                    "dim_commentary": {"0_basic": "公司基础信息稳定，业务结构清晰。"},
                                    "panel_insights": "评委观点主要围绕估值与短线催化分歧。",
                                    "great_divide_override": {"punchline": "估值与催化形成主要分歧"},
                                    "narrative_override": {"core_conclusion": "维持谨慎跟踪。"},
                                },
                                ensure_ascii=False,
                            )
                        }
                    }
                ]
            }
            body = json.dumps(content).encode("utf-8")
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def log_message(self, format, *args):
            return

    server = HTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    monkeypatch.setenv("UZI_RUNTIME_ROOT", str(runtime_root))
    main = load_main()
    payload = main.AnalyzeRequest(
        ticker="000001.SZ",
        ai_model=main.AIModelConfig(
            provider_name="DeepSeek",
            base_url=f"http://127.0.0.1:{server.server_port}",
            api_key="test-key",
            model_id="deepseek-v4-pro",
        ),
    )

    try:
        result = main.generate_agent_analysis(payload, main.build_analyze_env(payload))
    finally:
        server.shutdown()

    written = json.loads((cache_root / "agent_analysis.json").read_text(encoding="utf-8"))
    assert Handler.seen_path == "/v1/chat/completions"
    assert result["agent_reviewed"] is True
    assert written["panel_insights"] == "评委观点主要围绕估值与短线催化分歧。"
