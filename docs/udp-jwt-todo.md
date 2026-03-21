# UDP JWT 接入实现计划

## 一、概述

### 1.1 目标
为 Android/iOS/Windows 客户端提供 UDP + JWT 认证接入方式，避免 WebSocket 跨协议转发导致的性能问题。

### 1.2 设计决策
| 项目 | 决策 |
|------|------|
| 设备类型 | 幽灵设备 (Ghost Device) |
| 数据库存储 | 不存储，仅内存 |
| 设备管理可见 | **不可见** (幽灵设备不显示在设备管理页面，仅内存存储) |
| SSID 分配 | SSID = DevModel |
| 单端登录 | **UDP 幽灵设备** (101-104) 同一用户 + 同一 SSID 只能一个在线；Web (105) 保留原有互斥逻辑 |
| 认证方式 | **双轨制** (见 1.2.1) |

### 1.2.1 认证方式（双轨制）

| 设备类型 | SSID 范围 | 认证方式 | 说明 |
|----------|----------|----------|------|
| **普通设备** | 1-99, 106-235 | 设备密码认证 | ESP32、射频互联盒子等嵌入式设备 |
| **幽灵设备** | 101-105 | JWT Token 认证 | App/PC 客户端 (Android/iOS/Windows/Web) |
| **服务器互联** | 236-255 | 保留 | 服务器间通信，暂未使用 |

**设计说明**:
- **普通设备**（如 ESP32 链路台/手咪）性能有限，不适合进行 JWT 解析，继续使用传统的设备密码认证
- **幽灵设备**（App/PC 客户端）性能充足，使用 JWT 认证可以复用现有登录接口，无需预共享密钥
- 两种认证方式共存，互不影响

**单端登录规则**:
- **UDP 幽灵设备 (101-104)**: 同一用户 + 同一 SSID 只能一个在线，新登录自动踢掉旧设备
- **Web 幽灵设备 (105)**: 保留原有 WebSocket 互斥逻辑，不在此处处理

**设备管理可见性**:
- 设备管理页面（包括管理员后台）**只显示普通设备** (SSID 1-99, 106-235)
- 幽灵设备 (SSID 101-105) 不显示在设备管理页面，仅存在于内存中

### 1.3 设备型号定义
| DevModel | 说明 | 认证方式 | 单端登录 |
|----------|------|----------|----------|
| 100 | 微信小程序 (WebSocket) | - | 原有逻辑 |
| 101 | Android App (UDP JWT) | JWT | 踢旧设备 |
| 102 | iOS App (UDP JWT) | JWT | 踢旧设备 |
| 103 | Windows PC (UDP JWT) | JWT | 踢旧设备 |
| 104 | macOS (UDP JWT, 预留) | JWT | 踢旧设备 |
| 105 | Web 浏览器 (WebSocket JWT) | JWT | 原有互斥逻辑 |
| 106 | 1W 射频互联盒子 | 设备密码 | - |
| 107 | ESP32 链路台/手咪 | 设备密码 | - |

### 1.4 SSID 分配规则

| SSID 范围 | 用途 | 认证方式 | 说明 |
|----------|------|----------|------|
| **1-99** | 普通设备 | **设备密码** | 用户自定义，适用于 ESP32 等嵌入式设备 |
| **100** | 预留 | - | 原微信小程序 |
| **101** | Android 幽灵设备 | **JWT** | UDP JWT 认证 |
| **102** | iOS 幽灵设备 | **JWT** | UDP JWT 认证 |
| **103** | Windows 幽灵设备 | **JWT** | UDP JWT 认证 |
| **104** | macOS 幽灵设备 | **JWT** | UDP JWT 认证 (预留) |
| **105** | Web 幽灵设备 | **JWT** | WebSocket JWT 认证 |
| **106-235** | 普通设备扩展 | **设备密码** | 用户自定义，适用于嵌入式设备 |
| **236-255** | 服务器互联保留 | - | 系统保留，用户不可分配 |

