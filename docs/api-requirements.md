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
- `code != 0` 表示业务失败，通常等于 HTTP 状态码。
- 失败响应可携带稳定、机器可读的 `error_code`；前端优先按 `error_code` 展示本地化文案，缺失时回退到 `message`。`message` 仅作技术兜底，不保证稳定。
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
GET /admin/stats/ttft
GET /admin/stats/usage
GET /admin/channels
GET /admin/stats/channels
GET /admin/usage-logs
GET /admin/profile
GET /admin/keys
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

注册成功即建立会话（注册即登录）并通过 `Set-Cookie` 下发会话。用户名需 3–64 字符且不含空白，密码需 8–72 字节。失败时 HTTP 状态与响应 `error_code` 对应如下（`message` 仅为技术回退文案，前端按 `error_code` 本地化）：

| 场景 | HTTP | error_code |
|---|---|---|
| 用户名已存在 | 409 | `username_taken` |
| 用户名长度非法 | 400 | `username_length` |
| 用户名含空白 | 400 | `username_whitespace` |
| 密码长度非法 | 400 | `password_length` |
| 已关闭注册 | 403 | `registration_disabled` |

### POST /admin/auth/login

请求体同注册。响应通过 `error_code` 区分失败：用户名不存在返回 401 `username_not_found`，密码错误返回 401 `wrong_password`。

> 登录区分会允许攻击者枚举已注册用户名。开启注册时 `username_taken` 本身也会暴露该信息；若部署在 `REGISTRATION_ENABLED=false` 且需要抗枚举，应在网关入口统一失败文案。

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

统计消耗统一采用已结算口径，适用于 overview、daily、channels 和 usage：

- `actual_tokens` 汇总 `status=success` 和已结算 `partial_actual_*` 的上游确认 usage（排除 `pricing_error` / `settlement_failed` 后缀）；`total_tokens = actual_tokens`。`estimated_tokens` 独立汇总所有 `status!=success` 且 `error_code=partial_estimated_*` 的本地估算审计计数，包括诊断后缀，不参与 total 的不变量。所有 `partial_estimated_*` 均未结算，只记录零费用审计，不计费用、quota used、限流 Token 消耗或渠道余额扣减。
- `total_cost` 与 Token 使用同一已结算集合；daily 的 `input_tokens`、`output_tokens`、`cached_input_tokens` 也只汇总该集合。输入包含缓存子集，不重复累加缓存 Token。
- `pricing_error`、`settlement_failed` 及以这两者结尾的 `partial_actual_*` / `partial_estimated_*` 日志仅用于审计，不计入 Token 或费用消耗；日志详情即使保留上游 usage，也不代表结算成功。
- `request_count`、`success_count`、`error_count` 仍按日志状态计数；已部分结算的日志保持 `status=error`，因此错误请求也可能有消耗。
- stats 按完成时写入日志的 `created_at` 归属时间；overview、channels 的 RFC3339 范围为半开区间 `[start_time,end_time)`。不要将该规则泛化到日志列表或 TTFT 的结束边界。
- 历史记录不会自动补结算、修复或重新分类，本次口径调整不创建迁移或补数据。quota 按请求开始时预留的 UTC 桶和当时启用的 policy 记账，stats 按完成时间聚合；跨日/月、策略创建/删除或未启用策略等情况下，两者不能保证绝对相等。

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
  "actual_tokens": 0,
  "estimated_tokens": 0,
  "total_cost": "0.000000",
  "active_key_count": 0
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
      "actual_tokens": 0,
      "estimated_tokens": 0,
      "input_tokens": 0,
      "output_tokens": 0,
      "cached_input_tokens": 0,
      "total_cost": "0.000000"
    }
  ],
  "total": 1
}
```

`input_tokens` 为 prompt token 总量(含缓存命中部分),`cached_input_tokens` 是其中子集;缓存命中率 = `cached_input_tokens / input_tokens`。

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
      "actual_tokens": 0,
      "estimated_tokens": 0,
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

### POST /admin/channels/:id/health/reset

仅解除当前用户指定渠道的熔断：在同一事务内清除该渠道的 half-open 探测租约和健康窗口 buckets，并将已有 `channel_health` 的 `state` 设为 `closed`、`consecutive_failures` 设为 `0`、`opened_at` 设为 `NULL`、`updated_at` 更新为当前时间。保留健康记录及累计 `success_count`、`failure_count`，不影响其他渠道或用户熔断策略。健康记录不存在时不创建记录，重复调用幂等；非本人渠道或渠道不存在返回 404。

### GET /admin/breaker-config

读取当前用户的生效熔断策略。每个用户只有一套策略，统一应用于本人所有渠道（含新建渠道）；解析顺序仅为进程默认值 → 用户策略，未配置用户策略时返回进程默认值。不同渠道的窗口计数、连续失败计数、健康状态与 half-open 探测租约各自独立，不跨渠道累计或共享。

渠道级熔断配置接口已移除。旧 `channel_breaker_configs` 表由迁移直接 DROP，旧渠道策略直接废弃，不合并或迁入用户策略。

返回字段：

```json
{
  "window_seconds": 60,
  "minimum_samples": 10,
  "error_rate_percent": 50,
  "timeout_rate_percent": 50,
  "cooldown_seconds": 30
}
```

### PUT /admin/breaker-config

写入或替换当前用户唯一的熔断策略，统一应用于本人所有渠道；不会将不同渠道的计数、状态或探测租约合并。

请求体：

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
- 连续失败阈值（`CHANNEL_BREAKER_FAILURE_THRESHOLD`）仅使用进程默认值，不可按用户覆盖。

### DELETE /admin/breaker-config

删除当前用户的熔断策略，本人所有渠道恢复使用进程默认值；各渠道计数、状态与探测租约仍独立。

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
  "model_name": "gpt-4o-mini",
  "enabled": true
}
```

