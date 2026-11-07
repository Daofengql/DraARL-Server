# DraARL Server 优化清单

> 生成时间：2026-11-07  
> 项目版本：v2.0.1-alpha4  
> 分析范围：性能、安全、逻辑、细微 bug、UI/UX

> 2026-11-07 最终记录：可实施修改已在 `dev` 完成；验证与剩余边界见 [最终验证记录](docs/todo-validation-2026-11-07.md)。
>
> 保留 `master`、`dev`、`edge` 三个分支。AES 可逆存储与 `OwerID` 拼写按设计保留，不作为待修缺陷。未部署到生产。

## 优先级说明

- **P0 (Critical)**: 安全漏洞、数据丢失风险、系统崩溃
- **P1 (High)**: 明显的性能问题、严重的逻辑错误
- **P2 (Medium)**: 代码质量问题、潜在 bug、次要性能优化
- **P3 (Low)**: UI 改进、代码风格、文档优化

---

## P0 - 关键安全与稳定性问题

### 🔒 安全 Security

- [x] **S-001: 完善 refresh token 吊销机制**
  - 已复核：基线已在身份变更、禁用、删除及撤销审批时调用 `revokeUserRefreshSessions`，并配合会话版本校验。
  - 文件：`internal/handler/auth_user_admin.go`、`internal/handler/upload.go`。
  - 验证：虚拟机 MySQL/Redis 认证回归通过，禁用后重新启用仍拒绝旧 access/refresh token。

- [x] **S-002: 邮箱验证码恒定时间比较**
  - 已复核：实际比较位于 `internal/email/verification.go`，基线已使用 `subtle.ConstantTimeCompare()`。
  - 验证：`internal/email` 测试通过。

- [x] **S-003: context.Background() 应传递请求上下文**
  - 请求处理的数据库、缓存、存储和下游调用传递请求 context；必要的后台清理使用独立且有界的超时。真实 MySQL 取消与 DB trace 继承测试通过。

### 📦 上游依赖与 CVE 整改

- [x] **DEP-001: 建立依赖 CVE 清单与影响核查**
  - 完成 Go/npm/四个最终容器的基线与升级扫描，记录模块 finding 与调用链证据的区别。见 [依赖审计](docs/security/dependency-audit-2026-11-07.md)；仍有一条无上游修复版本的 imaging 公告，见 DEP-005。

- [x] **DEP-002: 从上游升级 Go 工具链和后端依赖**
  - 工具链、CI 与容器统一 Go 1.27.3，最低 Go 1.26.10；模块更新见 go.mod/go.sum。x/crypto 使用上游修复提交的 pseudo-version。Windows 与 Linux embed govulncheck 均为 0 finding；test/vet/lint、Linux race 和真实依赖回归通过。

- [x] **DEP-003: 从上游升级前端依赖并更新锁文件**
  - 按上游版本更新依赖与锁文件，Node 24.22.2 全新 npm ci、lint、类型、13 项 legacy 测试、31 项 Vitest 和生产构建通过。完整 npm 审计由 11 项降至 0，生产依赖审计 0。

- [x] **DEP-004: 更新容器基础镜像并保留升级证据**
  - 虚拟机构建并启动应用、MySQL、Redis、MinIO 四镜像；记录镜像 ID、报告 SHA-256 与启动、持久化、备份恢复证据。最终 MySQL/Redis/MinIO 扫描 0，应用剩 imaging 一项 LOW（NVD 评分为 MEDIUM）。

- [ ] **DEP-005: 跟进 imaging 无修复版本公告**
  - 最终应用镜像仍有 `github.com/disintegration/imaging v1.6.2` / CVE-2023-36308；Trivy 为 LOW，报告另含 NVD 5.5/MEDIUM。现有图片大小/尺寸校验仅是缓解，不记为已修复；等待上游修复或另行验证替换库。

### 💥 稳定性 Stability

- [x] **ST-001: UDP 设备下线时的群组遍历性能问题**
  - 文件：`internal/udphub/runtime_index.go`
  - 问题：设备下线清理按设备反向索引定位群组，索引缺失时需遍历全部群组
  - 影响：大规模部署时（100+ 群组）下线延迟明显
  - 修复：确保设备到群组的反向索引始终维护，添加兜底遍历的日志告警
  - 代码位置：设备下线清理逻辑
  - 完成：通过设备 `GroupID` 直接定位当前群组，缺失时保留兼容扫描、节流告警及计数；保留历史私有群组清理。
  - 验证：未分组、正常群组、缺失群组兜底及设备清理回归通过。

