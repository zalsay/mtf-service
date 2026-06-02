# daily_stock_analysis 与 FinTrack 集成设计

## 目标

将 `fintrack` 作为一个可嵌入的功能域接入 `daily_stock_analysis`，同时满足以下约束：

- `fintrack` 保持独立 Git 仓库，可单独 fork、单独更新
- `fintrack` 保持独立数据库，不与宿主共享用户表和业务表
- `daily_stock_analysis` 与 `fintrack` 保持独立登录、独立会话
- 集成改动尽量小，不重写 `fintrack` 现有前后端主流程
- 在 `fintrack` 内保存 `daily_stock_analysis` 的 `user_id` 作为关联字段
- 用户从宿主进入 `fintrack` 时，在首次进入阶段自动建立关联

## 非目标

- 不合并两个系统的数据库
- 不实现单点登录
- 不将 `fintrack` 的源码直接揉入宿主仓库主代码
- 不重写 `fintrack` 的主前端页面为 `daily_stock_analysis` 原生页面
- 不要求 `daily_stock_analysis` 反向驱动 `fintrack` 的核心业务逻辑

## 方案结论

采用 `mixed mode + git submodule`：

- `daily_stock_analysis` 作为宿主平台
- `fintrack` 以独立 Git 仓库形式通过 `git submodule` 接入
- 宿主侧新增 `FinTrack` 入口、少量摘要卡片和跳转桥接
- `fintrack` 继续保有自己的前端、Go API、TimesFM 服务和数据库
- 用户从宿主跳转进入 `fintrack` 时，使用短时效签名 token 携带 `daily_stock_analysis_user_id`
- `fintrack` 在首次进入时自动建立 `daily_stock_analysis_user_id` 关联

这是在“可持续更新”和“改动最小”之间最稳的平衡点。

## 方案对比

### 方案 A：Submodule + FinTrack 主表增加宿主用户字段

做法：

- 在宿主仓库中通过 `git submodule` 挂载 `fintrack`
- 在 `fintrack.users` 增加 `daily_stock_analysis_user_id`
- 首次进入时自动绑定宿主用户 ID

优点：

- Git 边界最清晰
- 后续跟踪 `fintrack` 上游更新最容易
- 数据库独立
- 代码改动最少

缺点：

- 关联模型默认只面向一个宿主系统

### 方案 B：Submodule + 独立账号映射表

做法：

- 保持 submodule
- 在 `fintrack` 里额外新增 `external_account_links`

优点：

- 模型扩展性更强

缺点：

- 比当前需求多一层抽象
- 不符合“最小改动优先”

### 方案 C：宿主重写 FinTrack 功能页，只复用后端能力

优点：

- 宿主体验最统一

缺点：

- 与 `fintrack` 上游更新强耦合
- 长期维护成本最高

最终采用方案 A。

## 仓库与代码边界

### 宿主仓库 `daily_stock_analysis`

负责：

- 导航入口
- 宿主首页摘要卡片
- 跳转桥接
- 可选的反向代理配置
- 展示集成状态与错误提示

不负责：

- `fintrack` 核心业务页逻辑
- `fintrack` 用户体系
- `fintrack` 的数据库和模型服务

### 子应用仓库 `fintrack`

负责：

- 自身前端页面
- 自身 Go API
- 自身 TimesFM 推理与回测服务
- 自身数据库
- 宿主用户关联字段落库与校验

## Git 集成方式

宿主仓库以 `git submodule` 引入 `fintrack`，目录建议：

```text
daily_stock_analysis/
  external/
    fintrack/   # git submodule
```

推荐远端模型：

### `daily_stock_analysis`

- `origin`: 你的 fork
- `upstream`: `ZhuLinsen/daily_stock_analysis`

### `fintrack`

- `origin`: 你的 fork
- `upstream`: `zalsay/ai-finance`

宿主仓库只记录“当前引用的 `fintrack` commit”，不吞并其历史。

