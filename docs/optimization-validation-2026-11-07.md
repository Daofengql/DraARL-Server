# 第一批 TODO 修复与验证记录

> 本文保留第一批实施时的历史状态；后续已在 `dev` 继续完成 PROXY v2 移除、依赖升级、
> 容器扫描、真实服务与播报隔离回归。当前结论以 [最终验证记录](todo-validation-2026-11-07.md)
> 和 [todo.md](https://github.com/Daofengql/DraARL-Server/blob/master/todo.md) 为准，下文“未实施”等说明不代表当前状态。

- 日期：2026-11-07。
- 分支：`optimization/p0-security-stability`；基线提交：`6a606fa`。
- 本批为工作区修改，尚未提交、合并或部署到生产。
- 清单入口：[todo.md](https://github.com/Daofengql/DraARL-Server/blob/master/todo.md)。本次完成或复核 15 项，其余按清单继续跟进。

## 清单范围

根据本次确认，已从 TODO 删除 AES/密码可逆存储项和 `OwerID` 拼写项，这两项按设计特性保留。

新增的 `L-005` 记录彻底移除 PROXY v2 及专用保护配置：已有配置中的旧选项直接忽略，无效旧值也不阻止启动，保留独立 HTTP 反向代理信任配置。此项目前为待办。

新增 `DEP-001`～`DEP-004`，分别跟进 CVE 影响清单、Go 工具链/模块升级、npm 依赖/锁文件升级和容器基础镜像更新。本批仅登记这些任务，尚未进行漏洞扫描或依赖升级，也未确认具体 CVE 已修复。

## 已完成与已复核项目

| TODO | 结果与依据 |
| --- | --- |
| S-001 | 基线已有用户禁用、删除、撤销审批及身份变更时的 refresh 会话吊销。复核相关调用；MySQL/Redis 认证回归确认禁用后重新启用仍拒绝旧 access/refresh token。 |
| S-002 | 实际邮箱验证码比较在 `internal/email/verification.go`，基线已有恒定时间比较；邮箱模块测试通过。 |
| ST-001 | 运行时清理按设备 `GroupID` 定位当前群组；缺失时兼容扫描、节流告警并累计 `device_group_cleanup_fallbacks`。保留历史私有群组清理。已覆盖空群组、已知群组、缺失群组及连接池清理。 |
| ST-002 | 设备登记、入口同步、呼号变更采用设备锁→索引锁顺序，移除索引锁内再获取设备锁的路径。两条并发路径各 5000 次操作的回归及 Linux 竞态测试通过。 |
| ST-003 | 基线已按 64 位字批量清理重放窗口，跨整窗直接重置；既有边界、随机序列和竞态测试通过。 |
| P-001 | 设备列表已有批量所有者查询，但随后又逐个访问用户缓存，冷缓存时仍发生额外 SQL。本批移除重复访问并处理批量查询失败；真实 MySQL 测试证明 6 个不同所有者仅产生 1 次所有者查询。 |
| L-001 | 前端补齐 106/107 历史型号显示，并保持不可新选，与后端接受历史型号的行为一致。 |
| L-004 | 共用分页默认值和上限；修复设备、用户、群组和操作日志中 `limit=1` 的边界误拒绝。群组 handler 复用仓库分页校验；内部 1000 条快照查询继续可用。 |
| CQ-002 | 中心和边缘设备认证的呼号/SSID 拼接复用 `protocol.GetCallSignSSID()`。 |
| PB-002 | 串行执行手动/后台设备缓存刷新，删除检查与缓存失效使用设备运行时快照，保留群组静态快照发布机制。 |
| PB-005 | 普通包、转发包、心跳包固定宽度用户名/呼号字段仅在完整 UTF-8 字符边界截断；测试中文、emoji、ASCII 边界及转发不修改源报文。 |
| UI-002 | 预配置工具各异步动作增加加载状态和进度条，操作期间禁用两类表单、其他动作、设备选择和关闭入口，防止并发操作。 |
| UI-003 | 调试输出经仅开发环境启用的 `debugLog`；ESLint 禁止直接调试输出，保留 warn/error。Node 测试也能加载这些模块。 |
| UI-004 | 使用 BLE 客户端能力检测代替 UA 判断；同一 Chromium 浏览器移除蓝牙 API 后能显示不支持提示并禁用连接。 |
| UI-007 | 动态码过滤非数字并限制 6 位，实时提示剩余位数；未填满时禁止提交，填满后恢复。 |

以下项目仍未整体完成：S-003 已覆盖设备/群组设备/用户管理、成员权限和设备/群组缓存 miss 的请求取消传播；仓库便捷方法保留作用域上下文，后台缓存查询使用 8 秒超时。真实 MySQL 验证取消后的列表、用户查询/更新和设备事务均返回取消错误。其他 handler 与后台任务仍待核查。

CQ-003、PB-001、UI-005、UI-006 也只完成了清单中标注的部分：新增缓存同步常量、设备报文空参数检查、预配置标题/输入的可访问名称及提示关联、小屏弹窗全屏布局。全站无障碍、其他页面响应式细节和其余入口尚未完成。

## 验证环境与结果

Windows 工作区执行生产源码和测试包的完整 Go 测试、`go vet`，以及前端 lint、测试、构建。

Ubuntu Hyper-V 虚拟机通过 SSH 别名 `daofeng` 测试，Go 工具链为 `go1.26.10`。使用专属目录 `/home/daofeng/draarl-todo-test-20261107`，专属 MySQL/Redis 容器只绑定虚拟机 localhost。各 MySQL 集成套件分别创建新的 `draarl_test_` 数据库，避免不同夹具的部分表结构互相影响。

| 验证 | 结果 |
| --- | --- |
| Windows Go 完整源码包测试 | 通过 |
| Windows / Linux `go vet` | 通过 |
| Linux 后端构建 | 通过 |
| Linux Go 完整源码包测试 | 1006 个测试及子测试通过，40 个跳过 |
| Linux MySQL/Redis 集成及关联包回归 | 479 个测试及子测试通过，21 个跳过 |
| Linux 关键包 `-race` | 698 个测试及子测试通过，26 个跳过 |
| 前端 `npm run lint` | 通过 |
| 前端 `npm test` | 13 项通过 |
| 前端 `npm run build` | 通过 |
| Playwright 预配置交互检查 | 通过，见下文范围 |

通过数包含子测试，跳过项不算作通过；这些结果不是覆盖率或容量指标。

Go 完整源码包命令为：

```sh
go test ./cmd/... ./internal/... ./pkg/... ./test/...
go vet ./cmd/... ./internal/... ./pkg/... ./test/...
go build -o ../draarl-test ./cmd/draarl
```

使用明确包范围是因为工作区忽略目录 `output/` 存有旧 Go 辅助程序，`go test ./...` 会把这些旧程序也纳入构建。

竞态检查包为：

```sh
go test -race -count=1 ./internal/udphub ./internal/interconnect ./internal/handler ./internal/gormdb ./internal/auth ./pkg/cache ./internal/protocol ./pkg/websocket
```

单独启用并通过的 MySQL 端到端测试：

- `TestDeviceListBatchOwnersAndRequestCancellationMySQL`：冷缓存批量所有者查询、未分组设备、请求取消及作用域仓库取消。
- `TestRemediationAuthenticationAndLogbookMySQL`：认证会话与通信记录权限。
- `TestGroupPasswordHTTPFlowMySQL`：群组密码流程及退出群组后 SQL `NULL`。
- `TestGroupQueryPaginationAndDeviceStatsMySQL`：群组分页、权限与设备统计。
- `internal/gormdb` 启用 `DRAARL_DEVICE_LIST_E2E` 的设备列表回归。

原有空群组修复继续通过回归：持久化使用可空 `group_id`，运行时及设备 API 以 `0` 表示未分组。

## 浏览器验证范围

在本地隔离前端地址 `http://127.0.0.1:9101/tools` 检查桌面 1440×1000 和小屏 320×568：

- 设备类型选择和未连接时的操作禁用状态。
- 小屏弹窗宽高分别为 320、568，弹窗及页面没有横向溢出。
- 对话框标题、设备选择器名称及动态码 helper 关联。
- 模拟 BLE 连接与保存时显示进度，阻止关闭和 Escape，禁用所有相关动作及表单；结束后恢复。
- 模拟连接后输入非数字、4 位和超过 6 位动态码，验证过滤、剩余位数和提交按钮状态。
- 模拟认证、配置读取、WiFi/DraARL 保存后的成功提示，各保存动作执行一次。
- 空闲时 Escape 可关闭；移除 `navigator.bluetooth` 后显示能力提示并禁用连接。

BLE 客户端方法仅在浏览器测试会话中模拟。设备端持久化、蓝牙真实传输及回读生效仍需硬件验证。本地前端未启动配套 API 服务，公共站点配置请求失败后使用默认配置；本次浏览器检查针对预配置弹窗，未覆盖登录后页面的服务端联调。

## 证据位置与后续

本地日志与浏览器截图保存在忽略目录 `output/`，包括 `todo-final-go-test.log`、`todo-frontend-*.log` 和 `output/playwright/todo-preconfig-*.png`。虚拟机的测试 JSONL 与 `run-tests.py` 保存在上述专属测试目录。日志可能含测试账号标识，未写入发布资源。

测试结束后停止本次创建的 `draarl-todo-mysql-3da47d`、`draarl-todo-redis-3da47d` 容器，保留测试源代码、私有配置与日志；重跑前可启动这两个容器，再执行：

```sh
python3 /home/daofeng/draarl-todo-test-20261107/run-tests.py unit
python3 /home/daofeng/draarl-todo-test-20261107/run-tests.py integration
python3 /home/daofeng/draarl-todo-test-20261107/run-tests.py race
```

本批未验证独立服务器进程启动或生产部署，未进行负载容量测试。PROXY v2 移除、依赖安全升级、全站上下文传播以及清单中其余未勾选项继续作为后续工作。
