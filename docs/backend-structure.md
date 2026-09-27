# Backend Structure

后端目录按业务能力纵向组织，目录本身体现 catalog、accounts、usage、ratelimit、proxy 等高内聚模块边界。

```text
server/                             Go 模块根（go.mod / go.sum / sqlc.yaml）
server/cmd/llmgateway/              进程入口与 HTTP 路由表（router.go）：config -> store -> httpapi -> http.Server
server/internal/config/             环境变量配置读取，集中管理默认值
server/internal/catalog/            目录、渠道、模型映射、定价、渠道连通性测试及其管理能力
server/internal/accounts/           用户、余额、网关 Key、认证上下文及权限能力
server/internal/usage/              用量日志、审计查询和统计能力
server/internal/ratelimit/          限流规则管理和运行时限流能力
server/internal/quota/              UTC 日/月 token/费用业务配额策略与管理能力
server/internal/httpapi/            顶层 HTTP 装配、响应 envelope 和业务入口委托
server/internal/httpcommon/         共享 HTTP 请求解析、路径、分页、存储错误映射和删除响应 helper 的唯一归属
server/internal/proxy/              下游代理业务：OpenAI 适配、路由、计费、限流、熔断、结算和上游调用
server/internal/proxy/openai/       OpenAI 兼容 wire DTO 与协议适配；归属 proxy 业务模块
server/internal/proxy/settlement/   结算事务 contract（proxy 拥有），供 Store 实现
server/internal/errors/             跨模块通用错误
server/internal/money/              定点金额（int64 最小单位）解析与格式化
server/internal/crypto/             渠道 api_key 加解密、网关 Key 生成与哈希
server/internal/store/              仅保留错误兼容别名，不定义业务端口
server/internal/store/postgres/     唯一生产 Store 实现，只提供持久化原语与事务边界
server/internal/testutil/storefake/ 不需要数据库的测试专用 fake，生产代码不得导入
server/internal/testutil/app/       测试专用模块装配 helper，生产代码不得导入
server/internal/db/migrate/         tern 迁移执行入口
server/internal/db/sqlc/            sqlc 生成代码输出目录，不手写业务逻辑
server/db/migrations/               PostgreSQL schema 迁移 SQL（SQL 资产）
server/db/queries/                  sqlc 查询 SQL（SQL 资产）
deployments/                        本地开发部署配置，如 PostgreSQL docker compose
dashboard-react/                    React + TypeScript + Vite 控制台源码，由独立 nginx 服务托管
```

Go 模块路径为 `LLMGateway/server`；Go 命令需在 `server/` 目录下执行（或在仓库根使用 `go -C server ...`）。

两个 `db` 目录职责不同，不要混淆：

- `server/db/`：SQL 资产（`migrations/` 迁移、`queries/` sqlc 查询），由 `sqlc.yaml` 读取。
- `server/internal/db/`：Go 包（`migrate/` 的 tern 执行入口、`sqlc/` 生成代码），由 Go 代码导入。

`CHANNEL_KEY_ENCRYPTION_KEY` 环境变量名常量位于 `server/internal/config`（env 解析职责）；`server/internal/crypto` 只负责密钥长度/算法校验，不再定义 env 常量。

## 约定

