# API Requirements

本文档整理当前 `dashboard-react/` 前端项目对后端 API 的业务要求。可机器验证的路径、方法、请求和响应契约以仓库根目录 `contracts/openapi.yaml` 为唯一来源；本文档保留业务语义、时序和兼容性说明。后端实现时优先保证本文档中的管理端接口可用，Dashboard 才能正常启动和操作。

## 基础约定

前端 API 类型由 `contracts/openapi.yaml` 生成到 `dashboard-react/src/api/generated/schema.ts`，后端响应 DTO 仍由各业务模块持有。后端 `server/internal/httpapi/api_contract_test.go` 会验证关键 DTO 的 JSON 字段和敏感字段排除；修改接口字段时先更新 OpenAPI 契约，再重新生成前端类型并同步后端实现和页面 API 调用。

### 管理端地址

Dashboard 由 nginx 与后端保持同源，默认请求相对路径管理端：

```text
/admin
```

### 统一响应格式

所有 `/admin` 接口应返回 JSON：

```json
{
  "code": 0,
  "message": "ok",
  "data": {}
}
```

要求：

- `code = 0` 表示成功。
- `code != 0` 表示业务失败，前端会展示 `message`。
- HTTP 非 2xx 会被前端视为请求失败。
- 列表接口建议返回 `{ "list": [], "total": 0 }`。

### 分页参数

列表接口通用支持：

```text
page=1
page_size=20
```

