# DraARL Server 优化清单

> 生成时间：2026-11-07  
> 项目版本：v2.0.1-alpha4  
> 分析范围：性能、安全、逻辑、细微 bug、UI/UX

## 优先级说明

- **P0 (Critical)**: 安全漏洞、数据丢失风险、系统崩溃
- **P1 (High)**: 明显的性能问题、严重的逻辑错误
- **P2 (Medium)**: 代码质量问题、潜在 bug、次要性能优化
- **P3 (Low)**: UI 改进、代码风格、文档优化

---

## P0 - 关键安全与稳定性问题

### 🔒 安全 Security

- [ ] **S-001: 完善 refresh token 吊销机制**
  - 文件：`internal/handler/auth_user_admin.go`
  - 问题：管理员禁用、删除或拒绝审批用户后，该用户的 refresh token 未立即吊销
  - 影响：被禁用的用户仍可使用现有 refresh token 获取新的 access token
  - 修复：在用户状态变更时调用 `auth.RevokeUserTokens(userID)`
  - 相关文件：`internal/handler/upload.go`, `internal/handler/auth_user_admin.go:200-250`

- [ ] **S-002: 邮箱验证码恒定时间比较**
  - 文件：`internal/handler/auth_login.go`
  - 问题：验证码比较可能存在时序攻击风险
  - 修复：使用 `subtle.ConstantTimeCompare()` 进行验证码比较
  - 代码位置：邮箱登录验证流程

- [ ] **S-003: context.Background() 应传递请求上下文**
  - 文件：多个文件（30+ 处）
  - 问题：使用 `context.Background()` 替代请求上下文，无法正确传播取消信号和超时
  - 影响：请求取消时可能造成 goroutine 泄漏和资源浪费
  - 修复：将 `c.Request.Context()` 传递到数据库操作和下游调用
  - 关键位置：
    - `internal/gormdb/device.go:109`
    - `internal/udphub/server_cache_sync.go`
    - `internal/handler/device.go:114-150`

- [ ] **S-004: 设备认证密码存储检查**
  - 文件：`internal/gormdb/models.go`
  - 问题：需确认设备密码是否使用安全哈希存储
  - 检查：`Device.Password` 字段的存储和验证机制
  - 修复：如使用明文，应改为 bcrypt 或 argon2

### 💥 稳定性 Stability

- [ ] **ST-001: UDP 设备下线时的群组遍历性能问题**
  - 文件：`internal/udphub/server_device_session.go`
  - 问题：设备下线清理按设备反向索引定位群组，索引缺失时需遍历全部群组
  - 影响：大规模部署时（100+ 群组）下线延迟明显
  - 修复：确保设备到群组的反向索引始终维护，添加兜底遍历的日志告警
  - 代码位置：设备下线清理逻辑

- [ ] **ST-002: 锁序问题 - 全局索引写锁内获取设备锁**
  - 文件：`internal/udphub/server_cache_sync.go`
  - 问题：在全局索引写锁内获取设备锁，可能导致死锁
  - 风险：死锁、锁竞争导致性能下降
  - 修复：调整锁序，确保总是先获取设备锁再获取全局锁，或使用 RCU 模式
  - 代码位置：`refreshGroupCache()` 函数

- [ ] **ST-003: 重放窗口大跨度跳跃清理问题**
  - 文件：`internal/interconnect/replay_window.go`
  - 问题：大跨度跳跃可能执行最多 16,383 次逐 ID 清理
  - 影响：节点重启或网络中断恢复时可能造成性能尖刺
  - 修复：批量清理或直接重置窗口，保持正常顺序流量不变

---

## P1 - 重要性能与逻辑问题

### ⚡ 性能 Performance

- [ ] **P-001: N+1 查询问题 - 设备列表获取所有者信息**
  - 文件：`internal/handler/group_devices.go:64-81`
  - 问题：每个设备单独查询所有者呼号
  - 现状：代码已有批量查询逻辑，需确认其他类似场景
  - 检查：`internal/handler/device.go` 中设备列表接口
  - 优化：统一使用 `userRepo.GetUserBriefByIDs(uniqueOwnerIDs)` 批量查询

