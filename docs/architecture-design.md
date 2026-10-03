# LLM Gateway 架构设计

本文说明网关在身份、路由、故障处理、限流配额、用量结算和扩容方面的设计取舍。重点是请求从进入网关到完成结算时，哪些状态必须保持一致，哪些能力可以降级，以及系统在流量扩大后应先改哪里。

## 设计目标

网关是一个用户自助的多渠道聚合转发系统：用户注册后管理自己的上游渠道与网关 Key，通过 OpenAI 兼容接口调用。请求执行涉及会话身份、用户私有渠道、Token 用量、渠道健康状态和审计日志，但不包含平台计费。因此设计优先级是：

1. 不因为单个上游渠道故障拖垮整体服务。
2. 业务数据严格按用户隔离，任何请求只使用 Key 所属用户自己的渠道与规则。
3. 不重复写入成功 usage log，不把同一请求记成多次成功。
4. 限流和配额在多实例下仍有统一语义。
5. 流式响应已经交付给用户后，不尝试做语义不可靠的“续接”。
6. 在保证用量日志正确的前提下，渠道近似余额与健康指标允许 best-effort。

当前实现使用 Go `net/http`、PostgreSQL 和独立 nginx 托管的 React Dashboard。nginx 将管理端和下游 API 反向代理到 Go 网关；各业务模块在自身包内定义窄 port，进程边界负责组合这些 port；代理编排集中在 `server/internal/proxy`，协议适配集中在 `server/internal/proxy/openai`。浏览器身份使用服务端 session 与 HttpOnly Cookie。

## 1. 身份与归属

- 用户通过 `/admin/auth/{register,login,logout,me}` 使用用户名密码登录，session 只存 token 哈希。
- 除 `/admin/auth/*` 外的所有 `/admin` 接口都要求有效会话；`httpapi` 中间件校验会话并把用户 id 注入 `httpcommon.Identity`。
- `channels` 与 `rate_limit_rules` 携带 `owner_user_id`；`usage_logs` 与 `quota_policies` 按用户过滤；跨用户读写一律返回 404 或空列表。
- `/v1` 只依赖网关 Key：校验 Key 存在、启用且未过期，然后用 `Key.UserID` 选择该用户自己的渠道和限流规则。

## 2. 负载均衡与路由策略

### 2.1 先按 priority 做硬隔离

对一个 public model，系统先得到该用户所有启用且有模型映射的渠道，再排除：

- 熔断状态为 `open` 的渠道。
- 半开状态但没有取得探测 lease 的渠道。
- 手动停用的渠道。
- 余额为零或负数的计费渠道。
- 余额低于全局 `CHANNEL_MIN_ROUTE_BALANCE` 的计费渠道。

cold route 将剩余候选按 `priority` 分组，只在最高 priority 组内选择首选渠道。低 priority 渠道是故障切换后备，成功后可在最近成功绑定的剩余周期内继续走 fast path（见 2.3）。

这样设计的原因是：priority 通常表达用户的硬意图，例如主供应商、区域优先级或成本层级。如果把所有渠道放入一个动态分数池，低优先级渠道可能因为短期延迟更低而夺走主渠道流量，配置就失去可解释性。

### 2.2 同级使用 weight

同一 priority 组内使用静态 weight 分配流量。weight 是基础流量比例而不是绝对保证；例如权重 1 和 3 的长期目标是约 25% 和 75%。

当前不实现按延迟和健康度动态改权重，原因是动态评分容易混合请求长度、输出速度和网络延迟，少量样本会造成抖动，用户也难以解释流量变化。熔断已经承担“异常渠道隔离”的职责。

### 2.3 最近成功渠道 fast path 与固定周期重选

fast path 按 `owner_user_id + APIKeyID + public model` 查最近成功渠道，命中后只做单 channel 新鲜 query，立即校验归属、禁用、模型映射、cooldown 和余额，half-open 仍须取得单飞 probe lease。绑定不缓存密钥，实际尝试时另行获取。未命中、过期或渠道不再可用时走 cold route：仍用 `APIKeyID + model` 稳定哈希映射最高 priority 组的 weight 区间。

绑定只在成功结算后建立；流式还必须同时收到 usage 与 `[DONE]` 并正常成功结算，部分结算不建立绑定。进程本地 LRU 容量为 4096，重选周期固定 5 分钟。同 channel 且未过期时成功不会延长 expires，但仍更新 generation，避免旧请求的 late failure 清掉新 success；换 channel、已过期或新 binding 从当前时间起算 TTL。

低 priority fallback 成功后可在剩余周期内继续承接请求，但持续成功不能无限续期；周期结束后的下一次 cold route 会重新考虑已恢复的高 priority 渠道。候选集合及 priority/weight 变更最多等待剩余周期，不保证立即应用；禁用、映射、cooldown 和余额则逐请求立即校验。

粘性是可重建的路由优化而非账务状态，不写数据库。多实例各自持有缓存，绑定和淘汰互不共享。

## 3. 上游故障处理

### 3.1 请求级故障切换

