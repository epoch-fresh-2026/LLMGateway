# 前后端分离部署

1. 根据 `deployments/.env.example` 创建 `deployments/.env`，并替换其中的两个占位密钥。
   `CHANNEL_KEY_ENCRYPTION_KEY` 必须恰好为 16、24 或 32 个 ASCII 字节；渠道密钥已保存后，不能再修改该值，否则网关无法解密已有渠道密钥。
   会话与注册相关变量均可选：`SESSION_TTL_SECONDS`（默认 604800，即 7 天）、`SESSION_COOKIE_SECURE`（默认 `true`，纯 HTTP 本地部署需设为 `false`）、`REGISTRATION_ENABLED`（默认 `true`，设为 `false` 可关闭公开注册）、`BCRYPT_COST`（默认 10，取值 4–31）。这些变量值非法时进程会在启动阶段报错退出。
2. 在仓库根目录执行：

   ```powershell
   docker compose -f deployments/docker-compose.yml up -d --build
   ```

Docker Compose 会分别构建 Go API 镜像和 nginx 前端镜像，Node.js 只在前端镜像构建阶段使用，运行时不需要 Node.js。

3. 打开 `http://localhost:8080/` nginx 提供前端静态文件，并将 `/admin`、`/v1` 和 `/healthz` 同源反向代理到 Go 网关；网关启动时会自动执行数据库迁移。

查看网关日志：

```powershell
docker compose -f deployments/docker-compose.yml logs -f llmgateway
```

停止服务但保留 PostgreSQL 数据卷：

```powershell
docker compose -f deployments/docker-compose.yml down
```

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

类型检查、测试和生产构建：

```powershell
npm --prefix dashboard-react run typecheck
npm --prefix dashboard-react run test
npm --prefix dashboard-react run build
```
