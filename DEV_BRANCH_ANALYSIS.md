# DraARL Server `dev` 分支增量代码复核报告

- **复核对象**：`dev` 分支，HEAD `86cf4cc`；当前工作区包含大量未提交改动，均视为用户已有改动并保留。
- **复核时间**：2026-08-22（Asia/Shanghai），本轮针对最新 Git 差异做再次增量重分析；除操作日志和消息/群组查询闭环外，共享 Redis 登录/注册/验证码发送保护已完成接入和 Ubuntu 专项验证，并补齐兼容原生 SQL 用户/中继/服务器/操作日志查询、用户管理分页和设备列表分页的稳定投影、启动初始化锁、分页边界和扫描错误传播，本报告按当前实际证据重新标记，所有改动仍未提交。
- **复核方法**：静态阅读当前工作区与 `origin/dev`/HEAD 差异；Windows 仅执行 Git、文本和格式检查；Go 测试、竞态测试和 `go vet` 仅在 Hyper-V Ubuntu 的 Go 1.25.5 环境执行。
- **范围约束**：本轮不提交 Git、不回滚或清理用户改动；代码优化和测试文件允许保持未提交。Windows 不运行 Go 测试/构建/vet，Go 验证仅在 Hyper-V Ubuntu 执行；真实设备、FRP/PROXY v2、生产 MySQL、真实 S3、ffmpeg、容量和语音质量证据不能由单元测试替代。

## 一、当前结论

本轮已将已经形成“代码修复 + 对应专项验证 + 无已知剩余边界”的项目从当前风险清单移除。当前仍保留 24 条报告记录（S1 与表格中的“PROXY v2 配置”重复，按独立主题约 23 项）。其中约 5--6 项仍有明确代码/设计工作，约 10--12 项主要缺真实部署或生产规模证据，其余主要是协议、配置和运维决策边界；不能把全部记录都视为尚未修复的代码缺陷。当前发布前仍需关注：

1. **S1：UDP 双地址身份绑定已完成 release 配置 fail-closed，仍需真实 FRP/PROXY v2 验收**。代码已分离代理回包地址与可信真实源地址；release 构建启用 PROXY v2 时若未配置非空可信网段会拒绝启动，开发/测试构建才保留兼容性“信任所有来源”模式。
2. **H5：认证池与失败状态内存边界已部分修复，小规模真实设备接入已通过，容量和拓扑边界仍待验证**。Ubuntu 真实 UDP benchmark 已完成 50/100 个设备首次认证和语音接收；NAT 端口变化、FRP 批量重连、队列满载及生产 DB 慢响应仍没有实测证据。
3. **H6：fanout 已解除运行中同步回退，并补齐目标级丢帧统计；小规模真实 socket 验证零丢包，但过载仍会丢旧帧**。需要更大规模真实语音容量、丢帧率和端到端通话质量门槛。
4. **H9/H12：代码、专项测试和专用 MySQL 验证已通过，但生产旧库边界仍需保留**。H9 的设备切组 bcrypt 回归、历史明文惰性升级和限速已通过 Ubuntu HTTP E2E；H12 在全新专用 MySQL 库已验证首次迁移写入连续版本 `1,2`、重复启动幂等同步，并通过版本记账故障注入验证有界重试和独立回读确认。两项仍保留“部分修复”：历史超长群密码需要人工轮换，既有生产库升级、迁移失败恢复、DDL 锁等待和 EXPLAIN 尚未在生产规模验收，迁移步骤与版本记录之间也没有统一事务回滚。
5. **历史数据与部署边界**：历史 SVG/favicon、历史私有群密码、历史 `openai.*` 配置行不会被所有新代码自动清理；SiteConfig 敏感明文已在读取时尽力惰性加密，但生产密钥轮换和失败恢复仍需运维演练；release 已对 refresh-token 和登录/注册保护的 Redis 初始化失败 fail-closed，开发/测试仍允许内存兼容回退。登录/注册共享状态代码级和 Redis 双实例专项已闭环，但真实生产 Redis 高可用、网络故障和账号/邮箱响应策略仍需验收；S3/CDN 响应头、时区、ffmpeg 部署沙箱和生产容量仍需运维或专项处理。

状态含义：

- `⚠️ 待验证`：代码路径已有实现和测试，但缺真实目标环境证据。
- `⚠️ 部分修复`：主路径已改善，仍存在可达边界或明确容量/兼容风险。
- `⚠️ 评估/设计边界`：当前行为可能是部署或协议设计，需要产品/拓扑确认。
- `❌ 仍存在`：本轮确认问题尚未消除。

## 二、保留的高风险专题

### S1. UDP 数据面源地址与身份绑定

- **状态**：⚠️ **部分修复**。
- **现状**：普通设备运行时已分离 `RealUDPAddr` 与代理回包用的 `UDPAddr`；voice/text/config 身份校验、心跳地址迁移和冲突判断使用真实地址。中心和独立边缘分别使用 `System.ProxyTrustedCIDRs`、`Edge.ProxyTrustedCIDRs`；非法 CIDR fail-closed，解析失败不覆盖已有有效快照。新增中心配置、边缘配置和 `NewEdgeEndpoint` 三层一致校验：release 构建启用 v2 时必须配置非空可信网段，否则拒绝启动。设备 MAC 冲突缓存现在有 10 分钟惰性 TTL、100,000 条本地硬上限和 500ms Redis 操作截止时间；运行时索引先释放全局锁，再执行 Redis 写入。
- **验证证据**：Ubuntu `internal/config`、`internal/interconnect`、`internal/udphub` 10 轮 `go test -race` 通过；全仓 `go test -race ./... -count=1` 和 `go vet ./...` 通过。测试覆盖 release 空网段拒绝、合法网段接受、开发/测试空网段兼容、未授权代理来源不解析和直连数据报兼容。
- **剩余边界**：尚无真实 FRP/PROXY v2 批量设备回归；白名单漏配会使合法设备报文无法解包，必须按实际代理出口分别配置中心和边缘网段。开发/测试构建仍允许空列表信任所有来源，仅用于兼容和本地测试，不应部署到生产。Redis 故障或本地 MAC 缓存满载时按 fail-closed 处理，跨实例地址迁移可能等待后续心跳重试。
- **接入影响**：直连设备的代理地址和真实地址相同，协议和密码不变；release 生产启用 v2 的部署需要先配置非空代理网段，否则服务会在启动时明确失败而不会带着不安全默认值运行。共享 FRP/NAT 必须核对实际代理出口，否则合法设备可能无法接入。MAC 缓存边界只可能延迟/拒绝异常地址接管，不放宽合法设备的源地址校验。

### H5. 心跳重认证与认证池

