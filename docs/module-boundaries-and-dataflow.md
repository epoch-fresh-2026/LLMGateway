# 模块划分与数据流

本文描述 LLMGateway 的业务模块边界、依赖方向和主要请求数据流。模块划分按业务能力组织，不按 `handler`、`service`、`utils` 等技术层横向堆叠。

## 一、模块总览

```mermaid
flowchart TB
    CMD[cmd/llmgateway\n进程装配与路由表]
    HTTP[internal/httpapi\nHTTP 协议入口与错误映射]
    COMMON[internal/httpcommon\n路径、JSON、分页、身份、响应辅助]

    CATALOG[internal/catalog\n渠道、模型映射、定价、连通性测试]
    ACCOUNTS[internal/accounts\n自助账户、资料、Gateway Key、会话、权限]
    USAGE[internal/usage\n用量日志、审计、统计]
    RATELIMIT[internal/ratelimit\n限流规则管理入口]
    QUOTA[internal/quota\n周期配额管理入口]
    PROXY[internal/proxy\n认证、路由、代理、限流、结算]
    OPENAI[internal/proxy/openai\nOpenAI wire adapter 与 SSE 解析]

    TYPES[各业务模块\n类型、规则和窄 ports]
    STORE[internal/store\n仅错误兼容别名]
    PG[internal/store/postgres\nPostgreSQL Store 实现]
    SQLC[internal/db/sqlc\nsqlc 生成代码]
    DB[(PostgreSQL)]
    MONEY[internal/money\n定点金额]
    CRYPTO[internal/crypto\n加密、哈希、密钥生成]
    CONFIG[internal/config\n环境变量与默认值]

    DASH[dashboard-react\n独立 nginx 静态控制台]

    CMD --> HTTP
    CMD --> CONFIG
    CMD --> PG
    HTTP --> COMMON
    HTTP --> CATALOG
    HTTP --> ACCOUNTS
    HTTP --> USAGE
    HTTP --> RATELIMIT
    HTTP --> QUOTA
    HTTP --> PROXY

    CATALOG --> STORE
    ACCOUNTS --> STORE
    USAGE --> STORE
    RATELIMIT --> STORE
    QUOTA --> STORE
    PROXY --> STORE
    PROXY --> OPENAI
    PROXY --> MONEY
    ACCOUNTS --> CRYPTO
    CATALOG --> CRYPTO

    PG -.implements.-> STORE
    PG --> SQLC
    SQLC --> DB
    PG --> MONEY
    PG --> CRYPTO

    classDef boundary fill:#172554,stroke:#60a5fa,color:#dbeafe;
    classDef business fill:#164e63,stroke:#22d3ee,color:#cffafe;
    classDef infra fill:#3f3f46,stroke:#a1a1aa,color:#f4f4f5;
    class HTTP,COMMON boundary;
    class CATALOG,ACCOUNTS,USAGE,RATELIMIT,QUOTA,PROXY,OPENAI business;
    class STORE,PG,SQLC,DB,MONEY,CRYPTO,CONFIG,DASH infra;
```

### 1. 顶层装配与 HTTP 入口

- `cmd/llmgateway` 负责进程启动、优雅关闭、数据库初始化和顶层路由表。
- `internal/httpapi` 负责 HTTP 方法校验、请求体读取、统一错误映射、会话中间件和业务模块委托；Dashboard 由独立 nginx 服务托管。
- `internal/httpcommon` 只提供跨模块的 HTTP 辅助能力（含 `Identity` 身份传递），不承载渠道、配额、路由或限流规则。
- 路径到入口的映射集中在 `cmd/llmgateway/router.go`，避免业务模块各自注册顶层路由。

### 2. 业务模块

#### `catalog`

拥有渠道、模型映射、定价、定价成本公式和渠道连通性测试。渠道按 `owner_user_id` 隔离；下游请求的选路、重试和结算不在这里。上游失败分类（`ClassifyUpstreamResult`）与定价成本公式（`ComputeCost`/`EstimateReservationCost`）也归本模块，proxy 只调用。

#### `accounts`

拥有自助账户（注册/登录/登出/会话）、资料（昵称/改密码）、Gateway Key 及其归属、认证上下文。Gateway Key 只保存哈希；渠道 API Key 的加密由 `crypto` 提供。不含平台计费、用户状态或分组。

#### `usage`

拥有 usage log、审计查询和统计接口，全部按会话用户过滤。成功请求的日志由 proxy 通过 Store 结算端口写入。

#### `ratelimit`

拥有用户私有限流规则管理入口与纯规则判定：目标匹配、Key override、指标窗口和滑窗计数。proxy 负责加载规则、查询 usage 计数并预留 reservation；持久化端口属于 `store`。

#### `quota`

拥有 UTC 日/月周期配额管理入口，策略只作用于本人 Key；周期配额和短窗口限流使用不同的持久化模型。

#### `proxy`

拥有下游请求的业务编排：Key 认证、模型权限、按 owner 选路、渠道健康、故障切换、限流、配额预留、上游请求和结算。它是跨领域协调者，依赖 `catalog` 的失败分类与定价公式、`ratelimit` 的规则判定，但不直接依赖 PostgreSQL、pgx 或 HTTP `ResponseWriter`，也不向用户计费。