**普通设备可用范围**: 1-99 和 106-235 (共 229 个)，使用**设备密码认证**
**幽灵设备范围**: 101-105，使用 **JWT 认证**
**保留范围**: 100 (预留) 和 236-255 (服务器互联)

---

## 二、协议定义

### 2.1 新增数据包类型
**文件**: `internal/protocol/draarl.go`

- 新增常量 `DraARLTypeJWTAuth byte = 1`
- 用途：JWT 认证包，客户端发送此类型包进行身份认证

### 2.2 认证包格式

**请求 (Type=1)**:
```
┌──────────────────────────────────────────────────────────────┐
│  Header (90 字节)                                            │
│    Version: "DraA"                                           │
│    Length: 90 + Token长度                                    │
│    Username: 用户名 (可选，用于日志)                          │
│    DevicePassword: 留空                                      │
│    Type: 1 (DraARLTypeJWTAuth)                               │
│    DevModel: 101/102/103/104                                 │
│    SSID: 忽略 (服务器会用 DevModel 作为 SSID)                 │
│    DMRID: 0                                                  │
│    CallSign: 空 (服务器填充)                                  │
│    Reserved: 4字节保留                                        │
├──────────────────────────────────────────────────────────────┤
│  DATA 区域                                                    │
│    JWT Token 字符串 (UTF-8 编码，直到包尾)                    │
└──────────────────────────────────────────────────────────────┘
```

**响应 (Type=1)**:
```
┌──────────────────────────────────────────────────────────────┐
│  Header (90 字节)                                            │
│    Version: "DraA"                                           │
│    Length: 90 + DATA长度                                     │
│    Username: 回显用户名                                       │
│    DevicePassword: 空                                        │
│    Type: 1                                                   │
│    DevModel: 回显                                            │
│    SSID: 服务器分配 (等于 DevModel)                           │
│    DMRID: 0                                                  │
│    CallSign: 成功时填充用户呼号，失败时为空                    │
│    Reserved: 4字节保留                                        │
├──────────────────────────────────────────────────────────────┤
│  DATA 区域                                                    │
│    [0]: 状态码                                                │
│         0 = 认证成功                                          │
│         1 = Token 无效                                        │
│         2 = 用户不存在                                        │
│         3 = 用户已禁用                                        │
│         4 = 用户未审核                                        │
│    [1:]: 成功时为空，失败时为错误消息文本                      │
└──────────────────────────────────────────────────────────────┘
```

### 2.3 新增辅助函数
**文件**: `internal/protocol/draarl.go`

```go
// IsGhostDevModel 判断是否为幽灵设备型号
func IsGhostDevModel(devModel byte) bool

// GetGhostSSID 获取幽灵设备的 SSID (等于 DevModel)
func GetGhostSSID(devModel byte) byte
```

### 2.4 SSID 范围常量
**文件**: `internal/protocol/draarl.go`

```go
const (
    // 普通设备 SSID 范围（两段）
    SSIDRangeNormal1Min  byte = 1    // 普通设备第一段最小 SSID
    SSIDRangeNormal1Max  byte = 99   // 普通设备第一段最大 SSID
    SSIDRangeNormal2Min  byte = 106  // 普通设备第二段最小 SSID
    SSIDRangeNormal2Max  byte = 235  // 普通设备第二段最大 SSID

    // 幽灵设备保留 SSID 范围
    SSIDRangeGhostMin    byte = 100  // 幽灵设备保留最小
    SSIDRangeGhostMax    byte = 105  // 幽灵设备保留最大 (含 Web)

    // 服务器互联保留 SSID 范围
    SSIDRangeInterconnectMin byte = 236  // 服务器互联最小
    SSIDRangeInterconnectMax byte = 255  // 服务器互联最大

    // 幽灵设备 SSID（等于 DevModel）
    SSIDGhostAndroid  byte = 101  // Android App
    SSIDGhostIOS      byte = 102  // iOS App
    SSIDGhostWindows  byte = 103  // Windows PC
    SSIDGhostMacOS    byte = 104  // macOS (预留)
    SSIDGhostWeb      byte = 105  // Web 浏览器
)
```

