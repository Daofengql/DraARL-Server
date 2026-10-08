# DraARL Server 审计整改计划

> 来源：`report.md`（DraARL Server `v2.0.0-alpha14` 代码审计报告）
>
> 目标：按风险优先级完成依赖漏洞修复、权限撤销闭环、凭据治理和运行时性能优化，并通过自动化测试与发布前检查。
>
> 说明：本文档是执行计划，不代表对应代码已经修改完成。每个任务完成后勾选复选框，并在“验证记录”中补充命令、版本和结果。

## 0. 执行原则

- [ ] 所有依赖升级先在独立分支完成，保留升级前的 `go.mod`、`go.sum` 和构建产物信息。
- [ ] 安全修复优先于性能重构；P0/P1 项未完成前不发布新的生产版本。
- [ ] 不把真实凭据写入代码、文档、测试数据、提交记录或 CI 日志。
- [ ] 外部凭据轮换由运维在变更窗口执行；代码仓库内只提交模板、校验和部署说明。
- [ ] 修改认证、令牌、运行时索引和 UDP 转发后，必须同时运行单元测试、集成测试和竞态检测（环境允许时）。
- [ ] 每个性能优化都保留优化前后基准数据，确认吞吐、延迟、内存和正确性没有回退。
- [ ] 不为了“修复”报告而修改已验证正确的 SQL 参数化、路径穿越防护、fan-out 队列等模块，除非新增测试发现回归。

## 1. 当前基线与目标

### 1.1 当前基线

- [ ] 记录当前工具链：`go version`、`node --version`、`npm --version`、操作系统和架构。
- [ ] 记录当前依赖状态：`go list -m all`、`go mod graph`、`www/package-lock.json` 的锁定版本。
- [ ] 保存当前验证结果：
  - [ ] `go build ./...`
  - [ ] `go vet ./internal/... ./pkg/... ./cmd/...`
  - [ ] `go test ./...`
  - [ ] `npm --prefix www audit --omit=dev`
  - [ ] `npm --prefix www run build`
  - [ ] `go run golang.org/x/vuln/cmd/govulncheck@latest ./...`
- [ ] 将 `govulncheck` 的完整输出保存到整改记录中，逐项确认“可达漏洞”的调用路径、受影响服务和修复版本。
- [ ] 复核报告中的数量口径：报告正文写“31 个可达漏洞”，标准库表按编号展开后应重新统计，不能直接沿用“19 个标准库漏洞”的描述。
- [ ] 确认 `report.md`、`todo.md` 均不包含真实密钥、密码、Cookie、JWT 或完整连接字符串。

### 1.2 完成目标

- [ ] `govulncheck ./...` 不再报告本次审计列出的可达漏洞；如仍有未修复项，必须有书面风险接受和临时缓解措施。
- [ ] 管理员禁用、删除或拒绝审批用户后，该用户的全部 refresh token 立即被吊销。
- [ ] 生产配置不再依赖工作区明文凭据，所有生产密钥通过环境变量、Docker Secret、Kubernetes Secret 或其他 secrets manager 注入。
- [ ] 重放窗口的大跨度跳跃不会执行最多 16,383 次逐 ID 清理；正常顺序流量和窗口语义保持不变。
- [ ] 运行时索引不在全局索引写锁内获取设备锁，锁序规则有测试和注释约束。
- [ ] 设备下线清理按设备反向索引定位群组，避免每次遍历全部群组；索引缺失时仍有可控的兼容回退路径。
- [ ] 邮箱验证码比较使用恒定时间比较；邮箱登录的失败防护策略经过明确评估并有测试覆盖。
- [ ] CI 在合并和发布前执行构建、测试、静态检查、依赖漏洞扫描和前端审计。

## 2. P0：依赖漏洞与工具链升级

### 2.1 升级 Go 工具链

涉及：`go.mod`、`Dockerfile`、`.github/workflows/release.yml`、新增或调整的 CI workflow、开发环境文档。