- [ ] **P-002: 缓存未充分利用**
  - 文件：`pkg/cache/device_cache.go`, `internal/handler/device.go`
  - 问题：部分设备查询接口未使用已实现的缓存层
  - 影响：重复查询数据库，增加响应延迟
  - 修复：在设备列表、详情、群组设备等接口中启用缓存
  - 注意：缓存失效策略需与数据变更同步

- [ ] **P-003: UDP 转发数据包复用池不充分**
  - 文件：`internal/protocol/draarl.go:15-27`
  - 问题：已有 `forwardPacketPool` 和 `routingPacketPool` 但可能未在所有路径使用
  - 检查：确认所有 fan-out 路径都调用 `ReleaseForwardPacket`
  - 优化：减少高频路径的内存分配

- [ ] **P-004: 数据库连接池配置**
  - 文件：`internal/gormdb/gorm.go`
  - 问题：需确认连接池大小、空闲连接数、连接存活时间配置是否合理
  - 检查：`SetMaxOpenConns`, `SetMaxIdleConns`, `SetConnMaxLifetime`
  - 建议：根据负载测试调优，添加连接池监控指标

- [ ] **P-005: 频繁的时间格式化**
  - 文件：多处使用 `CreateTime.Format("2006-01-02 15:04:05")`
  - 问题：每次响应都执行字符串格式化
  - 优化：考虑在 JSON 序列化时统一处理，或缓存常用格式

### 🐛 逻辑 Logic

- [ ] **L-001: 设备型号验证不一致**
  - 文件：`internal/protocol/draarl.go`, `internal/udphub/server_device_session.go`
  - 问题：`IsValidClientReportedDevModel` 和实际型号定义可能不同步
  - 检查：确认前后端设备型号定义一致性
  - 相关：`www/src/utils/deviceModel.ts`

- [ ] **L-002: 群组权限检查逻辑**
  - 文件：`internal/handler/authorization.go:31`
  - 问题：`canManageGroup` 中使用 `OwerID`（拼写错误）
  - 影响：代码可读性差，可能是历史遗留的拼写错误
  - 修复：统一修正为 `OwnerID`，需数据库迁移支持
  - 相关文件：`internal/gormdb/models.go`

- [ ] **L-003: 错误处理不一致**
  - 文件：多个 handler 文件
  - 问题：有些返回 500，有些返回 400，错误消息格式不统一
  - 优化：建立统一的错误响应规范
  - 建议：创建 `internal/handler/errors.go` 统一错误处理

- [ ] **L-004: 分页参数验证**
  - 文件：`internal/handler/device.go:82-112`
  - 问题：limit 上限硬编码为 100，不同接口可能不一致
  - 优化：提取为配置常量，统一分页参数验证逻辑

---

## P2 - 代码质量与潜在问题

### 🔧 代码质量 Code Quality

- [ ] **CQ-001: TODO 注释未处理**
  - 文件：
    - `internal/udphub/device.go:625` - 设备操作日志
    - `internal/log/operator.go:60` - 从上下文获取用户信息
  - 建议：评估是否需要实现，或删除过期注释

- [ ] **CQ-002: 重复的呼号格式化逻辑**
  - 文件：`internal/udphub/server_device_session.go:150`
  - 问题：`fmt.Sprintf("%s-%d", authResult.CallSign, packet.SSID)` 多处重复
  - 优化：提取为 `FormatCallSignSSID(callSign string, ssid byte) string` 辅助函数

- [ ] **CQ-003: 魔法数字**
  - 问题：代码中存在大量魔法数字
  - 示例：
    - 心跳间隔 `10 * time.Second` (`internal/udphub/server_cache_sync.go:26`)
    - 缓存 TTL `5*time.Minute` (`pkg/cache/device_cache.go:95`)
  - 优化：提取为命名常量或配置项

- [ ] **CQ-004: 长函数拆分**
  - 文件：`internal/handler/auth_user_admin.go:GetUsers()`
  - 问题：函数超过 100 行，包含多个职责
  - 优化：拆分为更小的函数（权限检查、查询、转换）

- [ ] **CQ-005: 未使用的导入和变量**
  - 检查：运行 `go vet ./...` 和 `golangci-lint run`
  - 清理：移除未使用的导入和变量

### 🪲 潜在 Bug Potential Bugs