#### `proxy/openai`

只负责 OpenAI 兼容协议边界：请求解析、请求改写、响应改写、SSE 解析、usage 提取和本地 Token 估算，通过 `proxy.ProtocolAdapter` 向协议中立的 proxy 提供能力。

### 3. 共享领域与基础设施

- 各业务模块拥有自己的协议中立类型、纯规则和窄持久化 port；`domain` 已删除。
- `internal/store` 不定义业务 port；仅保留错误兼容别名。进程装配在 `httpapi`/`proxy`/cmd 边界组合模块 port。
- `store/postgres` 是生产 Store 唯一实现，负责事务、锁、SQL 错误映射和敏感数据存取。
- `db/sqlc` 是生成代码，源头是 `db/queries` 和迁移文件，禁止手改生成文件。
- `money` 是定点金额叶子包，`crypto` 是加密/哈希/密钥生成叶子包；禁止通过日志、响应或错误泄露密钥。

## 二、依赖方向

```mermaid
flowchart LR
    ENTRY[HTTP / cmd] --> BUSINESS[业务模块]
    BUSINESS --> TYPES[模块自有类型、规则和 ports]
    IMPLEMENTATION[PostgreSQL implementation] -.实现.-> TYPES
    IMPLEMENTATION --> GENERATED[sqlc generated code]
    GENERATED --> DATABASE[(PostgreSQL)]

    PROTOCOL[协议 adapter] --> CONTRACT[Proxy contracts]
    PROXY[Proxy orchestration] --> PROTOCOL

    BUSINESS -.禁止反向依赖.-> DATABASE
    BUSINESS -.禁止反向依赖.-> GENERATED
    TYPES -.禁止依赖.-> HTTP
    TYPES -.禁止依赖.-> DATABASE
    STORE[internal/store errors] -.禁止定义业务 port.-> TYPES
    PROXY -.禁止依赖.-> HTTP_WRITER[http.ResponseWriter]
```

依赖方向的核心原则是：业务模块表达业务意图，Store 实现持久化细节，HTTP 层表达外部协议，协议 adapter 处理 wire 细节。任何一层越过边界直接操作另一层，都会增加测试、替换实现和后续扩容的成本。

## 三、非流式请求数据流

```mermaid
sequenceDiagram
    autonumber
    participant Client as 下游客户端
    participant HTTP as httpapi.OpenAI
    participant Proxy as proxy.Service
    participant Store as Store port
    participant PG as PostgreSQL
    participant Upstream as 上游渠道

    Client->>HTTP: POST /v1/chat/completions
    HTTP->>Proxy: 认证上下文 + ChatRequest
    Proxy->>Store: 按 Key hash 查询 Key 权限（启用/未过期）
    Proxy->>Proxy: 估算输入/输出 Token
    Proxy->>Store: 申请限流 reservation（本人规则）
    Proxy->>Store: 查询该用户健康路由候选
    Proxy->>Proxy: 排除 open、停用、余额不足渠道
    Proxy->>Proxy: API Key + model 粘性选择首选
    Proxy->>Store: 申请周期 quota reservation
    Proxy->>Store: 获取渠道密钥
    Proxy->>Upstream: 改写模型后发起请求

    alt 可重试上游故障
        Upstream-->>Proxy: 网络错误 / 429 / 401-403 / 5xx
        Proxy->>Store: 记录渠道失败
        Proxy->>Proxy: 尝试下一个候选
    else 成功响应
        Upstream-->>Proxy: 2xx + usage
        Proxy->>Proxy: 解析 usage、计算费用
        Proxy->>Store: 原子结算渠道近似余额、quota、usage log
        Store->>PG: 同一事务提交
        PG-->>Store: commit
        Proxy->>Store: 完成限流 reservation
        Proxy-->>HTTP: 改写 public model 的响应
    else 最终失败
        Proxy->>Store: 释放 reservation，记录最终错误
        Proxy-->>HTTP: OpenAI 风格错误
    end

    HTTP-->>Client: 单一最终响应
```

### 请求中的状态所有权

一次请求中有三类状态：

1. **请求级状态**：request ID、context、候选顺序和最终候选，由 proxy 持有。
2. **临时资源占用**：限流和 quota reservation，由 Store 持久化并以状态机完成或释放。
3. **最终事实**：渠道近似余额变化、usage log 和实际 quota 使用量，在 PostgreSQL 事务中提交。

故障切换只改变候选和渠道健康，不创建新的 request ID，也不创建第二次最终写入。

## 四、流式 SSE 数据流