- [x] **ST-002: 锁序问题 - 全局索引写锁内获取设备锁**
  - 文件：`internal/udphub/runtime_index.go`
  - 问题：在全局索引写锁内获取设备锁，可能导致死锁
  - 风险：死锁、锁竞争导致性能下降
  - 修复：调整锁序，确保总是先获取设备锁再获取全局锁，或使用 RCU 模式
  - 完成：索引登记、入口同步、呼号变更统一按设备锁→索引锁执行，移除反向取锁路径。
  - 验证：两条并发路径各执行 5000 次的锁序回归及 Linux 竞态测试通过。

- [x] **ST-003: 重放窗口大跨度跳跃清理问题**
  - 文件：`internal/interconnect/replay_window.go`
  - 问题：大跨度跳跃可能执行最多 16,383 次逐 ID 清理
  - 影响：节点重启或网络中断恢复时可能造成性能尖刺
  - 修复：批量清理或直接重置窗口，保持正常顺序流量不变
  - 已复核：基线已按 64 位字批量清理，跨整窗直接重置；既有窗口边界及随机序列测试通过。

---

## P1 - 重要性能与逻辑问题

### ⚡ 性能 Performance

- [x] **P-001: N+1 查询问题 - 设备列表获取所有者信息**
  - 文件：`internal/handler/group_devices.go:64-81`
  - 问题：每个设备单独查询所有者呼号
  - 现状：代码已有批量查询逻辑，需确认其他类似场景
  - 检查：`internal/handler/device.go` 中设备列表接口
  - 优化：统一使用 `userRepo.GetUserBriefByIDs(uniqueOwnerIDs)` 批量查询
  - 完成：移除设备列表在批量查询后逐个加载所有者缓存的重复路径，补齐批量查询错误处理。
  - 验证：真实 MySQL 冷缓存测试返回 6 个不同所有者的设备，仅执行 1 次所有者查询，所有者字段与未分组状态完整。

- [x] **P-002: 缓存未充分利用**
  - 设备筛选列表使用权限、筛选与分页维度完整的短 TTL 缓存，页面与总数作为一致快照；变更失效、热查询和真实 MySQL 回归通过。

- [x] **P-003: UDP 转发数据包复用池不充分**
  - 审查同步/异步 fan-out 的所有权和 ReleaseForwardPacket 生命周期，异步队列持有独立缓冲；UDP/WS/边缘路由及 Linux race 回归通过，保留池化与新建缓冲微基准。

- [x] **P-004: 数据库连接池配置**
  - 沿用可配置的最大连接、空闲连接和生命周期；增加等待数、等待耗时、使用中/空闲连接指标，并验证真实 MySQL readiness、查询与恢复。配置说明区分基线与实际容量调优。

- [x] **P-005: 频繁的时间格式化**
  - 既有 API 共用 common.FormatAPITime 与命名布局，保留旧时间格式和数据库时区契约；未引入可能过期的时间字符串缓存，也不宣称已有性能收益。

### 🐛 逻辑 Logic

- [x] **L-001: 设备型号验证不一致**
  - 文件：`internal/protocol/draarl.go`, `internal/udphub/server_device_session.go`
  - 问题：`IsValidClientReportedDevModel` 和实际型号定义可能不同步
  - 检查：确认前后端设备型号定义一致性
  - 相关：`www/src/utils/deviceModel.ts`
  - 完成：补齐前端 106/107 历史型号展示，并保持不可新选；后端协议校验与型号测试通过。

- [x] **L-003: 错误处理不一致**
  - handler 共用 writeErrorResponse，统一 code/message/request_id，兼容原有 error/message 字段并隐藏内部 500 细节；参数、权限、缺失资源、冲突与服务错误分层，前端共用错误映射。

- [x] **L-004: 分页参数验证**
  - 文件：`internal/handler/device.go:82-112`
  - 问题：limit 上限硬编码为 100，不同接口可能不一致
  - 优化：提取为配置常量，统一分页参数验证逻辑
  - 完成：设备、用户、群组、操作日志共用默认值/上限；修复 `limit=1` 时溢出误拒绝合法页码，保留内部大批量查询能力。
  - 验证：默认值、上限、最大合法 offset 和超大页码回归通过；已先用新测试复现群组及操作日志的误拒绝。