`model_name` 可省略（保持现有对外模型名）；提供时重命名对外别名，同一渠道内不允许重复。上游模型名由上游设置，创建后不可修改；定价按上游模型绑定，因此改别名不影响价格。

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

## 我的资料与 Key

用户通过会话只访问自身资料与网关 Key；跨用户访问返回 404。

### GET /admin/profile

返回当前账户：

```json
{ "code": 0, "message": "ok", "data": { "id": 1, "username": "alice", "nickname": "Alice" } }
```

### PUT /admin/profile

请求体：

```json
{ "nickname": "Alice", "current_password": "old-password", "new_password": "new-password" }
```

- `nickname`、`new_password` 均可选；只传需要的字段。
- 设置 `new_password` 时必须提供匹配的 `current_password`，否则返回 401；新密码需 8–72 字节。

### GET /admin/keys

只返回当前用户的 Key，响应为 `{list,total}`；每项包含 `id`、`key_name`、`prefix`、`key_suffix`、`is_active`、`created_at`、`last_used_at`、`expires_at`。`created_at` 为持久化的创建时间（RFC3339），不会随使用或状态更新改变；`key_suffix` 用于掩码展示，不返回完整密钥。

### POST /admin/keys

创建自身网关 Key，明文只在响应 `full_key` 返回一次。请求体为 `KeyCreateInput`（`key_name`、`prefix`、可选 `permissions`、`rate_limit_overrides`、`expires_at`、`is_active`）。

### PUT /admin/keys/:id

切换自身 Key 的 `is_active`。

### DELETE /admin/keys/:id

删除自身 Key；不存在或不属于本人返回 404。

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
user
api_key
model
channel
```

支持的 `metric`：

```text
rpm
tpm
concurrency
```

> 限流只覆盖短窗口速率控制：`rpm`、`tpm` 固定按 **60 秒**统计（`rpm` 为每分钟请求数、`tpm` 为每分钟 token 数），`concurrency` 按当前活跃请求数瞬时判定。不再提供 `window_seconds` 字段。
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
  "action": "reject",
  "priority": 100,
  "enabled": true,
  "extras": {}
}
```

### PUT /admin/rate-limits/:id

仅切换规则启停状态，路径和 PUT 方法保持不变。请求体只允许必填布尔字段 `enabled`，`false` 是有效值；缺失、`null`、非布尔值或包含其他字段返回 400。规则不存在或不属于当前用户返回 404。成功返回完整规则，其他字段保持不变，仅更新 `enabled` 和 `updated_at`：

```json
{
  "enabled": false
}
```

### DELETE /admin/rate-limits/:id

删除限流规则。

## 计费定价

定价按**上游真实模型**绑定(唯一键 `(channel_id, upstream_model)`),同一渠道内多个对外别名映射到同一上游模型时共享一条定价。上游模型名由上游设置、网关不可修改,因此改对外别名不会影响价格。渠道未配置对应上游模型的映射时,定价创建返回 400。

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
  "upstream_model": "gpt-4o-mini",
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
  "upstream_model": "gpt-4o-mini"
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

对 `usage_logs` 执行 PostgreSQL 实时聚合。必须提供 `group_by=user|api_key|model|channel`，返回 `{list,total}`；每项包含对应维度 ID 或模型名，以及 `request_count`、`success_count`、`error_count`、`total_tokens`、`actual_tokens`、`estimated_tokens`、字符串 `total_cost` 和 `duration_ms`。