- [ ] 安装并确认 `go1.25.13` 或更高的受支持补丁版本。
- [ ] 确认本地、GitHub Actions 和 Docker backend 使用同一个 Go 补丁版本；当前 Dockerfile 使用 `golang:1.25-alpine`，需要改成明确的补丁版本或可审计的 digest。
- [ ] 评估 `go.mod` 的 `go 1.25.0` 声明是否需要同步调整；不要把补丁版本变更误写成不必要的语言版本升级。
- [ ] 检查标准库行为变化，重点回归：HTTPS/TLS、HTTP/2、XML/ASN.1、URL 查询、邮件解析、模板渲染、Windows 网络路径和 `os.Root` 相关代码。
- [ ] 在 Windows 和 Linux 构建环境分别执行 `go version`、`go build ./...` 和关键测试。

### 2.2 升级 Go 第三方依赖

涉及：`go.mod`、`go.sum`，以及升级后受影响的适配代码和测试。

- [ ] 升级 `golang.org/x/text` 至报告要求的修复版本 `v0.39.0` 或之后已确认安全的版本。
- [ ] 升级 `golang.org/x/net` 至 `v0.55.0` 或之后已确认安全的版本；重点回归 HTTP/2、Punycode、TLS 和 WebSocket 相关路径。
- [ ] 升级 `golang.org/x/image` 至 `v0.43.0` 或之后已确认安全的版本；重点回归头像上传、PNG/JPEG/GIF/TIFF/BMP 解码和图片尺寸限制。
- [ ] 升级 `github.com/quic-go/quic-go` 至 `v0.59.1` 或之后已确认安全的版本；重点回归 HTTP/3 和互联相关代码。
- [ ] 升级 `github.com/redis/go-redis/v9` 至 `v9.7.3` 或之后已确认安全的版本；重点回归 refresh token、登录保护、缓存和 Redis 连接池。
- [ ] 使用仓库支持的 Go 版本执行依赖升级和整理：
  - [ ] `go get golang.org/x/text@v0.39.0`
  - [ ] `go get golang.org/x/net@v0.55.0`
  - [ ] `go get golang.org/x/image@v0.43.0`
  - [ ] `go get github.com/quic-go/quic-go@v0.59.1`
  - [ ] `go get github.com/redis/go-redis/v9@v9.7.3`
  - [ ] `go mod tidy`
- [ ] 如果某个模块因上游兼容性、Go 版本或其他间接依赖无法直接升级，记录阻塞原因、最小可行版本和临时缓解措施，不要强行覆盖 `go.sum`。
- [ ] 检查 `go mod why` 和 `go mod graph`，确认漏洞模块的实际调用路径已被修复版本覆盖。

### 2.3 图片上传专项验证

涉及：`pkg/storage/image.go`、`pkg/storage/upload_context_test.go`、头像上传 handler 测试和必要的测试资源。

- [ ] 保留现有 `DecodeConfig`、像素上限、请求取消和上传大小限制，不因依赖升级而移除已有防护。
- [ ] 为正常 JPEG、PNG、GIF、WEBP、TIFF、BMP 增加或确认解码回归测试。
- [ ] 增加恶意/边界图片测试：畸形 TIFF IFD、越界 strip/tile offset、异常 BMP 调色板、PackBits 压缩、超大尺寸和截断文件。
- [ ] 测试要求：请求不能 panic；错误必须可控返回；内存占用受限制；请求取消后及时结束。
- [ ] 在测试中使用仓库内生成的最小二进制 fixture 或临时构造数据，禁止提交来源不明的真实恶意样本。
- [ ] 运行 `go test ./pkg/storage ./internal/handler`，必要时运行 `go test -race ./pkg/storage`。

### 2.4 Redis、TLS、HTTP/2 与互联专项验证

