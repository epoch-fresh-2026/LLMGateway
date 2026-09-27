# AGENTS.md

本文件面向在本仓库中工作的 AI Agent 和协作者。项目是一个 LLM API Gateway：后端使用 Go 标准库 `net/http`、PostgreSQL 和 sqlc，前端是无需构建的静态 HTML/CSS/JS 控制台。

## 目录放置规则

- `server/` 是 Go 模块根。所有 Go 命令在该目录执行，或在仓库根使用 `go -C server ...`。
- `server/cmd/llmgateway/` 只放进程装配、启动、优雅关闭和顶层 HTTP 路由表。路径到入口的映射统一放在 `router.go`。
- `server/internal/catalog/` 放渠道、模型映射、定价、渠道连通性测试及对应管理端入口。
- `server/internal/accounts/` 放用户、余额、网关 Key、认证上下文和权限能力。
- `server/internal/usage/` 放用量日志、审计查询和统计能力。
- `server/internal/ratelimit/` 放限流规则管理；与代理请求执行强相关的限流编排放在 `server/internal/proxy/`。
- `server/internal/quota/` 放 UTC 日/月业务配额策略和管理端入口；`rate_limit_rules` 只做速率控制，配额策略不得复用其持久化模型。
- `server/internal/proxy/` 放下游代理业务编排，包括认证、选路、计费、限流、熔断、结算和上游调用。
- `server/internal/proxy/openai/` 只放 OpenAI 兼容 wire DTO、请求解析和响应适配。
- `server/internal/httpapi/` 只放顶层 HTTP 入口、统一响应、错误映射、静态 Dashboard 托管和对业务模块的委托，不集中放具体业务实现。
- `server/internal/httpcommon/` 放跨业务复用的 HTTP 路径、JSON、分页、存储错误映射和响应 helper。
- 业务类型、规则和窄端口由业务模块拥有：`catalog` 管理渠道/定价/健康，`accounts` 管理用户/Key/认证，`usage` 管理用量 DTO/校验，`ratelimit` 管理规则/reservation，`quota` 管理策略/reservation，`proxy` 管理代理编排 contract、失败和结算类型。`internal/domain` 已删除。
- `server/internal/store/` 不再定义业务端口或 aggregate `Store`，仅保留通用错误兼容别名；组合接口由 `httpapi`、`proxy` 或 cmd 装配边界定义。
- `server/internal/store/postgres/` 放 PostgreSQL Store 实现，只提供持久化原语（CRUD/lock/query）与事务边界，不承担业务规则或跨表流程编排；业务模块不得直接导入该包。
- `server/internal/store/postgres/` 是唯一生产 Store 实现；`server/internal/testutil/storefake/` 与 `server/internal/testutil/app/` 仅供不需要数据库的单元与 HTTP 契约测试使用，生产代码不得导入，运行时不得提供 memory fallback。
- `server/db/migrations/` 放 schema 迁移，`server/db/queries/` 放 sqlc 查询，`server/internal/db/migrate/` 放迁移 runner，`server/internal/db/sqlc/` 放生成代码。禁止手改 sqlc 生成文件。
- `server/internal/money/` 放定点金额能力，`server/internal/crypto/` 放渠道密钥加密、网关 Key 与 session token 哈希、bcrypt 密码哈希与 token 生成，`server/internal/config/` 放环境变量名称、解析和默认值。
- `dashboard-react/` 是 React + TypeScript + Vite 前端工程；`dashboard-react/src/api/` 放 `/admin` 数据接入，`dashboard-react/src/pages/` 按页面放视图代码，生产构建产物由独立 nginx 静态托管。
- `docs/api-requirements.md` 是管理端与下游 API 契约来源，`docs/backend-structure.md` 记录后端结构细节。接口或结构发生变化时同步更新对应文档。
- `deployments/` 放本地部署配置。`communication/` 只用于本地协作，不得提交。
- 新文件优先放入现有业务模块。只有出现独立、稳定且可清晰命名的业务能力时才新增顶层模块，禁止按 `service`、`handler`、`utils` 等泛化技术层创建兜底目录。

## 边界规则

