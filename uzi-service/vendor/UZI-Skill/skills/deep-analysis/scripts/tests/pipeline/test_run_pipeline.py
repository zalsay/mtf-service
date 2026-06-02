"""v3.0.0 Phase 6a · pipeline.run_pipeline · delegate wrapper smoke test."""
from __future__ import annotations

import sys
from datetime import date, datetime, timezone
from decimal import Decimal
from pathlib import Path

SCRIPTS = Path(__file__).resolve().parent.parent.parent
sys.path.insert(0, str(SCRIPTS))


def test_run_pipeline_exported():
    """run_pipeline 可从 lib.pipeline 顶层 import."""
    from lib.pipeline import run_pipeline
    assert callable(run_pipeline)


def test_score_from_cache_exists():
    from lib.pipeline.score import score_from_cache
    assert callable(score_from_cache)


def test_synthesize_and_render_exists():
    from lib.pipeline.synthesize import synthesize_and_render
    assert callable(synthesize_and_render)


def test_pipeline_run_has_load_and_write_cache():
    """run.py 内部 helper 存在 · 用于 resume + 落地 raw_data.json."""
    from lib.pipeline.run import _load_cache, _write_cache
    assert callable(_load_cache)
    assert callable(_write_cache)


def test_run_pipeline_signature():
    """run_pipeline(ticker, resume=True) · 签名稳定."""
    import inspect
    from lib.pipeline.run import run_pipeline
    sig = inspect.signature(run_pipeline)
    params = list(sig.parameters.keys())
    assert "ticker" in params
    assert "resume" in params


def test_sanitize_json_value_handles_non_json_scalars():
    """pipeline cache 写入前必须规约常见非 JSON 类型，避免回退 legacy."""
    from lib.pipeline.run import _sanitize_json_value

    value = {
        "trade_date": date(2026, 4, 30),
        "updated_at": datetime(2026, 4, 30, 12, 0, tzinfo=timezone.utc),
        "price": Decimal("12.34"),
        "path": Path("reports/demo.html"),
        "items": {Decimal("1.5"), date(2026, 5, 1)},
    }

    sanitized = _sanitize_json_value(value)

    assert sanitized["trade_date"] == "2026-04-30"
    assert sanitized["updated_at"] == "2026-04-30T12:00:00+00:00"
    assert sanitized["price"] == 12.34
    assert sanitized["path"] == "reports/demo.html"
    assert "2026-05-01" in sanitized["items"]
    assert 1.5 in sanitized["items"]