- **状态**：⚠️ **部分修复**。
- **已完成**：bcrypt/DB 查询已移出 UDP 数据面 worker；首次认证和重认证进入专用队列；同一 `username+ssid` 合并；失败状态、队列满载、停机排空、停止中禁止新 generation、单任务 panic 隔离和 context 传播均有测试。认证 worker generation Context 在停止时先取消进行中的认证任务，再关闭并排空队列；认证查询、密码校验后的迁移写库和成功返回前均检查取消状态，worker 在更新设备运行时状态前也检查 generation Context，避免服务停止后继续清理失败状态或发布认证结果。认证失败表现在 16 个分片内采用全局 100,000 条硬上限；满载时只在最多 128 条候选内淘汰已过期/已解除封禁记录，活跃封禁保留，找不到安全候选则拒绝新增状态。
- **验证证据**：Ubuntu `internal/udphub` 的有界表/认证专项 race 50 轮、包级 race/vet、全仓 `go test -race ./... -count=1` 和全仓 `go vet ./...` 均通过；测试覆盖全局容量、活跃封禁不被淘汰和并发更新不丢计数。既有 50、100 个设备档位首次心跳认证和语音 fanout benchmark 均 `loss_pct=0`；这证明正常低规模设备接入路径可用，但不覆盖共享 NAT/FRP。
- **剩余边界**：认证异步期间语音、文本和配置帧会丢弃；队列满时返回 `auth_busy`，依赖后续心跳重试；5 秒后端截止时间可以中断数据库等待，但 bcrypt 本身不可被 Context 中途取消，停止延迟仍受单次密码校验耗时影响。NAT 端口变化、FRP 批量重连、队列满载和生产 DB 慢响应仍未验证。
- **接入影响**：不改变 DraARLv1 报文、设备密码、失败码或地址绑定；新设备/地址变化重认证的可见变化是短暂等待和可能的 `auth_busy`，需设备端确认会重试。停止取消只发生在服务关闭路径，不影响正常运行中的设备接入。

### H6. Fanout 过载与语音质量

- **状态**：⚠️ **部分修复**。
- **已完成**：dispatcher 不再等待 writer；中心和边缘 sender 入队失败不再回退到 ingress 线程同步写 UDP；移除 `submitMu` 后多域可并发入队；慢 writer 队列满时采用 latest-wins，淘汰旧分片并计入指标。本轮新增 `targets_dropped`，统一覆盖 frame 淘汰、writer 淘汰、过期帧、关闭/过载拒绝和分片投递失败，避免只看 frame/event 数而低估实际受影响设备数；底层 `WriteToUDPAddrPort` 错误仍单独计入 `write_errors`，广播回调将其并入业务侧目标丢弃统计。通信录音上传队列新增 4,096 个会话硬上限，满载淘汰最旧会话并暴露 `dropped_uploads`，消除存储故障期间的无界上传队列增长（✅ 已验证修复）。
- **验证证据**：Ubuntu 专用 MySQL/真实 UDP socket benchmark 的 50、100 设备档位均零丢包；100 设备档位约 825 output pps、平均延迟 0.62 ms。测试数据和录音残留已自动清理。
- **剩余边界**：过载时仍会丢帧，`FrameQueueSize` 和 writer 缓冲只是有界背压，不是质量保证；本次仅验证低规模单入口、单群组，单 reader、FRP 汇聚点串行化、录音单 worker 和高基数 fanout 容量尚未做生产压测。
- **接入影响**：正常低负载设备接入条件和报文未变；高负载或慢接收端可能出现局部丢音，需结合 `writer_evictions`、`targets_dropped`、socket errors 和端到端 MOS/丢帧指标验收。统计字段只增加观测能力，不改变发送策略。

### H9. 私有群组密码历史迁移与消费路径兼容

- **状态**：⚠️ **部分修复**。
- **已完成**：新建/更新私有群组使用 bcrypt；Web 加入和设备切组统一使用 bcrypt/历史明文兼容校验；两条路径共享用户/IP 密码尝试限速；历史明文验证成功后用旧值条件更新惰性升级。schema migration v2 按 keyset 分页升级历史明文密码，条件更新避免覆盖并发修改；迁移仅筛选 `type=2`、非空密码，并用完整 bcrypt 结构校验避免损坏哈希被误判为已完成。
- **验证证据**：Ubuntu 已通过 handler/gormdb/middleware 包级 `go test -race`、H9 专项 20/50 轮和 `go vet`；2026-08-22 在 Docker MySQL 8.4 的专用库 `draarl_test_codex_h9_20260822` 执行 HTTP E2E 通过，覆盖 bcrypt 设备切组、错误密码、已验证成员免密码、历史明文惰性升级和设备切组/加入限速。测试使用代码直接签发 JWT，没有经过验证码流程。
- **剩余边界**：迁移在启动路径同步执行；超过 bcrypt 72 字节的历史密码现在保留原值、输出告警并继续完成版本迁移，不再阻止服务启动，但这些值仍是历史明文兼容债务，必须安排管理员人工轮换。生产旧库历史数据清理、失败恢复和大规模迁移性能仍待验收；专用库 E2E 不能替代生产备份和维护窗口验证。
- **接入影响**：不改变 UDP 实体设备认证、DraARLv1 报文或设备密码；修复后普通用户可用正确密码通过 Web 切换到已哈希私有群。公开群、管理员和已验证成员不增加密码校验；错误密码仍按 401/限速返回。

### H12. 版本化 AutoMigrate 与旧库 DDL

- **状态**：⚠️ **部分修复**。
- **已完成**：`schema_migrations` 避免已记录版本重复清洗；版本记录现在要求从 1 开始连续且拒绝高于当前版本的账本，避免跳号或旧程序误连新库后静默跳过迁移；只有迁移账本表的全新业务库会跳过历史清洗后正常建表；已有业务库的重复巡检、孤儿清理、遗留列删除和外键清理失败会 fail-closed，不再记录迁移完成；MySQL 使用连接级 `GET_LOCK` 防止跨实例同时迁移，等待有 30 秒和 context 取消边界；迁移账本增加 `running/completed` 状态，迁移步骤开始前先持久化 running，完成后才切换 completed，旧版本空状态按 completed 兼容；版本写入和状态切换均有 3 次有界重试与独立回读确认。
- **验证证据**：2026-08-22 在 Docker MySQL 8.4 的全新专用库 `draarl_test_codex_h12_20260822` 启动当前代码 `-auto-migrate` 成功，`schema_migrations` 写入连续版本 `1,2`，服务正常启动并可优雅退出；第二次启动记录为幂等 schema 同步，版本账本保持 `1,2`，未重复执行 v1/v2 清洗迁移。新增迁移 running/completed 状态恢复与 `retryMigrationVersionRecord` 测试；独立 MySQL 8.4 故障注入和状态 E2E 20 轮通过，验证首次写前失败会重试、写后模糊提交只需一次有效写入、running 状态不计入完成版本且可被后续启动重新执行；随后全仓 race 和 vet 均通过。
- **剩余边界**：状态账本缩小了“迁移已开始但进程崩溃后无法识别”的窗口，但 DDL、数据回填和版本状态切换仍没有统一事务回滚；进程可能在任意 DDL/回填中途退出，恢复仍依赖步骤幂等性。没有版本记录的既有库首次升级仍可能执行用户去重、孤儿清理、记录回填和 DDL；GORM 结构差异仍可能锁表。生产规模 MySQL 锁等待、备份恢复、失败重试和旧库数据兼容演练尚未完成。
- **接入影响**：不改变设备协议；启动/升级窗口可能影响 HTTP/DB 可用性，必须备份、预检查和固定维护窗口。

## 三、其他仍需保留的风险与设计边界