- [ ] **PB-001: nil 指针检查不足**
  - 文件：多处
  - 问题：部分函数未充分检查参数是否为 nil
  - 检查位置：
    - `internal/handler/authorization.go:26-44` - device, group 参数
    - `internal/udphub/server_device_session.go` - packet 处理
  - 修复：添加 nil 检查，使用 Go 1.25 的改进错误处理

- [ ] **PB-002: 竞态条件风险**
  - 文件：`internal/udphub/server_cache_sync.go`
  - 问题：缓存更新和读取之间可能存在竞态
  - 检查：运行 `go test -race ./internal/udphub/...`
  - 修复：确保所有缓存访问都有适当的锁保护

- [ ] **PB-003: 设备在线状态同步延迟**
  - 文件：`internal/udphub/server_cache_sync.go:26`
  - 问题：10 秒同步间隔可能导致状态不一致
  - 影响：UI 显示延迟，设备状态跳变
  - 优化：考虑事件驱动更新 + 定期全量同步

- [ ] **PB-004: 时区处理**
  - 文件：多处时间处理
  - 问题：未明确指定时区，可能造成跨时区部署问题
  - 检查：`time.Now()` 使用位置，数据库时间字段类型
  - 建议：统一使用 UTC，前端按用户时区显示

- [ ] **PB-005: 字符串编码问题**
  - 文件：`internal/protocol/draarl.go`
  - 问题：用户名、呼号字段处理是否正确处理 UTF-8
  - 检查：协议中 32B 用户名字段的截断逻辑
  - 测试：使用多字节 UTF-8 字符测试

---

## P3 - UI/UX 与优化建议

### 🎨 前端 UI/UX

- [ ] **UI-001: 错误提示不友好**
  - 文件：多个 React 组件
  - 问题：部分错误消息直接显示技术细节
  - 示例：`www/src/pages/devices/DevicesPage.tsx`
  - 优化：使用 `getErrorMessage()` 统一处理，提供用户友好的提示

- [ ] **UI-002: 加载状态缺失**
  - 文件：`www/src/components/devices/preconfig/PreConfigToolCard.tsx`
  - 问题：部分异步操作缺少 loading 状态
  - 影响：用户不知道操作是否在执行
  - 修复：所有异步操作添加 loading 状态和进度反馈

- [ ] **UI-003: console.log 未清理**
  - 文件：39 个 TypeScript 文件包含 console.log
  - 问题：生产环境残留调试日志
  - 清理：移除或使用条件编译
  - 建议：配置 ESLint 规则禁止 console.log

- [ ] **UI-004: 浏览器兼容性检查**
  - 文件：`www/src/components/devices/preconfig/PreConfigToolCard.tsx:58-69`
  - 问题：`isChromeBrowser()` 检测逻辑可能不准确
  - 优化：使用更可靠的特性检测而非 UA 嗅探
  - 建议：直接检查 `navigator.bluetooth` 可用性

- [ ] **UI-005: 无障碍性改进**
  - 问题：部分交互元素缺少 ARIA 标签
  - 检查：按钮、输入框、对话框的可访问性
  - 工具：使用 axe-core 或 Lighthouse 检查
  - 修复：添加适当的 `aria-label`, `aria-describedby`

- [ ] **UI-006: 响应式布局优化**
  - 文件：多个页面组件
  - 问题：移动端显示可能不理想
  - 检查：在小屏幕设备上测试关键页面
  - 优化：调整 Grid 断点，隐藏或折叠次要信息

- [ ] **UI-007: 表单验证反馈**
  - 文件：`www/src/components/devices/preconfig/PreConfigToolCard.tsx`
  - 问题：动态码验证反馈不够及时
  - 优化：添加实时验证，在输入时显示格式提示
  - 示例：6 位数字输入，显示剩余位数

### 📱 交互改进 Interaction

- [ ] **IX-001: 设备配置保存确认**
  - 文件：`www/src/components/devices/preconfig/PreConfigToolCard.tsx`
  - 问题：保存配置后无明确成功反馈
  - 优化：添加成功提示音或动画，确认配置已生效

- [ ] **IX-002: 批量操作支持**
  - 文件：`www/src/pages/devices/DevicesPage.tsx`
  - 功能：设备列表支持批量选择和操作
  - 建议：添加全选、批量禁用、批量切组功能

