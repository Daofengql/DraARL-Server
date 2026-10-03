# DraARL Server 整改记录（2026-10-03）

基于 [审计报告](./audit-2026-10-03.md)，在本地 `dev` 分支、原始 HEAD `7d62762` 上完成以下修复。这里记录最终实现和实测结果；审计报告保留修复前的证据。

修改尚未提交或推送。保留原有 `.gitignore` 修改、两个 tools 文件的删除以及 `report.md`、`todo.md`。
运行验证使用 Ubuntu Hyper-V 中的隔离服务、专用 MySQL 数据库及 Redis 前缀，没有替换原来的 29000 审计服务。

## 优先级与状态

| 编号 | 优先级 | 问题与处理结果 | 状态 |
|---|---|---|---|
| A1 | P0 | 禁用再启用可复用旧会话：增加会话版本并吊销 refresh | 已修复、回归通过 |
| A2 | P0 | 用户名复用造成 JWT 身份漂移：改为不可变 ID / sub | 已修复、回归通过 |
| A3 | P0 | 任意 XFF 可绕过限流：默认不信任转发头，仅允许明确 HTTP 代理 | 已修复、回归通过 |
| A11 | P0 | 进程内用户缓存可能延迟看到其他实例的吊销：认证读取数据库当前状态 | 已修复、缓存滞后回归通过 |
| A5 | P1 | 新库缺少公共频道 999：迁移种子、启动自检、禁止破坏系统频道 | 已修复、全新库回归通过 |
| A4 | P1 | 日志不能清空可选字段：省略、空值、零值有明确语义 | 已修复、API / 浏览器验证通过 |
| A8 | P1 | 修改日志时间时秒数拼接索引错误 | 已修复、浏览器保存验证通过 |
| A9 | P1 | 删除用户后未播放的播报录音残留 | 已修复、真实播报集成验证通过 |
| A10 | P1 | 管理员重置他人密码被旧密码 required 校验拦截 | 已修复、权限与会话回归通过 |
| A7 | P1 | Go / Node 安全依赖升级与扫描基线 | 已升级、发布扫描通过 |
| A6 | P1 | HTTP 页面不能用麦克风但缺少明确说明 | 提示与 PTT 保护已修复；生产 HTTPS 配置待部署验证 |
| U1 | P1 | 平板完整导航中文竖排 | 已修复、768 / 1024 / 1440 截图复核 |
| U2 | P1 | 手机设备表格拥挤、操作难找 | 已改卡片，320–1024 截图复核 |
| U3 | P1 | 手机用户表格裁剪、平板长用户名挤压 | 已改卡片，320–1920 截图复核 |
| U4 | P1 | 手机注册步骤标签竖排 | 已改紧凑进度，390 截图复核 |
| U5 | P2 | 图标按钮缺少名称 | 本次涉及的导航、刷新、用户、设备、日志、收发按钮已补充 |
| U6 | P2 | 文档头部版本落后 | 统一以根目录 VERSION 为准，新增升级说明 |
| U7 | P2 | 登录 / 注册卡片移动端纵向空间偏紧 | 已减小移动端外边距，320 / 390 截图复核 |
| U8 | P2 | 平板通联日志弹窗过长 | 小屏全屏编辑、双方信息 Tab，390 / 768 / 1440 验证 |
| U9 | P1 | 320px 设备配置和群组选择裁掉最后一个 Tab | 小屏全屏弹窗、等宽标签，实际交互及截图复核 |
| R1 | P2 | 互联重放窗口大跳跃逐位清理 | 已按 64 位字清理，随机回归及 benchmark 通过 |
| B1/B2 | P1 | 两项播报 MySQL 集成测试的夹具不满足真实运行条件 | 已修复，VM 集成测试通过 |

## 后端修复与验收

本轮继续复查的新增记录见 [后端复查与主题优化记录](./backend-review-2026-10-03.md)：Redis refresh 吊销并发一致性、内存会话清理频率、上传请求总量边界，以及全局日间 / 黑夜模式均已实现并完成对应回归和视觉检查。

### A1、A2、A11：身份绑定与会话吊销

access / discovery JWT 必须包含正数 `user_id`、`session_version` 和与 ID 相同的 `sub`。
用户名仅用于展示。HTTP、WS、UDP 和 discovery 验证都按 ID 加载用户并校验会话版本；HTTP 下游 context 使用数据库中的当前用户名和角色。
旧用户名 JWT 不再兼容接受，防止兼容路径重新引入串号问题。

`users.session_version` 默认 1。密码、用户名、角色、状态、审批变更递增版本；综合资料更新在事务中锁定用户并比较关键字段。
refresh 的内存及 Redis 记录保存同一版本，旧格式记录在刷新时拒绝；相关接口主动吊销 refresh，删除用户在数据库删除成功后吊销。
用户缓存键升级为 v2。认证读取数据库当前状态，避免其它实例修改后本地缓存仍接受旧版本。