- 业务模块按能力纵向组织：`catalog`、`accounts`、`usage`、`ratelimit`、`quota`、`proxy` 各自聚合规则、端口使用和 HTTP 入口契约；这不是按 HTTP/store/protocol 的横向分层。
- `rate_limit_rules` 只表达短窗口速率控制；`quota_policies` 独立表达用户/Key 的 UTC 自然日/月 token 与费用预算，两者在 proxy 准入阶段统一执行但不共用持久化模型。
- `server/internal/httpapi` 只负责顶层 HTTP 装配、通用响应和委托，不作为跨业务 admin 文件集中地。
- 每个业务模块在自身包内拥有类型、规则和窄 port，不直接访问 PostgreSQL、sqlc 或 `internal/store`。
- 代理编排位于 `server/internal/proxy`，依赖各业务模块 port 和自身协议中立 contract；OpenAI wire 转换只在 `server/internal/proxy/openai` 完成。
- `internal/domain` 已删除，避免跨业务共享类型重新形成隐式 aggregate。
- OpenAI 兼容 JSON wire type 放在 `server/internal/proxy/openai`；业务模块不依赖 OpenAI 协议 DTO。
- PostgreSQL 是唯一运行时存储；`DATABASE_URL` 与 `CHANNEL_KEY_ENCRYPTION_KEY` 均为必填配置，缺失或非法时进程启动失败。
- sqlc 查询写在 `server/db/queries/*.sql`，schema 写在 `server/db/migrations/*.sql`，生成代码输出到 `server/internal/db/sqlc`。
- 不要手改 `server/internal/db/sqlc` 生成文件；修改 SQL 后运行 `sqlc generate`。
- 初始 schema 覆盖渠道、模型映射、定价、用户、余额、Key、限流和用量日志，后续 issue 应优先扩展现有表而不是新建重复概念。
- 统计接口（overview/daily/channels）在 `usage_logs` 上实时聚合，按 UTC 自然日分组；不存在 `daily_usage_stats` 表，因其未被使用且复合主键无法表达全局日汇总。
- 进程启动时建立 pgxpool 连接、执行迁移并装配 PostgreSQL store，不提供无数据库运行模式。
- PostgreSQL store 只实现持久化原语（CRUD/lock/query）与事务边界；渠道/健康、用户/Key/余额、配额、限流等规则与编排位于各自业务模块的 `Server`，跨聚合结算编排位于 `proxy`。
- 业务模块通过自身 `Port` 读取，通过模块自有的 `Tx`/`TxManager`（`InTx(ctx, func(Tx) error)`）在事务内编排写入；Store 不实现多步流程或业务判定。事务管理器以每模块一个适配器类型实现，经 `httpapi.Port` 的 `AccountsTx()`、`CatalogTx()`、`QuotaTx()`、`SettlementTx()` 注入。
- `server/cmd/llmgateway` 使用 `http.Server` 并在收到 `SIGINT`/`SIGTERM` 后优雅关闭。
- 金额能力集中在 `server/internal/money`，密钥能力集中在 `server/internal/crypto`；由业务模块（catalog/quota/accounts/proxy）使用，禁止重复实现金额解析或加密。`store/postgres` 不得 import `money`/`crypto`，只存取字符串与密文（由架构测试强制）。
- 禁止用 `float64` 参与计费；金额在 DB 用 `NUMERIC`，在 Go 用定点整数，对外输出字符串。
- 渠道 `api_key` 落库为密文，网关 Key 只存哈希；任何响应、日志、错误都不得出现明文密钥。
- `server/internal/money` 与 `server/internal/crypto` 为叶子包，不得依赖 `server/internal/store` 或 `server/internal/httpapi`。
- `server/internal/crypto` 的渠道密钥加密密钥来自环境变量 `CHANNEL_KEY_ENCRYPTION_KEY`（原始字节，长度 16/24/32）；缺失或非法时返回错误，禁止明文回退。
- 网关 Key 仅保存 `server/internal/crypto.HashKey` 的哈希，明文 `full_key` 只在创建/重置时返回一次。
- PostgreSQL 以 `api_key_ciphertext` 保存渠道密钥；测试 fake 仅存在于 `internal/testutil`，不得作为生产持久化实现。
- 共享 HTTP parsing/response glue 由 `server/internal/httpcommon` 统一持有；业务模块只保留领域相关的请求分派，顶层 HTTP 路由仍由 `server/cmd/llmgateway/router.go` 统一装配。
- HTTP 路由表（路径到入口的映射）集中在 `server/cmd/llmgateway/router.go`；`server/internal/httpapi` 只提供入口方法，不构造 mux。

## 文件组织约定

目录保持较浅层级：包内按领域拆文件，仅在协议与存储实现处使用子包，使目录能直接呈现模块边界。

