# 动态码认证功能实现计划

## 一、功能概述

为没有输入方式的 UDP 普通设备提供短时效动态码认证，使用户可以在 Web 端完成设备绑定和配置。

---

## 二、安全考虑

### 2.1 密码存储变更
- **原方案**: `device_password` 使用 bcrypt 加密存储
- **新方案**: `device_password` 改为 **AES 加密存储**（可逆加密）
- **原因**: 需要能够解密后返回给设备端
- **实现**:
  - 使用 AES-256-GCM 加密
  - 密钥从配置文件读取
  - 用户修改/重新生成密码时自动更新加密存储
  - **注意**: 不需要迁移，直接修改现有字段为 AES 加密存储

### 2.2 接口限速策略
| 接口 | 限速规则 | 说明 |
|------|----------|------|
| `/api/device/pre-check` | 同一 IP 1次/秒，同一 MAC 5次/分钟 | 防止暴力破解 |
| `/api/device/request-code` | 同一 IP 1次/10秒，同一 MAC 1次/分钟 | 防止动态码滥用 |
| `/api/device/confirm-bind` | 同一 MAC 1次/5秒 | 确认绑定状态限速 |
| `/api/device/bind` | 同一用户 5次/分钟 | 防止枚举攻击 |

### 2.3 动态码安全
- 6 位数字，有效期 60 秒
- 单次使用，验证后立即失效
- 绑定状态有效期 10 分钟（用户需在此期间完成配置）

### 2.4 设备身份验证
- 使用 **MAC 地址 + IP** 作为设备唯一标识
- 设备请求时必须携带 MAC 地址
- 动态码、配置都与 MAC 绑定

---

## 三、数据库变更

### 3.1 users 表字段变更
```
-- 将 device_password 字段改为 AES 加密存储
-- 认证时先尝试 AES 解密验证，失败则回退 bcrypt（兼容旧数据）
-- 用户修改密码时直接使用 AES 加密存储
```

### 3.2 新增 device_bindings 表（可选，用于持久化记录）
```sql
CREATE TABLE device_bindings (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    user_id BIGINT NOT NULL,           -- 绑定的用户ID
    device_mac VARCHAR(17) NOT NULL,   -- 设备MAC地址
    ssid INT DEFAULT 1,                -- 分配的SSID
    dmr_id INT DEFAULT 0,              -- DMR ID
    status TINYINT DEFAULT 1,          -- 1=活跃 0=禁用
    bound_at DATETIME,                 -- 绑定时间
    last_online DATETIME,              -- 最后在线时间
    INDEX idx_user_id (user_id),
    INDEX idx_device_mac (device_mac)
);
```

---

## 四、内存缓存设计

### 4.1 待绑定设备缓存
```go
type PendingDevice struct {
    MAC           string    // 设备MAC地址
    IP            string    // 设备IP
    Code          string    // 6位动态码
    CodeExpires   time.Time // 动态码过期时间
    Bound         bool      // 是否已绑定
    BoundUserID   uint      // 绑定的用户ID
    BoundAt       time.Time // 绑定时间
    ConfigReady   bool      // 配置是否就绪
    Config        *DeviceConfig // 设备配置
    CreatedAt     time.Time // 创建时间
}

type DeviceConfig struct {
    Username       string
    DevicePassword string // 明文密码（AES解密后）
    SSID           int
    DMRID          int
}
```

### 4.2 缓存键设计
- 动态码查询: `pending:code:{dynamic_code}` → MAC
- MAC 查询: `pending:mac:{mac}` → *PendingDevice
- 过期时间: 动态码 60s，绑定状态 10min

---

## 五、API 接口设计

### 5.1 设备端接口（公开接口，无需 JWT）

#### POST /api/device/pre-check
**描述**: 设备上电后检查存储的账号密码是否有效
**限速**: 同一 IP 1次/秒，同一 MAC 5次/分钟

**Request**:
```json
{
    "mac": "AA:BB:CC:DD:EE:FF",
    "username": "BG7XXX",
    "device_password": "abc12345"
}
```

**Response 200** (认证成功):
```json
{
    "code": 200,
    "data": {
        "status": "authenticated",
        "call_sign": "BG7XXX"
    }
}
```

**Response 200** (需要绑定):
```json
{
    "code": 200,
    "data": {
        "status": "need_bind",
        "message": "请使用动态码绑定设备"
    }
}
```