- [ ] 验证 refresh token 创建、读取、轮换、重放检测、按用户全部吊销和过期清理。
- [ ] 验证 Redis 超时、连接重连、并发请求和响应顺序；确认 `go-redis` 升级没有改变错误处理和 fail-closed 行为。
- [ ] 验证 HTTPS 服务端、HTTP/2、WebSocket、节点 TLS 控制面、节点数据面和连接超时。
- [ ] 验证 QUIC/HTTP3（如果当前构建或部署启用）以及 QPACK trailer 异常输入。
- [ ] 运行完整验证：`go test ./...`、`go vet ./...`、`go build ./...`、`govulncheck ./...`。

## 3. P1：权限剥夺时立即吊销 refresh token

涉及：`internal/handler/session_security.go`、`internal/handler/auth_user_admin.go`、`internal/handler/upload.go`、认证存储实现和 handler 测试。

### 3.1 统一吊销入口

- [ ] 保留并复用 `revokeUserRefreshSessions(userID, reason)`，避免在 handler 中直接操作 Redis 或内存存储。
- [ ] 确认该入口在 Redis 存储和 development/test 内存存储上行为一致。
- [ ] 确认吊销失败的日志包含用户 ID、原因和错误，但不包含 refresh token 原文。
- [ ] 评估管理员操作的错误策略：用户状态/删除操作已经提交时，吊销失败应进入可观测的补偿流程，不应返回“操作未发生”的误导信息。
- [ ] 必要时为吊销失败增加计数器、结构化日志或告警，方便运维在 Redis 短暂故障后补偿。

### 3.2 禁用用户

位置：`internal/handler/auth_user_admin.go` 的 `UpdateUserStatus`，当前禁用分支已经执行 ghost session 和路由撤销。

- [ ] 在 `repo.UpdateUserStatus` 成功后、返回成功响应前调用 `revokeUserRefreshSessions(id, "user_disabled")`。
- [ ] 保持现有顺序：数据库状态提交成功后执行运行时会话清理、路由撤销、refresh token 吊销和缓存失效。
- [ ] 确认重新启用用户不会恢复已吊销的旧 refresh token；用户需要重新登录。
- [ ] 测试：禁用用户后，原有 access token 被中间件拒绝，原有 refresh token 被 refresh handler 拒绝，其他用户 token 不受影响。
- [ ] 测试：禁用操作失败时不应吊销 token，避免数据库未改变但用户被强制登出。

### 3.3 删除用户

位置：`internal/handler/auth_user_admin.go` 的 `DeleteUser`，当前级联删除成功后执行 ghost session、路由和设备清理。

- [ ] 在 `DeleteUserWithCascade` 成功后调用 `revokeUserRefreshSessions(id, "user_deleted")`。
- [ ] 确认用户删除前后调用时机不会依赖已删除的用户记录；吊销使用已解析的用户 ID。
- [ ] 保持设备、群组、广播对象、缓存和路由的既有清理顺序，避免为了加入吊销逻辑引入事务边界变化。
- [ ] 测试：删除用户后全部 refresh token 均标记为吊销；删除用户之外的 token 不受影响；删除失败时旧 token 不被误吊销。
- [ ] 测试 Redis 不可用时记录可检索错误，确认既有删除语义和补偿策略符合运维要求。

### 3.4 拒绝用户审批

位置：`internal/handler/upload.go` 的 `ApproveUser`，当前 `req.Status != 1` 分支执行 `routesync.RevokeOwner`。

- [ ] 在 `UpdateUserApproval` 成功且 `req.Status != 1` 的分支调用 `revokeUserRefreshSessions(userID, "approval_rejected")`。
- [ ] 明确状态语义：仅拒绝/撤销审批时吊销；审批通过不吊销已有 token，除非产品要求“重新审批即重新认证”。
- [ ] 确认 `auth_refresh.go` 中对 `Status`、`ApprovalStatus` 的下游检查继续保留，形成源头吊销加下游防线的纵深保护。
- [ ] 测试拒绝审批后 refresh token 立即不可用；再次通过审批后旧 token 仍不可恢复。
- [ ] 测试审批更新失败时不吊销 token。

### 3.5 认证回归

