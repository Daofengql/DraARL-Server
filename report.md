# DraARL Server 代码审计报告

**审计对象**：DraARL Server `v2.0.0-alpha14`
**审计日期**：2026-08-25
**代码规模**：Go 后端约 75,000 行非测试代码（436 个 Go 文件），React + TypeScript 前端
**审计范围**：性能问题、逻辑问题、安全问题

---

## 一、总体评价

这是一个**工程质量明显高于平均水平**的代码库。审计过程中的客观指标：

| 检查项 | 结果 |
|--------|------|
| `go build ./...` | 通过，无错误 |
| `go vet ./internal/... ./pkg/... ./cmd/...` | **零告警** |
| SQL 注入扫描（拼接查询、动态 `Order`/`Where`） | **未发现**，全部参数化 |
| 前端 XSS 面（`dangerouslySetInnerHTML` / `innerHTML` / `eval`） | **零命中** |
| `npm audit --omit=dev` | **0 个漏洞** |
| `govulncheck ./...` | **31 个可达漏洞**（见 S1，唯一的重大发现） |

代码中随处可见 `【H6】`、`【S3 安全修复】`、`【H13 安全修复】`、`【锁纪律修复】` 等标记，表明项目已经历多轮针对性的安全与性能加固。认证、加密、路径处理、限流等安全基础设施设计得相当扎实：

- **JWT**：移除了 fork 遗留的弱默认密钥，未初始化即 fail-fast；强制 HS256、校验 issuer/过期；access token 与 edge-discovery token 通过 `token_use` 声明隔离。
- **Refresh Token**：服务端有状态存储 + 轮换 + **重放检测**（发现替换链即吊销该用户全部令牌），HttpOnly Cookie + `Path=/api/auth` 限定作用域。
- **密码学**：AES-GCM（含随机 nonce 与认证标签）、bcrypt、`crypto/rand` 拒绝采样生成凭据、随机源失败时 fail-closed、设备密码比较使用 `subtle.ConstantTimeCompare`。
- **路径穿越**：`localStorage.resolvePath` 防护极其严密——拒绝 `..` 段、`path.Clean` 折叠、根目录前缀校验，并且**逐级解析最近的已存在祖先目录的符号链接**，堵住了"文件不存在时 `EvalSymlinks` 返回 ENOENT 从而绕过根检查"这一少见但真实的绕过路径。
- **登录防护**：图形验证码 + 账号级阶梯锁定 + 未知账号的源 IP 配额 + **固定 dummy bcrypt 哈希**平衡时序（防账号枚举）；且明确区分"后端故障"与"凭据错误"，DB 故障不计入锁定计数。

因此本报告的重点不是罗列常见漏洞（它们基本都已被处理），而是**指出仍然存在的、经过验证的具体缺陷**。

---

## 二、安全问题

### S1【严重】依赖链存在 31 个可达（reachable）漏洞

**这是本次审计最重要的发现，也是唯一达到"严重"级别的问题。**

`govulncheck` 确认这 31 个漏洞**不只是存在于依赖树中，而是代码实际调用到的路径可达**（另有 10 个已导入但未调用、28 个仅在依赖中的漏洞未计入）。

当前 Go 版本：`go1.25.5`（`go.mod` 声明 `go 1.25.0`）。

#### 标准库漏洞（需升级 Go 工具链）