**Response 429** (限速):
```json
{
    "code": 429,
    "message": "请求过于频繁，请稍后重试"
}
```

---

#### POST /api/device/request-code
**描述**: 请求生成动态码（仅当 pre-check 返回 need_bind 时调用）
**限速**: 同一 MAC 1次/分钟

**Request**:
```json
{
    "mac": "AA:BB:CC:DD:EE:FF"
}
```

**Response 200**:
```json
{
    "code": 200,
    "data": {
        "dynamic_code": "123456",
        "expires_in": 60
    }
}
```

**Response 400** (用户未设置设备密码):
```json
{
    "code": 400,
    "message": "请先在平台设置设备密码"
}
```

---

#### POST /api/device/confirm-bind
**描述**: 设备确认绑定状态（用户在 Web 端完成配置后调用）
**限速**: 同一 MAC 1次/5秒

**Request**:
```json
{
    "mac": "AA:BB:CC:DD:EE:FF"
}
```

**Response 200** (绑定已完成，配置就绪):
```json
{
    "code": 200,
    "data": {
        "status": "ready",
        "username": "BG7XXX",
        "device_password": "abc12345",
        "ssid": 1,
        "dmr_id": 4601234
    }
}
```

**Response 200** (等待中):
```json
{
    "code": 200,
    "data": {
        "status": "waiting",
        "message": "等待用户完成绑定"
    }
}
```

**Response 404** (未找到/已过期):
```json
{
    "code": 404,
    "message": "设备未请求绑定或已过期，请重新获取动态码"
}
```

---

### 5.2 Web 端接口（需要 JWT 认证）

#### POST /api/device/bind
**描述**: 用户输入动态码绑定设备
**限速**: 同一用户 5次/分钟
**权限**: 需要登录，需要已审核通过

**Request**:
```json
{
    "dynamic_code": "123456"
}
```

**Response 200**:
```json
{
    "code": 200,
    "data": {
        "device_mac": "AA:BB:CC:DD:EE:FF",
        "call_sign": "BG7XXX",
        "message": "绑定成功，请配置设备参数"
    }
}
```

**Response 400** (动态码无效):
```json
{
    "code": 400,
    "message": "动态码无效或已过期"
}
```

**Response 400** (已被绑定):
```json
{
    "code": 400,
    "message": "该设备已被其他动态码绑定"
}
```

---

#### POST /api/device/submit-config
**描述**: 提交设备配置（绑定成功后调用）
**限速**: 同一用户 10次/分钟
**权限**: 需要登录

**Request**:
```json
{
    "device_mac": "AA:BB:CC:DD:EE:FF",
    "ssid": 1
}
```

**Response 200**:
```json
{
    "code": 200,
    "data": {
        "message": "配置已保存",
        "udp_auth_info": {
            "username": "BG7XXX",
            "device_password": "abc12345"
        },
        "dmr_id": 4601234
    }
}
```

**说明**:
- `ssid` 由用户输入
- `dmr_id` 从当前登录用户的 DMR ID 字段自动获取，无需用户输入
- 此接口返回 `udp_auth_info` 供设备端获取
- 完整配置（SSID、DMR ID、频率参数）通过 UDP Type=3 Config 包下发

---

---

## 六、文件修改清单

### 6.1 新建文件

| 文件路径 | 说明 |
|----------|------|
| `internal/handler/device_bind.go` | 设备绑定相关 HTTP API |
| `internal/udphub/pending_device.go` | 待绑定设备内存缓存管理 |
| `internal/middleware/device_rate_limit.go` | 设备接口限速中间件 |
| `internal/utils/aes.go` | AES 加密/解密工具函数 |

### 6.2 修改文件

| 文件路径 | 修改内容 |
|----------|----------|
| `internal/gormdb/user.go` | 新增加密密码字段，修改密码设置逻辑 |
| `internal/handler/device_password.go` | 修改密码设置时同时更新加密字段 |
| `internal/server/server.go` | 添加新路由 |
| `internal/config/config.go` | 新增 AES 加密密钥配置 |

### 6.3 bcrypt → AES 加密迁移点

以下位置需要将 bcrypt 加密改为 AES 加密存储：

