FROM cc-dev:py312

ARG DEEPSEEK_TUI_VERSION=0.8.39

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PYTHONIOENCODING=utf-8 \
    UZI_RUNTIME_ROOT=/opt/uzi-skill \
    UZI_REPORTS_ROOT=/opt/uzi-skill/skills/deep-analysis/scripts/reports \
    UZI_RUNTIME_BACKEND=uzi_skill \
    UZI_NO_UPDATE_CHECK=1 \
    DEEPSEEK_TUI_BASE_URL=http://127.0.0.1:7878 \
    DEEPSEEK_TUI_DEFAULT_MODEL=deepseek-v4-pro \
    DEEPSEEK_TUI_CLI_BIN=deepseek \
    VIRTUAL_ENV=/opt/uzi-venv \
    PATH=/opt/uzi-venv/bin:$PATH

WORKDIR /srv/uzi-service

COPY requirements.txt /srv/uzi-service/requirements.txt
COPY vendor/UZI-Skill/requirements.txt /tmp/uzi-requirements.txt

RUN python3 -m venv /opt/uzi-venv \
    && /opt/uzi-venv/bin/pip install --no-cache-dir -r /srv/uzi-service/requirements.txt \
    && /opt/uzi-venv/bin/pip install --no-cache-dir -r /tmp/uzi-requirements.txt \
    && npm install -g "deepseek-tui@${DEEPSEEK_TUI_VERSION}"

COPY vendor/UZI-Skill /opt/uzi-skill
COPY app /srv/uzi-service/app

EXPOSE 9011

CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "9011"]
