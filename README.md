# LLMGateway

用户自助的多渠道 LLM 聚合网关：任何人可以注册账户，配置自己的上游渠道与网关 Key，通过 OpenAI 兼容的 `/v1` 接口调用；渠道完全私有，不存在平台计费或超级管理员。

## 本地部署

前置：Docker Compose。

1. 复制环境文件并填写两个必填密钥：

   ```powershell
   Copy-Item deployments/.env.example deployments/.env
   ```

    - `POSTGRES_PASSWORD`：数据库密码，使用足够长的随机值。
    - `CHANNEL_KEY_ENCRYPTION_KEY`：恰好 16、24 或 32 个 ASCII 字节；用于加密上游渠道密钥，保存渠道后不可再修改，否则无法解密已有渠道密钥。

   以下会话/安全变量可选（缺省值见 `deployments/.env.example`）：

    - `SESSION_TTL_SECONDS`：登录会话有效期，默认 604800（7 天）。
    - `SESSION_COOKIE_SECURE`：默认 `true`。**纯 HTTP 本地部署必须设为 `false`**，否则浏览器不会保存会话 Cookie。
    - `REGISTRATION_ENABLED`：默认 `true`；设为 `false` 可关闭公开注册。
    - `BCRYPT_COST`：密码哈希成本，默认 10，范围 4–31。

2. 在仓库根目录启动：

   ```powershell
   docker compose -f deployments/docker-compose.yml up -d --build
   ```

   Compose 会分别构建 Go API 镜像和 nginx 前端镜像；Node.js 只在前端镜像构建阶段使用。

3. 打开 `http://localhost:8080/`，注册账户后即可在控制台里创建渠道、创建网关 Key，并调用 `/v1/chat/completions`。

`/v1` 请求体上限为 32 MiB，Go 网关与 HTTP/HTTPS nginx 配置保持一致。更新此限制后需重新构建并启动 Go 与前端容器（运行上面的 `up -d --build` 命令）；若 nginx 配置通过挂载提供，更新后需执行 `nginx -t` 并重载 nginx。超过上限时返回 HTTP 413；Go 入口返回 `request_body_too_large`，nginx 提前拒绝时返回默认 413 页面。这是请求体字节限制，不代表模型上下文 token 上限。

查看日志：

```powershell
docker compose -f deployments/docker-compose.yml logs -f llmgateway
```

停止服务但保留 PostgreSQL 数据卷：

```powershell
docker compose -f deployments/docker-compose.yml down
```

### 基线重建

数据库基线仍在 `server/db/migrations/000001_init.sql` 内迭代（项目未上线，不提供升级迁移）。使用过旧版本的本地开发库必须重建后再启动：

```powershell
docker compose -f deployments/docker-compose.yml down -v
docker compose -f deployments/docker-compose.yml up -d --build
```

## 公网部署

公网部署需要 HTTPS，否则会话 Cookie 无法在浏览器中安全保存。

1. 在 `deployments/.env` 中设置 `SESSION_COOKIE_SECURE=true`（默认即为 `true`），并按需设置 `REGISTRATION_ENABLED` 控制是否开放注册。
2. 准备 TLS 证书，参考 `deployments/nginx.tls.example.conf` 启用 HTTPS：该示例将 80 端口重定向到 443，终止 TLS 后把 `/admin`、`/v1`、`/healthz` 反向代理到 Go 网关，其余路径回退到前端 SPA；请替换示例中的证书路径与 `server_name`。
3. 将示例配置作为 nginx 容器配置挂载或替换 `deployments/nginx.conf`，并把对外端口从 80 改为 443（例如 `DASHBOARD_PORT=443`）。

## React 前端开发

启动 Go 网关后，在另一个终端运行：

```powershell
npm --prefix dashboard-react install
npm --prefix dashboard-react run dev -- --host 127.0.0.1 --port 5173
```

打开：

```text
http://127.0.0.1:5173/
```

Vite 开发服务器把 `/admin`、`/v1`、`/healthz` 同源代理到 `http://localhost:8080`，因此本地开发同样需要 `SESSION_COOKIE_SECURE=false`。

类型检查、测试和生产构建：

```powershell
npm --prefix dashboard-react run typecheck
npm --prefix dashboard-react run test
npm --prefix dashboard-react run build
```

## 验证

后端（在 `server/` 目录或仓库根用 `go -C server ...`）：

```powershell
go test ./...
go build ./...
go vet ./...
gofmt -l cmd internal
```

前端：

```powershell
npm --prefix dashboard-react run api:check
npm --prefix dashboard-react run typecheck
npm --prefix dashboard-react run test
npm --prefix dashboard-react run build
```