### 2.5 SSID 验证函数
**文件**: `internal/protocol/draarl.go`

```go
// IsValidNormalSSID 检查是否为有效的普通设备 SSID
// 普通设备可用: 1-99 或 106-235
func IsValidNormalSSID(ssid byte) bool {
    return (ssid >= SSIDRangeNormal1Min && ssid <= SSIDRangeNormal1Max) ||
           (ssid >= SSIDRangeNormal2Min && ssid <= SSIDRangeNormal2Max)
}

// IsGhostSSID 检查是否为幽灵设备保留 SSID (100-105)
func IsGhostSSID(ssid byte) bool {
    return ssid >= SSIDRangeGhostMin && ssid <= SSIDRangeGhostMax
}

// IsInterconnectSSID 检查是否为服务器互联保留 SSID (236-255)
func IsInterconnectSSID(ssid byte) bool {
    return ssid >= SSIDRangeInterconnectMin && ssid <= SSIDRangeInterconnectMax
}

// IsReservedSSID 检查是否为保留 SSID (用户不可分配)
// 保留范围: 100-105 (幽灵设备) 和 236-255 (服务器互联)
func IsReservedSSID(ssid byte) bool {
    return IsGhostSSID(ssid) || IsInterconnectSSID(ssid)
}
```

---

## 三、UDP 幽灵设备管理器

### 3.1 复用现有结构
**决策**: 直接复用 `models.Device` 结构体，不新建 UDPGhostDevice

**理由**:
- `models.Device` 已包含所有需要的字段 (OwnerID, Username, CallSign, SSID, DevModel, GroupID, UDPAddr, LastPacketTime, ISOnline 等)
- 现有转发逻辑可直接用于幽灵设备，无需额外适配
- 代码维护简单，统一处理

**区分方式**: 通过 `DevModel` 字段区分
- `DevModel` 在 101-104 范围内 = 幽灵设备
- 使用 `protocol.IsGhostDevModel(dev.DevModel)` 判断

### 3.2 新建文件
**文件**: `internal/udphub/ghost.go`

### 3.3 数据结构

```go
// 直接复用 models.Device，不新建结构体
// 幽灵设备存储在独立的 map 中，不写入数据库

// UDPGhostManager UDP 幽灵设备管理器
type UDPGhostManager struct {
    devices map[string]*models.Device  // key: username-ssid
    mu      sync.RWMutex
}

// IsGhostDevice 判断是否为幽灵设备
func IsGhostDevice(dev *models.Device) bool {
    return protocol.IsGhostDevModel(dev.DevModel)
}
```

### 3.4 管理器方法

| 方法 | 功能 | 说明 |
|------|------|------|
| `Register(username, ssid, device)` | 注册设备 | 自动踢掉同 key 的旧设备 |
| `Get(username, ssid)` | 获取设备 | 返回单个设备 |
| `GetByUsername(username)` | 获取用户所有设备 | 返回该用户所有平台的幽灵设备 |
| `GetByGroup(groupID)` | 获取群组内设备 | 用于群组广播 |
| `Remove(username, ssid)` | 移除设备 | 主动下线 |
| `GetAll()` | 获取所有设备 | 用于遍历 |
| `CheckTimeout()` | 检查超时 | 移除长时间无心跳的设备 |

### 3.5 全局实例
```go
var GlobalUDPGhostManager = &UDPGhostManager{
    devices: make(map[string]*models.Device),
}
```

---

## 四、JWT 认证处理

### 4.1 新建文件
**文件**: `internal/udphub/jwt_auth.go`

### 4.2 主要函数

#### 4.2.1 HandleJWTAuthPacket
**功能**: 处理 Type=1 JWT 认证包