- [ ] **IX-003: 搜索优化**
  - 文件：设备、用户、群组列表页面
  - 优化：添加搜索防抖，减少请求频率
  - 建议：显示搜索历史，支持高级筛选

- [ ] **IX-004: 快捷键支持**
  - 建议：为常用操作添加键盘快捷键
  - 示例：`Ctrl+S` 保存配置，`Esc` 关闭对话框
  - 实现：使用 `useHotkeys` hook

---

## 工具与自动化

### 🔨 开发工具 Development Tools

- [ ] **T-001: 配置 pre-commit hooks**
  - 工具：husky + lint-staged
  - 检查：格式化、linting、类型检查
  - 配置文件：`.husky/pre-commit`

- [ ] **T-002: 添加 golangci-lint 配置**
  - 文件：`.golangci.yml`
  - 启用检查器：gosec, errcheck, ineffassign, staticcheck
  - 集成到 CI/CD

- [ ] **T-003: 性能基准测试**
  - 目录：`test/bench/`
  - 场景：UDP 转发、设备认证、数据库查询
  - 目标：建立性能基线，防止回归

- [ ] **T-004: 集成测试覆盖**
  - 文件：`internal/handler/*_test.go`
  - 当前：部分单元测试
  - 目标：关键流程端到端测试覆盖率 > 80%

- [ ] **T-005: 前端单元测试**
  - 工具：Vitest + React Testing Library
  - 优先级：关键组件（设备配置、无线电页面）
  - 目标：核心工具函数测试覆盖率 > 90%

### 📊 监控与可观测性 Monitoring

- [ ] **M-001: 添加结构化日志**
  - 替换：`log.Printf` → 结构化日志库（zerolog/zap）
  - 好处：便于日志聚合和分析
  - 字段：timestamp, level, trace_id, user_id, device_id

- [ ] **M-002: 关键指标暴露**
  - 端点：`/metrics` (Prometheus 格式)
  - 指标：
    - 在线设备数
    - UDP 包处理速率
    - 数据库查询延迟
    - 缓存命中率
    - WebSocket 连接数

- [ ] **M-003: 健康检查增强**
  - 文件：`internal/server/server.go`
  - 当前：`/healthz` 仅返回 OK
  - 优化：检查数据库连接、Redis 连接、关键依赖状态
  - 返回：详细的健康状态 JSON

- [ ] **M-004: 分布式追踪**
  - 工具：OpenTelemetry
  - 追踪：HTTP 请求、数据库查询、UDP 处理链路
  - 集成：Jaeger 或 Zipkin

---

## 文档与规范

### 📚 文档 Documentation

- [ ] **D-001: API 文档自动生成**
  - 工具：Swagger/OpenAPI
  - 来源：代码注释 + 结构体标签
  - 目标：自动化 API 文档更新

- [ ] **D-002: 部署文档补充**
  - 文件：`docs/usage/01-部署与配置.md`
  - 补充：
    - 生产环境性能调优指南
    - 高可用部署架构
    - 备份恢复流程
    - 监控告警配置

- [ ] **D-003: 故障排查手册**
  - 文件：`docs/usage/08-运维与排障.md`
  - 补充：
    - 常见问题清单
    - 日志分析方法
    - 性能问题诊断
    - 紧急恢复流程

- [ ] **D-004: 代码贡献指南**
  - 文件：`CONTRIBUTING.md`
  - 内容：
    - 代码风格规范
    - 提交消息格式
    - PR 流程
    - 测试要求

### 🎯 规范 Standards

- [ ] **S-001: 建立错误码规范**
  - 文件：`internal/handler/errors.go`
  - 规范：统一的错误码体系
  - 格式：`{"code": 40001, "message": "设备不存在", "detail": "..."}`

- [ ] **S-002: 数据库迁移流程**
  - 工具：golang-migrate 或 GORM AutoMigrate
  - 流程：版本化 SQL 文件，向后兼容
  - 文档：每次迁移的 rollback 方案

- [ ] **S-003: 前端组件库规范**
  - 目录：`www/src/components/common/`
  - 规范：可复用组件的 props、样式、文档
  - 工具：Storybook 展示组件库

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
- 所有修改都应先在独立分支完成
- 安全修复优先于性能优化
- 保持向后兼容性
- 充分测试后再合并到主分支