| 子系统 | 状态 | 当前结论与剩余工作 |
|---|---|---|
| 设备模型运行时快照 | ⚠️ 设计边界/待容量验证 | 当前生产调用方已改用 `DeviceRuntimeSnapshot`/`UpdateRuntime` 并通过 race；`models.Device` 字段仍公开可变，未来绕过 API 会重新引入竞态；深拷贝和锁开销尚无容量数据。 |
| PROXY v2 配置 | ⚠️ 部分修复 | 中心/边缘白名单已分离、非法 CIDR fail-closed；release 构建启用 v2 且空列表时拒绝启动，开发/测试构建仍保留空列表兼容模式。 |
| 互联 UDP 时效 | ⚠️ 待验证 | 已移除跨节点墙钟过期判断，改用本地接收时间和 monotonic 排队年龄；真实时钟偏差、网络延迟、旧节点互通仍未回归。 |
| 跨节点设备会话 | ⚠️ 评估/设计边界 | 长期节点凭据、设备包重认证和在线冲突检查已降低抢占风险；物理连接点强绑定取决于部署拓扑，需专项确认。 |
| 互联重连 PTT | ⚠️ 评估/设计边界 | 旧控制会话的待认证/控制请求必须清理；当前重连会清空本地 speaker 状态，正确恢复需要协议级租约重同步。 |
| WebSocket Origin | ⚠️ 设计边界 | 空 Origin 继续允许非浏览器设备；生产应收窄 `AllowedOrigins`，否则 HttpOnly `ws_token` 的跨源重放风险取决于部署。 |
| HTTP 登录/注册防护 | ⚠️ 部分修复（共享 Redis 已接入，生产边界待验收） | 已有账号失败锁定、未知账号来源 IP 20 次/10 分钟锁定、注册 IP 每小时 5 次、验证码发送 IP 每分钟 5 次和固定 bcrypt dummy 校验均有界；四条路径现由 `cmd/draarl/main.go` 初始化的共享 Redis 执行跨实例 Lua 原子计数，key 使用 SHA-256，不记录用户名/IP 明文。验证码 IP 预算在邮箱存在性查询前消费，未知邮箱探测不能绕过限速；IP 使用标准格式、邮箱冷却忽略大小写和首尾空格。Redis 初始化失败在 release fail-closed，development/test 回退内存；运行期错误 fail-closed，密码登录、邮箱验证码登录和密码重置成功都会清除账号失败窗口。验证码使用均匀 `crypto/rand.Int`，会话 ID 使用 128 位 CSPRNG，随机源失败不发送邮件。Ubuntu 真实 Redis 双实例已验证账号/未知 IP 锁定、跨实例 Clear、注册和验证码发送总额；email/handler/cmd race、全仓 race/vet 通过。仍需真实生产 Redis 高可用/故障切换、代理来源 IP 配置、账号/邮箱差异响应和长期容量验收。 |
| ConfirmBind | ⚠️ 设计边界 | 动态码日志已不再输出明文；`ConfirmBind` 仍可凭 MAC 返回用户名、设备密码和 DMR ID，现依赖接口限速，这是现有设备协议设计的敏感边界。 |
| SiteConfig 密钥 | ⚠️ 部分修复 | 敏感 key 的新写入统一使用带 `enc:v1:` 版本前缀的 AES-GCM 密文；仓储读取透明解密，并对历史敏感明文执行带行锁和并发保护的惰性加密，handler/审计日志继续脱敏。专用 MySQL 惰性迁移已通过；历史 `openai.*` 行、生产密钥轮换、旧密文备份恢复和迁移失败演练仍待处理。 |
| AES 密钥熵 | ⚠️ 用户决策保留 | 当前生成方式有效熵约 128 bit；改动会影响既有密文解密，用户明确选择不改，作为已知设计债务记录。 |
| 消息/群组查询 | ⚠️ 部分修复 | 消息列表已分块 `IN` 并使用快照游标索引；群组搜索在 SQL 层执行可见性分页，群组列表设备统计改为只聚合当前页群组，分页页码增加整数溢出保护，普通用户不再列出禁用群组。Ubuntu 已通过百万行消息 `EXPLAIN ANALYZE`/游标、消息 HTTP E2E、群组统计/权限分页 MySQL E2E、包级 race 和全仓 race/vet；生产规模群组表的实际 EXPLAIN、索引选择和长期分页容量仍待验收。 |
| 数据层索引/级联 | ⚠️ 评估 | `comm_records` 多索引写入放大、`DeleteUserWithCascade` 跨表大事务仍需生产数据评估；不能仅凭 SQLite/单元测试决定删索引或拆事务。 |
| 时间语义 | ⚠️ 部分修复 | 新增 `Database.Timezone`：默认 `Local` 完全保持旧部署行为；显式 IANA 时区（如 `UTC`/`Asia/Shanghai`）同时写入 MySQL DSN 的 `loc` 与 session `time_zone`，并让 GORM `NowFunc` 使用同一 `Location`，避免 Go 解码、GORM 写入和 SQL `NOW()` 分裂。跨时区生产旧数据解释、MySQL 时区表可用性和完整链路仍需真实数据库验收。 |
| 存储迁移 | ⚠️ 部分修复 | `Workers=0` 现在真正默认 4 个 worker（负数/1 为串行，上限 16）；对象级 `Put`/删除和瞬态哈希读取采用可取消的有界重试（默认 3 次、上限 5 次），默认情况下同尺寸目标对象做 SHA-256 校验，哈希不一致按永久完整性失败处理并直接重新复制，不重复读取大对象；只有显式设置 `SkipExistingVerification` 才跳过该读取（`DeleteSource` 仍强制校验）。迁移 CLI 现支持 `-migrate-max-bytes-per-second`，所有 worker 共享同一个可取消字节令牌预算，默认 0 不限速，不会因提高 worker 数放大源端读取压力。S3 服务端 CopyObject、真实对象规模和跨云故障场景仍待专项。 |
| S3 预签名 URL | ⚠️ 配置/代理契约 | `DownloadURLPrefix` 是反向代理入口，不是任意静态 URL 重写：代理必须把请求还原到签名时的原始 S3 host/path，并原样转发查询串。现有 contract test 已覆盖该代理模式；直接把签名查询串挂到不还原 host/path 的 CDN/域名仍会 SigV4 403，需在真实 MinIO/CDN 配置下验收。 |
| 历史 SVG/favicon | ⚠️ 运维待处理 | 新上传已按正文/签名限制为安全格式，历史 SVG 对象不会自动删除；S3/CDN 仍需 CSP、下载响应头，管理员需主动替换历史对象。 |
| 广播媒体容量 | ⚠️ 待验证 | 恢复分页、重复入队去重、状态条件回写已完成；生产 MySQL/ffmpeg backlog、转码资源、持久化重试和并发容量仍需验收。 |
| ffmpeg 沙箱 | ⚠️ 评估/部署加固 | Linux 转码/探测命令现在由 wrapper 在 `exec` 目标前设置继承式 `RLIMIT_AS`/`RLIMIT_CPU`，消除原先 `Start` 后 `prlimit` 的窗口；非 Linux 仍仅告警。受限用户、seccomp/cgroup、noexec 临时目录和生产容量仍属于部署加固项。 |
| 操作日志/指标 | ⚠️ 部分修复 | 操作日志按事件类型/操作人增加 `(filter,id)` 复合索引，分页参数统一做上限和整数溢出保护，兼容统计接口现在传播总数查询错误；保留清理仍按时间索引有界批量执行。Ubuntu 已通过新库 AutoMigrate、索引列/EXPLAIN、事件/操作人分页与统计 E2E、包级 race 和全仓 race/vet；生产日志规模、真实索引选择和统计接口耗时仍待验收。 |

## 四、最新增量变更复核

### 1. 头像资料更新失败回滚（新增）

`internal/handler/upload.go` 新增头像引用持久化边界：头像对象上传成功后，若 `UpdateUserAvatar` 失败，使用独立 5 秒后台 Context 删除本次新对象并返回 500；即使原请求已取消，回滚仍可执行。旧头像不删除，避免破坏仍可能被引用的历史对象。缩略图 URL 仅在缩略图对象上传成功后返回。