- [x] **L-005: 彻底移除 PROXY v2 协议及相关保护配置**
  - 已删除 UDP PROXY v2 解析、地址提取、专用保护、字段、示例与相关测试；旧字段即使类型非法也直接忽略。保留 System.HTTPTrustedProxyCIDRs，UDP 只认 socket 对端；旧配置启动和普通 UDP 回归通过。

---

- [x] **L-006: 自动播报在互联状态下保持组内投递**
  - A/B 互联时，A 的定时/手动播报只发给明确接收 A 的会话；普通语音保留互联转发。中心 UDP/WS、边缘 UDP/TLS、来源组录音权限均有接收测试；静默检测与话权仍覆盖互联域，不转发跨中心。旧边缘须支持组内播报能力后才能接收播报。

---

## P2 - 代码质量与潜在问题

### 🔧 代码质量 Code Quality

- [x] **CQ-001: TODO 注释未处理**
  - 设备活动摘要保留诊断日志，通信记录由专用 recorder 持久化，移除重复审计的过期 TODO；删除无人调用的日志兼容空壳，AddLog 显式接收用户身份。上下线事件另有真实 MySQL 回归。

- [x] **CQ-002: 重复的呼号格式化逻辑**
  - 文件：`internal/udphub/server_device_session.go:150`
  - 问题：`fmt.Sprintf("%s-%d", authResult.CallSign, packet.SSID)` 多处重复
  - 优化：提取为 `FormatCallSignSSID(callSign string, ssid byte) string` 辅助函数
  - 完成：中心和边缘设备认证路径统一复用既有 `protocol.GetCallSignSSID()`，设备会话测试通过。

- [x] **CQ-003: 魔法数字**
  - 提取设备详情/列表 TTL、缓存同步周期、后台查询超时、在线事件队列、操作日志批量/刷新和分页参数常量；保留协议规范定义的固定字段。

- [x] **CQ-004: 长函数拆分**
  - GetUsers 分离身份权限、分页查询与响应转换，复用 userListItem；用户与设备接口真实 MySQL 回归通过。

- [x] **CQ-005: 未使用的导入和变量**
  - 生产包、测试和工具的 go vet 与 golangci-lint 检查通过；清理 errcheck、staticcheck、ineffassign 与 gosec 检出的实际问题。

### 🪲 潜在 Bug Potential Bugs

- [x] **PB-001: nil 指针检查不足**
  - 补齐设备注册/心跳、JWT 身份断言、权限辅助、缓存转换、FFmpeg 命令入口和协议边界的空参数/类型检查；对应单元与真实链路回归通过。

- [x] **PB-002: 竞态条件风险**
  - 文件：`internal/udphub/server_cache_sync.go`
  - 问题：缓存更新和读取之间可能存在竞态
  - 检查：运行 `go test -race ./internal/udphub/...`
  - 修复：确保所有缓存访问都有适当的锁保护
  - 完成：设备缓存刷新串行化，删除设备与缓存失效使用运行时快照；保留现有群组静态快照发布机制。
  - 验证：虚拟机 UDP 会话/缓存回归及 Linux `-race` 检查通过。

- [x] **PB-003: 设备在线状态同步延迟**
  - 上线、迁移、下线触发事件持久化，短延迟合并后刷新列表缓存；周期同步作为兜底。真实 MySQL 验证最新会话覆盖旧事件，Linux race 通过。

- [x] **PB-004: 时区处理**
  - 站点提供数据库时区，前端 parseAPIDate 正确解释无偏移的历史 DATETIME 并按用户本地显示；显式 ISO 偏移、UTC/BJT 日志簿契约保留。UTC、固定偏移、IANA 时区测试通过；未批量改写历史时间数据。

- [x] **PB-005: 字符串编码问题**
  - 文件：`internal/protocol/draarl.go`
  - 问题：用户名、呼号字段处理是否正确处理 UTF-8
  - 检查：协议中 32B 用户名字段的截断逻辑
  - 测试：使用多字节 UTF-8 字符测试
  - 完成：普通包、转发包和心跳包的用户名/呼号字段按完整 UTF-8 字符截断；协议长度、密码字段布局保持兼容。
  - 验证：中文、emoji、ASCII 混合边界和源报文未被修改的回归通过。

---

## P3 - UI/UX 与优化建议

### 🎨 前端 UI/UX

- [x] **UI-001: 错误提示不友好**
  - 表单/列表统一 getErrorMessage，显示操作相关提示并过滤后端内部异常；网络、权限与业务错误回归通过。

