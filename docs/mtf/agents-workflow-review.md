# MTF AGENTS.md 执行评审记录

生成日期：2026-06-07

## 评审目标

测试根目录 `AGENTS.md` 是否足以指导“用户侧 MTF ETF 研究 agent”执行任务，并记录执行风险与文档问题。

测试场景：

> 从热门 ETF 中筛选 3 只适合 7 日 MTF 预测的标的，优先复用缓存，必要时说明下一步 API。

## 子代理执行记录

按用户要求启动子代理进行只读评审，均要求不修改文件。

| 子代理 | 任务 | 结果 |
| --- | --- | --- |
| `019ea148-3609-7ec0-9887-03d3adcb68b4` | 用户 ETF 研究 agent 纸面推演 | 失败：503 Service Unavailable |
| `019ea148-3675-7fe1-9522-f5f95ae3875f` | 文档 QA 与一致性检查 | 失败：503 Service Unavailable |
| `019ea149-2704-7760-aa73-05c2837b4f1a` | 合并版只读评审重试 | 失败：503 Service Unavailable |

结论：子代理服务连续三次返回 503，未产生可用评审内容。本记录中的问题清单来自主会话本地只读复核。

## 按 AGENTS.md 的纸面执行步骤

1. 读取 `AGENTS.md`，确认用户要求属于 MTF ETF 研究任务。
2. 读取 `skills/mtf-etf-a-share-assistant/SKILL.md` 和 Open API 合约，确认接口和输出结构。
3. 使用 `GET /api/open/v2/etf/hot` 获取热门 ETF 候选。
4. 使用 `POST /api/open/v1/etf/quotes` 补充行情。
5. 使用 `GET /api/open/v1/mtf/best?stock_type=2&include_validation=true` 查询已有 best 与验证分块。
6. 对缺失或过期标的，优先考虑 `POST /api/open/v1/mtf/predict-once` 且 `prefer_cache=true`。
7. 给出 3 只候选的证据表、风险、数据缺口和下一步 API/payload。

## 问题清单

### High

1. **多用户 header 名称不一致**
   - 位置：`AGENTS.md` 的 Open API 鉴权示例使用 `X-MTF-User`。
   - 对照：`docs/mtf/fintrack-open-api-contract.md` 使用 `X-FinTrack-User`。
   - 影响：外部 agent 可能按 `AGENTS.md` 传错 header，导致多用户代理或用户映射不生效。
   - 建议：统一为实际后端支持的 header；若当前后端尚未实现 header 映射，则在用户 agent 文档中删除该 header 或标注“暂不可用”。

2. **`predict-once` 的副作用边界不够明确**
   - 位置：`AGENTS.md` 标准流程第 5 步。
   - 对照：Open API 合约说明 `prefer_cache=true` 时先查缓存，缓存 miss 后会触发 `/predict_once`。
   - 影响：用户要求“优先复用缓存”时，agent 可能在缓存 miss 后直接触发新计算，产生耗时、配额或费用风险。
   - 建议：明确区分“只查缓存/纸面建议”和“允许触发计算”。缓存 miss 后应先向用户说明并请求确认，除非用户已明确授权触发预测。

3. **凭证获取流程可能引导用户 agent 处理账号密码**
   - 位置：`AGENTS.md` 的 `get_open_api_key.sh` 示例。
   - 影响：用户侧 agent 可能主动要求或处理 `MTF_API_USERNAME`/`MTF_API_PASSWORD`，不适合作为默认工作流。
   - 建议：默认要求使用已有 `FINTRACK_OPEN_API_KEY` 或用户已配置的 `.env.open-api`；只有用户明确要求创建 key 时，才提示凭证输入流程。

### Medium

4. **生产 base URL 默认调用风险**
   - 位置：`AGENTS.md` Open API 使用规范。
   - 影响：评审、演示或测试型任务可能误调用生产接口，尤其是预测、回测、策略保存、自选股写入等动作。
   - 建议：加入“纸面推演/测试时不得调用远端；写入和计算类接口必须明确授权”的用户 agent 规则。

5. **失败场景处理不够具体**
   - 位置：`AGENTS.md` 分析与输出要求。
   - 缺口：未明确无 API key、401/403、scope 不足、rate limit、upstream unavailable、cache miss、job queued 等情况的用户回复方式。
   - 影响：agent 可能只报告“失败”，而没有给出下一步可执行动作。
   - 建议：补充失败处理模板：说明错误码、是否可重试、需要的 scope 或用户动作、可替代的只读分析路径。

6. **`watchlist` 写入动作缺少显式确认门槛**
   - 位置：`AGENTS.md` 标准流程第 6 步。
   - 影响：`POST /watchlist`、`POST /watchlist/bind-strategy`、`POST /strategy/params` 都是写入动作，用户 agent 不应在“分析”任务中自动执行。
   - 建议：明确写入类动作只作为“下一步 API”输出，除非用户明确要求执行。

7. **AGENTS.md 与 skill 重复较多，存在漂移风险**
   - 位置：Open API base URL、脚本命令、ETF 工作流在 `AGENTS.md` 和 `skills/mtf-etf-a-share-assistant/SKILL.md` 中均有描述。
   - 影响：后续只改一处会造成接口或脚本说明不一致。
   - 建议：`AGENTS.md` 保留高层流程和安全边界；接口细节、脚本命令和 OpenAPI 片段以 skill 为准。

### Low

8. **最低 scopes 未在 AGENTS.md 中集中列出**
   - 位置：`AGENTS.md` 只要求不绕过 scopes，但没有列出常用 scopes。
   - 影响：用户 agent 需要跳转 skill 或合约文档才能判断缺少哪个 scope。
   - 建议：可添加极简映射：`etf:read`、`mtf:read`、`mtf:predict`、`mtf:backtest`、`strategy:write`、`watchlist:write`。

9. **“会员等级”仍出现在目标澄清中**
   - 位置：`AGENTS.md` 标准流程第 1 步。
   - 影响：这不是开发性内容，但用户 agent 可能不知道如何获取会员等级。
   - 建议：改为“权限/额度约束”，避免暗示 agent 需要查询或管理会员体系。

10. **内部接口禁止项仍出现 `/save-predictions/*`**
   - 位置：`AGENTS.md` 禁止事项。
   - 影响：这是安全提醒，但对纯用户 agent 略偏内部实现。
   - 建议：可以保留，或改成更用户侧的表述：“不要调用未列在 Open API 中的内部写入接口”。

## 总体结论

`AGENTS.md` 已能指导用户 agent 完成 ETF 研究的主路径，但还需要收紧三类边界：

1. 统一 Open API 用户映射 header。
2. 对计算/写入类接口增加显式确认门槛。
3. 将凭证创建、生产调用和失败处理从“默认动作”改为“条件动作/下一步建议”。