- 每个包内按领域命名文件，禁止把多个领域堆进同一个文件：
  - `server/internal/catalog/`：渠道、模型、定价、健康、失败原因、路由 DTO 和 catalog/health ports
  - `server/internal/accounts/`：用户、余额、Key、认证 DTO 和 accounts port
  - `server/internal/usage/`：usage DTO、时间校验、结算输入和 usage port
  - `server/internal/ratelimit/`：限流规则、reservation、规范化规则和 ratelimit port
  - `server/internal/quota/`：配额策略、reservation、周期规则和 quota port
  - `server/internal/proxy/`：代理请求/响应、失败、限流/配额编排和 settlement contract
  - `server/internal/store/`：仅错误兼容别名，不定义业务 port 或 aggregate interface
  - `server/internal/store/postgres/`：`postgres.go`（结构体/构造函数）、`channel.go`、`user.go`、`usage.go`、`ratelimit.go`、`channelhealth.go`
  - `server/internal/testutil/storefake/`：测试专用 Store fake，仅供测试夹具使用
  - `server/cmd/llmgateway/`：`main.go`（装配与优雅关闭）、`router.go`（唯一 HTTP 路由表）
  - `server/internal/catalog/`：渠道、模型映射、定价和渠道连通性测试业务模块（HTTP 入口由顶层装配）
  - `server/internal/accounts/`：用户、余额、网关 Key 和身份业务模块（HTTP 入口由顶层装配）
  - `server/internal/usage/`：用量日志、审计和统计业务模块（HTTP 入口由顶层装配）
  - `server/internal/ratelimit/`：限流规则和运行时限流业务模块（HTTP 入口由顶层装配）
  - `server/internal/httpapi/`：`handler.go`（顶层入口/分派/响应）、`openai.go`（/v1 分派与错误映射）
  - `server/internal/httpcommon/`：共享 HTTP 请求解析、路径解析、分页、存储错误映射和删除响应 helper；这些通用行为只在此处实现
- `server/internal/proxy/`：`proxy.go`（编排依赖装配与代理错误）、`contracts.go`（协议中立请求/响应/usage contract）、`auth.go`（认证）、`routing.go`（选路）、`billing.go`（计费）、`ratelimit.go`（限流）、`orchestration.go`（代理编排）
    - `server/internal/proxy/openai/`：`types.go`、`adapter.go`（OpenAI 兼容 wire DTO、请求解析和响应适配；proxy 业务模块的协议边界）
- 进程装配边界可以组合业务 port，但禁止在 `internal/store` 恢复 aggregate `Store`。

```text
type Port interface {
    accounts.Port
    catalog.Port
    usage.Port
    ratelimit.Port
    quota.Port
}
```

- PostgreSQL 实现以编译期断言固定其端口契约：

```text
var _ catalog.Port = (*postgres.Store)(nil)
```

- 领域文件边界与 `server/db/queries/*.sql` 的领域划分保持一致（channels/models/pricing/users/rate_limits/usage_logs）。
- 拆分与移动只做等价搬迁，不得顺手改变路由、响应结构、状态码或错误语义。

## 下游代理（/v1）