## 部署与访问模型

采用独立部署：

- `daily_stock_analysis` 部署为自己的 Web/API 服务
- `fintrack` 部署为自己的前端、Go API、Python 服务

推荐访问方式：

- 宿主 Web 中新增 `FinTrack` 入口
- 入口跳转到宿主统一地址前缀，例如 `/integrations/fintrack`
- 宿主网关或前端路由将其转发到 `fintrack` 的独立地址

实现上可选两种轻量方式：

1. 直接新标签页跳转到 `fintrack` 独立域名或子路径
2. 由宿主反向代理到 `fintrack` 服务

为保持最小改动，优先推荐：

- 第一阶段使用“跳转到独立地址”
- 第二阶段如有统一域名需求，再加反向代理

## 用户与账号关系模型

### 登录体系

- `daily_stock_analysis` 独立登录
- `fintrack` 独立登录
- 两边各自维持自己的 session/cookie/token

### 关联字段

在 `fintrack.users` 增加以下字段：

- `daily_stock_analysis_user_id`

字段约束建议：

- 可空：允许 `fintrack` 用户独立存在
- 唯一：一个 `daily_stock_analysis` 用户只能绑定一个 `fintrack` 用户

如果需要更好的审计，可追加：

- `daily_stock_analysis_bound_at`

但从“最小改动”出发，第一阶段不是硬要求。

## 首次进入自动绑定流程

### 场景 1：宿主用户已登录，FinTrack 用户也已登录

1. 用户在 `daily_stock_analysis` 中点击 `FinTrack`
2. 宿主服务生成短时效签名 token
3. 浏览器跳转到 `fintrack` 桥接入口，例如 `/bridge/dsa-entry`
4. `fintrack` 校验 token
5. 若当前 `fintrack` 登录用户尚未绑定 `daily_stock_analysis_user_id`
6. 自动写入该字段，完成首次绑定
7. 跳转到 `fintrack` 首页或指定页面

### 场景 2：宿主用户已登录，FinTrack 尚未登录

1. 用户从宿主进入 `fintrack`
2. `fintrack` 校验宿主 token 后，将待绑定信息暂存到短期 session
3. 若本地未登录，则跳转到 `fintrack` 登录/注册页
4. 用户在 `fintrack` 完成登录或注册
5. 登录成功后读取待绑定信息
6. 若当前用户未绑定，则自动写入 `daily_stock_analysis_user_id`
7. 绑定完成后进入 `fintrack`

### 场景 3：当前 FinTrack 用户已绑定到其他宿主用户

1. `fintrack` 校验发现当前登录用户已有不同的 `daily_stock_analysis_user_id`
2. 拒绝覆盖
3. 返回明确错误提示，要求用户切换 `fintrack` 账号

### 场景 4：宿主用户已绑定到其他 FinTrack 用户

1. 数据库唯一约束触发
2. `fintrack` 返回冲突提示
3. 不自动解绑，不自动迁移

这保证自动绑定只发生一次，不会悄悄覆盖现有映射。

## Token 桥接设计

宿主生成一个短时效签名 token，传给 `fintrack`。

建议 token 载荷包含：

- `iss`: `daily_stock_analysis`
- `sub`: 宿主用户 ID
- `iat`
- `exp`
- `nonce`
- `return_to`

校验方式：

- `fintrack` 使用预共享密钥或非对称公钥校验签名
- token 时效尽量短，建议 1 到 5 分钟
- token 只用于“建立关联和首次进入桥接”，不作为 `fintrack` 登录凭证

这可以保证：

- 保持独立登录
- 只共享最小身份事实
- 不要求 `fintrack` 信任宿主 cookie

## 宿主侧 UI 集成

宿主只做最小 UI 改动：

- 左侧导航增加 `FinTrack`
- 首页增加 `FinTrack` 摘要卡片
- 卡片展示内容优先只读，例如：
  - 是否已绑定
  - 关注列表数量
  - 最近公开预测数量
  - 最近一次回测状态