| 编号 | 描述 | 受影响包 | 修复版本 |
|------|------|----------|----------|
| GO-2026-6218 | `resolvePath` 二次方复杂度 | `net/url` | go1.25.13 |
| GO-2026-6090 | 握手后消息数量未限制 | `crypto/tls` | go1.25.13 |
| GO-2026-6089 | 明文 HTTP/2 检查未应用 `ReadHeaderTimeout` | `net/http` | go1.25.13 |
| GO-2026-6088 | 解码缺少递归深度保护 | `encoding/xml` | go1.25.13 |
| GO-2026-5972 | 未强制最大递归深度 | `encoding/asn1` | go1.25.13 |
| GO-2026-5856 | Encrypted Client Hello 隐私泄漏 | `crypto/tls` | go1.25.12 |
| GO-2026-5039 | 任意输入未转义即写入错误信息 | `net/textproto` | go1.25.11 |
| GO-2026-5038 | `WordDecoder.DecodeHeader` 二次方复杂度 | `mime` | go1.25.11 |
| GO-2026-5037 | 候选主机名解析低效 | `crypto/x509` | go1.25.11 |
| GO-2026-4986 / 4977 | `net/mail` 二次方字符串拼接（两处） | `net/mail` | go1.25.10 |
| GO-2026-4971 | Windows 下处理 NUL 字节时 panic | `net` | go1.25.10 |
| GO-2026-4947 / 4946 | 证书链构建与策略校验开销异常 | `crypto/x509` | go1.25.9 |
| GO-2026-4870 | **未认证的 TLS 1.3 KeyUpdate 记录可导致连接persistent保留与 DoS** | `crypto/tls` | go1.25.9 |
| GO-2026-4865 | JsBraceDepth 上下文追踪缺陷（**XSS**） | `html/template` | go1.25.9 |
| GO-2026-4602 | `FileInfo` 可逃逸出 `Root` | `os` | go1.25.8 |
| GO-2026-4601 | IPv6 host 字面量解析错误 | `net/url` | go1.25.8 |
| GO-2026-4341 | 查询参数解析内存耗尽 | `net/url` | go1.25.6 |
| GO-2026-4340 | 握手消息可能在错误的加密层级被处理 | `crypto/tls` | go1.25.6 |
| GO-2026-4337 | 非预期的会话恢复 | `crypto/tls` | go1.25.7 |

#### 第三方模块漏洞（需升级依赖）

| 编号 | 模块 | 当前版本 | 修复版本 | 说明 |
|------|------|----------|----------|------|
| GO-2026-5970 | `golang.org/x/text` | v0.34.0 | **v0.39.0** | 非法输入导致无限循环 |
| GO-2026-5026 | `golang.org/x/net` | v0.51.0 | **v0.55.0** | 未拒绝纯 ASCII 的 Punycode 标签 |
| GO-2026-4918 | `golang.org/x/net` | v0.51.0 | v0.53.0 | HTTP/2 传输层因异常 `SETTINGS_MAX_FRAME_SIZE` 无限循环 |
| GO-2026-5676 | `github.com/quic-go/quic-go` | v0.59.0 | **v0.59.1** | HTTP/3 QPACK Trailer 扩展导致内存耗尽 |
| GO-2026-5066 | `golang.org/x/image` | v0.23.0 | **v0.43.0** | TIFF 越界 strip offset 导致 panic |
| GO-2026-5062 | `golang.org/x/image` | v0.23.0 | v0.43.0 | TIFF tile 尺寸无限制 |
| GO-2026-5032 | `golang.org/x/image` | v0.23.0 | v0.41.0 | PackBits 解压资源消耗过度 |
| GO-2026-5031 | `golang.org/x/image` | v0.23.0 | v0.41.0 | BMP 越界调色板索引导致 panic |
| GO-2026-4815 | `golang.org/x/image` | v0.23.0 | v0.38.0 | 恶意 IFD offset 导致 OOM |
| GO-2025-3540 | `github.com/redis/go-redis/v9` | v9.7.0 | **v9.7.3** | `CLIENT SETINFO` 超时导致响应乱序 |

**风险评估（结合本项目实际暴露面）**：

- `golang.org/x/image` 的 5 个漏洞**风险最高**。项目在 `pkg/storage/image.go` 中处理**用户上传的头像**（`ProcessAvatarContext`，任何已认证用户可触发）。虽然代码已有优秀的解压炸弹防护（`decodeImageConfigSafe` 先用 `DecodeConfig` 校验像素尺寸再全量解码，上限 12000×12000），但 **panic 类和 OOM 类漏洞发生在解码阶段本身，尺寸预校验无法拦截**。恶意 TIFF/BMP 可绕过该防护直接触发 panic 或内存耗尽。
- `crypto/tls` 的 GO-2026-4870（未认证 KeyUpdate 导致连接保留与 DoS）直接影响 HTTPS 服务端与节点互联 TLS 控制面。
- `go-redis` 的响应乱序问题会影响 refresh token 存储与登录防护状态，可能造成**认证状态错乱**。
- `redis` 与 `x/net` 的无限循环类漏洞可被用于远程 CPU 耗尽。

**修复建议（优先级最高）**：