```mermaid
sequenceDiagram
    autonumber
    participant Client as 下游客户端
    participant HTTP as httpapi.OpenAI
    participant Stream as completionStream
    participant Adapter as OpenAI SSE adapter
    participant Upstream as 上游渠道
    participant Store as Store / PostgreSQL

    Client->>HTTP: stream=true
    HTTP->>Stream: 持有响应流
    Stream->>Upstream: 建立 SSE 请求
    Upstream-->>Adapter: data: JSON delta
    Adapter->>Adapter: 重写 public model，提取文本 delta
    Adapter-->>Stream: StreamEvent
    Stream->>HTTP: emit(frame)
    HTTP-->>Client: flush SSE frame
    Stream->>Stream: 仅累计成功 emit 的文本

    alt 收到 usage 与 [DONE]
        Upstream-->>Adapter: usage chunk
        Upstream-->>Adapter: data: [DONE]
        Stream->>Store: 按上游实际 usage 原子结算
        Store-->>Stream: 结算成功
        Stream-->>Client: data: [DONE]
    else 上游中断或协议错误
        Upstream--xAdapter: EOF / malformed frame
        Stream->>Store: 对已 emit 文本本地 tokenizer 估算并部分结算
        Stream-->>Client: SSE error + [DONE]
    else 客户端取消
        Client--xHTTP: 断开连接
        HTTP->>Stream: context cancel
        Stream->>Upstream: 取消上游请求
        Stream->>Store: 已 emit 文本按估算部分结算
    end
```

流一旦向下游写出数据，就不再切换到其他渠道。这样做牺牲了中途续接能力，但避免重复内容、重复生成和无法解释的用量。

## 五、限流与配额数据流

```mermaid
flowchart TD
    REQUEST[请求进入] --> PRECHECK[解析请求与估算 Token]
    PRECHECK --> RULES[读取该用户的匹配限流规则]
    RULES --> ATOMIC[PostgreSQL 原子申请]
    ATOMIC -->|超限| REJECT[429 rate_limit_error / insufficient_quota]
    ATOMIC -->|成功| RESERVE[写入 reservation]
    RESERVE --> ROUTE[路由并调用上游]
    ROUTE --> RESULT{请求结果}
    RESULT -->|成功 + 实际 usage| SETTLE[按实际 Token/费用结算]
    RESULT -->|失败/取消| RELEASE[释放 reservation]
    SETTLE --> LOG[写 usage log]
    SETTLE --> DONE[reservation settled]
    RELEASE --> RELEASED[reservation released]
    EXPIRE[后台 reaper] --> EXPIRED[回收超时 pending reservation]
```

限流 reservation、周期 quota reservation 和成功结算的 quota 状态不能用进程内变量表达。进程内变量只能用于缓存或性能优化，不能作为多实例环境的最终计数。限流规则按 owner 加载，`target_type` 不含 `global`。

## 六、用量数据流

```mermaid
flowchart LR
    OUTCOME[上游结果]
    OUTCOME --> CLASSIFY[proxy 分类\n成功 / 上游错误 / 取消 / 部分流]
    CLASSIFY --> INPUT[UsageLogInput]
    INPUT --> TX[PostgreSQL 事务]
    TX --> CHANNEL[锁定并近似更新渠道余额]
    TX --> QUOTA[reserved 转 used]
    TX --> LOG[插入 usage_logs]
    CHANNEL --> COMMIT[全部成功才 commit]
    QUOTA --> COMMIT
    LOG --> COMMIT
    COMMIT --> AUDIT[可查询、可聚合、可审计]
    TX -->|任一步失败| ROLLBACK[回滚全部变化]
```

`usage_logs.request_id` 的唯一约束和 reservation 状态共同防止重复成功结算。管理端统计从 usage_logs 聚合且按会话用户过滤；未来增加 rollup 时，rollup 只是读模型，不能替代 usage_logs 事实表。用户侧没有余额或余额流水。

## 七、扩展时的边界

### 可以独立扩容的部分

- `proxy` 多实例：只要共享 PostgreSQL，路由和配额可以横向扩展。
- Dashboard/管理端：前端由 nginx 独立部署，通过同源反向代理访问 Go 网关的 `/admin`、`/v1` 和 `/healthz`。
- 统计读路径：可以增加异步 rollup 或只读数据库连接。
- 短窗口限流：高吞吐时可以迁移到 Redis/Lua，但必须定义数据库与 Redis 的一致性边界。

### 不应直接拆开的部分

- 渠道近似余额、quota 结算和成功 usage log：必须保持同一事务语义。
- 流式输出和结算：流式完成的判定依赖 `[DONE]`、usage 和客户端写入状态，不能简单丢到异步队列后“最终再记录”。
- 渠道健康状态与路由筛选：健康状态可以由 Store 管理，但路由必须在尝试前读取有效状态。

### 诊断问题的顺序

当请求失败时，先按以下顺序定位：

1. 客户端是否取消或请求 context 是否超时。
2. Key 是否有效，是否被配额或限流拒绝。
3. 是否没有健康候选或渠道余额阈值排除了全部渠道。
4. 上游是否返回可重试故障，故障切换是否用尽。
5. 上游是否已写出流，是否进入部分结算。
6. 最终结算事务是否提交，reservation 是否 settled/released/expired。

这个顺序将“请求没有发出去”“上游失败”“已经产生部分交付”和“结算没有提交”区分开，避免把所有错误都归结为上游不可用。