- [x] **UI-002: 加载状态缺失**
  - 文件：`www/src/components/devices/preconfig/PreConfigToolCard.tsx`
  - 问题：部分异步操作缺少 loading 状态
  - 影响：用户不知道操作是否在执行
  - 修复：所有异步操作添加 loading 状态和进度反馈
  - 完成：连接、认证、刷新、读取、保存、断开均有加载状态；操作期间互斥、禁用表单及关闭入口。
  - 验证：浏览器模拟 BLE 连接与保存时检查进度条、禁用状态和 Escape 防误关，完成后恢复。

- [x] **UI-003: console.log 未清理**
  - 文件：`www/src` 调试输出调用处
  - 问题：生产环境残留调试日志
  - 清理：移除或使用条件编译
  - 建议：配置 ESLint 规则禁止 console.log
  - 完成：调试输出集中到仅开发环境启用的 `debugLog`，ESLint 仅允许业务 warn/error；源码检查与 lint 通过。

- [x] **UI-004: 浏览器兼容性检查**
  - 文件：`www/src/components/devices/preconfig/PreConfigToolCard.tsx:58-69`
  - 问题：`isChromeBrowser()` 检测逻辑可能不准确
  - 优化：使用更可靠的特性检测而非 UA 嗅探
  - 建议：直接检查 `navigator.bluetooth` 可用性
  - 完成：直接采用 BLE 客户端的能力检测结果；浏览器移除蓝牙 API 后显示提示并禁用连接，即使 UA 仍为 Chromium。

- [x] **UI-005: 无障碍性改进**
  - 补齐页面标题层级、导航列表、按钮/输入/Select 名称、弹窗关联、滚动区键盘访问、进度条名称和浅色对比度。13 个关键路由的桌面/768px/320px axe 扫描零违规；范围限已扫描的浅色页面，未宣称所有主题/页面完全合规。

- [x] **UI-006: 响应式布局优化**
  - 13 个关键路由在桌面、768px、320px 检查无页面横向溢出；小屏预配置全屏、表格区域可滚动，已保存截图证据。

- [x] **UI-007: 表单验证反馈**
  - 文件：`www/src/components/devices/preconfig/PreConfigToolCard.tsx`
  - 问题：动态码验证反馈不够及时
  - 优化：添加实时验证，在输入时显示格式提示
  - 示例：6 位数字输入，显示剩余位数
  - 完成：过滤非数字、限制 6 位、实时显示剩余位数，码未填满时禁用提交；浏览器模拟连接后验证通过。

### 📱 交互改进 Interaction

- [x] **IX-001: 设备配置保存确认**
  - 保存后回读 WiFi/DraARL 配置并逐项比较，匹配后才提示成功；不一致拒绝成功提示。浏览器模拟 BLE 已验证，物理硬件持久化/断电验证仍需设备。

- [x] **IX-002: 批量操作支持**
  - 增加全选、批量启用/禁用、切组和清空群组；部分失败保留失败项并可重试。真实 API 浏览器验证三台设备的部分 503、重试与切组后回读。

- [x] **IX-003: 搜索优化**
  - 设备、用户与群组搜索增加 useDebouncedValue，减少逐字请求，保留现有筛选条件；防抖单元与浏览器回归通过。搜索历史和新增高级筛选属于可选产品功能，未纳入本轮。

- [x] **IX-004: 快捷键支持**
  - 预配置 Ctrl+S/Cmd+S 保存、Escape 关闭，busy 状态禁止关闭；PTT 键盘交互避开输入框与组合键。hook/组件单元和浏览器回归通过。

---

## 工具与自动化

### 🔨 开发工具 Development Tools

- [x] **T-001: 配置 pre-commit hooks**
  - 使用 .githooks/pre-commit 与 Go 工具检查暂存 Go 格式、go vet、前端 lint/类型/Vitest，支持 Windows npm.cmd。启用命令与检查范围见 CONTRIBUTING.md；本次提交实际执行 hook。

- [x] **T-002: 添加 golangci-lint 配置**
  - .golangci.yml 启用 govet、gosec、errcheck、ineffassign、staticcheck，CI 同步运行；AES/协议兼容例外有明确说明，其余具体例外在源码解释。

- [x] **T-003: 性能基准测试**
  - 保留 UDP header 池化/新建、AES 设备认证、设备分页冷 DB/热缓存的基准；虚拟机三轮基线见最终验证文档。结果是微基准，不代表整机并发容量或百万行规模。