- 后端按业务能力纵向组织。`catalog`、`accounts`、`usage`、`ratelimit`、`quota`、`proxy` 各自持有本领域入口和规则，不把业务重新集中到通用 handler 或 service 包。
- 业务模块通过自身包内的窄端口访问持久化，不直接依赖 PostgreSQL、pgx、sqlc 或 `internal/store`。Store 实现不得反向依赖 HTTP 层。
- Store 只暴露持久化原语：业务模块通过 `Port` 读取，通过模块自有的 `Tx`/`TxManager`（`InTx(ctx, func(Tx) error)`）在事务内编排写入。事务管理器以每模块一个适配器类型实现（Go 不支持按回调签名重载方法），经装配边界注入，例如 `httpapi.Port` 的 `AccountsTx()`、`CatalogTx()`、`QuotaTx()`、`SettlementTx()`。禁止在 Store 内实现多步流程或业务判定。
- 单聚合用例（如配额预留、渠道余额）归各自业务模块；跨聚合原子用例（如结算）由 `proxy` 拥有并定义事务原语，Store 只实现原语。
- `server/internal/httpapi` 负责协议入口和委托，不负责选路、计费、认证、限流、熔断或结算等代理业务。
- OpenAI JSON wire type 只能出现在 `server/internal/proxy/openai` 和必要的 HTTP 适配边界，业务模块不能依赖 OpenAI 协议 DTO。
- `server/internal/proxy` 使用协议中立 contract 编排业务，不直接操作 `http.ResponseWriter`，也不依赖具体 Store 实现。
- `money` 和 `crypto` 是叶子能力包，不得依赖 `store`、`httpapi` 或业务模块。禁止各业务模块或 Store 重复实现金额、加密、密码哈希和哈希逻辑；`store/postgres` 不得 import `money` 或 `crypto`（由架构测试强制）。
- 会话与注册安全变量 `SESSION_TTL_SECONDS`、`SESSION_COOKIE_SECURE`、`REGISTRATION_ENABLED`、`BCRYPT_COST` 由 `server/internal/config` 解析，缺省值集中在 `config.go`；这些安全变量非法时 `config.Load` 返回错误使进程启动失败，而历史非安全变量仍保持静默回退。密码哈希成本范围以 `crypto.MinPasswordCost`/`crypto.MaxPasswordCost` 为准。
- 自助账户认证与会话归 `server/internal/accounts`：`/admin/auth/{register,login,logout,me}` 与 `sessions` 表（只存 token 哈希），Cookie 名为 `llmgateway_session`；`server/internal/httpapi/session.go` 的 `requireSession` 对 `/admin`（除 `/admin/auth/*`）强制会话并注入 `httpcommon.Identity`，业务模块通过 `server/internal/httpcommon.IdentityFrom` 读取当前用户。同源部署不返回 CORS 头。
- `/admin` 接口统一返回 `{code,message,data}`，列表统一返回 `{list,total}`。时间使用 RFC3339，自然日使用 `YYYY-MM-DD`。
- `/v1` 尽量保持 OpenAI 兼容；认证使用 `Authorization: Bearer <gateway-key>`，错误响应保持稳定、可识别。
- 金额禁止使用 `float64` 参与计算。数据库使用 `NUMERIC`，Go 使用定点整数，对外使用字符串。
- 渠道 API Key 必须加密存储；网关 Key 只存哈希，明文只在创建或重置时返回一次。响应、日志和错误信息不得泄露密钥、Token、数据库密码或其他敏感配置。
- 修改 SQL 查询或 schema 后运行 `sqlc generate`；迁移只能新增，不修改已发布迁移的既有语义。
- Dashboard 由独立 nginx 托管，默认通过同源相对路径 `/admin` 访问管理端；nginx 将 `/admin`、`/v1` 和 `/healthz` 反向代理到 Go 网关。如增加管理端认证、修改响应结构或接口路径，必须同步修改前端数据层和 nginx 配置。
- Dashboard 启动会并发请求多个管理端接口，任一失败都会进入错误页。修改启动接口时必须同时验证 `/admin/stats/overview`、`/admin/stats/daily`、`/admin/channels`、`/admin/stats/channels`、`/admin/usage-logs`、`/admin/users`、`/admin/rate-limits`、`/admin/models`、`/admin/quota-policies` 和 `/admin/quota-usage`。
- 前端当前使用带 JSON 请求体的 `DELETE /admin/pricing`；除非同步修改前端，否则后端必须保持兼容。
- 修改前检查工作区状态，不覆盖或回退他人改动，不做无关的大范围重排或格式化。提交应聚焦一个目标，不得提交 `communication/`、本地密钥或其他敏感文件。
- 后端变更至少执行 `go test ./...`、`go build ./...`、`go vet ./...` 和 `gofmt -l cmd internal`。PostgreSQL 集成测试使用 `TEST_DATABASE_URL`；未设置时测试会跳过，不能据此宣称 PostgreSQL 路径已验证。

## 注释规则

- 注释解释设计原因、边界条件、事务语义、并发约束、安全要求或不直观的兼容行为，不复述代码表面动作。
- 公共 Go 标识符仅在名称和类型不足以表达契约时补充简洁注释；注释应以标识符名称开头并描述调用方需要知道的行为。
- 复杂业务流程只在关键决策点加注释，例如重试条件、熔断状态转换、计费舍入、结算原子性和 best-effort 行为。不要逐行注释。
- 已知限制必须说明影响和后续条件，优先关联对应 Issue，例如 `TODO(#56): ...`。禁止使用没有责任边界的 `TODO`、`FIXME` 或“临时处理”。
- 协议兼容代码应注明兼容对象和不能简化的原因，例如 OpenAI SSE 帧语义或前端 `DELETE` 请求体约定。
- 安全相关注释不得包含真实密钥、Token、连接串或可用凭据；示例统一使用明显的占位值。
- SQL 注释说明查询意图、锁或事务要求，不重复字段名。迁移注释说明不可逆操作和数据兼容风险。
- 前端注释只说明数据来源、派生口径、浏览器兼容或非显然交互，不为普通 DOM 操作添加说明。
- 不在 `server/internal/db/sqlc/` 生成文件中添加或修改注释；应修改源 SQL 后重新生成。
- 移动或重构代码时同步更新失效注释。与实现不一致的注释视为缺陷，不保留历史描述。
