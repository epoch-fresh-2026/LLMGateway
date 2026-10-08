# Vercel 完整网关部署

仓库根目录的 `vercel.json` 将 Vite Dashboard 和现有 Go Dockerfile 配置为两个 Vercel Services。`/admin`、`/v1` 和 `/healthz` 路由到 Go；其余路径交给前端，刷新 SPA 页面时回退到 `index.html`。前端继续使用同源 Cookie 和相对 API 路径。

## 创建项目和 PostgreSQL

在仓库根目录执行；首次登录及 Neon 服务条款确认需要在浏览器完成：

```powershell
npx vercel@63.1.0 link --yes --project llmgateway
npx vercel@63.1.0 integration add neon --name llmgateway-postgres --plan free_v3 --metadata region=sin1 --metadata auth=false --environment production --no-env-pull
```

Neon 集成自动注入 `DATABASE_URL` 和 `DATABASE_URL_UNPOOLED`。本后端启动迁移使用 tern 的会话级 advisory lock，因此 production 的 `DATABASE_URL` 必须使用 `DATABASE_URL_UNPOOLED` 的直连值，并在查询参数中追加 `pool_max_conns=4&connect_timeout=15`。不要用带 `-pooler` 的地址执行此启动迁移：Neon 的事务连接池不支持会话级 advisory lock。每个实例仍由 pgxpool 管理最多 4 个连接。

仅将此数据库连接到 production；预览部署必须使用独立数据库及独立加密密钥，避免迁移或测试影响生产数据。Go 后端仍使用 pgx/sqlc，启动时执行 `server/db/migrations` 中的迁移，无需 Neon SDK。

## 生产环境变量

在 Vercel 项目 Settings → Environment Variables 中设置以下 production 变量：

| 名称 | 值 |
| --- | --- |
| `DATABASE_URL` | Neon 直连 PostgreSQL 连接串，附加上述 pgxpool 参数，以 Secret 保存 |
| `CHANNEL_KEY_ENCRYPTION_KEY` | 随机生成的 32 个 ASCII 字节，以 Secret 保存 |
| `PORT` | `8080` |
| `ADDR` | `:8080` |
| `MIGRATIONS_DIR` | `/app/migrations` |
| `UPSTREAM_REQUEST_TIMEOUT` | `240` |
| `QUOTA_RESERVATION_TTL_SECONDS` | `360` |
| `SESSION_COOKIE_SECURE` | `true` |
| `REGISTRATION_ENABLED` | `true`，可按需关闭公开注册 |

加密密钥必须长期保留；已有渠道数据时更换会导致渠道密钥无法解密。不要使用 `VITE_` 前缀、提交密钥或把连接串写入部署配置。`.vercel/`、`.env*` 和本地协作文件均被 Git 及部署上传忽略。

```powershell
npx vercel@63.1.0 deploy --prod --yes
```

部署完成后检查 `/healthz`、注册、登录、Dashboard 全部启动接口，以及真实上游的普通和 SSE 请求。没有配置自己的上游渠道和网关 Key 前，`/v1` 调用不能生成模型响应。

## 平台限制

- 使用 Container Images Beta，实际可用性以项目的部署结果为准。前后端与 PostgreSQL 均选择新加坡区域。
- 后端配置最大执行时间 300 秒，上游超时 240 秒，给数据库结算和响应收尾留出余量。原本的 600 秒请求不适用于此配置。
- Vercel Functions 的请求/响应体上限为 4.5 MB；平台可能在 Go 的 32 MiB 校验之前拒绝大请求。大型多模态请求应使用其他容器托管环境。
- 无流量时容器会缩容，后台清理任务不会持续运行。过期会话在认证查询中排除，过期限流预留不计入计数，配额预留在新请求中惰性回收；后台批量清理在容器运行时恢复。此部署不保证无人访问期间的定时清理。
- Neon 免费数据库和 Vercel Hobby 有各自资源配额与冷启动。超出免费额度或需要更长请求时，应先核对价格和计划限制再升级。

官方参考：[Services](https://vercel.com/docs/services)、[容器](https://vercel.com/docs/functions/container-images)、[Functions 限制](https://vercel.com/docs/functions/limitations)。