**流程**:
```
1. 从 packet.DATA 提取 JWT Token
2. 调用 jwt.ParseToken() 验证 Token
3. 验证失败 → 发送错误响应，返回
4. 从 Token 获取 username
5. 查询数据库获取用户信息
6. 检查用户状态 (Status, ApprovalStatus)
7. 计算SSID: ssid = GetGhostSSID(packet.DevModel)
8. 创建 UDPGhostDevice 结构
9. 调用 GlobalUDPGhostManager.Register() 注册
   - 如果已存在同 key 设备，会被新设备替换 (踢下线)
10. 发送成功响应
```

#### 4.2.2 sendJWTAuthResponse
**功能**: 发送认证响应包

**参数**:
- `packet *protocol.DraARLv1Packet`: 原始请求包
- `conn *net.UDPConn`: UDP 连接
- `success bool`: 是否成功
- `callSign string`: 成功时为用户呼号
- `errorCode byte`: 失败时为错误码
- `errorMsg string`: 失败时为错误消息

**实现**:
```go
func sendJWTAuthResponse(packet *protocol.DraARLv1Packet, conn *net.UDPConn,
    success bool, callSign string, errorCode byte, errorMsg string) {

    var data []byte
    var responseCallSign string

    if success {
        data = []byte{0}  // 状态码 0 = 成功
        responseCallSign = callSign
    } else {
        data = append([]byte{errorCode}, []byte(errorMsg)...)
        responseCallSign = ""
    }

    ssid := protocol.GetGhostSSID(packet.DevModel)

    // 组装返回的数据包
    response := protocol.EncodeDraARLv1(
        packet.Username,    // 回显
        "",                 // password 空
        ssid,               // 服务器分配的 SSID
        protocol.DraARLTypeJWTAuth,  // Type=1
        packet.DevModel,    // 回显
        0,                  // dmrid
        responseCallSign,   // 呼号
        data,               // DATA
    )

    conn.WriteToUDP(response, packet.UDPAddr)
}
```

### 4.3 错误码定义
| 错误码 | 常量名 | 说明 |
|--------|--------|------|
| 0 | JWTAuthSuccess | 认证成功 |
| 1 | JWTAuthInvalidToken | Token 无效或过期 |
| 2 | JWTAuthUserNotFound | 用户不存在 |
| 3 | JWTAuthUserDisabled | 用户已禁用 |
| 4 | JWTAuthUserNotApproved | 用户未审核 |
| 5 | JWTAuthInvalidDevModel | 无效的设备型号 (非 101-104) |

---

## 五、路由分发修改

### 5.1 修改文件
**文件**: `internal/udphub/server.go`

### 5.2 processDraARLPacket 函数修改

**当前逻辑**:
```
1. 检查数据包大小
2. 限速检查
3. 解析数据包
4. 查找设备 (devUsernameSSIDMap)
5. 设备不存在 → handleNewDraARLDevice
6. 设备存在 → 处理业务
```

**修改后逻辑**:
```
1. 检查数据包大小
2. 限速检查
3. 解析数据包

4. 【新增】如果是 Type=1 JWT 认证包
   → 直接调用 HandleJWTAuthPacket()，然后 return

5. 【新增】检查 SSID 合法性
   - 如果 IsReservedSSID(packet.SSID) 为 true，拒绝普通设备使用保留 SSID
   - 保留 SSID 范围: 100-105 (幽灵设备) 和 236-255 (服务器互联)
   - 普通设备可用: 1-99 和 106-235

6. 查找设备
   a. 先查 devUsernameSSIDMap (普通设备)
   b. 【新增】再查 GlobalUDPGhostManager (幽灵设备)

7. 设备都不存在 → handleNewDraARLDevice
8. 设备存在 → 处理业务
```

### 5.3 新增辅助函数