真实 MySQL + Redis 回归覆盖：禁用→启用后旧 access/refresh 拒绝、新 refresh 成功；改名并复用旧用户名后不会串号；签名中的旧展示名仍只解析到原 ID；WS / UDP 拒绝旧版本；未主动失效的本地缓存不会绕过吊销；改密码后旧 access/refresh 均拒绝。
JWT 单元测试覆盖旧格式、无过期时间、用途混用和 sub / ID 不匹配。

### A3：HTTP 代理信任边界

新增 `System.HTTPTrustedProxyCIDRs`，默认 `[]`。Gin 仅信任列表中的代理，HTTP 设置独立于 UDP PROXY 协议。
`X-Forwarded-Proto=https` 仅在请求对端属于可信 HTTP 代理时影响 Cookie Secure；直接 TLS 仍设置 Secure。
配置校验拒绝非法网段，构造引擎时遇到错误也回退到直连模式。

测试覆盖伪造 XFF 仍命中同一个限流键、可信代理真实 IP、直连 TLS、非可信转发 HTTPS、IPv6 和多值转发协议头。

### A5：公共频道与 schema v3

迁移幂等创建启用、公开、非虚拟的 999，系统 owner 为 0，不覆盖已有设置。
启动检查认证版本列和频道契约；缺失时明确提示 `--auto-migrate`。
群组 API 禁止删除、停用或私有化 999。

在全新数据库执行迁移和重复迁移，验证新用户的 GhostClientPreference 可保存 999；浏览器 `/radio` 实际连接显示公共频道“已连接”。

### A4、A8：通联日志数据语义

普通及管理员更新接口区分省略字段与空字符串；可选文本可清空，功率 `null` 清除、`0` 保留。
拒绝空必填字段、非法时间、非正频率、负分区和负功率。前端保存时不再把空 RST 回填 59，也不覆盖零功率 / 零分区。
时间编辑保留 `:SS` 的索引从 `slice(14,19)` 改为 `slice(16,19)`。

API 回归验证省略保留、空文本清除、null 功率、非法时间拒绝，分别覆盖普通及管理员接口。
浏览器实际编辑保存后重新读取确认：`time_utc=2026-10-03 01:30:34`（BJT 09:30:34）、备注 / 对方 QTH / RST 为空、对方功率为 null、我方功率为 0。
小屏我方呼号缺失时，保存自动切换到“我方信息”，不会把错误藏在另一个 Tab 中。

### A9、B1/B2：播报删除与真实接收者

删除用户拥有的群组时，收集无通信历史引用的 `EffectiveRecordObjectKey()` 并清理对象；仍被通信历史引用的录音保留。
播报 E2E 建立真实回环 WebSocket 接收者，订阅测试群组并验证收到消息帧，避免没有接收者却预期播放成功。
schema 夹具要求全新 `draarl_test_` 数据库，在版本化迁移前插入旧数据，验证首次回填和重复迁移。
Windows 恢复测试改用测试可执行文件进行路径存在性检查，消除对 Unix `true` 的依赖。

VM 集成通过：实际 FFmpeg 转码 / 播放、历史录音保留、删除用户时清理未播放的独立录音、旧 schema 回填及重复迁移。

### A10：管理员密码重置

重置 DTO 不再无条件要求 `old_password`；管理员重置其它账号可以省略，任何账号修改自身密码仍需 bcrypt 验证旧密码。
回归确认管理员缺少本人旧密码时返回 400，重置他人密码成功且吊销目标的旧会话。

### A7：工具链、依赖、CI

Go 声明升级到 1.26.0、toolchain 1.26.8，Docker 构建工具链一致；更新 go-redis、QUIC、x/net、x/crypto、x/image 等相关模块。
axios 升级到 1.20.0，并更新 npm 锁文件中的间接依赖。
新增 `.github/workflows/security-checks.yml`，从前端构建生成 embed 资源后扫描发布后端，并运行后端 test / vet、前端 lint / test / build / audit。

最终 `govulncheck -tags=embed ./cmd/draarl`：0 个代码可达漏洞，0 个已导入包漏洞。
模块列表有 GO-2026-5932（未使用的 `x/crypto/openpgp`，无修复版本）；项目没有导入或调用该包，不能写成“依赖模块完全没有公告”。
完整 `npm audit` 为 0，生产依赖审计也为 0。

## 视觉与交互复核

使用 Playwright 操作真实 Chromium，逐张打开 PNG 检查，包含有数据的用户 / 设备列表、长名称、IPv6、表单、验证错误和连接状态。
用户与设备列表在 1200px 以下使用卡片，桌面使用表格；公共导航也在 1200px 展开。
768px 页面标题与工具栏纵向排列，防止标题被挤成两行；用户 ID / 呼号单独成行；320px 操作图标成组，设备型号允许换行。
设备配置与群组选择在 600px 以下使用全屏弹窗和等宽标签；320px 可访问全部分类，不再裁掉平台设置 / 群组搜索。