| 位置 | 说明 |
|------|------|
| `internal/udphub/auth.go` | UDP 设备认证时，密码验证逻辑 |
| `internal/handler/device_password.go` | 用户设置/重新生成设备密码时 |
| `internal/gormdb/user.go` | 用户注册时写入 device_password |

---

## 七、实现步骤

### Phase 1: 基础设施 (预计 2h)
- [ ] 1.1 添加 AES 加密工具函数 `internal/utils/aes.go`
- [ ] 1.2 数据库迁移：新增 `device_password_encrypted` 字段
- [ ] 1.3 修改用户模型，添加加密密码字段
- [ ] 1.4 配置文件添加 AES 密钥

### Phase 2: 密码存储迁移 (预计 1h)
- [ ] 2.1 修改 `UpdateDevicePassword` 同时更新两个字段
- [ ] 2.2 修改 `RegenerateDevicePassword` 同时更新两个字段
- [ ] 2.3 （可选）写迁移脚本将现有密码转为加密存储

### Phase 3: 缓存管理 (预计 1.5h)
- [ ] 3.1 实现 `PendingDevice` 结构体
- [ ] 3.2 实现待绑定设备缓存管理器
- [ ] 3.3 实现动态码生成和验证
- [ ] 3.4 实现过期清理机制

### Phase 4: 限速中间件 (预计 1h)
- [ ] 4.1 实现基于 IP/MAC 的限速器
- [ ] 4.2 配置不同接口的限速策略

### Phase 5: 设备端 API (预计 2h)
- [ ] 5.1 实现 `POST /api/device/pre-check`
- [ ] 5.2 实现 `POST /api/device/request-code`
- [ ] 5.3 实现 `POST /api/device/confirm-bind`
- [ ] 5.4 添加路由和限速配置

### Phase 6: Web 端 API (预计 1.5h)
- [ ] 6.1 实现 `POST /api/device/bind`
- [ ] 6.2 实现 `POST /api/device/submit-config`
- [ ] 6.3 添加路由和权限验证

### Phase 7: UDP Hub 配合 (预计 1h)
- [ ] 7.1 设备认证成功后，检查是否需要下发绑定配置（SSID、DMR ID）
- [ ] 7.2 通过 Type=3 Config 协议下发完整配置

### Phase 7: 测试与文档 (预计 1h)
- [ ] 7.1 单元测试：AES 加解密
- [ ] 7.2 单元测试：动态码生成验证
- [ ] 7.3 集成测试：完整流程
- [ ] 7.4 更新 API 文档

---

## 八、设备端固件配合

1. 启动时读取 NVS 中的配置
2. 调用 `/api/device/pre-check` 检查认证状态
3. 认证失败时调用 `/api/device/request-code` 获取动态码
4. 在屏幕显示动态码
5. 轮询 `/api/device/confirm-bind` 检查绑定状态
6. 绑定完成后，获取 username 和 device_password
7. 保存基础认证信息到 NVS
8. 启动 UDP 客户端，使用新凭据进行认证
9. UDP 认证成功后，通过 Type=3 Config 协议获取完整配置（频率、亚音等）

---

## 九、前端配合

### 9.1 设备管理页面修改
在「设备密码」区域旁边添加按钮：**动态码绑定**

### 9.2 绑定流程弹窗
点击按钮后弹出绑定流程窗口，包含以下步骤：

**步骤 1：输入动态码**
- 显示输入框，提示用户输入设备屏幕上显示的 6 位动态码
- 调用 `POST /api/device/bind`

**步骤 2：配置设备参数**
- 绑定成功后显示配置表单：
  - SSID 输入（1-99）
  - DMR ID 自动显示（从用户信息获取，只读）
- 调用 `POST /api/device/submit-config`

**步骤 3：完成**
- 显示成功提示
- 告知用户设备将自动获取配置并上线

---

## 十、风险与注意事项

1. **密码安全**: AES 密钥需要妥善保管，建议使用环境变量或密钥管理服务
2. **重放攻击**: 动态码单次使用，验证后立即失效
3. **枚举攻击**: 动态码 6 位数字，限速 + 短有效期可降低风险
4. **并发问题**: 使用互斥锁保护缓存操作
5. **内存泄漏**: 定期清理过期的缓存条目
6. **向后兼容**: 保留 bcrypt 字段直到所有用户迁移完成