- [ ] 覆盖密码修改、管理员重置密码、邮箱登录/重置、用户禁用、用户删除、审批拒绝六类令牌生命周期。
- [ ] 覆盖 Redis refresh token store 和 memory refresh token store。
- [ ] 覆盖并发 refresh 与管理员吊销：不能出现吊销后又成功轮换出新 token 的竞态；如当前存储语义无法保证，补充原子条件或明确返回错误。
- [ ] 运行：`go test ./internal/auth ./internal/handler`。
- [ ] 运行：`go test -race ./internal/auth ./internal/handler`，环境不支持时记录原因和替代验证。

## 4. P1：生产凭据治理与轮换

> 本节分为仓库内整改和外部运维操作。报告中的 `config.yaml` 未被 Git 跟踪，但工作区明文凭据仍应按已暴露处理。不要把真实值复制到本文件。

### 4.1 立即处置与取证

- [ ] 确认当前 `config.yaml` 的权限为仅运行用户可读（目标至少 `0600`），并确认备份、编辑器临时文件和日志没有复制凭据。
- [ ] 检查工作区、构建目录、CI artifact、容器层、备份和共享目录是否存在这些凭据的副本。
- [ ] 检查 Git 历史、分支、标签和远程仓库；确认没有把 `config.yaml`、`.env` 或密钥写入提交、issue、构建日志和 release artifact。
- [ ] 在提交审计材料前对 `report.md` 中列出的凭据值做不可逆脱敏，仅保留配置项名称和风险说明；原始报告若需留档，应加密并限制访问。
- [ ] 如果任意凭据曾进入远程系统，按“已泄漏”处理，不等待进一步证据后再轮换。
- [ ] 建立轮换时间、负责人、影响系统和回滚联系人记录，但不记录密钥内容。

### 4.2 外部服务凭据轮换

- [ ] 轮换数据库密码，优先新建专用应用账号，禁止继续使用 `root` 作为应用密码/账号。
- [ ] 轮换 Redis 密码，先确认所有中心、边缘、容器和监控客户端的配置来源。
- [ ] 轮换 MinIO/S3 access key 和 secret key；确认 bucket 权限最小化，区分读写、管理和部署账号。
- [ ] 轮换 Keycloak client secret，并在 Keycloak 与 DraARL 两侧协调更新时间。
- [ ] 检查互联节点注册 token、TLS 私钥、边缘 identity 文件和其他未在报告表格列出的共享凭据。
- [ ] 在轮换窗口前准备旧配置备份、健康检查、数据库连接检查、Redis 检查、对象存储上传/下载检查和 Keycloak 登录检查。
- [ ] 轮换完成后验证：服务启动、登录、刷新 token、头像上传、文件下载、设备认证、节点互联和管理员操作均正常。

### 4.3 JWT 与设备 AES 密钥轮换

- [ ] 先确认 JWT 密钥的全部用途：登录 access token、refresh 相关签名、local storage 签名 URL、上传/下载授权、互联恢复票据。
- [ ] 评估 JWT 轮换影响：现有 access token 将失效；决定是否同时吊销所有 refresh token，并提前通知用户/运维。
- [ ] 验证 `pkg/storage/local.go` 的旧签名密钥兼容窗口只存在于进程内还是持久化；服务重启后旧 URL 是否仍可用必须明确记录。
- [ ] 设计 JWT 轮换步骤：生成高熵新密钥、原子更新 Secret、滚动重启、验证新旧 token 预期行为、观察错误率和登录成功率。
- [ ] 设备 AES key 轮换前确认历史设备密码密文是否必须继续解密；如果需要迁移，设计“旧 key 解密 + 新 key 重加密”的逐条迁移方案。
- [ ] 若无法无损迁移设备密码，先导出受控的迁移清单并准备用户重新设置设备密码的产品/运维方案，不直接覆盖旧 key。
- [ ] 轮换后确认旧 JWT、旧本地签名 URL、旧设备密码密文和旧 refresh token 的行为符合批准的安全策略。