专项测试覆盖“DB 更新失败必回滚”和“DB 成功不误删”，与 handler/storage 及全仓 race/vet 证据一致。该改动只影响 Web 头像对象与用户资料的一致性，不改变设备密码、UDP/DraARLv1、WebSocket、设备地址绑定或既有设备接入条件。

增量横向检查已将资产新增/覆盖、资产直传、操作证 multipart/直传、固件发布、广播音频、通信记录及资源删除相关的对象回滚/清理统一改为独立且有界的补偿 Context；取消的 HTTP 请求不再直接阻断这些补偿动作。该项已完成代码级闭环并通过 Ubuntu 专项、包级、全仓 race/vet，故不再作为当前风险项保留。历史对象迁移和清理策略仍按各专题单独记录。

### 2. 上传 Context 与资产 MIME

头像读取/处理/缩略图、Logo、favicon、操作证、资产新增/覆盖和通用 multipart 均已传递 `c.Request.Context()`；旧无 Context API 保留，避免外部编译兼容破坏。资产数据库 `mime_type` 现在复用正文嗅探出的规范 MIME，不再信任客户端 Header。

### 3. 其他已核对的增量

- 设备准入密码生成器保持 CSPRNG 均匀取样且随机源失败即 fail-closed，不再使用时间戳/计数器降级；本轮将自动生成长度恢复为历史 8 位，服务端手动设置仍保留 6–10 位兼容。既有设备密码、密文和在线会话不会被改写，新注册/重建密码也不再放大旧客户端硬编码 8 位的兼容风险；Ubuntu VM 已通过定向 50 轮 race、handler 包级 race 和 handler vet。
- 无状态 JWT 续期保留导出符号但对有效 access token 明确拒绝，不再绕过 refresh-token store 签发新 token。
- SSO OAuth `state` 和一次性登录交换码生成统一改为 CSPRNG fail-closed；随机源失败时不再用时间戳/计数器降级签发可预测 state，登录 URL、绑定 URL 和交换码保存路径都会返回错误。Ubuntu VM 已通过 SSO/设备密码生成定向 50 轮 race、handler 包级 race 和 handler vet。
- `86cf4cc` 删除 OpenAI 配置系统后，运行时代码已无该配置的读写调用；旧数据库历史行不会自动清理。
- H9 增量优化已让 `ChangeDeviceGroup` 与 Web 加入路径共享 bcrypt/历史明文校验、惰性升级和密码尝试限速；2026-08-22 Ubuntu Docker MySQL 专用库 HTTP E2E 通过，测试 JWT 直接签发，不经过验证码。
- 密码变更/重置成功后吊销目标用户 refresh session；已有 access/WS JWT 仍按其最长 3 小时 TTL 自然失效。
- refresh-token Redis store 的所有操作现同时受单命令读写超时和总 Context 截止时间约束；`RevokeAllByUser` 由逐 token 顺序 N+1 往返改为批量读取和批量更新。登录/注册保护现由主程序初始化共享 Redis，release 初始化和运行期错误均 fail-closed，development/test 保留内存兼容；该共享状态只用于 Web 登录/注册，不进入设备认证或 UDP 数据面。
- HTTP 登录本轮增加未知账号来源 IP 保护：确认查询成功但无用户时，按单 IP 20 次/10 分钟锁定 10 分钟，状态表上限 100,000 且有界淘汰；不存在用户路径执行固定有效 bcrypt dummy 校验，减少账号/邮箱枚举的密码计算时序差异。数据库查询错误不计入未知账号状态，避免 DB 故障期间误锁合法来源；已存在账号仍走原有账号级失败锁定，密码登录、邮箱验证码登录和密码重置成功后都会清理该账号的失败窗口。该改动只影响 Web 登录，不改变设备 JWT、设备密码、DraARLv1/UDP 或既有设备接入。
- 验证码发送限速本轮改为原子检查/消费：release 使用共享 Redis、development/test 可回退互斥保护的有界内存 map，最多保留 100,000 个来源；IP 预算在合法用途解析后、邮箱存在性查询前消费，避免未注册邮箱探测绕过限速，数据库查询错误返回 503 而不是误判不存在。IP 标准化合并等价 IPv6 表示；邮箱 60 秒冷却改为原子预留并对大小写/首尾空格归一化，限制 100,000 个唯一邮箱条目，SMTP 或 CSPRNG 失败只释放本次预留。验证码改用均匀 `crypto/rand.Int`，会话 ID 使用 128 位 CSPRNG；会话验证串行保护 `Attempts`/`VerifiedAt`，成功后立即一次性消费。该改动只影响验证码发送和验证，不改变设备认证或设备接入。
- refresh-token 本轮收紧 Redis 信任边界：release 构建初始化失败直接返回错误并阻止启动，开发/测试继续降级内存；新增 release fail-closed 和开发 fallback race 测试 50 轮。该改动只影响 Web refresh-token 会话存储，不改变设备 JWT、设备密码、DraARLv1/UDP 或既有设备接入。
- S1/H5 本轮收紧设备 MAC 运行时缓存：本地条目惰性 TTL 和全局容量上限防止唯一 owner/SSID 组合造成内存增长；Redis Set/Get/Del 均使用 500ms Context 截止时间，初始化会关闭旧客户端；`indexRuntimeDevice` 不再持有运行时索引全局锁执行外部 Redis I/O。Ubuntu `internal/udphub` 专项 50 轮、包级 race/vet、全仓 race/vet 通过；该改动不改变 DraARLv1、设备密码或正常设备接入协议。
- SiteConfig 仓储现对 `password/secret/api_key/token/private_key` 类 key 的新写入统一 AES-GCM 加密并加 `enc:v1:` 前缀，读取透明解密；历史敏感明文首次读取时在独立短事务中按主键加行锁、比较旧值后惰性加密，AES 未初始化或写库失败只告警且保留可读值，避免升级后 SMTP 突然不可用；密文损坏仍显式返回错误，不静默返回不可用值。
- 存储迁移本轮增加对象级有界重试和同尺寸目标完整性保护：瞬态 `Put`、删除和哈希读取失败按默认 3 次、200ms 间隔重试，最多 5 次；默认对目标端同尺寸对象做 SHA-256 校验，哈希不一致会自动重新复制；`SkipExistingVerification` 仅作为显式性能开关，`DeleteSource` 时仍强制校验。该改动只影响运维迁移工具，不改变普通对象上传、下载、预签名 URL 或设备接入路径。
- 兼容原生 SQL 的 `internal/db` 用户仓储本轮移除了 `SELECT *`：`GetUser`、按呼号/手机号/OpenID 查询、密码校验、分页列表和用户名查询统一使用稳定的显式列清单，并将 `dmrid`/`mdcid` 正确回填；分页扫描错误和 `rows.Err()` 不再被静默忽略。新增查询构造回归测试，Ubuntu `internal/db` race 100 轮、包级 vet、受影响 `cmd/draarl`/`internal/handler` race/vet 及全仓 race/vet 均通过。该兼容层不进入 UDP 数据面，也不改变 DraARLv1、设备密码或既有设备接入协议；真实生产旧库仍需确认列存在和数据兼容。
- `InitAdminUser` 本轮改为在同一数据库事务中执行 `SELECT ... FOR UPDATE` 后再创建，已存在管理员时幂等返回，避免多实例首次启动同时通过 COUNT 检查后出现重复键启动失败；新增锁定查询回归检查，Ubuntu `internal/db` race 100 轮、全仓 race/vet 通过。该改动只影响首次启动初始化，不改变已存在管理员、设备认证或正常设备接入。
- 兼容原生 SQL 的 `ListRelays` 本轮移除 `SELECT *`，固定按当前旧表契约列出中继字段、稳定按 ID 排序，并将单行扫描/迭代错误向上传播；新增查询回归检查，Ubuntu `internal/db` race 100 轮、全仓 race/vet 通过。`internal/db` 的 `ServerRepository` 仍与当前 GORM 节点模型存在历史字段/类型差异，当前无生产调用证据，保留为待旧库/调用方确认的兼容边界，未强行重写。
- `internal/db` 的 `ListServers` 本轮进一步移除 `SELECT *`，只投影该旧接口实际返回的 `id/name/dns_name/is_online/create_time/update_time`，在线值统一转换为兼容模型的 `0/1`，扫描及迭代错误向上传播；新增查询回归检查，Ubuntu `internal/db` race 100 轮、全仓 race/vet 通过。服务器创建/更新/单条读取仍保留历史类型契约，未在没有调用证据时重写；当前服务实际使用 `internal/gormdb`，不改变互联节点或设备接入。
- `internal/db/operatorlog.go` 本轮补齐原生操作日志分页边界：默认页大小 20、最大 100，极大页码在整数溢出前拒绝；日志行扫描错误不再静默跳过，避免返回条数与总数不一致。新增分页默认/上限/溢出测试，Ubuntu `internal/db` race 100 轮、`cmd/draarl`/`internal/log` race/vet、全仓 race/vet 通过。该兼容仓储只影响管理日志查询，不改变 UDP、DraARLv1 或设备接入。
- 用户管理列表、关键字搜索和待审核列表本轮统一使用 `gormdb.NormalizeUserPagination`：默认页大小 20、上限 100、极大页码拒绝溢出；HTTP 管理接口对该错误返回 400，直接仓储调用也在 SQL 前失败。新增 gormdb/handler 分页回归测试，Ubuntu 两包 race 100 轮、包级 vet、全仓 race/vet 通过。正常页码、用户数据、设备认证和设备接入条件不变。
### 4. 消息与群组查询增量优化