| 视口 | 实际复核页面 / 状态 | 代表截图 |
|---|---|---|
| 320×568 | 用户卡片、设备完整页面、登录、群组选择及设备设置 | [设备完整页](../output/playwright/remediation-devices-320x568-full.png)、[用户列表](../output/playwright/remediation-users-320x568.png)、[登录](../output/playwright/remediation-login-320x568.png) |
| 390×844 | 设备、用户详情、注册、日志全屏编辑、HTTP 收发 | [注册](../output/playwright/remediation-register-390x844.png)、[日志](../output/playwright/remediation-logbook-390x844.png)、[收发](../output/playwright/remediation-radio-http-390x844.png) |
| 768×1024 | 导航、用户 / 设备卡片、日志双方 Tab 与错误切换 | [设备](../output/playwright/remediation-devices-768x1024.png)、[用户](../output/playwright/remediation-users-768x1024.png)、[错误定位](../output/playwright/remediation-logbook-validation-768x1024.png) |
| 1024×768 | 折叠导航、用户 / 设备卡片，操作不再藏在表格右端 | [首页](../output/playwright/remediation-home-1024x768.png)、[用户](../output/playwright/remediation-users-1024x768.png) |
| 1440×900 | 完整导航、桌面表格、日志双列、HTTP 文字消息 | [设备](../output/playwright/remediation-devices-1440x900.png)、[日志](../output/playwright/remediation-logbook-1440x900.png)、[文字发送](../output/playwright/remediation-radio-text-1440x900.png) |
| 1920×1080 | 用户管理桌面布局 | [用户](../output/playwright/remediation-users-1920x1080.png) |

注册小屏步骤条显示图标及“第 N/4 步”，登录 tabs 保持单行；320px 高度仍需正常纵向滚动。
通联日志在 900px 以下全屏显示，并把双方信息拆成保留状态的 Tab。
HTTP 收发页明确说明麦克风需要 HTTPS / localhost，禁用按钮及键盘 PTT；实测空格没有启动发射，切换为文字后成功发送并显示在消息区。

## 测试与性能结果

- `go test ./...`、`go vet ./...` 通过。
- auth、interconnect、middleware、gormdb、handler、JWT、WebSocket 的 `go test -race` 通过。
- 新认证 / 日志 MySQL + Redis 回归、两项播报 MySQL E2E 通过，使用专用可丢弃库。
- 前端 ESLint、13 项 Node 测试、TypeScript / Vite build 通过。
- 完整 npm audit 为 0；发布 embed 构建 govulncheck 无可达 / 导入包漏洞。
- 重放窗口随机参考模型比较 30,000 步通过。相同 Windows 环境的大跳跃 benchmark：11,406 → 358.4 ns/op，约 32 倍改善；顺序路径 5.659 ns/op、0 分配。
- 最终 VM HTTP 校验：20 并发 × 400 请求，`/api/me` 全部 200，约 1567 req/s、p95 20.10ms；`/api/users` 约 1778 req/s、p95 16.80ms；`/healthz` 约 1574 req/s、p95 18.42ms。压测客户端为 Python / VM 回环，只用于环境内验证，不代表生产容量或严格前后对比。

日志保存在本地忽略目录：

- [后端测试](../output/remediation-go-test.log)、[vet](../output/remediation-go-vet.log)、[race](../output/remediation-go-race.log)
- [认证 / 日志集成](../output/remediation-mysql-e2e.log)、[播报集成](../output/remediation-broadcast-e2e.log)
- [发布漏洞扫描](../output/remediation-govulncheck-release.log)、[前端构建](../output/remediation-frontend-build.log)、[HTTP 校验](../output/remediation-http-performance.log)
- [最终完整 npm audit](../output/remediation-npm-final.json)

## 升级与后续优先级

升级前备份数据库，使用 `-auto-migrate` 迁移 schema v3；所有旧 JWT / refresh 会话需要重新登录。
多实例应一起升级，配置明确 HTTP 反代网段。详见 [会话安全与代理升级说明](./usage/13-会话安全与代理升级说明.md)。

后续建议按下面顺序继续，避免把环境内测试写成生产全覆盖：

1. 在真实 HTTPS / WSS 部署下验证麦克风授权、实际双向语音、iOS Safari / Android Chrome。当前 HTTP 提示及文字路径已验证，真实浏览器录音和跨浏览器语音还未覆盖。
2. 验证多中心实例中已经建立的 WS / UDP 会话的路由撤销传播；本次已保证新认证读到数据库版本，但没有重写实时连接的分布式撤销机制。
3. 以真实设备和跨边缘链路压测音频、FFmpeg 长时任务、对象存储及断线恢复；核对 socket buffer 的内核上限。
4. 在测试配置中接入真实 SMTP、Keycloak、APRS、外网 MinIO TLS 并验证失败 / 超时路径。
5. 大数据消息查询继续保留 `message_type` 条件，评估四组无类型游标查询的索引与执行计划；暂不以小数据 UI 服务推断百万级查询性能。
6. 扩展其他管理页面的卡片适配和无障碍检查；本次 U5 仅覆盖所修改页面，未声称所有图标都已修复。

隔离 UI 服务暂保留在 `http://172.30.50.2:29004` 供复核，专用目录为 `/tmp/draarl-remediation-20261003`。临时认证回归库每次测试后删除；本地和 VM 的临时令牌文件已删除，浏览器测试会话已关闭，配置和日志仅允许账号本人读取。服务与 UI 测试库不属于生产部署。