第一阶段不在宿主里重做：

- `fintrack` Dashboard
- `fintrack` Watchlist
- `fintrack` Portfolio
- `fintrack` 自己的登录页

## FinTrack 侧最小改动点

第一阶段只需要增加：

- 用户表字段迁移
- 宿主桥接 token 校验入口
- 首次绑定逻辑
- 冲突处理逻辑
- 绑定完成后的跳转逻辑

不修改：

- 现有 TimesFM 预测主链路
- 现有 watchlist 业务
- 现有回测主链路
- 现有独立登录模型

## 数据库与迁移策略

`fintrack` 独立数据库继续保留，新增字段采用增量迁移：

- 不修改宿主数据库
- 不共享 `users` 表
- 不共享 `sessions`
- 不共享业务数据表

迁移方式建议：

- 在 `fintrack-api/database` 下新增 migration
- 为 `users.daily_stock_analysis_user_id` 增加唯一索引
- 保持旧用户数据兼容

## 冲突与错误处理

需要明确处理以下错误：

- 宿主 token 无效
- 宿主 token 过期
- 宿主用户 ID 缺失
- `fintrack` 用户未登录
- 当前 `fintrack` 用户已绑定其他宿主用户
- 当前宿主用户已被其他 `fintrack` 用户占用

返回原则：

- 不自动覆盖已存在绑定
- 不静默失败
- 给出明确的前端文案和下一步动作

## 安全边界

- 宿主 token 仅用于桥接，不用于代替 `fintrack` 登录
- token 必须短时效
- token 必须签名校验
- 建议校验 `nonce`，避免重放
- 建议限制 `iss` 和允许来源

## 更新与同步策略

### 更新宿主主仓库

1. 在 `daily_stock_analysis` 根目录同步 `upstream/main`
2. 处理宿主代码冲突
3. 保留当前 `fintrack` 子模块指针不变，除非明确要升级

### 更新 FinTrack 子模块

1. 进入 `external/fintrack`
2. 同步 `fintrack` 自己的 `upstream/main`
3. 将你的集成改动维护在自己的 fork 分支上
4. 测试无误后，回到宿主根目录提交一次“子模块指针更新”

### 宿主仓库中的表现

宿主仓库对 `fintrack` 的升级只体现为：

- 子模块 commit 从 A 更新到 B

这样 review 面最小，也方便回滚。

## 推荐维护策略

建议把对 `fintrack` 的集成改动尽量收束在以下位置：

- 桥接入口
- 用户字段迁移
- 绑定服务
- 少量配置项

不要在 `fintrack` 内部散落大量宿主耦合逻辑，否则后续跟上游会越来越痛。

## 风险与权衡

### 优点

- 独立仓库边界清楚
- 独立数据库边界清楚
- 登录体系隔离，风险低
- 后续同步两个上游仓库都可控

### 代价

- 用户首次进入 `fintrack` 仍可能需要二次登录
- 宿主体验不是完全一体化
- 需要维护一层桥接 token 协议

## 推荐实施顺序

1. 先用 `git submodule` 接入 `fintrack`
2. 在宿主加 `FinTrack` 入口和配置
3. 在 `fintrack` 增加 `daily_stock_analysis_user_id`
4. 实现桥接 token 校验与首次自动绑定
5. 增加冲突提示页
6. 最后再加宿主摘要卡片

## 当前推荐结论

在当前约束下，最优方案是：

- `daily_stock_analysis` 作为宿主
- `fintrack` 作为独立 submodule 与独立部署的子应用
- 独立数据库、独立登录
- 使用短时效签名 token 做首次自动绑定
- 在 `fintrack.users` 内增加 `daily_stock_analysis_user_id` 作为宿主关联字段

这个方案改动最小、边界最清晰，也最适合后续 fork 与持续更新。