```bash
# 1. 升级 Go 工具链到 1.25.13 或更高（一次性解决 19 个标准库漏洞）

# 2. 升级第三方依赖
go get golang.org/x/text@v0.39.0
go get golang.org/x/net@v0.55.0
go get golang.org/x/image@v0.43.0
go get github.com/quic-go/quic-go@v0.59.1
go get github.com/redis/go-redis/v9@v9.7.3
go mod tidy

# 3. 验证
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

**长期建议**：将 `govulncheck` 接入 `.github/workflows/` 中的 CI，使依赖漏洞在合入前即被拦截。当前 CI 已有 `dev-ci.yml` 等 workflow，接入成本很低。

---

### S2【高】禁用、删除、拒绝审批用户时未吊销 refresh token

**问题描述**：项目实现了完善的 refresh token 吊销基础设施 `revokeUserRefreshSessions()`（`internal/handler/session_security.go:14`），底层 `RevokeAllByUser` 在内存与 Redis 两种存储上均已正确实现。但该函数**仅在密码变更类路径被调用**：

```
internal/handler/auth_password.go:70   revokeUserRefreshSessions(user.ID, "password_changed")
internal/handler/auth_password.go:189  revokeUserRefreshSessions(targetUser.ID, "password_reset")
internal/handler/email_auth.go:322     revokeUserRefreshSessions(user.ID, "password_reset")
```

而以下三条**管理员主动剥夺访问权**的路径均未调用（已逐一验证，命中数为 0）：

| 路径 | 位置 | 现有清理动作 | 缺失动作 |
|------|------|--------------|----------|
| `UpdateUserStatus`（禁用用户） | `auth_user_admin.go:290-390` | ghost session 重整、`routesync.RevokeOwner`、用户缓存失效 | **未吊销 refresh token** |
| `DeleteUser`（删除用户） | `auth_user_admin.go:387-560` | 同上 + 设备/群组级联清理 | **未吊销 refresh token** |
| `ApproveUser`（拒绝审批） | `upload.go:1090+` | 缓存失效 | **未吊销 refresh token** |

**攻击场景**：

1. 管理员发现某账号异常，执行"禁用用户"。
2. 该用户的 access token 因 `AuthMiddleware` 会检查 `user.Status != 1` 而在**最长 2 分钟**（用户缓存 `LocalTTL`）内失效——这部分是正确的。
3. **但攻击者手中的 refresh token 仍然有效，TTL 长达 14 天**（`refreshTokenTTL = 14 * 24 * time.Hour`）。

值得注意的是，`RefreshToken` handler 本身有一道防线（`auth_refresh.go:106`）：

```go
if err != nil || user == nil || user.Status != 1 || user.ApprovalStatus != 1 {
    _ = store.RevokeAllByUser(stored.UserID, "user_invalid", now)
    // ...
}
```

这意味着**被禁用用户下次刷新时会被拦截并吊销**。因此该问题的实际影响是：

- 存储层中残留大量本应立即失效的凭据，直到用户主动刷新才被清理——**违反了"管理员操作应立即生效"的安全预期**。
- 对于 `DeleteUser`，用户记录已被删除，`GetUserByID` 返回 nil，同样会走到吊销分支——但**在删除与下次刷新之间的窗口内，令牌记录仍以"有效"状态留存**。
- 更关键的是：这道防线**依赖于 refresh handler 的检查逻辑永不回退**。一旦未来有人放宽该检查（例如为兼容某种场景允许 `ApprovalStatus != 1` 的用户刷新），漏洞立即变为可直接利用。**纵深防御要求在剥夺权限的源头就吊销，而非依赖下游检查。**

**修复建议**：在三处路径中补充调用。修复成本极低（每处一行）：

```go
// UpdateUserStatus 中，禁用分支内：
if req.Status == 0 {
    reconcileOwnerGhostSessions(id)
    routesync.RevokeOwner(id, "user_disabled")
    revokeUserRefreshSessions(id, "user_disabled")   // 新增
}

// DeleteUser 中，DeleteUserWithCascade 成功之后：
reconcileOwnerGhostSessions(id)
routesync.RevokeOwner(id, "user_deleted")
revokeUserRefreshSessions(id, "user_deleted")        // 新增