```go
// getDeviceFromMemory 获取设备 (先查普通设备，再查幽灵设备)
// 返回: device, isGhost
func getDeviceFromMemory(username string, ssid byte) (*models.Device, bool) {
    // 1. 查普通设备
    usernameSSID := protocol.GetUsernameSSID(username, ssid)
    if dev, exists := devUsernameSSIDMap[usernameSSID]; exists {
        return dev, false
    }

    // 2. 查幽灵设备
    if ghost := GlobalUDPGhostManager.Get(username, ssid); ghost != nil {
        return ghost, true
    }

    return nil, false
}
```

### 5.4 parseDraARL 函数修改

**简化参数** (因为幽灵设备也是 `*models.Device`):
```go
func parseDraARL(packet *protocol.DraARLv1Packet, data []byte, dev *models.Device,
    isGhost bool, conn *net.UDPConn, gp *models.Group, realAddr *net.UDPAddr) {

    switch packet.Type {
    case protocol.DraARLTypeJWTAuth:  // Type=1
        // JWT 认证包已在 processDraARLPacket 中处理，这里不应该到达
        log.Printf("[WARN] Unexpected JWT auth packet in parseDraARL")

    case protocol.DraARLTypeHeartbeat:  // Type=2
        // 幽灵设备和普通设备心跳处理逻辑略有不同，但可以统一处理
        // 区别：幽灵设备不验证密码
        handleDraARLHeartbeat(packet, data, dev, conn, gp, realAddr, isGhost)

    case protocol.DraARLTypeOpus16K:  // Type=5
        // 语音处理逻辑相同，直接复用
        handleDraARLVoice(packet, data, dev, conn, gp)

    // ... 其他类型类似处理
    }
}
```

---

## 六、幽灵设备业务处理

### 6.1 心跳处理
**修改函数**: `handleDraARLHeartbeat`

**新增参数**: `isGhost bool`

**幽灵设备心跳处理**:
```
1. 如果 isGhost 为 true，跳过密码验证
2. 更新 ghost.LastPacketTime = now
3. 更新 ghost.UDPAddr (可能变化)
4. 解析 GPS 数据 (如果有)
5. 如果 ghost.GroupID == 0，设置为默认群组 999
6. 发送心跳响应 (填充 CallSign)
7. 如果 ghost.ISOnline == false，标记为上线，打印日志
```

**注意**: 幽灵设备心跳**不验证密码**，因为已经在 JWT 认证时验证过了。

### 6.2 语音处理
**无需修改**: `handleDraARLVoice`

幽灵设备的语音处理逻辑与普通设备相同，直接复用。

### 6.3 文本消息处理
**无需修改**: `handleDraARLTextMessage`

幽灵设备的文本消息处理逻辑与普通设备相同，直接复用。

---

## 七、转发逻辑适配

### 7.1 修改 forwardToUDPDevices 函数

**当前逻辑**: 只遍历群组内的普通设备

**修改后**: 遍历群组内的所有设备（包括幽灵设备）

```go
// forwardToUDPDevices 统一的 UDP 设备转发逻辑
// 遍历设备列表，将数据转发给所有有效的目标设备
func forwardToUDPDevices(devices []*models.Device, sourceID int, expectedGroupID int, skipSelf bool, data []byte) {
    for _, target := range devices {
        if canForwardToDevice(target, sourceID, expectedGroupID, skipSelf) {
            globalConn.WriteToUDP(data, target.UDPAddr)
        }
    }
}

// 【新增】同时转发给幽灵设备
func forwardToGhostDevices(sourceKey string, groupID int, data []byte) {
    ghosts := GlobalUDPGhostManager.GetByGroup(groupID)
    for _, ghost := range ghosts {
        key := fmt.Sprintf("%s-%d", ghost.Username, ghost.SSID)
        if key == sourceKey {
            continue  // 不发给自己
        }
        if !ghost.ISOnline || ghost.UDPAddr == nil {
            continue
        }
        globalConn.WriteToUDP(data, ghost.UDPAddr)
    }
}
```

### 7.2 修改现有转发函数

