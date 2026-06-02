# FinTrack UZI-Skill 集成说明

## 背景

`UZI-Skill` 上游的 Codex 安装方式是：

```bash
git clone https://github.com/wbh604/UZI-Skill.git
cd UZI-Skill
pip install -r requirements.txt
```

这套方式默认假设你直接在 `UZI-Skill` 仓库里运行 Codex，因为上游是靠仓库根目录的 `AGENTS.md` 驱动的。

但 `fintrack` 原本没有 `.codex/` 和自己的 Codex skill 目录，所以不能只做“全局安装后口头约定使用”。

## 当前落地方式

本仓库采用“项目级 Codex 外壳 + 独立 UZI 运行时”的结构：

```text
fintrack/
├── AGENTS.md
├── .codex/
│   ├── bin/
│   │   ├── setup-uzi-skill.sh
│   │   └── uzi-run.sh
│   ├── skills/
│   │   └── uzi-stock-analysis/
│   │       └── SKILL.md
│   └── vendor/
│       └── UZI-Skill/        # 本地独立 git 仓库，默认不纳入 fintrack 版本控制
```

## 这样做的原因

- `fintrack` 以后单独在 Codex 中打开时，能直接识别项目级 skill
- UZI 依赖隔离在自己的 `.venv`，不污染 `fintrack-api` / `fintrack-front`
- UZI 上游代码保持独立，可单独 `git pull`
- 不把 UZI 直接并进 `fintrack` 主业务代码，降低后续升级冲突

## 常用命令

首次安装或补装：

```bash
.codex/bin/setup-uzi-skill.sh install
```

更新上游：

```bash
.codex/bin/setup-uzi-skill.sh update
```

快速分析：

```bash
.codex/bin/uzi-run.sh 600519.SH --no-browser
```

## 注意

- `.codex/vendor/UZI-Skill` 是本地独立仓库，默认通过 `.gitignore` 忽略
- 若要升级 UZI，优先更新 vendor 仓，不直接复制粘贴上游代码进 `fintrack`
- 若后续真要把 UZI 能力产品化接入 `fintrack` 主链路，应通过适配层接入，而不是直接让 `fintrack-api` 调 UZI 内部脚本

## 生产上线

容器化上线时，前端不直接调用 `.codex` skill。

正式运行架构见：

- [2026-04-23-fintrack-uzi-container-architecture.md](./2026-04-23-fintrack-uzi-container-architecture.md)
