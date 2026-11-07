# 贡献指南

仓库保留 `master`（发布）、`dev`（开发）和 `edge`（边缘部署变体）三个长期分支。本轮维护在 `dev` 完成；涉及边缘功能时同时验证中心与边缘的编译和互联。

## 环境和检查

使用 Go 1.27.3、Node.js 24.22.2 及锁文件依赖。升级依赖须附上安全扫描和兼容验证，不能手工修改 go.sum 或 package-lock.json。

```sh
git config core.hooksPath .githooks
cd www
npm ci
npm run lint -- --max-warnings=0
npm run typecheck
npm test
npm run build
cd ..
go test ./cmd/... ./internal/... ./pkg/... ./test/... ./tools/...
go vet ./cmd/... ./internal/... ./pkg/... ./test/... ./tools/...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run ./cmd/... ./internal/... ./pkg/... ./test/... ./tools/...
go run ./tools/openapi -check
```

pre-commit hook 检查暂存 Go 文件的 gofmt 格式、后端 vet、前端 lint/类型/单元测试，不重写部分暂存文件。Windows 通过 Git shell 运行 hook，Go 和 Node 需要在 PATH 中。完整检查由 CI 执行。

## 集成和性能测试

准备专用 MySQL、Redis、ffmpeg/ffprobe。环境变量 `DRAARL_TEST_MYSQL_DSN` 必须使用 `draarl_test_` 开头的库名，账号需要 CREATE/DROP DATABASE 权限；设置 `DRAARL_REDIS_ADDR`，按需提供 `DRAARL_REDIS_PASSWORD` 和 `DRAARL_REDIS_DB`。不要提交凭据。

```sh
go run ./tools/integration -out output/integration
go run ./tools/integration -only device-list,device-cache,online-events
go run ./tools/integration -race -only authentication,radio-routing,edge-ghost
go test -race -count=1 ./internal/udphub ./internal/interconnect ./internal/handler ./internal/gormdb ./internal/auth ./pkg/cache ./internal/protocol ./pkg/websocket
go test -run='^$' -bench='Benchmark(ForwardHeader|DecodeVoice|VerifyDevicePassword)' -benchmem ./internal/protocol ./pkg/crypto
```

runner 为每组夹具创建独立临时数据库，结束时删除，保存逐组 JSONL、覆盖率及汇总。通过数含子测试，跳过不算通过。数据库微基准需额外设置 `DRAARL_BENCH_MYSQL=true`，在新的专用库执行 `go test -run='^$' -bench=BenchmarkFilteredDeviceListMySQL -benchmem ./pkg/cache`。它会写入测试数据，用后删除该库。微基准不是容量承诺。

## 代码约定

Go 使用 gofmt、显式错误处理和请求上下文。HTTP 下游传递 `c.Request.Context()`，后台任务设超时；提交后的必要缓存清理使用独立有界上下文。锁序为设备锁到全局索引锁。异步队列拥有的报文必须复制，不能先释放到池。

未分组设备合法，SQL `devices.group_id` 用 NULL 表示，不能插入不存在的 ID（包括 0）。API 保持既有未分组表示。设备密码 AES 可逆存储和 `OwerID` 拼写均为设计特性，不在清理中修改。协议兼容 MD5/AES 的 gosec 排除写在配置中；其他抑制必须逐行说明真实边界和理由。

handler 使用 `writeErrorResponse`/`writeRepositoryError`。`code` 保留 HTTP 数字状态，`error_code` 为稳定类别，`message` 是用户提示，保留已有业务错误码。取消为 408/request_canceled、超时为 504/request_timeout、缺失为 404/resource_not_found。5xx 不包含内部 error/detail；不要在 message 拼接 SQL、路径或 err.Error()。鉴权中间件与 WS 握手保留各自协议格式。

API 路由/请求类型修改后运行 `go run ./tools/openapi`，提交生成文件。迁移遵循 [迁移与回退](docs/database-migrations.md)，前端遵循 [组件规范](docs/frontend-components.md)。测试应复现用户行为，不能只重复实现。

## 提交和审查

使用 fix:/feat:/perf:/docs:/test:/chore: 描述具体变化。提交前审阅暂存差异，排除密钥、构建产物和无关用户编辑。PR 说明触发条件、修复行为、测试范围和未验证条件；配置与数据库变更附升级、备份、回退步骤。模拟 BLE 不能写成硬件持久化验证，扫描未报告不能写成不存在漏洞。