- `GET /v1/models` 与 `POST /v1/chat/completions` 由 `server/internal/httpapi/openai.go` 暴露 HTTP 入口、方法校验、body 读取、客户端 IP 提取、SSE write/flush 和 OpenAI 错误响应映射；代理业务编排及其 `openai` 协议适配位于 `server/internal/proxy`。
- 认证使用 `Authorization: Bearer <gateway-key>`；密钥经 `server/internal/crypto.HashKey` 后查询，明文不落日志/响应。
- 路由候选按 `priority` 越大越优先；同一 API Key 使用同一 public model 时，在最高优先级候选组内按 `APIKeyID + model` 稳定哈希结合 `weight` 选择粘性首选渠道。熔断渠道和余额低于全局 `CHANNEL_MIN_ROUTE_BALANCE` 的计费渠道被排除，未设置余额的渠道不受该阈值影响。首选渠道失败时仍按本次请求的候选顺序故障切换；未设置或设置为 `0` 时仍排除非正余额渠道。
- 计费：缓存 token 已包含在 `prompt_tokens` 中，仅按 `(prompt_tokens - cached_tokens)` 计输入价，缓存部分计缓存价，避免重复计费。
- 成功结算：非流式 chat completion 成功后通过 store 级 `SettleChatCompletion` 端口统一处理用户扣费、可扣费渠道余额扣减与 success usage log。PostgreSQL 实现在单一事务中提交；`last_used_at` 仍为成功响应后的 best-effort 更新。
- 流式结算：`stream=true` 时网关强制向上游请求 `stream_options.include_usage=true`，逐事件重写 public model 并 flush；首个合法 JSON data 帧记录 TTFT。收到 usage 与 `[DONE]` 时按上游实际 usage 一次原子结算。中途断流、客户端取消或流协议错误时，仅对已成功写入下游的文本 delta 使用本地 tokenizer 估算 completion token，并与请求 prompt 估算一起结算；日志以 `partial_estimated_*` 错误码标识该估算口径。没有已转发文本、缺 usage 或本地估算失败时不扣费；客户端取消会传播到上游且不计渠道失败。
- 上游故障切换：一次请求只查询一次健康路由候选，proxy 在内存中按最高优先级组的权重选择首选，并以 `channel_id` 去重保留后备。仅传输错误、429、401/402/403 和 5xx 可切换；流式 2xx 后不再切换。`UPSTREAM_REQUEST_TIMEOUT` 控制请求总 deadline，`UPSTREAM_MAX_ATTEMPTS` 控制最大候选尝试数。
- 运行时限流：请求预检按 global -> user -> api_key -> model 顺序检查 RPM/TPM/RPD/concurrency，路由后检查 channel；日 token 预算不再由限流承担（`tpd` 指标已废弃，改由配额模块支持）；Token 预留使用输入 Token 加 `max_tokens` 的保守估算，完成后按实际 usage 结算。限流 reservation 与计数器独立持久化，过期记录由 reaper 清理。
- 周期配额：所有边界使用 UTC，日桶为 `[00:00, 次日 00:00)`，月桶为 `[当月 1 日, 下月 1 日)`。请求选定最终渠道后，使用内嵌 tokenizer 估算输入 token，并按 `max_completion_tokens > max_tokens > QUOTA_DEFAULT_MAX_TOKENS` 预留最大输出 token；费用按最终渠道价格预留。用户 policy 与 Key policy 必须全部满足。
- 配额持久化：`quota_buckets` 原子维护 `used_*` 与 `reserved_*`，`quota_reservations`/`quota_reservation_items` 保存请求级占用。正常失败主动释放，申请新额度时小批回收相关过期占用，进程后台 reaper 使用 `FOR UPDATE SKIP LOCKED` 兜底。成功结算在同一 PostgreSQL 事务中将 reserved 转为实际 used，并同时完成余额、渠道余额和 usage log。
- 限流：`rpm` + `reject` 规则基于 `usage_logs` 统计最近 1 分钟请求次数。`global`/`user`/`api_key` 保持按当前用户/Key 计数；`model` 规则额外按 public model 精确过滤；`channel` 规则在路由选中最终渠道后、调用上游前评估，超限直接返回 429 且写入 `error_code=rate_limited` 的 error usage log，不自动改选其他渠道。
- 熔断：每个渠道有 `channel_health` 状态（closed/open/half-open）。判定取并集：确定性失败（上游 401/402/403）或 half-open 探测失败立即 open；窗口错误率/超时率超阈值（默认窗口 60s、最小样本 10、错误率 50%、超时率 50%）时 open；低流量下回退到连续失败阈值（默认 5）。窗口统计使用固定 10s 分桶的 `channel_health_buckets`，每次尝试（成功或渠道可归因失败）在同一健康事务内 upsert 并聚合，bucket 由后台 reaper 按保留期清理。冷却（默认 30s）后惰性转为 half-open 允许探测，探测成功回 closed、失败回 open。half-open 探测通过 `channel_breaker_probes` 租约实现单飞：一次请求只放行一个探测，请求返回（或流关闭）时按 `lease_id` 条件释放，租约到期仅作崩溃/超时兜底。全局阈值由环境变量配置，`channel_breaker_configs` 提供每渠道覆盖（管理端 `/admin/channels/{id}/breaker`）。`ListRouteCandidates` 按每渠道 cooldown（未覆盖时用全局默认）排除 open 渠道；当无可用渠道（无映射或全部 open）时返回 `503 no_healthy_channel`（错误码由 `no_available_channel` 变更而来，同时覆盖这两种情况）。失败分类仅计入传输错误、上游 429/401/403/402 与 5xx，其余 4xx 透传且不计渠道失败。健康记录为 best-effort。
- 已知限制（后续 issue 处理）：
  - 未配置 `model_pricing` 的渠道×模型按 cost=0 放行（建议为所有可路由模型配置定价）。