可选过滤：`user_id`、`api_key_id`、`channel_id`、`model`、`status`。`api_key_id` 是日志保存的内部数值 ID，仅接受正整数；不能传递 Gateway Key 明文、前缀或哈希。支持分页。

时间范围只能使用一组：`start_time`/`end_time`（RFC3339 UTC，半开区间 `[start_time,end_time)`），或 `date_from`/`date_to`（`YYYY-MM-DD`，按 UTC 自然日，含 `date_to`）。两组同时传递返回 `400`。空结果返回 `{"list":[],"total":0}`。

### GET /admin/stats/ttft

按流式请求的首个有效 JSON `data:` 帧聚合首 Token 延迟。非流式请求的 `ttft_ms` 固定为 `null`，不伪造延迟且不参与本接口统计。即使流式请求最终取消、超时或上游中断，已经收到有效数据帧时仍会记录其 TTFT。

计时从 `ChatCompletions` 内预检开始（request_id 生成后、Token 估算前）到首个合法 JSON data 帧，包含估算、限流、路由、配额和上游连接/等待，不包含 HTTP 认证、读取请求 body 和首帧下游写出。该本地口径不能直接与上游自身 TTFT 比较。`LOG_LEVEL=DEBUG` 并重启可启用 stderr JSON `proxy_latency` 分段日志：流式在首帧 emit 时立即记录，非流式在完整 response headers 返回时记录；安全字段、单调时钟及开启命令见 `docs/backend-structure.md`。日志不改变本接口或非流式 `ttft_ms=null` 的约定。

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

- `POST /v1/chat/completions` 的请求体上限为 32 MiB（33554432 字节），与 nginx `/v1` 入口一致；允许范围内完整读取，不截断。Go 入口超限返回 HTTP 413，OpenAI 错误的 `code` 和 `type` 均为 `request_body_too_large`，消息明确说明字节上限；nginx 提前拒绝的超限请求使用其默认 413 响应。该字节限制与模型上下文 token 限制、TPM 和业务配额独立。
- 校验网关 Key 是否存在、启用、未过期；不再校验用户状态或余额。
- 只使用该 Key 所属用户自己的渠道与限流规则。
- 按模型映射选择可用渠道。
- 按渠道 `priority`、`weight`、余额、状态进行路由；同一 API Key + public model 在健康候选未变化时保持同一首选渠道，首选渠道失败时仍允许本次请求故障切换。可用 `CHANNEL_MIN_ROUTE_BALANCE` 设置全局渠道余额预留，余额低于该值的计费渠道不参与路由，未设置余额的渠道不受影响。
- 请求上游并透传 OpenAI 风格响应。
- `stream=true` 返回 `text/event-stream`，按 SSE 事件持续 flush，并保持 OpenAI `data:` 与 `[DONE]` 语义。
- 流式请求会强制向上游设置 `stream_options.include_usage=true`；首个合法 JSON `data:` 帧记录 `ttft_ms`。SSE 空帧、心跳和注释不会被计为首个 token；非流式请求保持 `ttft_ms=null`。
- 上游可切换故障仅包括传输错误、429、401/402/403 和 5xx；400/404/409/422 等调用方错误保持透传。请求级配额预留只执行一次，只有最终候选按成功或部分结算口径记账；流式响应收到 2xx 后不再切换渠道。
- RPM/TPM/concurrency 限流拒绝统一返回 OpenAI `rate_limit_exceeded` 错误；Token 预检采用保守 `InputTokens` 加最大输出预留的估算，无法解析请求 Token 时对 Token 规则稳定拒绝。只加载该 Key 所属用户的规则（`target_type` 为 `user`/`api_key`/`model`/`channel`，不含 `global`）；Key override 仅覆盖 `api_key` 级对应规则。
- 请求总 timeout 默认 600 秒（`UPSTREAM_REQUEST_TIMEOUT`，兼容旧 `UPSTREAM_TIMEOUT_SECONDS`）；所有候选尝试和流式读取共享 deadline。quota TTL 至少为 timeout + 60 秒，默认 660 秒，低于下限会自动提升。普通与 TLS 示例 nginx 的 `/v1` 均关闭缓冲，读写等待时限（`proxy_read_timeout` / `proxy_send_timeout`）均为 600 秒，不是请求总时限。
- 请求级预检只执行一次 Token estimate，限流和配额复用该结果；所属用户的规则列表每请求只加载一次，候选尝试共享 snapshot。路由使用候选携带的健康状态 snapshot，避免逐渠道健康查询；half-open 仍需原子获取单飞 probe lease，不能仅凭 snapshot 放行。
- Token 估算区分准入预留与本地审计：`InputTokens` 是保守预留量，至少采用完整请求 JSON 字节数 + 16 的上界，再加媒体预算；`PromptTokens` 是完整请求 JSON 的 tokenizer estimate + 16 和媒体预算，不使用字节上界。未知模型回退到 `cl100k_base`；每个 `image_url` / `input_audio` 标记加 8192 Token。输出按 `max_completion_tokens > max_tokens > QUOTA_DEFAULT_MAX_TOKENS` 并乘 `n` 预留；准入使用 `InputTokens + OutputTokens`，不能把保守预留直接当作 prompt 消耗。
- 流式正常成功必须同时收到 usage 与 `[DONE]`，随后按上游实际 usage 一次原子结算。中途断流、超时、缺 `[DONE]`、协议错误或客户端断开时，只要已经收到 usage，就优先按该实际用量部分结算，即使没有成功写出文本；日志以 `partial_actual_*` 标识，保持 `status=error`。
- 只有异常结束且没有上游 usage 时，才对确认成功写入下游的文本 delta 本地 tokenizer 估算 completion Token，并与合理的 `PromptTokens` estimate 一起记录零费用审计；日志以 `partial_estimated_*` 标识，保持 `status=error`，不结算、不计消耗。没有上游 usage 时释放 quota 与限流 reservation，不将估算转为 used 或扣减渠道余额。没有确认写出的文本或本地估算失败时不扣费；收到 `[DONE]` 但缺 usage 的正常解析结束仍报 `upstream_usage_missing`，不使用估算补齐。客户端取消及时传播到上游，但定价与结算使用独立、有限时的 context，避免取消直接中止记账。
- 定价或结算失败记录的 `pricing_error` / `settlement_failed`（含 `partial_actual_*`、`partial_estimated_*` 前缀）仅用于审计，不计入已结算消耗；结算事务失败回滚 quota、渠道余额和用量日志的变化。
- 记录 `usage_logs`。
- 按 `model_pricing` 计算费用并近似扣减渠道余额（不向用户计费）。
- 执行该用户的限流规则。
- 在调用上游前执行用户与 API Key 的 UTC 日/月 token、费用配额预留；任一配额不足返回 `429 insufficient_quota`。