cold route 先生成去重后的候选顺序；fast path 仅在故障允许 retry、总 deadline 未到且尝试预算尚有剩余时 lazy 加载 fallback，按 channel ID 去重并排除已尝试渠道。两条路径共享 `UPSTREAM_MAX_ATTEMPTS` 总尝试预算和请求 deadline，不因加载后备而重置。可切换故障包括：

- 连接建立失败、DNS 失败、连接重置等传输错误。
- 上游超时。
- HTTP 429。
- HTTP 401、402、403。
- HTTP 5xx。

调用方错误如 400、404、409、422 不切换，因为这类错误通常是请求本身的问题。每次实际尝试都会更新对应渠道的健康状态，但客户端请求只允许有一个最终响应和一次最终结算；失败尝试不写成功 usage log。

### 3.2 总时限和取消传播

请求使用一个总 `context` deadline，`UPSTREAM_REQUEST_TIMEOUT` 默认 600 秒，所有候选尝试与流式读取共享该时限，而不是每次尝试重新计时。客户端断开、服务关闭或总时限到期时，取消当前上游请求、不再尝试后续渠道；已有上游 usage 时先尝试结算（见 4.3），无 usage 的估算交付仅写零费用审计，未结算的 quota 和 rate-limit reservation 释放或由 reaper 回收。

quota reservation TTL 默认 `timeout + 60` 秒（默认 660 秒），配置低于该下限会被提升；配置层和 proxy 配置入口均保证 `TTL >= timeout + 60`。普通与 TLS 示例 nginx 的 `/v1` 均设置 `proxy_read_timeout 600s`、`proxy_send_timeout 600s` 并关闭响应缓冲。这两项是相邻读/写操作的等待时限，不等于网关的请求总 deadline；调整网关 timeout 时应同步检查入口配置。

总时限比无限重试更重要。没有总时限时，多渠道故障切换可能把一个下游请求拖成多个上游慢请求，最终耗尽连接、goroutine 和数据库连接池。

### 3.3 流式请求中途挂断

流式请求分成两个阶段：上游返回 2xx 但尚未向下游写出任何数据，以及已经成功向下游写出 SSE 数据。第二阶段不能切换渠道，因为已发送内容无法撤回；重新请求会导致重复内容、上下文不连续、无法证明续接点和用量重复。

当前实现从流式上游返回 2xx 起就不再切换渠道，而不仅在首次写出后才禁止切换。中途断流会结束当前 SSE 并记录渠道故障（客户端取消不计为渠道故障）；部分结算优先使用已收到的上游实际 usage，只有无 usage 时才估算确认写出的文本（见 4.3）。

## 4. 用量记录与结算

网关不向用户计费，但保留完整的用量统计与渠道近似记账。

### 4.1 为什么选择 PostgreSQL reservation

限流和业务配额都需要处理“请求已经进入，但还没有完成”的状态。只查询 `usage_logs` 会产生竞态：两个请求同时看到剩余额度然后同时放行。当前方案把请求生命周期拆成：

```text
申请 reservation -> 执行请求 -> 成功结算或失败释放 -> 过期回收
```

周期配额仍按保守估算预留；没有上游 usage 时释放的是 reservation，不存在“释放 usage”，usage log 是审计事实。周期配额使用 PostgreSQL bucket 和 reservation；运行时限流使用独立的 rate-limit reservation。金额使用定点整数/NUMERIC，Token 使用整数，数据库事务负责行锁和原子更新。

### 4.2 成功结算事务

成功请求的核心动作在 Store 的一次事务中完成：

```text
锁定渠道余额（如适用）
结算 quota reservation
近似扣减渠道余额
写入 success usage_log
提交事务
```

任何一步失败都回滚，避免出现“写成功日志但没有结算”或部分提交。用户侧没有余额，因此不再有用户扣费步骤，也不写余额流水。

### 4.3 失败请求和部分结算

失败请求仍可以写 error usage log，用于审计和渠道诊断。流式正常结束必须同时收到上游 usage 和 `[DONE]`，按实际 usage 结算。异常结束（断流、缺 DONE、超时、协议错误或客户端断开）的部分结算按以下优先级执行：

- 已收到 usage：优先按上游实际 Token（含缓存）与费用结算，即使缺 DONE 或没有成功写出文本；日志使用 `partial_actual_*`。
- 没有 usage：只有存在确认成功 `emit` 的文本且 tokenizer 能得到正数 completion Token 时，才以合理 prompt estimate + 已写出文本 Token 记录零费用审计；日志使用 `partial_estimated_*`，始终未结算，不计 total、cost、quota used、限流 Token 消耗或渠道余额扣减。无 usage 时释放 quota 和限流 reservation。缓存 Token 不凭空推断。
- prompt 使用 `EstimatedUsage.PromptTokens`，是完整请求 JSON 的 tokenizer estimate + 16 及媒体预算；保守准入的 `InputTokens` 还采用请求字节数 + 16 的上界，两者不能混用。每个 image_url/input_audio 标记加 8192 的媒体预算，未知模型回退 `cl100k_base`。最大输出预留按 `max_completion_tokens > max_tokens > QUOTA_DEFAULT_MAX_TOKENS` 并乘 `n`；准入总量为 `InputTokens + OutputTokens`。
- 没有确认写出的文本或本地估算失败时不扣费；正常解析到 DONE 但缺 usage 仍报 `upstream_usage_missing`，不以本地估算补齐。
- 部分结算使用与成功结算相同的原子事务，但日志保持 `status=error`；有消耗不意味着请求成功。定价与结算使用独立且有限时的 context，客户端取消不直接取消记账。
- 定价/结算失败日志（`pricing_error`、`settlement_failed` 及对应 partial 前缀）可保留 usage 作为审计证据，但不计入 Token/费用消耗；结算失败回滚全部写账变化。