**修改**: `forwardDraARLVoice`

**修改后**:
```
1. 重编码数据包
2. forwardToUDPDevices (UDP 普通设备)
3. 【新增】forwardToGhostDevices (UDP 幽灵设备)
4. forwardVoiceToLinkedGroups (互联群组)
5. BroadcastVoiceFromUDP (WebSocket 设备)
```

### 7.3 WS → UDP 转发适配

**检查**: `BroadcastVoiceFromUDP` 和相关 WS 桥接函数

确保 WebSocket 语音能转发给 UDP 幽灵设备:
- 遍历 `GlobalUDPGhostManager.GetByGroup(groupID)`
- 发送数据到幽灵设备的 UDPAddr

---

## 八、在线状态与超时检测

### 8.1 修改 checkDeviceOnline 函数

**新增**: 幽灵设备超时检测

```go
func checkDeviceOnline() {
    // ... 现有的普通设备检测逻辑 ...

    // 【新增】幽灵设备超时检测
    GlobalUDPGhostManager.CheckTimeout(30 * time.Second)
}
```

### 8.2 实现幽灵设备超时检测

```go
// CheckTimeout 检查并移除超时的幽灵设备
func (m *UDPGhostManager) CheckTimeout(timeout time.Duration) {
    m.mu.Lock()
    defer m.mu.Unlock()

    now := time.Now()
    for key, ghost := range m.devices {
        if now.Sub(ghost.LastPacketTime) > timeout {
            log.Printf("[GHOST] 设备超时下线: %s-%d", ghost.Username, ghost.SSID)
            delete(m.devices, key)
        }
    }
}
```

---

## 九、群组管理适配

### 9.1 分平台群组偏好存储

**问题**: 如果所有平台共用 `users.last_group_id`，一个客户端切换群组会影响其他平台。

**解决方案**: 新建 `user_device_preferences` 表，按平台独立存储群组偏好。

#### 9.1.1 数据库表设计

```sql
CREATE TABLE user_device_preferences (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT UNSIGNED NOT NULL COMMENT '用户ID，外键关联 users表',
    dev_model TINYINT UNSIGNED NOT NULL COMMENT '设备型号: 101=Android, 102=iOS, 103=Windows, 104=macOS, 105=Web',
    last_group_id INT UNSIGNED DEFAULT 0 COMMENT '该平台最后使用的群组ID',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    UNIQUE KEY uk_user_devmodel (user_id, dev_model),
    INDEX idx_user_id (user_id),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) COMMENT='用户各平台设备偏好设置';
```

#### 9.1.2 数据示例

| id | user_id | dev_model | last_group_id | 说明 |
|----|---------|-----------|---------------|------|
| 1 | 1001 | 101 | 999 | 用户A的 Android 群组 |
| 2 | 1001 | 103 | 888 | 用户A的 Windows 群组 |
| 3 | 1001 | 105 | 777 | 用户A的 Web 群组 |
| 4 | 1002 | 101 | 999 | 用户B的 Android 群组 |

#### 9.1.3 数据迁移

```
1. 创建新表 user_device_preferences
2. 迁移 users.last_group_id → user_device_preferences (dev_model=105, Web端)
3. WebSocket 相关查询改为读新表
4. 可选：后续删除 users 表的 last_group_id 字段
```

### 9.2 幽灵设备加入群组

**时机**: JWT 认证成功后

**逻辑**:
```
1. 查询 user_device_preferences (user_id, dev_model)
2. 如果记录不存在或 last_group_id == 0，使用默认群组 999
3. 验证群组是否存在且未禁用
4. 设置 ghost.GroupID
```

**代码示例**:
```go
func getGhostDeviceGroupID(userID uint, devModel byte) uint {
    var pref models.UserDevicePreference
    result := db.Where("user_id = ? AND dev_model = ?", userID, devModel).First(&pref)

    if result.Error != nil || pref.LastGroupID == 0 {
        return 999  // 默认群组
    }

    // 验证群组是否存在且未禁用
    var group models.Group
    if err := db.First(&group, pref.LastGroupID).Error; err != nil || group.Status != 1 {
        return 999  // 群组无效，使用默认
    }

    return uint(pref.LastGroupID)
}
```

