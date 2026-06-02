# MTF 智能体设计

日期：2026-05-19

## 背景

FinTrack 已有用户 AI 模型配置、UZI 研报生成、watchlist、预测与组合相关能力，但现有 `/api/v1/llm/chat` 只是无状态 OpenAI-compatible 转发，只保留最近若干轮消息，不具备 FinTrack 侧会话绑定、DeepSeek-TUI thread 集成或长期偏好记忆。

本设计新增全局 AI 对话功能，名称为「MTF 智能体（MTF Agent）」。目标是围绕 FinTrack 数据提供投资辅助问答，通过 DeepSeek-TUI runtime 实现多轮对话，通过 FinTrack 后端治理用户权限、数据上下文与长期偏好记忆。

## 目标

- 登录后提供全局固定入口，点击后以右侧抽屉打开 MTF Agent。
- 第一版面向 FinTrack 数据投资助手，而不是通用闲聊机器人。
- 通过 DeepSeek-TUI thread 承载多轮上下文。
- 每个用户默认一个长期会话，并提供「重置会话」能力。
- 长期记忆只保存稳定偏好，不保存完整聊天原文，也不默认保存每轮对话摘要。
- FinTrack 前端不做完整历史会话列表。
- DeepSeek-TUI runtime 不直接暴露给前端。

## 非目标

- 不做多会话列表、会话搜索或完整聊天归档。
- 不把完整对话自动总结进长期记忆。
- 不让前端直接调用 DeepSeek-TUI runtime。
- 第一版不开放任意数据库查询或任意工具执行。
- 第一版不承诺交易建议，只提供投资研究辅助。

## 产品体验

登录后页面右下显示固定按钮。按钮打开一个右侧滑入抽屉，标题为「MTF 智能体」，英文环境显示「MTF Agent」。桌面端抽屉宽度约 420-520px；移动端使用全屏或接近全屏的抽屉，并避开底部导航。

抽屉包含：

- 顶部：标题、模型状态、关闭按钮。
- 中部：当前前端会话消息流。
- 底部：输入框、发送按钮、停止或重试状态。
- 更多菜单：重置会话、查看长期偏好、清空长期偏好。

关闭抽屉不结束会话。刷新页面后，前端可以不恢复完整消息流，但后端仍继续绑定同一个 DeepSeek-TUI thread。重置会话会创建新的 DeepSeek-TUI thread，并更新当前用户默认绑定。

## 架构

采用方案：FinTrack API 作为薄代理 + DeepSeek-TUI Thread Runtime。

数据流：

```text
fintrack-front MTFAgentDrawer
  -> fintrack-api MTFAgentHandler
  -> fintrack-api MTFAgentService
  -> ai-functions-gateway /deepseek-tui/* token proxy
  -> DeepSeek-TUI runtime API
```

职责划分：

- `fintrack-front`：抽屉 UI、本地消息流、发送/重试/重置/查看偏好交互。
- `fintrack-api`：鉴权、用户 AI 配置读取、会话绑定、长期偏好记忆、FinTrack 数据上下文组装、通过 gateway token proxy 调用 DeepSeek-TUI runtime。
- `ai-functions-gateway`：对 `/deepseek-tui/*` 做基础 token 鉴权并反向代理到内部 DeepSeek-TUI runtime。
- DeepSeek-TUI：thread、多轮上下文、turn/event 生命周期、SSE、manual compact / configured compaction。

DeepSeek-TUI HTTP runtime 是 Rust 架构，由 `deepseek serve --http --host 0.0.0.0 --port 7878 --insecure` 在 `ai-functions-deepseek-tui` 容器内启动。该容器不映射宿主机端口，只在 Docker 网络内暴露 `http://ai-functions-deepseek-tui:7878`。FinTrack 后端通过 `http://ai-functions-gateway:9010/deepseek-tui` 访问，携带 `Authorization: Bearer <MTF_AGENT_RUNTIME_TOKEN>`；gateway 校验 token 后转发到 runtime。

## 后端接口

新增鉴权路由组：`/api/v1/mtf-agent`。

- `GET /session`
  - 返回当前用户默认会话状态、模型、runtime 状态、memory 状态。
- `POST /messages`
  - 发送用户消息。
  - 后端确保默认 DeepSeek-TUI thread 存在。
  - 后端动态生成 FinTrack 上下文包与长期偏好 block。
  - 第一版可先返回完整响应；契约验证通过后再升级为流式响应。
- `POST /reset`
  - 创建并绑定新的默认 DeepSeek-TUI thread。
  - 不清理旧 thread，仅从 FinTrack 默认绑定中移除。
- `GET /memory`
  - 返回当前用户稳定偏好。
- `DELETE /memory`
  - 清空当前用户稳定偏好。

## 数据模型

FinTrack 只保存薄状态。

`mtf_agent_sessions`：

- `id`
- `user_id`
- `deepseek_thread_id`
- `model_id`
- `last_used_at`
- `created_at`
- `updated_at`

约束：`user_id` 唯一，默认每个用户只有一个 active session。

`mtf_agent_memories`：

- `id`
- `user_id`
- `memory_type`
- `content`
- `source`
- `confidence`
- `created_at`
- `updated_at`