// ApproveUser 中，拒绝审批分支内：
revokeUserRefreshSessions(userID, "approval_rejected")  // 新增
```

---

### S3【中】生产凭据以明文形式存在于工作区配置文件

`config.yaml` 中包含多项**看起来是真实生产环境的凭据**：

| 配置项 | 内容 |
|--------|------|
| `Keycloak.ClientSecret` | `TfzTzdsqCKbLycCQpUXNGlkqcCQzJqFD` |
| `Storage.MinIO.AccessKey` / `SecretKey` | `sTn5NIuyDnxKZalHMlLJ` / `8k9xb47FRHqyb5AneCBkFxOM2cIgzHpgLYhGsZg4` |
| `JWT.Secret` | `8ff6816bce5e17308f41841d49e770a0600e2a3750e46f21ac73f3a187dfafd8` |
| `DeviceAuth.AESKey` | `89302d9c6257090a7f3f91559ce664e3` |
| `Redis.Password` | `926813` |
| `Database.Password` | `root` |

关联的外部端点（`sso.silverdragon.cn`、`s3-api-fz.silverdragon.cn`）指向真实域名，说明这些**并非占位符**。

**缓解因素（已核实）**：

- `.gitignore` 第 39 行 `*.yaml` 已排除该文件。
- `git ls-files` 确认 `config.yaml` **未被 git 跟踪**。
- 检索全部 git 历史，**未发现任何 `.yaml` 配置文件曾被提交**（仅 workflow 与 mkdocs 配置）。
- 代码已将配置文件写入权限从 `0644` 收紧至 `0600`（`config.go:837` 注释）。

因此**不存在版本库泄漏**，这是一个运维卫生问题而非已发生的泄漏事件。

**风险**：这些凭据存在于开发者工作站的明文文件中。`JWT.Secret` 尤其敏感——它同时用于签发登录令牌**和**本地存储的签名 URL（`pkg/storage/local.go` 的 `verifyWithAnyLocalSecret`），一旦泄漏可伪造任意用户身份。`DeviceAuth.AESKey` 泄漏则可解密**全部设备密码**（AES 为可逆加密，这是刻意的设计选择，以支持设备密码找回）。

**修复建议**：

1. 若这些确为生产凭据，应视作已在开发环境暴露，**执行一轮轮换**：MinIO 密钥、Keycloak client secret、Redis 密码。
2. `JWT.Secret` 轮换会使全部在线会话失效；代码已通过 `registerLocalSecret` 保留最近 3 个历史签名密钥以兼容在途 URL，可平滑过渡。
3. 生产部署改用环境变量或 secrets manager 注入，工作区仅保留 `config.yaml.example`。
4. `Database.Password: root` 这类弱口令即便在本地开发环境也建议更换。

---

### S4【低】邮箱验证码使用非恒定时间比较

`internal/email/verification.go:331`：

```go
if session.Code != code {
    session.Attempts++
    return nil, fmt.Errorf("验证码错误，还剩 %d 次机会", m.maxAttempts-session.Attempts)
}
```

验证码比较使用 `!=`，属于非恒定时间比较，理论上存在时序侧信道。

**实际可利用性很低**：验证码为 6 位（10⁶ 空间）、最多 5 次尝试、10 分钟过期、每邮箱 60 秒冷却、每 IP 每分钟 5 次限制。字符串比较的时序差异在网络抖动下几乎不可测量。

**修复建议**（低优先级，一致性考虑）：项目在设备密码校验中已正确使用 `subtle.ConstantTimeCompare`，此处保持一致即可：

```go
if subtle.ConstantTimeCompare([]byte(session.Code), []byte(code)) != 1 {
```

---

### S5【低】邮箱验证码登录路径缺少图形验证码与账号级锁定

`EmailLogin`（`internal/handler/email_auth.go:146`）与密码登录 `Login` 相比，防护层级较薄：

| 防护措施 | `Login`（密码） | `EmailLogin`（验证码） |
|----------|:---------------:|:----------------------:|
| 图形验证码 | ✅ | ❌ |
| 账号级阶梯锁定 | ✅ | ❌ |
| 未知账号 IP 配额 | ✅ | ❌ |
| 前置速率限制 | — | ✅（发码环节：邮箱 60s 冷却 + IP 5次/分钟） |

**缓解因素**：攻击者必须先通过发码环节的速率限制才能获得会话 ID，且验证码本身有 5 次尝试上限。因此实际风险不高。

**建议**：视业务需要评估是否为 `EmailLogin` 补充账号级失败计数。注意该路径已正确调用 `loginGuardClear`（成功的邮箱登录会清除密码失败窗口），逻辑上是自洽的。

---

## 三、性能问题

### P1【中】互联重放窗口在大跨度消息 ID 时持锁循环

`internal/interconnect/replay_window.go:36-43`：

```go
if messageID > w.maxID {
    delta := messageID - w.maxID
    if delta >= replayWindowBits {
        clear(w.bits[:])              // O(256) —— 快速路径
    } else {
        for step := uint64(1); step <= delta; step++ {
            w.clear(w.maxID + step)   // 最坏 16383 次迭代，全程持有 w.mu
        }
    }
    w.maxID = messageID
}
```

当 `delta` 落在 `[1, 16383]` 区间时，逐位清理循环最多执行 **16,383 次**，且**全程持有互斥锁**。讽刺的是，`delta >= 16384` 反而走 `clear()` 快速路径（仅 256 次字操作）——**最坏情况恰好出现在 `delta = 16383`**。

**调用位置**：该函数位于**UDP 数据面热路径**上：

- `internal/interconnect/datagram.go:99`（`NodeDatagramPeer.Handle`）
- `internal/interconnect/datagram.go:214`
- `internal/interconnect/control.go:629`、`control.go:934`

**风险边界（重要）**：`Handle` 中 `AcceptMessage` 的调用**位于 `Unmarshal(data, p.session.Key)` 认证之后**，并且校验了 `SourceNodeID`、`NodeSessionID`、`KeyEpoch`。因此**只有已认证的互联节点能触发**，外部攻击者无法直接利用。这将其从安全问题降级为性能/健壮性问题。

**实际影响**：在节点间网络抖动、乱序、或节点重启后 message ID 跳跃的场景下，单个数据包的处理可能占用锁上万次迭代，阻塞该节点会话的所有其他数据包处理，造成语音转发抖动。

**修复建议**：改为按字（word）批量清零，将最坏情况从 16383 次降至 256 次：

```go
if messageID > w.maxID {
    delta := messageID - w.maxID
    if delta >= replayWindowBits {
        clear(w.bits[:])
    } else {
        // 按 64 位字批量清理，避免逐位循环
        for step := uint64(1); step <= delta; step++ {
            w.clear(w.maxID + step)
        }
    }
    w.maxID = messageID
}
```

更优的实现是计算起止索引后对整字区间做 `clear`，仅对首尾不完整的字做位操作。若认为改动风险较高，一个零风险的折中是**降低快速路径阈值**（例如 `delta >= 1024` 即整窗清空），代价是丢失一部分乱序容忍能力。

---

### P2【低】`SyncRuntimeDeviceEntry` 在持有全局索引写锁时调用设备锁

`internal/udphub/runtime_index.go:347-365`：

```go
runtimeIndexMu.Lock()
if online {
    dev.UpdateRuntime(func(current *models.Device) {   // 内部获取 dev.runtimeMu
        current.OnlineTime = seenAt
    })
    // ...
}
// ...
if remote || !online {
    dev.UpdateRuntime(func(current *models.Device) {   // 再次获取 dev.runtimeMu
        current.UDPAddr = nil
        current.RealUDPAddr = nil
    })
}
runtimeIndexMu.Unlock()
```

在持有**全局** `runtimeIndexMu` 写锁期间获取**每设备** `dev.runtimeMu` 锁，形成 `runtimeIndexMu → dev.runtimeMu` 的锁序。同一文件中的 `indexRuntimeDeviceLocked`（102 行）和 `SyncUserCallSignChange`（519 行）也存在相同锁序。

**当前不构成死锁**：全代码库中未发现反向锁序（先持 `dev.runtimeMu` 再取 `runtimeIndexMu`）。`RuntimeSnapshot` 与 `UpdateRuntime` 的回调都很短且不回调外部代码——`models/device.go:215` 的注释明确要求 *"keep the callback short and avoid invoking code that may re-enter the device"*。

**风险**：这是一个**脆弱的不变式**，仅靠注释约束。未来若有人在 `UpdateRuntime` 回调中加入需要查询运行时索引的逻辑，将立即产生死锁。同时，在全局锁内做每设备锁操作会放大锁竞争——`runtimeIndexMu` 保护着所有设备的路由索引，是 UDP 转发的关键路径。

**修复建议**：将设备状态变更移出全局锁范围：

```go
// 先在全局锁外完成设备字段更新
dev.UpdateRuntime(func(current *models.Device) {
    current.OnlineTime = seenAt
    if remote || !online {
        current.UDPAddr = nil
        current.RealUDPAddr = nil
    }
})
// 全局锁只保护索引 map 的增删
runtimeIndexMu.Lock()
if online {
    onlineDevMap[dev.ID] = dev
    onlineDevMapDraARL[dev.ID] = dev
} else {
    delete(onlineDevMap, dev.ID)
    delete(onlineDevMapDraARL, dev.ID)
}
runtimeIndexMu.Unlock()
```

注意 `indexRuntimeDevice`（89 行）已经采用了这一模式——它刻意把 Redis I/O（`syncRuntimeDeviceMAC`）放在锁外，并附有清晰注释。建议将同样的纪律推广到上述几处。

---

### P3【低】`RemoveRuntimeDevice` 与 `SyncRuntimeDeviceEntry` 遍历全部群组

`internal/udphub/runtime_index.go:367` 与 `434-447`：

```go
for _, gp := range GetAllGroupsFromCache() {
    removeDeviceConnectionFromGroup(gp, dev)
}
// RemoveRuntimeDevice 中还额外遍历 userList 中每个用户的每个群组
userList.Range(func(_, value any) bool {
    info, ok := value.(*UserInfo)
    // ...
    for _, gp := range info.Groups {
        removeDeviceFromGroupRuntime(gp, dev)
    }
    return true
})
```

每次设备下线都会遍历**全部群组**，且每个群组内部还要遍历其连接池（`removeDeviceConnectionFromGroup` 对 `pool.DevConnMap` 做全表扫描）。复杂度为 `O(群组数 × 每组连接数)`，并且每个群组都要获取 `pool.mu`。

`RemoveRuntimeDevice` 更进一步：在遍历全局群组缓存之后，**又遍历 `userList` 中每个用户的每个群组**，存在明显的重复工作。

**影响**：在群组数量较多的部署中，大批设备同时掉线（如网络分区恢复、FRP 隧道重启）会产生 `O(设备数 × 群组数)` 的清理开销。文档 `docs/UDP单机转发压力测试.md` 表明项目关注大规模 fan-out 场景，此路径值得优化。

**修复建议**：为设备维护一个反向索引（设备 → 所属群组集合），使下线清理从全量遍历降为按需定位。考虑到设备的 `GroupID` 已在 `DeviceRuntimeSnapshot` 中，实现成本不高。

---

### P4【提示】UDP fan-out 架构设计良好，无需改动

审计中重点检查了 `internal/udphub/fanout_sender.go`（687 行），未发现问题，此处记录以说明该模块已达到较高水准：

- 使用 `net.FilePacketConn` **复制 UDP socket**，每个 writer 拥有独立的 Go poll.FD 视图，规避了单 FD 的 writeLock 竞争（Windows IOCP 的链式复制处理尤其细致）。
- 帧队列与 writer 队列均为**有界**，过载时通过 `enqueueLatestWorkerJob` **淘汰最旧帧而非丢弃最新帧**——对实时语音这是正确的选择。
- 通过 `fanoutCollector` 的原子计数实现**异步完成通知**，dispatcher 无需等待 writer 即可处理下一帧。
- 帧携带 `snapshotGen`，与 `domainReceiverGen` 比对以丢弃基于陈旧拓扑的帧。
- 明确拒绝在 enqueue 失败时回退为同步写（`writeUDPDomain` 中的注释说明了理由：保护 ingress worker 的延迟）。

同样地，`internal/udphub/rate_limit.go` 的 32 分片令牌桶 + `O(1)` 有界清理游标、`udp_pipeline.go` 的**按 username+ssid 稳定分片**（保证逐设备串行处理）也都是经过深思的设计。

---

## 四、逻辑问题

### L1【低】`Register` 中的存在性检查早于速率限制，存在账号枚举窗口

`internal/handler/auth_login.go:80-146` 的执行顺序：

```go
if err := validateNewUserPassword(req.Password); err != nil { ... }   // 1. 密码策略
if !allowRegistration(c.ClientIP()) { ... }                           // 2. 速率限制
existing, _ := repo.GetUserByName(req.Username)                       // 3. 用户名查重 → 409
available, err := repo.IsCallSignAvailable(req.CallSign, 0)           // 4. 呼号查重 → 409
existingPhone, _ := repo.GetUserByPhone(req.Phone)                    // 5. 手机查重 → 409
existingEmail, _ := repo.GetUserByEmail(req.Email)                    // 6. 邮箱查重 → 409
```

代码注释明确说明了设计意图（*"Consume the registration budget before database lookups"*），速率限制**确实**在数据库查询之前——这一点是正确的。

但注册接口通过不同的 409 响应**区分了用户名、呼号、手机号、邮箱四类冲突**，攻击者在速率限制配额内仍可枚举"某邮箱/手机号是否已注册"。对于业余无线电平台，**呼号本身是公开信息**，因此呼号枚举无实际危害；但邮箱与手机号属于个人信息。

**权衡说明**：区分错误类型对用户体验是必要的（用户需要知道是哪个字段冲突）。这是可用性与隐私之间的常见取舍，多数产品选择前者。

**建议**：确认 `allowRegistration` 的配额足够严格即可，不建议合并错误消息。

### L2【提示】未发现越权（IDOR）问题

对权限模型做了系统性检查，未发现缺陷。授权逻辑集中在 `internal/handler/authorization.go`，抽象干净：

```go
func canManageGroup(user *gormdb.User, group *gormdb.Group) bool {
    return user != nil && group != nil && (isAdminUser(user) || group.OwerID == user.ID)
}
```

值得肯定的几处设计：

- **路由与 handler 双重校验**：`AdminSwitchLogin` 已挂载 `RequireAdmin()` 中间件，handler 内仍再次调用 `isAdminUser(actor)`，注释说明理由是"避免以后调整路由时放宽权限"。
- **主管理员保护**：ID=1 不可被删除、不可作为切换登录目标。
- **旧接口兼容路径的权限对等**：`POST /group/update`、`POST /group/delete` 这类无 `:id` 路径参数的兼容接口，注释明确说明"处理器从 JSON/query 读取 ID 并自行执行同等权限校验"。
- **反射式角色检查的安全降级**：`hasRole` 通过接口断言调用 `HasRole`，断言失败时返回 `false`（fail-closed）。
- **分页边界统一**：所有列表接口均有 `limit > 100` 截断 + `NormalizeXxxPagination` 溢出保护，未发现可导致全表扫描的输入。

---

## 五、修复优先级建议

| 优先级 | 编号 | 问题 | 修复成本 |
|:------:|:----:|------|:--------:|
| **P0** | S1 | 31 个可达依赖漏洞（尤其 `x/image` 影响用户上传路径） | 低（升级依赖 + Go 工具链） |
| **P1** | S2 | 禁用/删除/拒绝审批时未吊销 refresh token | 极低（3 行） |
| **P1** | S3 | 生产凭据明文存于工作区（需轮换） | 中（需协调轮换窗口） |
| **P2** | P1 | 重放窗口持锁循环最坏 16383 次 | 低 |
| **P3** | P2 | 全局锁内嵌套设备锁（锁序脆弱） | 中 |
| **P3** | P3 | 设备下线时全量遍历群组 | 中（需加反向索引） |
| **P4** | S4 | 验证码非恒定时间比较 | 极低（1 行） |
| **P4** | S5 | 邮箱登录缺少账号级锁定 | 低 |

---

## 六、审计方法说明

本次审计采用的手段：

1. **静态分析**：`go build ./...`、`go vet`（全包）、`govulncheck ./...`、`npm audit`。
2. **模式扫描**：SQL 注入（拼接查询、动态 `Order`/`Where`）、路径穿越（`filepath.Join`/`os.OpenFile`/`ServeFile`）、未检查类型断言、`defer` in loop、未校验的 `strconv.Atoi`、前端 XSS 汇（`dangerouslySetInnerHTML`/`innerHTML`/`eval`）。
3. **人工代码审查**：逐行审阅认证授权链路（JWT、refresh token、中间件、WebSocket 认证、SSO）、加密实现、存储路径解析、UDP 数据面热路径（pipeline、fan-out、限流、运行时索引）、缓存一致性、互联控制面与数据面。
4. **交叉验证**：对每个疑似问题，检索全部调用点确认可达性与实际影响；对已发现的缺陷（如 S2）通过 `grep -c` 在具体行号区间内确认命中数为 0。

**审计局限**：

- 未执行动态测试（渗透测试、模糊测试、竞态检测器 `-race` 下的压力测试）。建议对 UDP 数据面在 `go test -race` 下跑一轮 `test/bench/udp_fanout` 压力测试，以验证 P2 提到的锁序在真实并发下的表现。
- 未审计 `docs/` 下的部署配置与 `deploy/` 下的 systemd/docker 配置的运维安全（如容器权限、网络隔离）。
- 前端仅做了 XSS 汇与凭据存储的针对性检查，未做完整的组件级审查。