### 9.3 群组切换

**方式**: HTTP API 切换

**流程**:
```
1. App 调用 HTTP 接口切换群组
2. 更新 user_device_preferences 表 (user_id, dev_model)
3. 同时更新内存中的 ghost.GroupID
4. 下次心跳时使用新群组
```

**HTTP API 修改点**:
```go
// 原来更新 users.last_group_id
// 改为更新 user_device_preferences

func SwitchGroup(userID uint, devModel byte, groupID uint) error {
    // Upsert 操作
    return db.Assign(map[string]interface{}{
        "last_group_id": groupID,
    }).FirstOrCreate(&UserDevicePreference{}, map[string]interface{}{
        "user_id":   userID,
        "dev_model": devModel,
    }).Error
}
```

### 9.4 WebSocket 相关修改

**影响范围**: Web 客户端 (DevModel=105) 的群组查询

**修改点**:
| 文件 | 修改内容 |
|------|----------|
| WebSocket 认证 | 改为查 `user_device_preferences` (dev_model=105) |
| Web 切换群组 | 更新 `user_device_preferences` (dev_model=105) |
| Web 获取当前群组 | 查 `user_device_preferences` (dev_model=105) |

---

## 十、通信记录适配

### 10.1 幽灵设备语音记录

**修改**: `handleDraARLVoice`

在转发语音前，调用 `RecordCommPacket`:
```go
if len(packet.DATA) > 0 {
    gid := uint(gp.ID)
    uid := uint(dev.OwnerID)
    // 幽灵设备使用正数 ID + SSID 区分
    RecordCommPacket(dev.OwnerID, uint8(dev.SSID), &gid, &uid, packet.DATA)
}
```

### 10.2 幽灵设备文本记录

**修改**: `handleDraARLTextMessage`

在转发消息后，调用 `RecordTextMessage`。

---

## 十一、实现顺序与依赖关系

```
阶段1: 协议基础
├── 1.1 新增 DraARLTypeJWTAuth 常量
├── 1.2 新增 IsGhostDevModel 函数
├── 1.3 新增 GetGhostSSID 函数
├── 1.4 新增 SSID 范围常量
└── 1.5 新增 SSID 验证函数

阶段2: 数据库表
├── 2.1 创建 user_device_preferences 表
├── 2.2 迁移 users.last_group_id 数据
└── 2.3 修改 WebSocket 群组查询

阶段3: 幽灵设备管理器
├── 3.1 定义 UDPGhostManager 结构
├── 3.2 实现 Register 方法
├── 3.3 实现 Get 方法
├── 3.4 实现 GetByGroup 方法
├── 3.5 实现 Remove 方法
├── 3.6 实现 CheckTimeout 方法
└── 3.7 创建全局实例 GlobalUDPGhostManager

阶段4: JWT 认证处理
├── 4.1 实现 HandleJWTAuthPacket
├── 4.2 实现 sendJWTAuthResponse
└── 4.3 定义错误码常量

阶段5: 路由分发
├── 5.1 修改 processDraARLPacket (添加 Type=1 处理)
├── 5.2 修改 processDraARLPacket (添加 SSID 合法性检查)
├── 5.3 新增 getDeviceFromMemory 函数
└── 5.4 修改 parseDraARL (添加 isGhost 参数)

阶段6: 业务处理
├── 6.1 修改 handleDraARLHeartbeat (支持幽灵设备)
└── 6.2 通信记录适配

阶段7: 转发适配
├── 7.1 新增 forwardToGhostDevices
├── 7.2 修改 forwardDraARLVoice
├── 7.3 修改 forwardDraARLMessage
└── 7.4 检查 WS 桥接函数

阶段8: 在线状态
├── 8.1 修改 checkDeviceOnline
└── 8.2 集成幽灵设备超时清理
```