- [ ] **T-004: 集成测试覆盖**
  - 新增可复现 tools/integration，28 个独立真实 MySQL/Redis/FFmpeg suite 已执行，涵盖关键流程并有机器可读证据；不能把 suite 通过率等同语句覆盖率。合并 handler 语句覆盖约 30.52%，仓库约 66.82%，整体 >80% 目标尚未达到，继续作为覆盖率改进项。

- [x] **T-005: 前端单元测试**
  - 引入 Vitest/React Testing Library，设备配置回读、PTT、交互 hook 与工具测试 31 项通过；5 个核心工具文件行/语句/函数覆盖 100%，分支 98.73%，另保留 13 项 legacy 测试。

### 📊 监控与可观测性 Monitoring

- [x] **M-001: 添加结构化日志**
  - 使用标准库 slog 提供 text/json 输出，HTTP 日志包含 trace_id/request_id，启动时接管既有 log 输出；保留测试和兼容日志文本。

- [x] **M-002: 关键指标暴露**
  - 受管理员认证保护的 /metrics 暴露 UDP、在线设备/WS、缓存命中及数据库池/查询指标；匿名访问真实返回 401，不使用 owner/device 作为高基数标签。

- [x] **M-003: 健康检查增强**
  - /healthz 保持进程存活检查，新增 /readyz 有界探测 MySQL/Redis。虚拟机停止各依赖得到 healthz=200/readyz=503，恢复后 readyz=200。

- [x] **M-004: 分布式追踪**
  - 可选 OTLP/HTTP 接入 HTTP、DB、UDP，有限队列与采样。虚拟机 collector 实收并解码 3325 spans，其中 1185 个 DB span 直接继承 HTTP，SQL 文本属性为 0。

---

## 文档与规范

### 📚 文档 Documentation

- [x] **D-001: API 文档自动生成**
  - tools/openapi 从路由、请求/响应类型生成 docs/api/openapi.json；go run ./tools/openapi -check 接入 CI，避免手写文档漂移。

- [x] **D-002: 部署文档补充**
  - 补充连接池/资源调优、中心与边缘边界、监控、四镜像升级、MinIO 卷权限、备份恢复与回退说明，并链接真实验证记录。

- [x] **D-003: 故障排查手册**
  - 补充 readiness、数据库池、缓存/在线事件、OTLP、FFmpeg 资源限制和恢复排查；区分探活失败与依赖不可用。

- [x] **D-004: 代码贡献指南**
  - 新增 CONTRIBUTING.md，记录提交格式、分支、格式/lint/test、hook、兼容设计与安全扫描要求。

### 🎯 规范 Standards

- [x] **S-001: 建立错误码规范**
  - 规范 code/message/request_id 和旧字段兼容，HTTP 状态与错误类别一致，内部异常通过请求标识关联日志；见 internal/handler/errors.go。

- [x] **S-002: 数据库迁移流程**
  - 补充 docs/database-migrations.md，记录版本化 AutoMigrate、备份、停机、验证和不自动降级边界；真实 MySQL 32 表备份恢复逐表行数一致，两次应用重启迁移记录稳定。

- [x] **S-003: 前端组件库规范**
  - 新增 docs/frontend-components.md，规定 props、交互/加载/错误、ARIA、测试及复用边界，列出现有公共组件用法。

---

## 执行计划

### 第一阶段（1-2 周）：关键安全与稳定性

1. 完成所有 P0 级别问题
2. 建立 CI/CD 基础（linting, testing）
3. 配置基础监控

### 第二阶段（2-3 周）：性能与逻辑优化

1. 完成 P1 级别问题
2. 性能基准测试
3. 增加测试覆盖率

### 第三阶段（3-4 周）：代码质量与 UI 改进

1. 完成 P2 和 P3 级别问题
2. 前端优化和无障碍性改进
3. 文档完善

### 持续改进

- 每周代码审查
- 每月性能回归测试
- 每季度安全审计

---

## 验证清单

完成每项优化后，请在对应的复选框打勾，并记录：

- 修改的文件和提交哈希
- 测试结果（单元测试、集成测试、性能测试）
- 相关的 Issue 或 PR 链接
- 部署验证结果

**注意**：
- 按用户要求在 `dev` 继续开发，保留 `master`、`dev`、`edge`，本轮不合并到 master
- 安全修复优先于性能优化
- 保持向后兼容性
- 充分测试后再合并到主分支