`internal/handler/group_query.go` 现在先执行可见群组分页，再按当前页的群组 ID 聚合设备在线/总数，避免原先每次请求对整张 `devices` 表做派生表分组；普通用户列表增加 `status=1` 约束，与群组访问授权规则一致。`GetGroups` 和 `SearchGroups` 共用分页规范化，页码乘页大小溢出时返回 400，不会向数据库传递负 offset。该改动只影响 Web 群组列表/搜索，不改变 UDP、DraARLv1、设备密码或设备接入协议。

消息查询当前代码在专用 MySQL 8.4 库完成百万行普通/类型游标 `EXPLAIN ANALYZE` 和四群组联查；群组查询完成统计、禁用群组过滤、权限分页 HTTP E2E。Ubuntu `internal/handler`、`internal/gormdb`、`internal/middleware` race 20 轮、相关 MySQL E2E 20 轮、全仓 `go test -race ./... -count=1` 和 `go vet ./...` 均通过。生产规模群组表的真实计划、索引选择及极端深分页仍保留为待验收边界。

### 5. 操作日志查询增量优化

`OperatorLog` 新增事件类型/操作人到 ID 的复合索引定义，保留旧库已有单列索引，不执行破坏性删索引；按事件类型和操作人查询可以同时利用过滤与倒序分页键。`NormalizeOperatorLogPagination` 统一限制页大小并拒绝会溢出整数的页码，HTTP handler 对非法数字参数返回 400；仓储层直接调用也执行同一边界检查。兼容的 `GetLogStats` 现在不再忽略总数查询错误。

Ubuntu Docker MySQL 8.4 专用库已通过新表 AutoMigrate、复合索引结构/EXPLAIN、事件类型与操作人分页、统计结果 E2E 20 轮；handler/gormdb race 20 轮、全仓 `go test -race ./... -count=1` 和 `go vet ./...` 均通过。该改动只影响 Web 管理日志查询、统计和后台清理，不改变 UDP、DraARLv1、设备密码或现有设备接入。

### 6. 操作证、通联日志与固件分页边界（✅ 已验证修复）

操作证待审核/审批仓储新增统一的页大小、非负 offset 和页码溢出校验；管理员待审核、已拒绝、已通过和逐条审批接口统一使用该边界，极大页码在 SQL 前返回 400。用户审核列表的 Count、操作证批量查询和操作证审批用户查询错误不再被静默忽略。固件列表及用户/管理员通联日志查询复用有界分页计算，避免 `(page-1)*limit` 溢出为负 offset；正常分页返回结构和排序保持不变。

Ubuntu `internal/gormdb`、`internal/handler` 定向 `-race` 3 轮、包级回归和全仓 `go test -race ./... -count=1`、`go vet ./...` 均通过；新增 `NormalizeOperatorCertPagination`/`NormalizeOperatorCertPage` 回归测试。该项已完成代码级闭环，故不再作为当前风险项保留。

### 7. APRS TCP 客户端停止与拨号生命周期（✅ 已验证修复）

`pkg/tcp.Client` 的拨号现在绑定停止信号和 10 秒拨号超时；停止期间取消中的拨号不会把连接安装回客户端，已安装连接关闭后也会保持 `connected=false`。读消息和读错误回调先在锁内取快照再调用，避免 `SetOnError`/回调读取的竞态；超时、行长度上限和 keepalive 行为保持不变。

Ubuntu `pkg/tcp` `-race` 3 轮、全仓 `go test -race ./... -count=1` 和 `go vet ./...` 均通过，新增停止前不拨号、连接关闭后状态收敛回归测试。该项仅影响 APRS/TCP 辅助连接，不改变 UDP、DraARLv1、WebSocket、设备密码或既有设备接入。

### 8. 通信录音上传队列内存边界（✅ 已验证修复）

`CommUploader.pendingQueue` 新增 4,096 个已完成会话的硬上限，满载时淘汰最旧会话、释放其音频缓冲引用并累计 `dropped_uploads` 统计；最新录音优先保留，存储长期故障不再导致上传队列无界增长。该项只收敛故障时的内存风险，过载下的录音完整性和生产容量仍归 H6 的待验收边界。

Ubuntu `internal/udphub` 录音专项 `go test -race` 5 轮、全仓 `go test -race ./... -count=1` 和 `go vet ./...` 均通过，新增队列边界与最新会话保留回归测试。该项已完成代码级闭环，故不从 H6 的整体容量风险中删除，仅标注该子项已验证。

## 五、既有设备接入兼容性判断

| 场景 | 判断 |
|---|---|
| 直连 UDP 设备 | 当前改动未改变 DraARLv1 报文、设备密码、认证失败码或直连地址语义；H5 异步认证会带来首次上线/地址变化的短暂等待，需确认设备会重试。 |
| 共享 NAT/FRP | S1 的真实源绑定提高了防伪性；release 构建启用 v2 必须分别配置中心/边缘实际代理出口网段，漏配会在启动时失败，错误网段仍会导致合法设备无法接入。 |
| WebSocket/幽灵客户端 | WebSocket 帧上限、Pong 活跃刷新、PTT lease 和连接回收已完成代码级验证；空 Origin、重连租约恢复仍是设计/部署边界。 |
| 设备配置 | 普通/管理员入口现拒绝非法频率、数值、NaN/Inf 和未知 tone mode；历史合法别名保留。旧客户端若依赖服务端自动归一化越界值会收到 400，但不影响正常设备接入。 |
| Web 设备切换群组 | H9 代码级和专用 MySQL HTTP E2E 已通过：正确 bcrypt/历史明文密码可切组，已验证成员和管理员免密码；错误密码仍 401 并受用户/IP 限速。生产旧库兼容仍需验收。 |
| 资源/头像/资产上传 | 只影响 Web 上传格式、取消传播、对象回滚和 MIME 元数据；不触及设备接入路径。历史对象按兼容原则保留，需运维主动迁移。 |
| 服务升级/重启 | H10 恢复已后台分页，H12 首次旧库迁移仍可能长时间占用 DB；H9 历史超长群密码会保留兼容值并告警，不再阻止启动，但需后续人工轮换。升级必须安排维护窗口、备份和迁移前数据预检。 |

