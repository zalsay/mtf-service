FROM docker.xuanyuan.run/ubuntu:24.04

ARG DEEPSEEK_TUI_VERSION=0.8.39

ENV DEBIAN_FRONTEND=noninteractive \
    NODE_ENV=production \
    DEEPSEEK_TUI_HOST=0.0.0.0 \
    DEEPSEEK_TUI_PORT=7878 \
    DEEPSEEK_TUI_DEFAULT_MODEL=deepseek-v4-pro \
    DEEPSEEK_TUI_CLI_BIN=deepseek \
    DEEPSEEK_TUI_CLI_TIMEOUT_SECONDS=900

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl libdbus-1-3 nodejs npm tini \
    && rm -rf /var/lib/apt/lists/* \
    && npm config set registry https://registry.npmmirror.com \
    && npm install -g "deepseek-tui@${DEEPSEEK_TUI_VERSION}" \
    && npm cache clean --force

RUN mkdir -p /root/.config/deepseek-tui /root/.deepseek-tui /root/.deepseek

COPY deepseek_tui_runtime.js /opt/deepseek_tui_runtime.js
COPY deepseek_tui_runtime_lib.js /opt/deepseek_tui_runtime_lib.js

EXPOSE 7878

ENTRYPOINT ["tini", "--"]
CMD ["node", "/opt/deepseek_tui_runtime.js"]