## 本地 PostgreSQL

```powershell
docker compose -f deployments/docker-compose.postgres.yml up -d
```

默认开发库：

```text
postgres://llmgateway:llmgateway_dev@localhost:5432/llmgateway?sslmode=disable
```

后端环境变量：

```text
ADDR=:8080
DATABASE_URL=postgres://llmgateway:llmgateway_dev@localhost:5432/llmgateway?sslmode=disable
CHANNEL_KEY_ENCRYPTION_KEY=0123456789abcdef0123456789abcdef
MIGRATIONS_DIR=db/migrations
QUOTA_DEFAULT_MAX_TOKENS=4096
QUOTA_RESERVATION_TTL_SECONDS=120
QUOTA_REAPER_INTERVAL_SECONDS=30
QUOTA_REAPER_BATCH_SIZE=100
CHANNEL_BREAKER_FAILURE_THRESHOLD=5
CHANNEL_BREAKER_COOLDOWN_SECONDS=30
CHANNEL_BREAKER_WINDOW_SECONDS=60
CHANNEL_BREAKER_MINIMUM_SAMPLES=10
CHANNEL_BREAKER_ERROR_RATE_PERCENT=50
CHANNEL_BREAKER_TIMEOUT_RATE_PERCENT=50
CHANNEL_BREAKER_BUCKET_RETENTION_SECONDS=600
```

路径均相对于运行目录 `server/`；`MIGRATIONS_DIR` 默认 `db/migrations`。生产环境由独立 nginx 容器托管前端并将 `/admin`、`/v1` 和 `/healthz` 反向代理到 Go 网关。

启动前必须设置 PostgreSQL URL 和 16/24/32 字节的渠道密钥加密密钥：

```powershell
cd server
$env:DATABASE_URL="postgres://llmgateway:llmgateway_dev@localhost:5432/llmgateway?sslmode=disable"
$env:CHANNEL_KEY_ENCRYPTION_KEY="0123456789abcdef0123456789abcdef"
go run ./cmd/llmgateway
```

普通 `go test ./...` 使用 `internal/testutil/storefake` 运行单元与 HTTP 契约测试；PostgreSQL 集成测试在设置 `TEST_DATABASE_URL` 后启用，未设置时会明确跳过。

## 迁移

本仓库使用 [`github.com/jackc/tern/v2`](https://github.com/jackc/tern) 管理 PostgreSQL 迁移，并保留 `server/internal/db/migrate` 作为启动和集成测试的调用入口。

- 迁移文件为 `server/db/migrations/NNNNNN_name.sql`；版本必须从 `000001` 连续递增且唯一。SQL 位于 `---- create above / drop below ----` 前的是 up 迁移；省略分隔符表示不可逆迁移。
- tern 在 `public.schema_version(version)` 中记录当前版本，以 PostgreSQL advisory lock 串行化迁移；默认每个迁移在独立事务中执行。
- 项目尚未上线，不存在旧 `schema_migrations` 部署；数据库以全新实例为基线，直接由 tern 从零执行全部迁移。
- 进程启动会使用必填的 `DATABASE_URL` 自动执行 `MIGRATIONS_DIR`（默认 `db/migrations`）下的待执行迁移；失败时启动报错退出。

## sqlc

在 `server/` 目录下执行：

```powershell
cd server
sqlc generate
```

配置文件：`server/sqlc.yaml`；`schema` 指向 `server/db/migrations`，`queries` 指向 `server/db/queries`，生成代码输出到 `server/internal/db/sqlc`。