---

## 十二、文件清单

### 12.1 新增文件
| 文件路径 | 说明 |
|----------|------|
| `internal/udphub/ghost.go` | UDP 幽灵设备管理器 |
| `internal/udphub/jwt_auth.go` | JWT 认证处理 |

### 12.2 修改文件
| 文件路径 | 修改内容 |
|----------|----------|
| `internal/protocol/draarl.go` | 新增常量和辅助函数 |
| `internal/udphub/server.go` | 路由分发修改 |
| `internal/udphub/forward.go` | 转发逻辑适配 (如果存在) |
| `pkg/websocket/bridge.go` | WS桥接适配 (如果需要) |

---

## 十三、注意事项

### 13.1 安全考虑
- JWT Token 在 UDP 中是明文传输，建议在生产环境使用短期 Token
- 幽灵设备的 SSID 固定为 101-105，不应与普通设备范围冲突
- 认证失败应有频率限制，防止暴力破解
- 普通设备禁止使用保留 SSID 范围 (100-105 和 236-255)

### 13.2 性能考虑
- 幽灵设备管理器使用读写锁，减少锁竞争
- 超时检测间隔建议 10-30 秒
- 转发时使用批量发送 (如有 BatchSender)

### 13.3 兼容性
- 现有 UDP 普通设备不受影响
- 现有 WebSocket 设备不受影响
- 协议版本号不变，仍为 "DraA"

---

## 十四、客户端实现参考

### 14.1 认证包构建 (伪代码)

```kotlin
// Android/Kotlin
fun buildJWTAuthPacket(jwtToken: String, devModel: Byte): ByteArray {
    val tokenBytes = jwtToken.toByteArray(Charsets.UTF_8)
    val totalSize = 90 + tokenBytes.size

    val packet = ByteArray(totalSize)
    val buffer = ByteBuffer.wrap(packet).order(ByteOrder.BIG_ENDIAN)

    // Header (90 bytes)
    buffer.put("DraA".toByteArray())           // 0-3: Version
    buffer.putShort(totalSize.toShort())       // 4-5: Length
    putPaddedString(buffer, "", 32)            // 6-37: Username (空)
    putPaddedString(buffer, "", 10)            // 38-47: Password (空)
    buffer.put(1)                              // 48: Type = 1
    buffer.put(devModel)                       // 49: DevModel (101)
    buffer.put(0)                              // 50: SSID (服务器分配)
    buffer.put(ByteArray(35))                  // 51-85: DMRID + CallSign + Reserved

    // DATA: JWT Token
    buffer.put(tokenBytes)

    return packet
}
```

### 14.2 认证流程 (伪代码)

```kotlin
// 1. 登录获取 Token
val loginResponse = api.login(username, password)
val jwtToken = loginResponse.data.token

// 2. 连接 UDP
val udpSocket = DatagramSocket()
val serverAddress = InetSocketAddress(serverIp, udpPort)

// 3. 发送认证包
val authPacket = buildJWTAuthPacket(jwtToken, DEV_MODEL_ANDROID)
udpSocket.send(DatagramPacket(authPacket, authPacket.size, serverAddress))

// 4. 接收认证响应
val responseBuffer = ByteArray(800)
val responsePacket = DatagramPacket(responseBuffer, responseBuffer.size)
udpSocket.receive(responsePacket)

// 5. 解析响应
val statusCode = responseBuffer[90]  // DATA 区域第一个字节
if (statusCode == 0.toByte()) {
    val callSign = String(responseBuffer, 54, 32).trimEnd('\u0000')
    println("认证成功: $callSign")
} else {
    val errorMsg = String(responseBuffer, 91, responsePacket.length - 91)
    println("认证失败: $errorMsg")
}

// 6. 后续业务: 发送心跳、语音等
```