## 周期配额

配额策略与 `rate_limit_rules` 独立。速率规则控制短窗口请求速度，`quota_policies` 控制业务预算。

```text
GET    /admin/quota-policies
POST   /admin/quota-policies
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

- 策略管理仅支持列表、创建与删除，不支持更新；已认证请求 `PUT /admin/quota-policies/:id` 返回 `405 method not allowed`。
- `scope_type` 为 `user` 或 `api_key`；同一 scope 的日/月策略各最多一条。
- `period_type` 为 `day` 或 `month`，全部按 UTC 自然周期和 `[start,end)` 边界计算。
- `token_limit`、`cost_limit` 至少提供一个；金额始终使用字符串。
- 用户与 Key 的所有启用策略必须同时满足，不存在 Key 覆盖用户配额的语义。请求开始阶段预留时确定当时启用的 policy 和 UTC bucket，结算沿用 reservation 中保存的桶，不按完成时间重新选桶；无匹配启用策略时不创建配额占用，也不追溯记账。
- quota 仍按保守估算预留；只有上游确认 usage 才将 reserved 转为 used，没有 usage 则释放 reservation。释放的是准入预留，不存在“释放 usage”；usage log 是审计事实。本地估算仅零费用审计。quota 的 `used_*` 与 stats 使用同一结算用量来源，但归桶时间和策略适用范围不同；跨周期请求、策略生命周期变化和历史未结算错误日志会造成差异，不能承诺任意范围内 quota 与 stats 绝对相等。
- `GET /admin/quota-usage` 返回当前 bucket 的 `used_tokens`、`reserved_tokens`、`used_cost`、`reserved_cost` 和周期边界；`period_start`、`period_end` 使用 UTC RFC3339 时间戳，不是 `YYYY-MM-DD` 日期。

## 前端相关注意事项

- `dashboard-react/src/api/client.ts` 中所有管理端读写操作都会请求同源 `/admin` 并解析 `{code,message,data}`。
- `/admin` 当前前端文案说明“不设认证”，如果后端加入认证，需要同步修改前端请求头逻辑。
- 文档页加载仓库中的后端结构和 API 需求文档；如果静态文档未复制到 Dashboard 构建产物，文档页会提示加载失败。