### 4.4 仓库和部署配置整改

涉及：`config.yaml.example`、`.env.example`、`deploy/docker/config.yaml.template`、`deploy/docker/docker-entrypoint.sh`、`compose.yaml`、部署文档。

- [ ] 保持 `config.yaml.example` 和 Docker 模板只包含占位符或 `${ENV_NAME}` 引用，禁止出现可用生产凭据。
- [ ] 检查所有示例默认密码，避免示例值被误用于公网部署；必要时在启动校验中拒绝明显默认值。
- [ ] 为非 Docker 部署补充环境变量/Secret 注入说明，确保密钥不会被程序回写到版本库目录。
- [ ] 验证 Docker entrypoint 的 `umask 077`、临时文件替换和持久配置权限；确认异常退出不会留下明文临时文件。
- [ ] 更新 `docs/usage/01-部署与配置.md` 和运维排障文档，写明密钥来源、轮换影响、备份保护和恢复流程。
- [ ] 增加配置扫描 CI：检测提交中出现 `JWT.Secret`、`AESKey`、`ClientSecret`、`SecretKey`、密码字段的真实样式；允许模板占位符但拒绝高熵疑似密钥。

## 5. P2：重放窗口大跨度跳跃优化

涉及：`internal/interconnect/replay_window.go`、`internal/interconnect/resource_guard_test.go`、`internal/interconnect/datagram_test.go`。

### 5.1 先固定现有语义

- [ ] 明确窗口规则：ID 为 0 拒绝；新最大 ID 向前滑动；窗口内重复 ID 拒绝；窗口外旧 ID 拒绝；窗口外新 ID 清空旧窗口后接受。
- [ ] 记录当前基准：顺序消息、乱序消息、`delta=1`、`delta=16383`、`delta=16384`、跨位图环绕和并发输入。
- [ ] 确认 `AcceptMessage` 只在认证成功后调用，优化不能改变认证边界或把未认证数据引入 replay window。

### 5.2 实现按字清理

- [ ] 将 `w.maxID + 1` 到 `messageID` 的清理改为位图字级批量清理，处理首尾非完整 64 位字和 16,384 位环绕。
- [ ] 避免直接采用报告中的逐步循环示例；该示例与当前实现相同，不能解决最坏 16,383 次迭代问题。
- [ ] 保证 `replayWindowBits`、`replayWindowWords`、位索引和掩码关系保持一致，并在代码前增加简短的环绕逻辑注释。
- [ ] 评估是否需要将“大跨度阈值”保留为整窗 `clear` 快速路径；阈值变化必须经过乱序容忍度评估，不能只为降低 CPU 而缩小窗口。
- [ ] 保持互斥锁保护范围足够小且语义正确；不得在锁内加入日志、网络、数据库或回调。

### 5.3 测试与基准

- [ ] 新增边界测试：`delta=1/63/64/65/16383/16384`。
- [ ] 新增环绕测试：位图尾部到头部、跨多个字、最大 `uint64` 附近的差值处理。
- [ ] 新增正确性测试：所有被滑出窗口的 ID 拒绝；窗口内未出现的 ID 接受一次；已出现的 ID 永不重复接受。
- [ ] 保留并扩展现有并发测试，运行 `go test -race ./internal/interconnect`。
- [ ] 增加跳跃场景 benchmark，分别测量顺序流量、`delta=16383` 和 `delta=16384`：
  - [ ] `go test -bench=ReplayWindow -benchmem ./internal/interconnect`
  - [ ] 记录 ns/op、allocs/op，并比较优化前后的结果。
- [ ] 在 UDP 数据面测试中确认单个异常跳跃不会明显阻塞同一节点会话的其他数据包。

## 6. P3：运行时索引锁序和锁竞争整改

涉及：`internal/udphub/runtime_index.go`、`internal/models/device.go`、对应测试和 UDP 压力测试。

### 6.1 建立锁规则