## 六、发布前建议顺序

1. 在安全的 `draarl_test_*` MySQL 库继续执行旧库密码迁移、失败恢复和 H12 DDL 锁等待/EXPLAIN；H9 HTTP E2E 与 H12 全新库/重复启动验证已完成。
2. 在真实 FRP/PROXY v2 拓扑中验证 S1 双地址绑定，确认中心与每个边缘的非空白名单。
3. 用真实设备完成 H5 首次上线、NAT 端口变化、批量重连、队列满载和 DB 慢响应测试。
4. 为 H6 建立 writer eviction、dropped、端到端丢帧/音质和录音完整性门槛。
5. 配置 Redis 共享登录/注册/设备限速状态，收敛 WebSocket Origin、S3/CDN CSP/attachment 和 ffmpeg 运行沙箱。
6. 单独规划历史私有群密码、历史 SVG/favicon、历史 `openai.*` 配置行的备份后清理/替换。

## 七、本轮检查记录

- 前一轮已修改对象补偿相关 handler、`internal/auth/refresh_token_store_redis.go` 及回归测试；本轮未提交或回滚任何工作区改动。Windows 仅执行 Git、文本检查与 `git diff --check`，无空白错误。
- Ubuntu 已通过专项 `go test -race ./internal/handler -run 'Test(PersistAvatarReference|DeleteStoredObject)' -count=100`、handler/storage 包级 `-race`、全仓 `go test -race ./... -count=1` 和 `go vet ./...`；本地与 Ubuntu 三个相关文件 SHA-256 一致。
- 本轮扩大后的七个 handler 文件与测试文件 SHA-256 已与 Ubuntu 副本核对一致；对象补偿 Context 仅影响 Web/后台对象生命周期，不改变设备密码、DraARLv1/UDP/WebSocket 报文、设备地址绑定或既有设备接入条件。
- refresh-store 专项 `-race` 100 轮、auth/handler 包级、全仓 `-race` 与 `go vet` 已在 Ubuntu 通过，相关两文件 SHA-256 一致；改动不改变 JWT/refresh token 格式、Cookie 名称、设备 JWT 或设备接入协议。
- SiteConfig 加密专项 `-race` 100 轮、gormdb/handler/config 包级、全仓 `-race` 与 `go vet` 已在 Ubuntu 通过，相关三文件 SHA-256 一致；改动只影响站点敏感配置落库格式，不改变设备密码密文、设备认证、UDP/DraARLv1 或既有设备接入。
- H9 本轮完成设备切组密码路径优化：`ChangeDeviceGroup` 与 `JoinGroup` 统一 bcrypt/历史明文校验、条件升级和用户/IP 限速；Ubuntu 受影响包 race、H9 专项 20/50 轮及 vet 通过。2026-08-22 在 Docker MySQL 8.4 专用库执行 HTTP E2E 通过，测试 JWT 直接签发，不经过验证码；生产旧库历史轮换和失败恢复仍保留边界。
- H12 本轮增强迁移账本连续性、未来版本拒绝、空业务库判定和清洗/遗留列失败 fail-closed；Ubuntu gormdb 专项 100 轮、包级 race、全仓 race 和全仓 vet 通过。2026-08-22 专用 MySQL 8.4 全新库首次迁移及重复启动幂等同步通过；生产旧库 DDL/回滚仍未验收。
- H9 本轮新增历史群密码迁移兼容分支：bcrypt 不可表示的超长旧值不再使 AutoMigrate 失败，继续保留历史验证路径并记录告警；正常长度旧值仍升级为 bcrypt。Ubuntu gormdb 专项 100 轮、handler/middleware/gormdb 包级 race、全仓 race 和全仓 vet 均通过。
- H5 本轮新增认证 worker generation Context：`StopDeviceAuthWorkers` 先取消进行中的认证任务，再关闭队列并等待已接收任务退出；Ubuntu H5 专项 50 轮、`internal/udphub` 包级 race/vet、全仓 race 和全仓 vet 均通过。改动仅影响关闭路径，不改变设备协议或正常认证结果。
- H5 本轮增量复核确认取消保护闭环：`AuthenticateDeviceContext` 在数据库查询、密码校验、历史迁移写库后检查 Context，`processDeviceAuthJob` 在认证成功后、更新设备运行时状态前再次检查 generation Context；取消后不会清除失败记录、返回认证成功或继续更新设备。Ubuntu H5 专项 race、`internal/udphub` 包级 race/vet、全仓 `go test -race ./... -count=1` 和全仓 `go vet ./...` 均通过，五个关键文件与 Ubuntu 副本 SHA-256 一致。2026-08-22 真实 UDP benchmark 50/100 设备首次认证通过且零丢包；bcrypt 计算不可抢占，生产停机延迟仍需实测。
- H5 本轮新增认证失败状态表硬上限：`ShardedAuthMap` 全局最多 100,000 条，满载时限制淘汰扫描并只淘汰过期/已解除封禁项，避免唯一 `IP+username` 攻击造成无界内存增长或长时间持锁扫描；Ubuntu 有界表专项 race 50 轮、`internal/udphub` 包级 race/vet、全仓 race 和全仓 vet 均通过。该项不改变正常认证、设备密码、DraARLv1 报文或设备接入条件；共享 NAT/FRP 批量重连和生产容量仍待实测。
- H6 本轮补齐 fanout 目标级丢弃统计：frame 队列淘汰、writer 淘汰、过期帧及关闭/过载拒绝都会累计 `targets_dropped`，并新增整帧淘汰/stale 回归测试；Ubuntu H6 专项 50 轮、`internal/udphub` 包级 race/vet、全仓 race 和全仓 vet 均通过。未改变正常发送路径或设备接入协议。
- H6 本轮增量复核确认异步 writer 完成回调对正常广播和过载路径均有终点：已接收分片、writer 淘汰、关闭竞争拒绝和过期整帧都会完成 collector，`targets_dropped` 与 `write_errors` 不重复计数；Ubuntu 全仓 race/vet 通过。2026-08-22 真实 UDP benchmark 50/100 设备档位零丢包，100 设备约 825 output pps、平均延迟 0.62 ms；真实多 writer 过载、MOS、录音完整性仍未验收。
- S1 本轮收紧 PROXY v2 信任边界：中心 `Configuration.SetDefaults`、边缘 `EdgeConfig.Validate` 和 `NewEdgeEndpoint` 均在 release 构建拒绝空 `ProxyTrustedCIDRs`，开发/测试构建保留兼容模式；新增中心/边缘/endpoint 回归测试。Ubuntu 相关包 10 轮 race、全仓 race 和全仓 vet 通过；真实 FRP/PROXY v2 拓扑及白名单配置仍需验收。
- SiteConfig 本轮新增历史敏感明文惰性 AES-GCM 迁移：读取保持明文兼容，迁移事务使用主键行锁和 Go 内旧值比较，避免把秘密放进 SQL 条件或覆盖并发管理员更新；AES/写库失败只告警不阻断 SMTP。新增检测、并发保护和专用 MySQL E2E，Ubuntu gormdb 专项 20 轮 race、专用 MySQL E2E 20 轮 race、全仓 race 和全仓 vet 均通过，测试库已清理。
- H12 本轮增量补齐迁移版本记账的有界重试与独立回读确认：Ubuntu `internal/gormdb` 逻辑 race 20 轮、专用 MySQL 8.4 故障注入 E2E 20 轮、全仓 race 和全仓 vet 均通过；该改动仅降低迁移完成后版本行写入不确定导致的重复风险，不改变设备协议或正常设备接入路径。生产旧库迁移与记账之间仍非原子，必须保留备份、维护窗口和失败恢复演练。
- H12 本轮进一步增加迁移执行状态记账：`schema_migrations.state` 记录 `running/completed`，启动前写入 running，成功后切换 completed；Ubuntu `internal/gormdb` race/vet、MySQL 故障注入与 running 状态恢复 E2E 20 轮、全仓 race/vet 均通过。该改动只增强迁移崩溃后的识别和重试，不改变设备协议或正常设备接入；DDL/数据回填仍非原子，生产旧库失败恢复和锁等待演练仍需完成。
- HTTP 登录防护本轮增量验证：Ubuntu `internal/handler` 未知账号/账号锁定/注册限速专项 race 50 轮、handler 包级 race/vet、全仓 race 和全仓 vet 均通过；新增未知账号 IP 阈值与满表 fail-closed、固定 bcrypt hash 有效性测试，并确认数据库错误路径不写入未知账号状态。该改动只影响 Web 登录，不改变设备协议或正常设备接入。
- 最新共享 Redis 登录/注册/验证码发送保护已接入 `cmd/draarl/main.go`，并在 Ubuntu 真实 Redis 容器中完成双实例 E2E：账号 5 次失败后跨实例锁定、跨实例 Clear、未知账号 IP 20 次锁定、注册 IP 每小时总计 5 次、验证码发送 IP 每分钟总计 5 次；另有 release 初始化失败、development fallback、运行期 Redis 错误 fail-closed、IPv4/IPv6 规范化和非法策略参数测试。相关 email/handler/cmd race、全仓 race 和全仓 vet 通过。该项仍保留生产 Redis 高可用、故障切换和容量边界。
- 本轮补齐邮箱验证码登录和邮箱重置密码成功后的账号失败窗口清理，避免旧密码失败计数阻断已经完成第二因素认证或密码重置的用户；Ubuntu handler race/vet 通过。该改动只影响 Web 认证状态，不改变设备认证、UDP/DraARLv1 或既有设备接入。
- 本轮将验证码发送 IP 额度前移到邮箱存在性查询之前，并接入共享 Redis；数据库查询错误不再被忽略。验证码/会话 ID 改为 CSPRNG 错误 fail-closed，验证码消除取模偏差，等价 IPv6 和大小写不同的同一邮箱不能绕过 IP/邮箱冷却。Ubuntu 真实 Redis 双实例专项 20 轮、email/handler 受影响包 race、全仓 race/vet 均通过。
- 验证码限速/冷却/会话本轮增量验证：Ubuntu `internal/email` 原子 IP 消费、IP/邮箱满表边界、邮箱冷却预留和验证码单次消费 race 30/50 轮、email/handler 包级 race/vet、全仓 race 和全仓 vet 均通过；并发 100 个发送请求在每分钟 5 次规则下严格只放行 5 次，并发 32 个同邮箱冷却预留只有一次成功，同一验证码只有一次验证成功。此前一次全仓 interconnect E2E 受共享 VM 并发影响偶发失败，单测 5 轮和随后全仓 race 重跑均通过。该改动只影响 Web 验证码发送/验证，不改变设备协议或正常设备接入。
- 存储迁移本轮增量验证：Ubuntu `pkg/storage` 瞬态失败/取消/并发迁移/哈希保护专项 race 30 轮、包级 race/vet、全仓 race 和全仓 vet 均通过；新增同尺寸损坏目标自动修复测试，确认默认不会把“大小相同但内容错误”的对象误计为 `Skipped`；Windows 仅执行 `gofmt`、Git 和文本检查。测试覆盖前两次目标写入失败后第三次成功、取消后不等待重试、DeleteSource 内容不一致时保留源对象。
- 存储迁移本轮进一步修正并验证 worker 与哈希重试语义：`Workers=0` 按文档实际使用 4 个 worker，负数/1 保持串行且上限 16；SHA-256 内容不一致视为永久完整性失败，不再对同一大对象重复读取最多五次，真正的 Open/Read 瞬态错误仍按有界策略重试。Ubuntu `pkg/storage` 定向 race 50 轮、包级 race、全仓 race 和全仓 vet 通过；该专题仍因 S3 CopyObject、限速、真实对象规模和跨云故障边界保留“部分修复”。
- 存储迁移本轮补齐全局限速：新增 `-migrate-max-bytes-per-second`（默认 `0` 不限速），令牌预算在迁移运行内由全部 worker 共享，低速大块读取分段等待且 Context 取消会立即停止，不会按每个 worker 各自放行一份带宽。Ubuntu `TestMigrateRateLimiter*` 与既有迁移专项 `-race` 20 轮、`pkg/storage` 包级 race/vet、全仓 `go test -race ./... -count=1` 和 `go vet ./...` 均通过；无配置时的普通迁移、普通对象上传/下载、预签名 URL 和设备接入路径不变。存储迁移仍保留 S3 服务端 CopyObject、真实对象规模和跨云故障验收边界。
- 时间语义本轮完成兼容式统一入口：新增 `Database.Timezone`，空值默认归一为 `Local`；显式 IANA 时区会同时生成 DSN `loc`/`time_zone`、配置 GORM `NowFunc`，原生 SQL 与 GORM 的当前时间来源一致，非法时区在启动前拒绝。配置专项 race 20 轮、全仓 `go test -race ./... -count=1` 和全仓 `go vet ./...` 通过；默认 `Local` 不改变既有库或设备接入。仍需在 UTC、Asia/Shanghai、夏令时和生产旧 DATETIME 数据上做真实 MySQL 全链路验收。
- ffmpeg 沙箱本轮消除 Linux fork 后限额窗口：`runCommand` 在 `Start` 前将 ffprobe/ffmpeg 包装为 `ulimit -v`/`ulimit -t` 后立即 `exec` 原命令，目标进程从第一条指令即继承地址空间和 CPU 限额；新增 `/proc/self/limits` 回归测试，并保留原 `prlimit` 单元测试兼容接口。Ubuntu 媒体专项 race 20 轮、包级 race/vet、全仓 race/vet 通过；该改动不改变正常音频处理、设备协议或设备接入。非 Linux 限制仍依赖部署编排，seccomp/cgroup/noexec 与生产转码容量继续保留边界。
- 2026-08-22 Ubuntu 验证：测试副本 `/home/daofeng/draarl-s1-cidr.Ab1X83` 使用 Go 1.25.5 执行全仓 `go test -race ./... -count=1`，退出码 0；随后执行全仓 `go vet ./...`，退出码 0。Windows 仅执行 `git diff --check`（通过），未运行 Go 测试、构建或 vet。
- 2026-08-22 关键文件一致性：`internal/udphub/auth.go`、`internal/udphub/auth_job.go`、`internal/udphub/auth_job_test.go`、`internal/udphub/fanout_sender.go`、`internal/udphub/fanout_sender_test.go` 与 Ubuntu 测试副本 SHA-256 全部一致；报告文件在测试副本中未同步，故不将报告哈希列为代码验证证据。
- 本轮 H5 有界失败表文件一致性：`internal/udphub/sharded_map.go`、`internal/udphub/auth.go`、`internal/udphub/auth_job_test.go` 与 Ubuntu 测试副本 SHA-256 全部一致；Windows 仅执行 `gofmt`、Git 和文本检查。
- 本轮存储迁移文件一致性：`pkg/storage/migrate.go`、`pkg/storage/migrate_verify_test.go` 与 Ubuntu 测试副本 SHA-256 全部一致。
- 本轮 refresh-token 文件一致性：`internal/auth/refresh_token_store.go`、`internal/auth/refresh_token_store_test.go` 与 Ubuntu 测试副本 SHA-256 全部一致。
- 本轮设备 MAC 缓存文件一致性：`internal/udphub/device_mac_store.go`、`internal/udphub/device_mac_store_test.go`、`internal/udphub/runtime_index.go` 与 Ubuntu 测试副本 SHA-256 全部一致。
- 本轮增量重分析未发现可将 S1/H5/H6/H9/H12 完全标记为“完成”的新证据：S1 的 release 配置 fail-closed 已通过代码和 Ubuntu 验证，但仍缺真实 FRP/PROXY v2 拓扑；H5 仍缺 NAT/FRP 批量重连和容量验收，H6 仍有高负载丢帧边界，H9 仍有历史明文轮换和生产旧库失败恢复边界，H12 仍有生产旧库迁移非原子失败恢复风险；SiteConfig 专用 MySQL 惰性迁移已通过，但生产密钥轮换、旧密文备份恢复和历史 `openai.*` 清理仍未完成；H5/H6 小规模真实 UDP benchmark、H9 专用 MySQL HTTP E2E 与 H12 专用 MySQL 首次/重复/旧密码迁移验证均已通过。
- 本轮消息/群组查询优化已完成代码级闭环：群组列表设备统计从全表派生聚合改为当前页定向聚合，普通用户过滤禁用群组，群组分页页码增加溢出保护；消息百万行游标计划、消息 HTTP E2E、群组统计/权限分页 MySQL E2E、handler/gormdb/middleware race 20 轮及全仓 race/vet 均通过。该专题仍标记“部分修复”，仅保留生产规模群组 EXPLAIN、索引选择和极端深分页容量边界。
- 本轮操作日志优化已完成代码级闭环：事件类型/操作人复合索引在新库 AutoMigrate 和 MySQL EXPLAIN 中生效，分页溢出与非法参数被拒绝，兼容统计接口传播总数查询错误；专用 MySQL E2E 20 轮、handler/gormdb race 20 轮、全仓 race/vet 均通过。该专题仍标记“部分修复”，仅保留生产日志规模、真实索引选择和统计耗时边界。
- 本轮只检查了当前工作区与报告后的 Git 差异，未执行 Windows Go 测试/构建/vet，未提交、回滚或清理任何用户改动；`git diff --check` 通过。
- 本轮完成共享 Redis 登录/注册/验证码发送保护接入：`main.go` 初始化并 defer 关闭 store；Redis Lua 计数、SHA-256 key、请求超时、运行期 fail-closed、IPv4/IPv6 来源规范化和非法策略参数均有测试。Ubuntu 真实 Redis 双实例专项 20 轮、email/handler/cmd race、全仓 race 和全仓 vet 通过。该改动只作用于 Web 认证，不改变设备 UDP、DraARLv1、设备密码校验或正常设备接入；自动生成设备密码已恢复历史 8 位长度，仍由 CSPRNG 均匀生成并在随机源失败时 fail-closed。
- 本轮设备准入密码兼容性收口：`generateDevicePasswordFromReader` 的固定长度从 10 位恢复为历史 8 位，测试契约同步更新；Ubuntu VM 独立目录 `/home/daofeng/draarl-devicepass.y82scn` 已通过 `go test -race ./internal/handler -run TestGenerateDevicePassword -count=50`、`go test -race ./internal/handler -count=1` 和 `go vet ./internal/handler`。该改动只影响新生成/重生成的 Web 设备准入密码，不改变 DraARLv1/WebSocket 可接受的 6–10 位协议范围、既有密码密文、JWT 或 UDP 数据面。
- 本轮 SSO state 随机源失败处理收口：`generateStateFromReader` 使用 `io.ReadFull` 读取 128 bit CSPRNG，失败时向调用方返回错误，SSO 登录 URL、绑定 URL 和登录交换码保存不再降级为时间戳/计数器；Ubuntu VM 独立目录 `/home/daofeng/draarl-devicepass.y82scn` 已通过 `go test -race ./internal/handler -run "TestGenerate(State|DevicePassword)" -count=50`、`go test -race ./internal/handler -count=1` 和 `go vet ./internal/handler`。该改动只影响 Web SSO CSRF state/交换码生成，不改变普通账号密码登录、设备 JWT、DraARLv1、WebSocket 或 UDP 数据面。
- 报告状态已按“只保留未闭环项”重标：完全闭环的 S2/S3/H1-H4/H8/H11/H14 等专题不再作为当前风险章节；H7/H10/H13 等只保留其公开接口、历史对象或真实环境边界。
- 本次补偿 Context 改动只影响 Web 对象生命周期，不改变设备密码、DraARLv1/UDP/WebSocket 报文、设备地址绑定或既有设备接入条件；本轮仍未将真实设备、FRP、生产 MySQL 或容量测试缺失的项目标为完成。
- 本轮操作证/审批、通联日志和固件分页边界统一收口：仓储与 handler 在 SQL 前拒绝负 offset/极大页码，操作证批量查询、用户 Count 和审批用户查询错误向上传播；Ubuntu `internal/gormdb`/`internal/handler` 定向 race 3 轮、全仓 `go test -race ./... -count=1`、`go vet ./...` 均通过。该项已标记 `✅ 已验证修复`，不改变设备协议或设备接入。
- 本轮修复 `pkg/tcp.Client` 的停止竞态和回调读取竞态：拨号绑定 stop Context，停止后不安装新连接，读/错回调采用锁内快照；Ubuntu `pkg/tcp` race 3 轮、全仓 race/vet 均通过。该项已标记 `✅ 已验证修复`，只影响 APRS 辅助 TCP 连接。
- 本轮测试 JWT 路径复核完成：Ubuntu `test/simulator/utils` Python 单测 5 项全部通过，`HTTPClient.authenticate_with_test_key` 使用显式 `DRAARL_TEST_JWT_SECRET`/传入 key 直接签发 access JWT，不调用验证码或密码登录接口；缺少或短 key 仍 fail-closed。该项继续保持为已验证测试能力，不加入保留风险。
- 本轮通信录音上传队列收口：`CommUploader` 增加 4,096 会话硬上限、最旧淘汰和 `dropped_uploads` 监控；Ubuntu `internal/udphub` 录音专项 race 5 轮、全仓 race/vet 均通过。该子项标记 `✅ 已验证修复`；H6 整体仍保留真实过载、录音完整性和生产容量边界。

*行号仅作增量复核参考，会随当前未提交工作区继续演进；本报告不替代真实设备、生产数据库和容量验收。*
