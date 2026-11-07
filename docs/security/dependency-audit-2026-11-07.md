# 2026-11-07 依赖与镜像审计

本轮在 `dev` 升级工具链、直接/间接依赖和最终容器，使用虚拟机实际构建、扫描及启动。机器可读结果、镜像标识和报告 SHA-256 见 [审计摘要](dependency-audit-2026-11-07.json)。完整原始报告保存在工作区 `output/security/`，不提交测试凭据和私密配置。

## Go 与 npm

Go 从 1.26.10 升到 1.27.3，模块最低版本保持 1.26.10，`toolchain`、发布流程和容器构建统一使用 1.27.3。后端模块版本以 `go.mod`/`go.sum` 为准，包含 Gin、MySQL、Redis、MinIO SDK、compress、x/net、x/sys、x/text 和 OpenTelemetry。x/crypto 使用下面的上游修复提交对应的 pseudo-version，不将其描述为正式 tag。

基线 govulncheck 使用 symbol/source 模式报告以下四个**模块级 finding**，trace 未包含包路径/函数/调用链，不能据此声称四个漏洞都运行时可达：

| 公告 | 基线模块 | 本轮版本 | 上游修复下限 |
| --- | --- | --- | --- |
| GO-2026-5932 | x/crypto v0.57.0 | v0.58.1-0.20261009181718-2f1f834330b2 | 同本轮版本 |
| GO-2026-6865 / CVE-2026-116591 | klauspost/compress v1.18.7 | v1.20.3 | v1.20.2 |
| GO-2026-6860 / CVE-2026-78661 | x/net v0.60.0 | v0.62.0 | v0.62.0 |
| GO-2026-6866 / CVE-2026-97045 | x/net v0.60.0 | v0.62.0 | v0.62.0 |

使用 govulncheck v1.9.0，Windows 源码和 Linux `-tags=embed ./cmd/draarl` 发布入口检查均为 **0 finding**。扫描使用的 Go 漏洞库更新时间为 2026-11-05T23:49:57Z。JSON 中出现 OSV 公告元数据不等于存在 finding，不能用 OSV 对象数量作为漏洞数。

前端使用 Node.js 24.22.2，按锁文件完成全新 `npm ci`。完整 npm 审计由 11 项（4 high、7 moderate）降为 0，生产依赖审计为 0；lint、类型、13 项 legacy 测试、31 项 Vitest 和生产构建通过。更新包含 React/MUI、Vite、Axios、React Router、测试和 lint 工具链，具体版本见锁文件。

## 最终镜像

Trivy 0.76.2 扫描 `docker save` 导出的最终镜像，启用 OS 和应用依赖漏洞扫描，没有对剩余公告添加忽略规则。镜像 ID、实际系统版本、扫描时间及报告摘要见 JSON。

| 镜像 | 变更 | 扫描结果 |
| --- | --- | --- |
| 应用 | Go 1.27.3、Node 24.22.2 构建，Alpine 3.24 运行并升级系统包 | 0 critical/high/medium；1 low |
| MySQL | 保留官方 8.4.13 server/entrypoint，升级 Oracle Linux 包，移除服务器不需要的 mysql-shell/Python 树，重建上游 gosu 并升级其 x/sys | 0 项 |
| Redis | 保留官方 7.4.12 二进制/入口脚本，迁移至 Alpine 3.24，补齐 OpenSSL 和入口所需 setpriv 路径 | 0 项 |
| MinIO | 从官方 RELEASE.2025-10-15T17-29-55Z 源码构建，更新有修复版本的传递依赖，使用非 root 用户并携带许可证 | 0 项 |

官方 MySQL 8.4.13 镜像的对照扫描为 111 项（2 critical、33 high、68 medium、7 low、1 unknown）。Redis 7.4.12 的官方 Alpine 镜像实际使用 Alpine 3.21.10，报告标记 EOL；仅 `apk upgrade` 无法改变发行版，因此迁移运行基底。对照试用的 bookworm 变体报告 216 项，未采用。

旧 `minio/minio:RELEASE.2025-04-22T22-12-26Z` 无法拉取，因此没有有效的旧镜像扫描结果，不宣称已有其前后对照。MinIO 本轮在源码中固定更新 edwards25519、ntlmssp、thrift、jsonparser、paho、go-jose、prometheus、amqp091-go、MongoDB driver、OTel SDK、gRPC、compress 和 golang.org/x 模块。依赖变动已通过编译、容器启动和签名 S3 建桶/上传/下载/删除、重启持久化测试；不能替代所有可选身份源、通知和复制插件的兼容验证。

## 剩余公告与边界

应用镜像保留 `github.com/disintegration/imaging v1.6.2` 的 **CVE-2023-36308**。Trivy 使用 GHSA 严重度 LOW，报告同时包含 NVD 5.5/MEDIUM 评分；扫描未给出修复版本。本项目使用其 Resize/Crop，不能声称依赖不受影响或此公告已修复。

当前已有图片上传大小、DecodeConfig 尺寸检查和取消检查，可减少部分资源消耗，但不足以证明消除该库的全部异常输入风险。后续条件是上游提供可用修复，或另行验证替换实现。本轮没有修改既定 AES 密码存储和 `OwerID` 命名设计。

所有扫描仅代表记录日期、数据库和构建目标的可见结果。CI 会重新执行完整 npm 审计、Linux govulncheck、四个最终镜像扫描，并阻止有已修复版本的 HIGH/CRITICAL 镜像公告进入检查通过状态。

## 运行验证与回退

四镜像在独立 Compose 项目实际启动，数据库迁移、认证接口、匿名指标拒绝、未分组物理 UDP 心跳及现代 WS ghost 握手通过。MySQL、Redis、MinIO 均验证重启后数据仍在；应用两次重启保持迁移记录稳定。MySQL/Redis 依赖中断时 liveness 保持 200、readiness 返回 503，恢复后 readiness 返回 200。

既有卷升级前须备份配置/密钥、SQL 和对象。MinIO UID 10001 的卷权限迁移见 [部署文档](../usage/01-部署与配置.md)。回退按 [数据库迁移与回退](../database-migrations.md) 操作，不能把更换镜像视作数据库自动降级。