- [ ] 明确并记录锁职责：`runtimeIndexMu` 只保护运行时索引 map；`dev.runtimeMu` 只保护设备运行时字段；群组连接池使用自己的锁。
- [ ] 统一锁序，首选在全局索引锁外完成设备字段更新，在全局锁内只做 map 增删改。
- [ ] 禁止持有 `runtimeIndexMu` 时调用可能阻塞、回调外部、触发 Redis/数据库或再次进入运行时索引的函数。
- [ ] 保留 `models/device.go` 中关于 `UpdateRuntime` 回调短小且不可重入的约束，并在关键函数旁补充锁边界注释。

### 6.2 重构 `SyncRuntimeDeviceEntry`

- [ ] 在获取 `runtimeIndexMu` 前完成 `OnlineTime`、`UDPAddr`、`RealUDPAddr` 等设备字段更新，或先读取不可变快照再在锁外更新。
- [ ] 全局锁内只维护 `onlineDevMap` 和 `onlineDevMapDraARL`。
- [ ] 处理并发状态变化：确认在线状态、节点模式和地址清理不会覆盖更新更晚的设备状态。
- [ ] 保持 remote/offline 设备从中心本地 fan-out 池移除的语义。
- [ ] 检查 `indexRuntimeDeviceLocked` 和 `SyncUserCallSignChange` 中相同的 `runtimeIndexMu -> dev.runtimeMu` 锁序并一并整改。

### 6.3 测试

- [ ] 为在线、离线、remote、center、节点会话切换和地址清理增加单元测试。
- [ ] 增加并发测试：多个 goroutine 同时更新设备状态、呼号、在线 map 和连接池，验证无数据竞争和最终索引一致。
- [ ] 运行 `go test -race ./internal/udphub`。
- [ ] 运行 UDP fan-out 压力测试，记录全局索引锁等待时间、设备锁等待时间、包处理延迟和丢包情况。
- [ ] 若仓库已有指标出口，为锁等待或清理耗时增加最小必要观测；不要在 UDP 热路径增加高频日志。

## 7. P3：设备下线清理改为反向索引

涉及：`internal/udphub/runtime_index.go`、`internal/udphub/device.go`、`internal/udphub/group.go`、ghost routing 相关模块和测试。

### 7.1 设计反向索引

- [ ] 先梳理设备进入群组连接池的所有路径：普通设备上线、设备切组、幽灵 Session 路由更新、节点互联设备同步、缓存刷新和重连恢复。
- [ ] 设计设备到群组的反向索引，至少覆盖设备当前 `GroupID` 以及幽灵设备的 `GhostRxGroupIDs`；不能只记录单一 `GroupID`。
- [ ] 选择与现有锁模型一致的数据结构，例如按设备 ID 保存群组 ID 集合；明确读写锁和生命周期。
- [ ] 设计索引更新接口，确保加入、移除、切组和路由变更都通过统一入口更新正向/反向关系。
- [ ] 设计兼容回退：反向索引缺失或检测到不一致时，可以有限次扫描群组修复，不能让异常索引导致设备连接残留。

### 7.2 改造清理路径

- [ ] 改造 `SyncRuntimeDeviceEntry` 的 remote/offline 清理，按反向索引找到相关群组后调用现有连接移除函数。
- [ ] 改造 `RemoveRuntimeDevice`，删除重复的“遍历全部群组 + 遍历 userList 中每个群组”逻辑。
- [ ] 设备清理完成后删除其反向索引项，避免内存泄漏和旧设备 ID 复用污染。
- [ ] 保持 `DevMap`、`DevList`、`DevConnMap`、连接池快照和幽灵路由状态最终一致。
- [ ] 对组缓存刷新、设备删除、用户删除、节点断开和网络分区恢复进行专项检查。

### 7.3 正确性与性能验收

