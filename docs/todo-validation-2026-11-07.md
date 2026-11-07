# TODO 实施与最终验证记录

日期为 2026-11-07，工作分支为 `dev`。`master`、`dev`、`edge` 三个长期分支保留，`edge` 保持
`8df54eade0b6445fe8dc08c7fe924c6ef668ffee`。第一批修复 `9536a05` 已合入 `dev`；本记录对应后续
清单实施和用户追加的播报隔离要求。未推送远端或部署生产。

## 完成范围

[todo.md](https://github.com/Daofengql/DraARL-Server/blob/master/todo.md) 逐项记录状态。已完成或复核 56 项；保留两项待办：后端整体语句覆盖目标、
无上游修复版本的 imaging 公告。AES 可逆密码存储与 `OwerID` 拼写按既有设计保留。

未设置默认群组的设备可正常认证和持久化：SQL `devices.group_id` 使用 `NULL`，运行时与
设备 API 继续用 `0` 表示未分组。原先外键 1452 的触发条件已有回归。

PROXY v2 解析、UDP 地址提取与协议专用保护配置已移除。旧配置文件中的相关字段直接忽略，
包括无效类型；UDP 使用真实 socket 对端地址。独立 HTTP 可信代理配置继续生效。

其他变更包括请求上下文、缓存与失效、在线事件、统一错误响应、分页/协议边界、异步缓冲
生命周期、结构化日志、readiness/metrics/OTLP，以及前端批量操作、防抖、快捷键、BLE 保存回读、
日期解释、无障碍和小屏布局。贡献、迁移、部署、排障、组件规范和 OpenAPI 检查已补齐。

## 播报隔离

A、B 互联后，A 的定时或手动播报仅投递来源实体组。主动订阅 A 的多组接收会话仍可听到 A；
只接收 B 的设备或会话不会因为互联而收到 A 的播报。普通用户语音保持互联。

| 路径 | 验证与行为 |
| --- | --- |
| 中心 UDP、UDP ghost | 固定接收快照只包含来源组；新增接收者不会扩大当前 UDP 播放快照 |
| 中心 WS | 仅向来源组调用群组投递；真实 WS A 接收、B 静默 |
| 中心到边缘 UDP/TLS | 使用独立组内播报 subtype 和能力协商；同一边缘与不同边缘的 B 均不接收 A |
| 边缘缓存 | A→B 切组即使通信域 ID 未变，也使组内接收计划失效 |
| 跨中心 | 自动播报不调用跨中心 RelayPeer；普通语音路径保留 |
| 自动录音 | 共享话权域可记录 A/B，录音投递权限仅记录 A；结束事务重试只生成一条记录 |
| 静默与占用 | 仍检查整个互联域并竞争同一半双工话权，避免与现有通话重叠 |
| 只有 B 在线 | A 没有明确接收者时跳过并释放话权，不把 B 当成 A 的接收者 |

旧边缘不支持 `NodeFeatureGroupBroadcast` 时不接收播报；升级后才能恢复安全的组内投递。
协议与用户行为见 [播报 API](api/13-群组自动播报.md)。

最新发布镜像在虚拟机实际启动后，通过 API 创建互联组、上传音频并触发播报：普通 A→B 语音
收到 1 包；A 组播报收到 9 包，B 组收到 0 包，执行状态为 `succeeded`。原始记录为
`output/vm-deployed-broadcast-isolation.log`，镜像 ID 与记录摘要见机器可读证据。

最终真实 MySQL 回归另外发现并修复广播结束事务的 InnoDB 死锁：仅对 1213/1205 重试整笔事务，
最多三次，有界退避并响应取消。测试用真实锁循环制造 1213，再确认最终成功和恰好一条自动录音。
FFmpeg 子进程设置 OpenBLAS/OMP 单线程，避免虚拟机中资源限制与线程启动冲突造成空转超时。

## 自动化与真实服务

Windows 工作区与 Ubuntu 虚拟机使用 Go 1.27.3；前端使用 Node.js 24.22.2。虚拟机测试位于
独立目录 `/home/daofeng/draarl-todo-test-20261107/src3`，仅使用本任务的 MySQL/Redis/MinIO 和应用容器。
每个集成 suite 创建独立 `draarl_test_` 库，结束后删除，凭据不写入 Git。

| 检查 | 实际结果 |
| --- | --- |
| Windows 生产包、测试与工具完整 Go 测试 | 通过 |
| Linux 完整 Go 测试与关键包 race | 通过；新增播报改动后重跑 UDP、互联、scheduler/runtime、player race |
| 真实 MySQL/Redis/FFmpeg 集成 runner | 28/28 suite，通过数包含子测试，跳过不计通过；详见 JSON |
| 互联状态真实 WS 播报与录音事务 | 通过，并验证 B 接收计数为零、录音投递表无 B |
| 真实 MySQL 播报 API 与录音仓库 race | 2/2 suite，通过 18 项（含子测试），无跳过与竞态报告 |
| go vet、golangci-lint | 通过，lint 0 issues |
| OpenAPI 生成一致性、workflow YAML、git diff --check | 通过 |
| 前端 lint、类型、测试、生产构建 | 通过：13 项 legacy、31 项 Vitest |
| 核心工具函数覆盖 | 5 个工具文件行/语句/函数 100%，分支 98.73% |
| 四镜像实际启动与迁移 | 通过，应用及三个依赖健康 |
| HTTP/UDP/WS 运行冒烟 | 探活、认证、匿名 metrics 拒绝、未分组物理 UDP、现代 WS ghost 握手通过 |
| 中断 MySQL/Redis 后恢复 | healthz=200、readyz=503，恢复后 readyz=200 |
| 持久化与备份恢复 | MySQL 卷、Redis AOF、MinIO 签名 S3 对象重启后保留；MySQL 32 表恢复行数一致 |
| 重复迁移 | 两次应用重启迁移记录稳定 |
| OTLP collector | 解码 3325 spans：HTTP 727、DB 2590、UDP 2；1185 DB span 直接继承 HTTP，SQL 文本属性 0 |

复现命令和集成开关见 [贡献指南](https://github.com/Daofengql/DraARL-Server/blob/master/CONTRIBUTING.md)。Git hook 实际执行暂存 Go 格式、vet 和前端
lint/类型/Vitest。GitHub workflow 已本地检查，尚未在 GitHub Actions 执行。

机器可读场景、实际语句覆盖和前端覆盖见
[验证证据](todo-validation-evidence-2026-11-07.json)。28 个 suite 的通过率不代表语句覆盖率；
handler 合并覆盖约 30.52%，broadcast/repository 约 66.82%，其他包详见 JSON。整体 >80% 目标继续待办。

## 浏览器与性能边界

发布镜像的 13 条关键路由在桌面、768px 和 320px 浅色布局扫描 axe 零违规，页面无横向溢出：
dashboard、devices、groups、profile、radio、admin/users、admin/devices、admin/groups、admin/settings、
comm-records/platform、comm-records/logbook、tools、relays。未对全部页面和深色主题作全站合规声明。

批量设备操作真实调用 API：一次注入 503 后保留失败项，再重试成功；切组与清空后 API 回读一致。
浏览器模拟 BLE 验证连接、认证、保存回读、不一致拒绝、Ctrl+S、Escape 和 busy 防关闭。没有真实
BLE 硬件，不能把这些结果视作固件持久化或断电验证。

虚拟机三轮微基准中位附近值：池化 header 约 40.7ns/24B，新建缓冲约 93.7ns/256B，AES 验证
约 623.5ns；100 行夹具的 20 条分页数据库查询约 1.55ms，热缓存约 46.2µs。未跑百万行规模或
真实射频大并发负载，结果不构成容量承诺。

## 安全结果与本地证据

上游工具链、Go/npm 依赖和四个容器已更新，详细版本、镜像 ID、报告 SHA-256、基线和剩余影响见
[依赖与镜像审计](security/dependency-audit-2026-11-07.md)。Windows/Linux embed govulncheck 和 npm
审计为 0 finding；MySQL/Redis/MinIO 最终扫描 0，应用保留 imaging 的 CVE-2023-36308 一项，
Trivy 为 LOW，报告另含 NVD 5.5/MEDIUM。现有尺寸检查是缓解，不记为漏洞已修复。

完整原始证据保存在忽略目录 `output/` 和上述虚拟机专属目录，包括：

- `vm-integration-isolation-final.log`、`integration-final/`：最终 28 组真实集成；
- `vm-broadcast-isolation.log`、`vm-broadcast-isolation-race.log`：新增播报隔离与竞态；
- `vm-broadcast-isolation-db-race.log`、`integration-broadcast-isolation-race-summary.json`：真实 MySQL 竞态验证；
- `vm-deployed-broadcast-isolation.log`：最新运行镜像的普通语音互联与组内播报实测；
- `security/govuln-linux-embed-isolation.jsonl`：最新 Linux embed 发布入口的完整扫描输出；
- `backend-broadcast-isolation.log`、`golangci-broadcast-isolation.log`：本地完整后端检查；
- `vm-container-smoke2.log`、`vm-recovery-final.log`、`vm-trace-verified.log`、`vm-benchmarks.log`：运行与恢复；
- `security/`：原始漏洞与镜像报告；
- `playwright/axe-release-*.log`、`devices-release-*.png`、`batch-real.log`、`batch-groups-result.log`、`ble-readback.log`：浏览器验证。

只提交脱敏摘要和源码，构建产物、测试身份和私密配置不入库。用户原有 `DEV_BRANCH_ANALYSIS.md`
与 `report.md` 删除保持工作区状态，未混入本次提交。