返回示例：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "list": [],
    "total": 0
  }
}
```

### 时间格式

前端会传递 RFC3339 时间，例如：

```text
2026-09-16T10:00:00.000Z
```

自然日查询使用：

```text
YYYY-MM-DD
```

## Dashboard 启动必需接口

页面启动时会并发请求以下接口，任一接口失败都会导致 Dashboard 加载失败：

```text
GET /admin/stats/overview
GET /admin/stats/daily
GET /admin/channels
GET /admin/stats/channels
GET /admin/usage-logs
GET /admin/users
GET /admin/rate-limits
GET /admin/models
```

## 认证与会话

浏览器登录使用服务端 Session + HttpOnly Cookie：`llmgateway_session`。请求不需要额外认证头，浏览器会自动携带 Cookie。

除 `/admin/auth/*` 外，所有 `/admin` 接口都要求有效会话；未登录、会话过期或已登出统一返回 401。部署为同源，网关不返回 CORS 头，也不反射任意 Origin。

Cookie 属性为 `HttpOnly; SameSite=Lax; Path=/`；`Secure` 由 `SESSION_COOKIE_SECURE` 控制；有效期由 `SESSION_TTL_SECONDS` 控制。

### POST /admin/auth/register

请求：

```json
{ "username": "alice", "password": "password123" }
```

注册成功即建立会话（注册即登录）并通过 `Set-Cookie` 下发会话。`REGISTRATION_ENABLED=false` 时返回 403；用户名需 3–64 字符且不含空白，密码需 8–72 字节；重名或非法输入返回 400。

### POST /admin/auth/login

请求体同注册。用户名不存在或密码错误统一返回 401，不区分具体原因。

### POST /admin/auth/logout

删除当前会话并清除 Cookie；幂等，重复调用仍返回成功。

### GET /admin/auth/me

返回当前会话账户：

```json
{
  "code": 0,
  "message": "ok",
  "data": { "id": 1, "username": "alice", "nickname": "alice" }
}
```

未登录、会话过期或已登出均返回 401。

## 健康检查

### GET /healthz

用于系统设置页展示健康检查链接。

建议返回：

```json
{
  "status": "ok"
}
```

## 统计接口

### GET /admin/stats/overview

查询参数：

```text
start_time=RFC3339
end_time=RFC3339
```

返回字段：

```json
{
  "request_count": 0,
  "success_count": 0,
  "error_count": 0,
  "total_tokens": 0,
  "total_cost": "0.000000",
  "active_user_count": 0
}
```

### GET /admin/stats/daily

查询参数：

```text
date_from=YYYY-MM-DD
date_to=YYYY-MM-DD
page=1
page_size=100
```

返回字段：

```json
{
  "list": [
    {
      "stat_date": "2026-09-16",
      "request_count": 0,
      "success_count": 0,
      "error_count": 0,
      "total_tokens": 0,
      "total_cost": "0.000000"
    }
  ],
  "total": 1
}
```

### GET /admin/stats/channels

查询参数：

```text
start_time=RFC3339
end_time=RFC3339
```

返回字段：

```json
{
  "list": [
    {
      "channel_id": 1,
      "channel_name": "OpenAI",
      "request_count": 0,
      "success_count": 0,
      "error_count": 0,
      "total_tokens": 0,
      "total_cost": "0.000000"
    }
  ]
}
```

## 渠道管理

### GET /admin/channels

查询参数：

```text
page=1
page_size=20
```

返回字段：

```json
{
  "list": [
    {
      "id": 1,
      "name": "OpenAI",
      "base_url": "https://api.openai.com",
      "auth_type": "bearer",
      "status": 1,
      "weight": 100,
      "priority": 0,
      "balance": "100.000000",
      "model_count": 3
    }
  ],
  "total": 1
}
```

字段说明：

- `status`: `1` 启用，`0` 停用。
- `balance`: 可为 `null`，表示不限余额。

### POST /admin/channels

请求体：

```json
{
  "name": "OpenAI",
  "base_url": "https://api.openai.com",
  "api_key": "sk-...",
  "auth_type": "bearer",
  "priority": 0,
  "weight": 100,
  "balance": "100.000000",
  "status": 1
}
```

要求：

- 创建时 `api_key` 必填。
- `api_key` 应加密存储或安全存储，不要明文返回。

### PUT /admin/channels/:id

请求体：

```json
{
  "name": "OpenAI",
  "base_url": "https://api.openai.com",
  "api_key": "sk-optional",
  "auth_type": "bearer",
  "priority": 0,
  "weight": 100,
  "balance": "100.000000",
  "status": 1
}
```

要求：

- `api_key` 为空或缺失表示不修改。
- `balance` 为空字符串时可按后端策略解释为清空余额或不限余额。

### DELETE /admin/channels/:id

删除渠道。

前端提示语要求：删除渠道会级联删除模型映射和定价，历史用量保留。

### PUT /admin/channels/:id/status

请求体：

```json
{
  "status": 1
}
```

### PUT /admin/channels/:id/balance

请求体：

```json
{
  "balance": "100.000000",
  "delta": "-12.340000",
  "description": "manual adjustment"
}
```

要求：

- `balance` 和 `delta` 至少提供一个。
- `balance` 表示设置绝对值。
- `delta` 表示增减值，可为负数。

### GET /admin/channels/:id/breaker

读取渠道熔断配置。返回全局默认值与渠道覆盖合并后的生效值：

```json
{
  "channel_id": 1,
  "window_seconds": 60,
  "minimum_samples": 10,
  "error_rate_percent": 50,
  "timeout_rate_percent": 50,
  "cooldown_seconds": 30
}
```

### PUT /admin/channels/:id/breaker

写入渠道级熔断覆盖，覆盖全局默认值。

```json
{
  "window_seconds": 60,
  "minimum_samples": 10,
  "error_rate_percent": 50,
  "timeout_rate_percent": 50,
  "cooldown_seconds": 30
}
```

要求：

- `window_seconds`、`minimum_samples`、`cooldown_seconds` 必须为正整数。
- `error_rate_percent`、`timeout_rate_percent` 取值 1–100。
- 连续失败阈值（`CHANNEL_BREAKER_FAILURE_THRESHOLD`）不可按渠道覆盖。

### DELETE /admin/channels/:id/breaker

删除渠道覆盖，恢复继承全局默认值。

### POST /admin/channels/:id/test

请求体：

```json
{
  "check_all": true
}
```

返回字段：

```json
{
  "list": [
    {
      "model_alias": "gpt-4o-mini",
      "upstream_model": "gpt-4o-mini",
      "http_status": 200,
      "latency_ms": 320,
      "ok": true,
      "error": ""
    }
  ]
}
```

也可返回单个结果对象，前端兼容。

测试会读取渠道的 Base URL、认证配置和已启用模型映射，并对每个待检查模型真实发起 OpenAI 兼容请求：

```json
{
  "model": "<upstream_model>",
  "messages": [{"role": "user", "content": "hi"}],
  "max_tokens": 1
}
```

`check_all=true`（默认）检查全部启用模型；`false` 只检查第一个启用模型。每项使用 10 秒超时，网络错误、超时和非 2xx 响应均返回该项的 `ok=false`，不会泄露上游 Key 或响应正文。管理员手工探测不更新渠道健康状态或熔断状态。

## 渠道模型映射

### GET /admin/channels/:id/models

返回字段：

```json
{
  "list": [
    {
      "id": 1,
      "model_name": "gpt-4o-mini",
      "upstream_model": "gpt-4o-mini",
      "enabled": true
    }
  ],
  "total": 1
}
```

### POST /admin/channels/:id/models

请求体：

```json
{
  "model_name": "gpt-4o-mini",
  "upstream_model": "gpt-4o-mini",
  "enabled": true
}
```

### PUT /admin/channels/:id/models/:model_id

请求体：

```json
{
  "upstream_model": "gpt-4o-mini",
  "enabled": true
}
```

### DELETE /admin/channels/:id/models/:model_id

删除模型映射。

### POST /admin/channels/:id/remote-models

用于从上游拉取可用模型。

返回字段：

```json
{
  "ok": true,
  "models": [
    { "id": "gpt-4o-mini" },
    { "id": "gpt-4o" }
  ]
}
```

失败时：

```json
{
  "ok": false,
  "error": "upstream error"
}
```

## 模型目录

### GET /admin/models

查询参数：

```text
status=1
```

返回字段：

```json
{
  "list": [
    {
      "model_name": "gpt-4o-mini",
      "status": 1,
      "channels": [
        {
          "channel_id": 1,
          "channel_name": "OpenAI",
          "upstream_model": "gpt-4o-mini",
          "enabled": true
        }
      ]
    }
  ],
  "total": 1
}
```

## 用户管理

### GET /admin/users

查询参数：

```text
page=1
page_size=20
```

返回字段：

```json
{
  "list": [
    {
      "id": 1,
      "nickname": "Alice",
      "user_group": "default",
      "status": "active",
      "balance": {
        "available_balance": "100.000000",
        "frozen_balance": "0.000000"
      }
    }
  ],
  "total": 1
}
```

字段说明：

- `status`: 前端支持 `active` 和 `suspended`。
- `user_group`: 前端默认选项为 `default`、`vip`、`enterprise`。

### POST /admin/users

请求体：

```json
{
  "nickname": "Alice",
  "user_group": "default",
  "status": "active",
  "password": "optional"
}
```

返回字段可选：

```json
{
  "id": 1,
  "password_plaintext": "generated-password"
}
```

如果后端自动生成密码，前端会展示 `password_plaintext`。

### PUT /admin/users/:id

请求体：

```json
{
  "nickname": "Alice",
  "user_group": "vip",
  "password": "optional"
}
```

### DELETE /admin/users/:id

永久删除用户。

前端提示语要求：删除用户会同时删除其 Key、余额、资金流水、用量日志与日汇总。

### PUT /admin/users/:id/status

请求体：

```json
{
  "status": "suspended"
}
```

### POST /admin/users/:id/recharge

请求体：

```json
{
  "amount": "50.000000",
  "related_order_id": "order-optional",
  "description": "manual recharge"
}
```

返回字段：

```json
{
  "balance_after": "150.000000"
}
```

### GET /admin/users/:id/balance

返回字段：

```json
{
  "available_balance": "100.000000",
  "frozen_balance": "0.000000"
}
```

### GET /admin/users/:id/balance-transactions

查询参数：

```text
page=1
page_size=20
```

返回字段：

```json
{
  "list": [
    {
      "id": 1,
      "tx_type": "recharge",
      "amount": "50.000000",
      "balance_after": "150.000000",
      "created_at": "2026-09-16T10:00:00Z"
    }
  ],
  "total": 1
}
```

## 网关 Key 管理

### GET /admin/users/:id/keys

查询参数：

```text
page=1
page_size=50
```

返回字段：

```json
{
  "list": [
    {
      "id": 1,
      "user_id": 1,
      "key_name": "default",
      "prefix": "sk-",
      "is_active": true,
      "last_used_at": null
    }
  ],
  "total": 1
}
```

### GET /admin/keys

查询参数：

```text
page=1
page_size=50
```

返回同 Key 列表，用于全局 Key 管理表。

### POST /admin/users/:id/keys

请求体：

```json
{
  "key_name": "default",
  "prefix": "sk-",
  "permissions": {
    "models": ["*"]
  },
  "rate_limit_overrides": {
    "rpm": 600,
    "tpm": 120000
  },
  "expires_at": "2027-01-01T00:00:00Z",
  "is_active": true
}
```

返回字段：

```json
{
  "id": 1,
  "full_key": "sk-plaintext-visible-once"
}
```

要求：

- `full_key` 只在创建时返回一次。
- 后端应只存储 Key 哈希，不应存储可直接使用的明文。

### PUT /admin/users/:id/keys/:key_id

请求体：

```json
{
  "is_active": false
}
```

### DELETE /admin/users/:id/keys/:key_id

删除 Key。

### POST /admin/users/:id/keys/:key_id/reset

重置 Key。

返回字段：

```json
{
  "full_key": "sk-new-plaintext-visible-once"
}
```

## 限流规则

### GET /admin/rate-limits

查询参数：

```text
page=1
page_size=20
enabled=true
```

返回字段：

```json
{
  "list": [
    {
      "id": 1,
      "rule_name": "default user rpm",
      "target_type": "user",
      "target_value": "*",
      "metric": "rpm",
      "limit_value": 600,
      "window_seconds": 60,
      "action": "reject",
      "priority": 100,
      "enabled": true,
      "extras": {}
    }
  ],
  "total": 1
}
```

支持的 `target_type`：

```text
global
user
api_key
model
channel
```

支持的 `metric`：

```text
rpm
tpm
rpd
concurrency
```

> `tpd`（自然日 token 上限）已废弃：日 token/费用预算由配额模块（`/admin/quota-policies`）承担，具备原子预留与费用维度。

支持的 `action`：

```text
reject
```

限流不支持在网关内排队；任何超限请求均立即返回 OpenAI 风格的 `rate_limit_exceeded` 错误。已有 `queue` 规则在迁移时会转换为 `reject`。

### POST /admin/rate-limits

请求体：

```json
{
  "rule_name": "default user rpm",
  "target_type": "user",
  "target_value": "*",
  "metric": "rpm",
  "limit_value": 600,
  "window_seconds": 60,
  "action": "reject",
  "priority": 100,
  "enabled": true,
  "extras": {}
}
```

### PUT /admin/rate-limits/:id

请求体同创建接口。

前端也会只传局部字段用于启停：

```json
{
  "enabled": false
}
```

### DELETE /admin/rate-limits/:id

删除限流规则。

## 计费定价

### GET /admin/pricing

查询参数：

```text
page=1
page_size=100
```

返回字段：

```json
{
  "list": [
    {
      "id": 1,
      "channel_id": 1,
      "channel_name": "OpenAI",
      "model_name": "gpt-4o-mini",
      "upstream_model": "gpt-4o-mini",
      "input_price_per_1m": "0.15000000",
      "output_price_per_1m": "0.60000000",
      "cached_input_price_per_1m": "0.07500000",
      "currency": "USD"
    }
  ],
  "total": 1
}
```

### POST /admin/pricing

用于创建或覆盖定价。

请求体：

```json
{
  "channel_id": 1,
  "model_name": "gpt-4o-mini",
  "input_price_per_1m": "0.15000000",
  "output_price_per_1m": "0.60000000",
  "cached_input_price_per_1m": "0.07500000",
  "currency": "USD"
}
```

### DELETE /admin/pricing

当前前端会用 DELETE 请求体删除定价：

```json
{
  "channel_id": 1,
  "model_name": "gpt-4o-mini"
}
```

后端需要支持 DELETE 请求体。也可以后续调整前端为查询参数形式。

## 请求日志

### GET /admin/usage-logs

查询参数：

```text
page=1
page_size=20
user_id=1
channel_id=1
api_key_id=1
model=gpt-4o-mini
status=success
start_time=RFC3339
end_time=RFC3339
```

返回字段：

```json
{
  "list": [
    {
      "id": 1,
      "request_id": "req_abc",
      "user_id": 1,
      "api_key_id": 1,
      "channel_id": 1,
      "channel_name": "OpenAI",
      "model": "gpt-4o-mini",
      "upstream_model": "gpt-4o-mini",
      "input_tokens": 100,
      "output_tokens": 200,
      "cached_input_tokens": 0,
      "total_tokens": 300,
      "unit_price_input_per_1m": "0.15000000",
      "unit_price_output_per_1m": "0.60000000",
      "total_cost": "0.000135",
      "duration_ms": 1200,
      "ttft_ms": 300,
      "status": "success",
      "error_code": "",
      "client_ip": "127.0.0.1",
      "created_at": "2026-09-16T10:00:00Z"
    }
  ],
  "total": 1
}
```

### GET /admin/usage-logs/:id

返回单条日志对象，字段同列表项。

### GET /admin/stats/usage

对 `usage_logs` 执行 PostgreSQL 实时聚合。必须提供 `group_by=user|api_key|model|channel`，返回 `{list,total}`；每项包含对应维度 ID 或模型名，以及 `request_count`、`success_count`、`error_count`、`total_tokens`、字符串 `total_cost` 和 `duration_ms`。

可选过滤：`user_id`、`api_key_id`、`channel_id`、`model`、`status`。`api_key_id` 是日志保存的内部数值 ID，仅接受正整数；不能传递 Gateway Key 明文、前缀或哈希。支持分页。

时间范围只能使用一组：`start_time`/`end_time`（RFC3339 UTC，半开区间 `[start_time,end_time)`），或 `date_from`/`date_to`（`YYYY-MM-DD`，按 UTC 自然日，含 `date_to`）。两组同时传递返回 `400`。空结果返回 `{"list":[],"total":0}`。

### GET /admin/stats/ttft

按流式请求的首个有效 JSON `data:` 帧聚合首 Token 延迟。非流式请求的 `ttft_ms` 固定为 `null`，不伪造延迟且不参与本接口统计。即使流式请求最终取消、超时或上游中断，已经收到有效数据帧时仍会记录其 TTFT。

可选查询参数：`user_id`、`api_key_id`、`channel_id`、`model`、`start_time`（RFC3339）和 `end_time`（RFC3339）。

`sample_count` 是有 TTFT 的流式样本数；`average_ms` 为向下取整的算术平均值；`p50_ms`、`p95_ms`、`p99_ms` 采用 nearest-rank（`ceil(n * p / 100)`）口径。无样本时所有字段为 `0`。

```json
{
  "sample_count": 100,
  "average_ms": 245,
  "p50_ms": 200,
  "p95_ms": 600,
  "p99_ms": 900
}
```

## 下游 OpenAI 兼容接口

当前 Dashboard 主要依赖 `/admin`，但产品语义中要求后端提供 OpenAI 兼容下游接口。

建议至少实现：

```text
GET  /v1/models
POST /v1/chat/completions
```

认证：

```text
Authorization: Bearer <gateway-key>
```

核心行为要求：

- 校验网关 Key 是否存在、启用、未过期。
- 校验用户状态和余额。
- 按模型映射选择可用渠道。
- 按渠道 `priority`、`weight`、余额、状态进行路由；同一 API Key + public model 在健康候选未变化时保持同一首选渠道，首选渠道失败时仍允许本次请求故障切换。可用 `CHANNEL_MIN_ROUTE_BALANCE` 设置全局渠道余额预留，余额低于该值的计费渠道不参与路由，未设置余额的渠道不受影响。
- 请求上游并透传 OpenAI 风格响应。
- `stream=true` 返回 `text/event-stream`，按 SSE 事件持续 flush，并保持 OpenAI `data:` 与 `[DONE]` 语义。
- 流式请求会强制向上游设置 `stream_options.include_usage=true`；首个合法 JSON `data:` 帧记录 `ttft_ms`。SSE 空帧、心跳和注释不会被计为首个 token；非流式请求保持 `ttft_ms=null`。
- 上游可切换故障仅包括传输错误、429、401/402/403 和 5xx；400/404/409/422 等调用方错误保持透传。请求级配额预留只执行一次，只有最终成功候选结算；流式响应收到 2xx 后不再切换渠道。
- RPM/TPM/RPD/concurrency 限流拒绝统一返回 OpenAI `rate_limit_exceeded` 错误；Token 预检失败采用输入 Token 加 `max_tokens` 的保守估算，无法解析请求 Token 时对 Token 规则稳定拒绝。Key override 仅覆盖 `api_key` 级对应规则，global/user/model/channel 规则仍照常检查。
- 流式成功必须同时收到 usage 与 `[DONE]`，随后按上游实际 usage 一次原子结算。中途断流、客户端取消或流协议错误时，网关仅对已成功写入下游的文本 delta 以本地 tokenizer 估算 completion token，并连同请求 prompt 估算结算；用量日志以 `partial_estimated_*` 标识。未写出文本、缺 usage 或估算失败时不扣费；客户端取消会及时取消上游请求。
- 记录 `usage_logs`。
- 按 `model_pricing` 计算费用。
- 更新用户余额和渠道余额。
- 执行限流规则。
- 在调用上游前执行用户与 API Key 的 UTC 日/月 token、费用配额预留；任一配额不足返回 `429 insufficient_quota`。

## 周期配额

配额策略与 `rate_limit_rules` 独立。速率规则控制短窗口请求速度，`quota_policies` 控制业务预算。

```text
GET    /admin/quota-policies
POST   /admin/quota-policies
PUT    /admin/quota-policies/:id
DELETE /admin/quota-policies/:id
GET    /admin/quota-usage
```

创建示例：

```json
{
  "policy_name": "production key monthly quota",
  "scope_type": "api_key",
  "scope_id": 37,
  "period_type": "month",
  "token_limit": 10000000,
  "cost_limit": "200.000000",
  "enabled": true
}
```

要求：

- `scope_type` 为 `user` 或 `api_key`；同一 scope 的日/月策略各最多一条。
- `period_type` 为 `day` 或 `month`，全部按 UTC 自然周期和 `[start,end)` 边界计算。
- `token_limit`、`cost_limit` 至少提供一个；金额始终使用字符串。
- 用户与 Key 的所有启用策略必须同时满足，不存在 Key 覆盖用户配额的语义。
- `GET /admin/quota-usage` 返回当前 bucket 的 `used_tokens`、`reserved_tokens`、`used_cost`、`reserved_cost` 和周期边界。

## 前端相关注意事项

- `dashboard-react/src/api/client.ts` 中所有管理端读写操作都会请求同源 `/admin` 并解析 `{code,message,data}`。
- `/admin` 当前前端文案说明“不设认证”，如果后端加入认证，需要同步修改前端请求头逻辑。
- 文档页加载仓库中的后端结构和 API 需求文档；如果静态文档未复制到 Dashboard 构建产物，文档页会提示加载失败。