- [ ] 扩展 `internal/udphub/runtime_index_test.go`：设备属于一个群组、多个接收群组、重复路由、空路由、索引缺失和重复下线。
- [ ] 验证下线设备不会留在任何连接池，也不会误删其他设备。
- [ ] 构造大量群组和少量设备，确认清理耗时随设备实际关联群组数量增长，而不是随全局群组数量增长。
- [ ] 记录改造前后 CPU、锁等待、清理耗时和内存占用。
- [ ] 运行 `go test -race ./internal/udphub` 和 UDP fan-out 压力测试。

## 8. P4：低风险安全与逻辑项

### 8.1 邮箱验证码恒定时间比较

涉及：`internal/email/verification.go` 及验证会话测试。

- [ ] 引入 `crypto/subtle`，将验证码比较改为 `subtle.ConstantTimeCompare([]byte(session.Code), []byte(code)) != 1`。
- [ ] 保持现有尝试次数、过期时间、单次使用、邮箱冷却和 IP 限制语义不变。
- [ ] 测试正确验证码、错误验证码、长度不同验证码、过期会话、达到最大尝试次数和并发单次使用。
- [ ] 运行 `go test ./internal/email` 和 `go test -race ./internal/email`。

### 8.2 邮箱验证码登录防护评估

涉及：`internal/handler/email_auth.go`、登录保护模块、验证码管理器和认证文档。

- [ ] 明确是否要求邮箱登录补充账号级失败计数；先评估验证码 session、邮箱冷却、IP 限速和未知账号信息泄露的组合风险。
- [ ] 如果增加账号级保护，复用现有 `loginGuard`，避免另建一套计数器导致策略不一致。
- [ ] 规定失败计数触发条件：验证码错误、会话不存在、用户不存在、账号禁用和后端故障不能混为一谈；后端故障不得污染锁定计数。
- [ ] 防止把“验证码验证成功但用户不存在”变成可枚举邮箱的更强信号；按产品需求决定统一错误响应或保留用户体验错误码。
- [ ] 测试成功邮箱登录会清除密码失败窗口的现有行为，避免回归报告中已确认的逻辑。
- [ ] 若评估后决定不改代码，在认证安全文档中记录风险接受理由、现有限速和监控指标。

### 8.3 注册信息枚举

涉及：`internal/handler/auth_login.go`、注册限速、错误响应文档。

- [ ] 保持“数据库查重前先消耗注册配额”的现有顺序。
- [ ] 评估用户名、呼号、手机号、邮箱分别返回 409 的隐私影响。
- [ ] 不在没有产品决定前合并错误信息；如保留差异化错误，确认 IP/账号/设备限速足够严格并有告警。
- [ ] 增加注册接口限速和重复字段查询的回归测试，确保数据库故障不会被误报为字段冲突。

## 9. CI、发布与运维门禁

### 9.1 CI 工作流

涉及：`.github/workflows/`，当前仓库存在 `release.yml` 和 `docs-pages.yml`，需要确认是否新增质量检查 workflow。

- [ ] 新增或扩展合并检查 workflow，至少执行：
  - [ ] `go build ./...`
  - [ ] `go test ./...`
  - [ ] `go vet ./...`
  - [ ] `go test -race ./...`（可拆分为允许更长时间的 job）
  - [ ] `go run <已批准版本>/golang.org/x/vuln/cmd/govulncheck ./...`
  - [ ] `npm --prefix www ci`
  - [ ] `npm --prefix www run build`
  - [ ] `npm --prefix www run lint`
  - [ ] `npm --prefix www audit --omit=dev`
- [ ] 固定 Go、Node、govulncheck 和关键 action 版本，减少 `latest` 导致的不可重复结果。
- [ ] CI 日志脱敏，禁止输出配置文件全文、环境变量全文、JWT、Cookie 和云服务密钥。
- [ ] 依赖漏洞门禁应区分可达漏洞和仅导入漏洞，并允许经过审批的临时例外，例外必须有到期日期。
- [ ] 在 release workflow 中增加升级后的 `govulncheck` 和关键集成测试，避免只验证 `go build`。

### 9.2 发布前检查