第一版 `memory_type` 可覆盖：

- `investment_preference`
- `risk_preference`
- `market_focus`
- `output_style`
- `constraint`

## 长期记忆策略

长期记忆只保存稳定偏好，例如：

- 偏好低波动、现金流稳定公司。
- 重点关注 A 股半导体和港股互联网。
- 回答先给结论，再列风险。
- 不希望推荐高杠杆或高波动标的。

不进入长期记忆的内容：

- 完整聊天原文。
- 每轮对话摘要。
- 实时行情、预测、研报索引等会过期的数据。
- API key、token、密码或其他敏感信息。

第一版以显式保存为主：用户表达「记住」「以后都按这个偏好」等稳定偏好时才写入。后续可加入轻量自动提炼，但自动提炼需要先展示确认，而不是静默写入。

每次发起对话时，后端将记忆渲染为短 block，并作为上下文的一部分发送给 DeepSeek-TUI。

## FinTrack 上下文包

每轮请求由 `MTFAgentService` 动态生成 compact context，不由前端传入。

第一版上下文包含：

- 用户 watchlist 摘要。
- 最近预测结果摘要。
- 最近 UZI 研报索引与可打开链接。

第二批可加入：

- portfolio 摘要。
- strategy 摘要。
- 回测或预测验证摘要。

上下文包需要长度限制，优先保留最新、最相关和用户明确提到的标的。数据上下文不写入长期记忆，避免过期数据污染 memory。

## DeepSeek-TUI 集成

FinTrack 后端调用 gateway 代理下的 DeepSeek-TUI runtime API：

- 创建 thread：`POST /v1/threads`
- 更新 thread 元数据或 system prompt：`PATCH /v1/threads/{id}`
- 发起 turn：`POST /v1/threads/{id}/turns`
- 获取事件：`GET /v1/threads/{id}/events?since_seq=...`
- 手动 compact：`POST /v1/threads/{id}/compact`

部署约定：

- `MTF_AGENT_RUNTIME_URL=http://ai-functions-gateway:9010/deepseek-tui`
- `MTF_AGENT_RUNTIME_TOKEN=<DEEPSEEK_TUI_PROXY_TOKEN>`
- gateway 接受 `Authorization: Bearer <token>`、`X-API-Key` 或 `X-Gateway-API-Token`，转发前移除这些鉴权头。

实现前必须做契约验证：

- `POST /v1/threads/{id}/turns` 的响应和事件字段。
- `PATCH /v1/threads/{id}` 是否满足更新 system prompt / title / model 的需求。
- runtime 是否支持服务端统一模型配置。
- gateway proxy token 是否正确拦截未授权访问。
- SSE 事件如何映射为前端可显示的 assistant delta。

若 per-request API key 不可用，第一版采用 DeepSeek-TUI runtime 的服务端统一模型配置。用户个人 AI key 继续用于现有 UZI CLI one-shot 路径，不混入 MTF Agent runtime。

## 错误处理

- 用户未配置 AI 模型：抽屉提示去设置页配置。
- DeepSeek-TUI runtime 不可用：提示 `MTF Agent 暂不可用`，后端记录日志。
- 发送超时：保留用户输入，允许重试。
- 重置失败：不清空当前本地消息。
- 上下文生成失败：降级为只带长期偏好和用户原始问题，并记录后端错误。

## 安全与隐私

- DeepSeek-TUI runtime 只允许 Docker 内部网络访问，不映射宿主机端口，不暴露给浏览器。
- 所有 FinTrack 到 runtime 的调用必须经过 `ai-functions-gateway` 的 `/deepseek-tui/*` token proxy。
- 前端永远不接触用户 API key。
- 后端不把完整聊天历史保存到 FinTrack 数据库。
- 长期偏好支持查看和清空。
- 禁止将 secrets 写入长期记忆。
- MTF Agent 输出必须保持投资辅助定位，不承诺收益，不替代用户决策。

## 验证策略

后端：

- session 创建与复用。
- reset 后 thread 绑定更新。
- memory 查询和清空。
- 上下文包生成在空 watchlist、无研报、无预测时可降级。
- DeepSeek-TUI runtime 不可用时返回可理解错误。

前端：

- 固定按钮在桌面和移动端不遮挡关键导航。
- 抽屉打开、关闭、发送、重试、重置状态正确。
- 未配置模型和 runtime 不可用时展示可操作提示。

集成：

- 通过 `docker compose up -d --build ai-functions-deepseek-tui ai-functions-gateway` 启动 runtime 与 gateway。
- 验证无 token 请求 `GET /deepseek-tui/health` 返回 `401`。
- 验证带 token 请求可完成 create thread、send turn、read events 的 smoke test。
- 验证长期偏好会进入 prompt 上下文，但不会污染 FinTrack 数据上下文。

## 实施顺序

1. DeepSeek-TUI runtime API 契约验证。
2. 新增后端配置、模型、数据库表和 service。
3. 新增 `mtf-agent` API。
4. 新增前端 API client、固定按钮和抽屉组件。
5. 接入基础上下文包：watchlist、预测摘要、UZI 研报索引。
6. 接入长期偏好查看和清空。
7. 做端到端 smoke test 与 UI 响应式验证。