历史记录不会自动修复、重新分类或补结算；本次口径调整不创建迁移或补数据。

### 4.4 防止重复成功日志

`usage_logs.request_id` 有唯一约束，结算会检查 reservation 状态和 request identity。重复调用同一个结算请求不会再次成功写账。请求内 failover 使用同一个 request ID，最终候选按成功或符合条件的部分用量进入结算。

### 4.5 统计接口的取舍

管理端统计从 `usage_logs` 实时聚合并按用户过滤，而不是维护另一份可能漂移的统计表。`actual_tokens` 汇总 success 与已结算 `partial_actual_*` 的实际 usage，`total_tokens = actual_tokens`；`estimated_tokens` 独立汇总所有 error 状态的 `partial_estimated_*` 本地审计估算，包括诊断后缀，均未结算且不计消耗。daily 的 input/output/cached 与各统计的 cost 使用同一已结算集合；定价/结算失败审计日志不计消耗。请求/成功/错误数仍按日志状态统计，部分结算不改变 error 状态。`active_key_count` 统计该用户启用且未过期的 Key 数。

stats 使用完成时写入的 `created_at`；overview/channels 使用 `[start_time,end_time)`，daily 按 UTC 自然日分组。quota 则在请求开始阶段预留时固定当时启用的 policy 和 UTC bucket，结算沿用原桶；未启用或不存在的 policy 不追溯记账。跨日/月请求、策略生命周期变化和历史错误日志等会造成差异，不能宣称 quota 与 stats 在任何情况下绝对相等。统计可按当前日志重算，但不会自动修复历史记账；日志量大后实时聚合也会成为读压力。

## 5. 瓶颈与百倍流量扩展路径

### 5.1 当前最可能的瓶颈

按影响顺序，当前瓶颈大致是：

1. PostgreSQL 事务和连接池：一次成功请求会涉及 reservation、配额 bucket、usage log 和渠道余额。
2. `usage_logs` 的实时聚合：Dashboard 启动会并发请求多个统计接口。
3. 限流/配额热行：同一 user、API Key 或渠道的高并发请求会竞争同一行。
4. 上游连接与流式响应：长连接会占用 HTTP transport、文件描述符和 goroutine。
5. 单体进程的管理端和代理流量共享资源。

CPU 和 Go 调度通常不会是第一瓶颈；LLM 请求的主要成本在网络等待、长连接和数据库事务。

### 5.2 扛住 100 倍流量的改造顺序

先建立基线：记录 QPS、SSE 并发数、上游响应时间、数据库事务耗时、连接池使用率、锁等待、慢查询和各类 reservation 失败率；对 usage 聚合查询执行 `EXPLAIN (ANALYZE, BUFFERS)`。

然后按顺序改造：

1. **代理进程与数据库扩容**：网关多实例 + 负载均衡；PostgreSQL 独立实例、合理连接池、只读统计连接；所有实例共享同一 PostgreSQL。
2. **统计读路径移出热路径**：`usage_logs` 仍是事实表，增加按小时/天的异步 rollup 或物化视图，Dashboard 优先查询 rollup。
3. **拆分高竞争限流计数**：把 RPM/TPM 等短窗口计数迁移到 Redis/Lua 或独立限流服务，PostgreSQL 仍是配额 reservation 的最终来源。
4. **拆分长连接与管理端流量**：代理服务独立扩容，管理端和统计查询独立部署，必要时引入异步任务 API。

### 5.3 哪些事情不要先做

- 不要先用本地内存计数器解决多实例限流。
- 不要把每次请求的所有尝试都写成成功 usage log。
- 不要为了动态路由先引入机器学习或复杂 bandit 算法。
- 不要在没有指标基线前盲目扩大 PostgreSQL 连接池。

## 结论

当前架构的核心选择是：路由偏好可以近似、渠道余额与健康指标可以 best-effort，但用户归属隔离、配额/限流 reservation 与成功用量日志必须由共享 PostgreSQL 以事务保证一致性。

这使系统适合先通过多实例和数据库优化扩展。真正达到 100 倍流量时，优先把统计查询和短窗口限流从 PostgreSQL 热路径中卸载，同时保留 PostgreSQL 作为配额与用量事实来源。