- [ ] 确认生产镜像使用修复后的 Go 工具链和依赖，检查最终二进制依赖清单。
- [ ] 确认镜像中没有 `config.yaml`、`.env`、测试 fixture、构建缓存和源代码中的真实凭据。
- [ ] 执行数据库、Redis、MinIO/S3、Keycloak、JWT、AES、refresh token 和互联健康检查。
- [ ] 执行用户禁用、删除、拒绝审批后的即时登出验收。
- [ ] 执行头像上传恶意输入、HTTPS/HTTP2、WebSocket、UDP/互联和设备认证冒烟测试。
- [ ] 观察发布后 15 分钟和 24 小时的登录失败率、refresh 失败率、Redis 错误、图片处理错误、UDP 丢包、锁等待和内存使用。

### 9.3 回滚准备

- [ ] 依赖升级准备可重新构建的上一版本镜像，不回滚到含已确认高危漏洞的版本，除非发生严重可用性事故并完成风险审批。
- [ ] 会话吊销逻辑通常可向后兼容；准备重新登录通知和 refresh token 清理脚本/操作手册。
- [ ] JWT 轮换保留旧配置的受控备份，但备份必须加密、限权并设置过期时间；禁止把备份提交到仓库。
- [ ] AES key 轮换保留可审计的旧 key 恢复方案，只有指定运维人员可访问。
- [ ] 运行时索引优化保留配置开关或可快速切回旧清理路径（如实现成本可接受），并准备指标判断是否回退。

## 10. 推荐执行顺序

- [ ] 阶段 A：完成基线、漏洞输出复核、凭据取证和外部轮换窗口排期。
- [ ] 阶段 B：完成 Go 工具链与依赖升级，先处理 `x/image`、TLS、Redis、`x/net` 和 `x/text` 的可达漏洞。
- [ ] 阶段 C：完成禁用/删除/审批拒绝的 refresh token 吊销，并补充认证集成测试。
- [ ] 阶段 D：完成生产凭据轮换、模板检查和部署文档更新。
- [ ] 阶段 E：完成重放窗口按字清理，跑正确性测试、竞态测试和 benchmark。
- [ ] 阶段 F：完成运行时锁序整改，验证最终一致性和并发安全。
- [ ] 阶段 G：完成设备反向索引和下线清理优化，跑大规模群组压力测试。
- [ ] 阶段 H：完成恒定时间验证码比较、邮箱登录防护决策、注册枚举风险记录。
- [ ] 阶段 I：启用 CI 门禁，执行完整发布前检查并更新审计报告状态。

## 11. 最终验收清单

- [ ] `go build ./...` 通过。
- [ ] `go test ./...` 通过。
- [ ] `go vet ./...` 零告警。
- [ ] 关键包 `go test -race` 通过，或已记录无法执行的环境原因。
- [ ] `govulncheck ./...` 对本次报告列出的可达漏洞无未处理项，或每项都有批准的例外。
- [ ] `npm --prefix www audit --omit=dev` 无生产依赖漏洞。
- [ ] `npm --prefix www run build` 和 `npm --prefix www run lint` 通过。
- [ ] 禁用、删除、拒绝审批用户后 refresh token 立即失效。
- [ ] JWT、设备 AES、数据库、Redis、MinIO、Keycloak 和互联凭据均已轮换或有明确的风险接受记录。
- [ ] 工作区、Git 历史、镜像和 CI artifact 中没有真实凭据。
- [ ] 重放窗口大跨度 benchmark 达标，且窗口语义测试全部通过。
- [ ] 运行时锁和设备下线清理压力测试没有新增竞态、死锁、残留连接或明显延迟回归。
- [ ] `report.md` 更新整改状态、剩余风险、例外项和验证日期。

## 12. 验证记录

| 日期 | 任务/版本 | 命令或操作 | 结果 | 负责人/备注 |
|------|-----------|------------|------|-------------|
|      |           |            |      |             |
|      |           |            |      |             |
|      |           |            |      |             |
