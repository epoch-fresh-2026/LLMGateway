# 测试覆盖率

验收口径是前后端手写运行时代码的**语句覆盖率精确 100%**：已覆盖语句数必须等于总语句数，未覆盖语句数必须为零。前端同时报告行、函数和分支覆盖率；分支不设 100% 门槛。

2026-10-08 本地验收结果：

| 范围 | 已覆盖 / 总语句 | 运行时文件 | 未覆盖语句 |
| --- | --- | --- | --- |
| 后端（Windows、Linux + race） | 3578 / 3578 | 71 | 0 |
| 前端 | 1075 / 1075 | 30 | 0 |

前端行、函数覆盖率均为 100%，分支为 86.23%；55 个 Vitest 测试与 7 个 Node 契约检查通过。后端真实 PostgreSQL 测试、Linux 竞态检测、build、vet、gofmt，前端 API 契约、类型检查与生产构建均通过。CI 配置已接入同一门禁命令；本地验收不代表 GitHub 已触发运行。

## 统计范围

- 后端包含 `server/cmd/llmgateway` 入口、迁移 runner、HTTP、业务模块和 PostgreSQL Store。排除 `server/internal/db/sqlc` 生成代码及 `server/internal/testutil` 测试支持代码。测试文件不属于生产分母。
- 前端包含 `dashboard-react/src` 全部有运行时输出的 TS/TSX，包括 `main.tsx` 和手写的 `api/generated/client.ts`。排除生成的 `api/generated/schema.ts`、声明文件和纯类型文件；测试/setup 在 `tests` 中，不属于生产分母。
- Go 和 Istanbul 的语句划分方式不同，因此分别检查两个分母，不合成一个百分比。Go 使用当前平台的构建文件清单；CI 在 Linux 上验证。
- 不使用 coverage ignore 注释，不 mock 生产模块。依赖通过业务窄端口、数据库查询故障夹具、真实 PostgreSQL、MSW 和生命周期依赖替身验证。

## 后端

Go 版本以 `server/go.mod` 为准。`TEST_DATABASE_URL` 必须指向可销毁的独立 PostgreSQL 数据库；集成测试会迁移和清空业务表，不能连接部署数据库。仅运行未设置该变量的 `go test` 时，数据库测试可能跳过，不能据此认定 PostgreSQL 已验证。

在 `server` 目录执行（PowerShell）：

```powershell
$env:TEST_DATABASE_URL = 'postgres://<test-user>:<test-password>@127.0.0.1:<test-port>/<test-database>?sslmode=disable'
go run ../scripts/coverage.go
go test -count=1 -p=1 ./...
go build ./...
go vet ./...
gofmt -l cmd internal
go test ../scripts/coverage.go ../scripts/coverage_test.go
```

Linux/macOS 使用 `export TEST_DATABASE_URL='...'` 设置相同变量。测试数据库由调用方或 CI PostgreSQL 服务提供，runner 不创建或删除数据库。

`coverage.go` 每次重新运行测试，使用 `-coverpkg` 纳入没有自带测试的生产包。数据库夹具在包间顺序执行。它按文件和完整代码块位置合并跨测试二进制重复记录，覆盖取执行结果的并集，避免重复计算分母；并检查有可执行代码的源文件都出现在 profile 中。

输出到 `server/coverage/backend.out`、`backend.out.json` 和 `backend.out.html`。JSON 含精确计数、逐文件计数、源码清单和遗漏代码块。无数据库配置、源码缺失、零分母或任意未覆盖语句都会返回非零退出码。报告目录已加入 gitignore。

支持 `-profile <path>` 指定报告路径。具备 C 编译器的环境可运行 `go run ../scripts/coverage.go -race`，或另行执行 `go test -race -count=1 -p=1 ./...`。

## 前端

在 `dashboard-react` 目录执行：

```sh
npm ci
npm run api:check
npm run typecheck
npm run test:coverage
npm run build
```

`test:coverage` 包含保留的 Node 源码契约检查和 Vitest 实际源码行为测试。Istanbul 纳入所有运行时源文件，`scripts/check-coverage.mjs` 独立核对源码清单并逐条检查语句命中数。未导入源文件也不能漏出统计。

输出到 `dashboard-react/coverage`：`coverage-final.json`、`coverage-summary.json` 和 HTML。该目录已加入 gitignore。

## CI

后端 PostgreSQL job 执行严格覆盖率命令及竞态检测；Dashboard workflow 执行 API 契约、类型、严格覆盖率和生产构建。两个 workflow 均上传机器可读和 HTML 覆盖率报告，便于检查失败位置。普通无数据库的后端 job 继续运行 build、vet、gofmt、单元测试与 sqlc 再生成检查。
