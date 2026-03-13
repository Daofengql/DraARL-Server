# 群组与设备管理逻辑优化任务清单

## 项目概述

优化群组和设备的管理逻辑，实现公开/私有群组机制，完���权限控制和设备管理功能。

### 核心业务逻辑

**用户与群组的关系：**
- 用户可以创建群组（公开或私有），修改自己的群组，删除自己的群组
- 用户可以看到所有公开群组
- 用户可以通过搜索找到私有群组（需要群组ID或名称），验证密码后加入
- 管理员可以查看全部群组，删除、修改任意群组

**设备与群组的关系：**
- 设备注册时默认进入 `id=1` 的默认公开群组
- 一个设备只能同时属于一个活跃群组
- 设备可以独立设置"禁发"和"禁收"状态来控制转发行为
- 设备切换到已验证过密码的私有群组时，无需再次验证密码
- 群组创建者可以查看、管理群组内的设备
- 设备的收发广播以群组为单位进行

**群组创建者的权限：**
- 查看群组内所有设备（包括在线/总设备数）
- 踢出设备（设备再次进入需要重新验证密码）
- 修改设备状态（禁收/禁发）

**私有群组加入流程：**
1. 用户A创建私有群组（Type=2），设置密码
2. 用户B在群组列表中看不到这个私有群组
3. 用户A将群组ID或名称分享给用户B
4. 用户B通过搜索功能找到该群组
5. 用户B输入密码验证，验证成功后"加入"群组
6. 此时用户B可以将自己的设备拉入这个私有群组
7. 用户B在自己的群组页面可以看到这个已加入的私有群组

### 群组类型定义（复用 Type 字段）

| Type 值 | 含义 | 可见性 | 加入方式 |
|---------|------|--------|----------|
| 1 | 公开群组 | 所有人可见 | 无需密码，直接加入设备 |
| 2 | 私有群组 | 仅创建者和已验证用户可见 | 需搜索+验证密码 |

---

## 一、数据模型修改

### 1.1 后端 - 新增 GroupMember 表

**文件**: `internal/models/group.go`

**用途**: 记录用户与群组的验证关系，支持私有群组的密码验证机制

```go
// GroupMember 群组成员关系（用户与群组的验证关系）
type GroupMember struct {
    ID          int       `json:"id" gorm:"primaryKey"`
    GroupID     int       `json:"group_id" gorm:"index"`       // 群组ID
    UserID      int       `json:"user_id" gorm:"index"`        // 用户ID
    IsVerified  bool      `json:"is_verified"`                 // 是否已验证密码
    JoinTime    time.Time `json:"join_time"`                   // 加入时间
    LastVerify  time.Time `json:"last_verify"`                 // 最后验证时间

    // 群主对设备的控制（可选：按设备控制）
    DeviceID    *int      `json:"device_id,omitempty" gorm:"index"` // 关联的设备ID（可为空）
    DisableSend bool      `json:"disable_send"`                // 禁用发送（群主设置）
    DisableRecv bool      `json:"disable_recv"`                // 禁用接收（群主设置）

    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`
}
```

**业务逻辑说明**:
- 当用户验证密码加入私有群组时，创建一条 `IsVerified=true` 的记录
- `LastVerify` 记录最后验证时间，可用于验证有效期判断
- `DeviceID` 可选字段，用于记录用户在该群组中的设备
- `DisableSend/DisableRecv` 由群组创建者设置，控制设备在该群组中的收发权限

**任务清单**:
- [ ] 创建 `GroupMember` 结构体
- [ ] 添加 GORM 数据库表结构定义
- [ ] 添加数据库迁移脚本
- [ ] 添加索引（GroupID, UserID, DeviceID）
- [ ] 编写 CRUD 基础方法

### 1.2 后端 - Device 表新增字段

**文件**: `internal/models/device.go`

**用途**: 设备级别的禁发/禁收控制，覆盖群组级别的设置

```go
// Device 设备信息（新增字段）
type Device struct {
    // ... 现有字段

    // 新增：设备级别的收发控制
    DisableSend   bool      `json:"disable_send"`   // 设备级禁发（优先级高于群组设置）
    DisableRecv   bool      `json:"disable_recv"`   // 设备级禁收（优先级高于群组设置）
}
```

**业务逻辑说明**:
- 设备级别的 `DisableSend/DisableRecv` 优先级高于群组成员级别的设置
- 具体判断逻辑：设备禁发 OR 群组成员禁发 → 最终禁发状态
- 设备所有者可以设置自己的设备禁发/禁收
- 群组创建者可以设置群组成员的设备禁发/禁收

**任务清单**:
- [ ] 添加 `DisableSend bool` 字段（设备级禁发）
- [ ] 添加 `DisableRecv bool` 字段（设备级禁收）
- [ ] 添加数据库迁移脚本

### 1.3 后端 - Group 表字段确认

**文件**: `internal/models/group.go`

**现有字段复用确认**:
- `Type int` - 群组类型（1=公开, 2=私有）
- `Password string` - 私有群组的验证密码
- `OwerID int` - 群组创建者ID
- `OwerCallSign string` - 群组创建者呼号
- `Status int` - 群组状态（0=禁用, 1=启用）
- `DevList []int` - 设备ID列表（保留兼容）

**任务清单**:
- [ ] 确认 `Type` 字段语义（1=公开, 2=私有）
- [ ] 确认 `Password` 字段用于私有群组验证
- [ ] 确认 `Status` 字段用于启用/禁用群组

---

## 二、后端 API 开发

### 2.1 群组相关 API

**文件**: `internal/handler/group.go` 或新建 `internal/handler/group_member.go`

#### 2.1.1 获取群组列表

**接口**: `GET /api/groups`

**功能**: 获取当前用户可见的群组列表

**返回内容**:
- 所有 Type=1 的公开群组
- 用户已验证密码加入的 Type=2 私有群组（通过 GroupMember 表查询）

**请求参数**:
```json
{
  "page": 1,
  "page_size": 20,
  "keyword": "搜索关键词（可选）"
}
```

**响应格式**:
```json
{
  "code": 200,
  "data": {
    "items": [
      {
        "id": 1,
        "name": "公共聊天室",
        "type": 1,
        "status": 1,
        "online_count": 5,
        "total_count": 10,
        "is_joined": true
      }
    ],
    "total": 100
  }
}
```

**任务清单**:
- [ ] 实现群组列表查询逻辑
- [ ] 区分公开/私有群组返回规则
- [ ] 添加当前用户是否已加入标识
- [ ] 添加在线/总设备数统计
- [ ] 添加分页支持
- [ ] 添加关键词搜索

#### 2.1.2 搜索群组

**接口**: `GET /api/groups/search`

**功能**: 通过群组ID或名称搜索群组（包括私有群组）

**业务场景**:
- 用户获取到私有群组的ID或名称后，通过此接口查找
- 搜索结果包含匹配的私有群组，但需要验证密码才能查看详情

**请求参数**:
```json
{
  "keyword": "群组ID或名称",
  "page": 1,
  "page_size": 10
}
```

**响应格式**:
```json
{
  "code": 200,
  "data": {
    "items": [
      {
        "id": 123,
        "name": "业余无线电交流群",
        "type": 2,
        "ower_callsign": "BG5ABC",
        "require_password": true,
        "is_verified": false
      }
    ],
    "total": 1
  }
}
```

**任务清单**:
- [ ] 实现群组搜索逻辑（支持ID精确匹配、名称模糊匹配）
- [ ] 返回私有群组的基本信息（不包含敏感信息）
- [ ] 标注是否需要密码验证
- [ ] 标注当前用户是否已验证

#### 2.1.3 加入群组（验证密码）

**接口**: `POST /api/groups/{id}/join`

**功能**: 用户验证密码加入私有群组

**业务逻辑**:
1. 检查群组是否存在
2. 检查群组类型（Type=2 才需要密码）
3. 验证密码是否正确
4. 检查用户是否已加入（查询 GroupMember 表）
5. 如果未加入，创建 GroupMember 记录，设置 `IsVerified=true`
6. 如果已加入，更新 `LastVerify` 时间

**请求体**:
```json
{
  "password": "群组密码"
}
```

**响应格式**:
```json
{
  "code": 200,
  "message": "加入成功",
  "data": {
    "group_id": 123,
    "is_verified": true,
    "join_time": "2026-03-13T10:00:00Z"
  }
}
```

**错误处理**:
- `400`: 群组不存在
- `401`: 密码错误
- `403`: 群组已禁用
- `200`: 已加入，更新验证时间

**任务清单**:
- [ ] 实现密码验证逻辑
- [ ] 创建或更新 GroupMember 记录
- [ ] 处理已加入用户重复加入的情况
- [ ] 添加错误处理和提示

#### 2.1.4 获取群组设备列表

**接口**: `GET /api/groups/{id}/devices`

**功能**: 获取群组内的设备列表

**权限规则**:
- 公开群组：所有登录用户可查看
- 私有群组：群组创建者、已验证用户可查看

**响应格式**:
```json
{
  "code": 200,
  "data": {
    "items": [
      {
        "id": 1,
        "name": "我的设备",
        "callsign": "BG5ABC",
        "ssid": 0,
        "is_online": true,
        "disable_send": false,
        "disable_recv": false,
        "owner_id": 10
      }
    ],
    "total": 5,
    "online_count": 3
  }
}
```

**任务清单**:
- [ ] 实现权限验证
- [ ] 查询群组内所有设备
- [ ] 添加在线状态统计
- [ ] 添加禁发/禁收状态

#### 2.1.5 获取群组成员列表

**接口**: `GET /api/groups/{id}/members`

**功能**: 获取已加入群组的用户列表

**权限**: 仅群组创建者可查看

**响应格式**:
```json
{
  "code": 200,
  "data": {
    "items": [
      {
        "id": 1,
        "user_id": 10,
        "username": "user1",
        "callsign": "BG5ABC",
        "is_verified": true,
        "join_time": "2026-03-13T10:00:00Z",
        "device_count": 2
      }
    ],
    "total": 5
  }
}
```

**任务清单**:
- [ ] 实现权限验证（仅创建者）
- [ ] 查询群组成员列表
- [ ] 关联用户基本信息
- [ ] 统计每个成员的设备数

#### 2.1.6 设置设备禁发/禁收

**接口**: `PUT /api/groups/{id}/devices/{deviceId}`

**功能**: 群组创建者设置设备的禁发/禁收状态

**权限**: 仅群组创建者可操作

**请求体**:
```json
{
  "disable_send": true,
  "disable_recv": false
}
```

**任务清单**:
- [ ] 实现权限验证
- [ ] 更新 GroupMember 或 Device 表的禁发/禁收状态
- [ ] 通知设备状态变更

#### 2.1.7 踢出设备

**接口**: `DELETE /api/groups/{id}/devices/{deviceId}`

**功能**: 群组创建者将设备踢出群组

**业务逻辑**:
1. 验证操作者是群组创建者
2. 将设备的 GroupID 设置为默认群组（id=1）
3. 删除对应的 GroupMember 记录（或标记为未验证）
4. 设备再次进入该群组需要重新验证密码

**任务清单**:
- [ ] 实现权限验证
- [ ] 将设备移到默认群组
- [ ] 清除验证记录
- [ ] 通知设备被踢出

#### 2.1.8 离开群组

**接口**: `POST /api/groups/{id}/leave`

**功能**: 用户主动离开私有群组

**业务逻辑**:
1. 删除用户的 GroupMember 记录
2. 将该用户在此群组中的所有设备移到默认群组

**任务清单**:
- [ ] 实现 leave 接口
- [ ] 清除成员记录
- [ ] 处理相关设备

### 2.2 设备相关 API

#### 2.2.1 切换设备群组

**接口**: `PUT /api/devices/{id}/group`

**功能**: 将设备切换到指定群组

**业务逻辑**:
1. 检查目标群组是否存在
2. 如果是公开群组（Type=1）：直接允许切换
3. 如果是私有群组（Type=2）：检查用户是否已验证（查询 GroupMember 表）
4. 更新设备的 GroupID
5. 通知设备群组变更

**请求体**:
```json
{
  "group_id": 123,
  "password": "密码（可选，私有群组且未验证时需要）"
}
```

**任务清单**:
- [ ] 实现群组切换逻辑
- [ ] 检查公开/私有群组权限
- [ ] 处理需要验证密码的情况
- [ ] 更新设备 GroupID
- [ ] 发送群组切换通知

### 2.3 权限中间件

**文件**: `internal/middleware/group_permission.go`

#### 2.3.1 群组创建者验证

```go
// IsGroupOwner 检查是否是群组创建者
func IsGroupOwner(groupID int, userID int) bool
```

**任务清单**:
- [ ] 实现 `IsGroupOwner` 函数
- [ ] 添加中间件 `RequireGroupOwner`

#### 2.3.2 已验证用户验证

```go
// IsGroupMember 检查用户是否已验证加入群组
func IsGroupMember(groupID int, userID int) bool
```

**任务清单**:
- [ ] 实现 `IsGroupMember` 函数
- [ ] 添加中间件 `RequireGroupMember`

#### 2.3.3 管理员权限验证

```go
// IsAdmin 检查是否是管理员
func IsAdmin(user *User) bool
```

**任务清单**:
- [ ] 实现 `IsAdmin` 函数（复用现有）
- [ ] 添加中间件 `RequireAdminOrOwner`

---

## 三、前端类型定义

### 3.1 更新 Group 类型

**文件**: `www/src/types/index.ts`

```typescript
export interface Group {
  id: number
  name: string
  type: number  // 1=公开, 2=私有
  callsign?: string
  password?: string  // 仅创建时可编辑，不返回给前端
  allow_callsign_ssid?: string
  ower_id?: number
  ower_callsign?: string
  devlist?: string
  master_server?: number
  slave_server?: number
  status?: number  // 0=禁用, 1=启用
  note?: string
  devices?: Device[]

  // 新增字段
  is_joined?: boolean       // 当前用户是否已加入（私有群组）
  is_owner?: boolean        // 当前用户是否是创建者
  online_count?: number     // 在线设备数
  total_count?: number      // 总设备数
  require_password?: boolean // 是否需要密码（私有群组且未验证）

  master_server_str?: string
  slave_servers?: string[]
  create_time?: string
  created_at?: string
  update_time?: string
  updated_at?: string
}
```

**任务清单**:
- [ ] 更新 `Group` 接口定义
- [ ] 添加 `is_joined` 字段
- [ ] 添加 `is_owner` 字段
- [ ] 添加 `online_count` 字段
- [ ] 添加 `total_count` 字段
- [ ] 添加 `require_password` 字段

### 3.2 新增 GroupMember 类型

**文件**: `www/src/types/index.ts`

```typescript
export interface GroupMember {
  id: number
  group_id: number
  user_id: number
  username?: string
  callsign?: string
  is_verified: boolean
  join_time: string
  last_verify: string
  device_count?: number    // 该成员在群组中的设备数
  disable_send: boolean
  disable_recv: boolean
}
```

**任务清单**:
- [ ] 添加 `GroupMember` 接口定义

### 3.3 更新 Device 类型

**文件**: `www/src/types/index.ts`

```typescript
export interface Device {
  // ... 现有字段
  disable_send?: boolean  // 禁用发送
  disable_recv?: boolean  // 禁用接收
  group_name?: string     // 所属群组名称（前端扩展）
}
```

**任务清单**:
- [ ] 添加 `disable_send` 字段
- [ ] 添加 `disable_recv` 字段
- [ ] 添加 `group_name` 字段

---

## 四、前端服务层

### 4.1 更新 groupService

**文件**: `www/src/services/group.ts`

```typescript
export const groupService = {
  // 现有方法...

  // 搜索群组（支持私有群组）
  async search(params: {
    keyword: string
    page?: number
    page_size?: number
  }): Promise<ListResponse<Group>>

  // 加入群组（验证密码）
  async join(id: number, password: string): Promise<{
    group_id: number
    is_verified: boolean
    join_time: string
  }>

  // 获取群组成员列表
  async getMembers(id: number): Promise<ListResponse<GroupMember>>

  // 设置设备禁发/禁收
  async updateDevice(
    groupId: number,
    deviceId: number,
    data: { disable_send?: boolean; disable_recv?: boolean }
  ): Promise<void>

  // 踢出设备
  async kickDevice(groupId: number, deviceId: number): Promise<void>

  // 离开群组
  async leave(id: number): Promise<void>
}
```

**任务清单**:
- [ ] 更新 `BackendGroup` 接口（添加新字段）
- [ ] 更新 `normalizeGroup` 函数
- [ ] 实现 `search` 方法
- [ ] 实现 `join` 方法
- [ ] 实现 `getMembers` 方法
- [ ] 实现 `updateDevice` 方法
- [ ] 实现 `kickDevice` 方法
- [ ] 实现 `leave` 方法

### 4.2 更新 deviceService

**文件**: `www/src/services/device.ts`

```typescript
export const deviceService = {
  // 现有方法...

  // 切换设备群组
  async switchGroup(
    deviceId: number,
    groupId: number,
    password?: string
  ): Promise<Device>
}
```

**任务清单**:
- [ ] 添加 `switchGroup` 方法
- [ ] 处理公开群组直接切换
- [ ] 处理私有群组需要密码的情况
- [ ] 处理已验证群组无需密码的情况

---

## 五、前端页面开发

### 5.1 管理员后台页面

**文件**: `www/src/pages/admin/GroupPage.tsx`

**功能需求**:
- 显示所有群组（公开+私有）
- 显示群组类型（公开/私有）
- 显示在线/总设备数
- 创建/编辑群组（包括类型选择、密码设置）
- 设备管理（展开显示设备列表）
- 踢出设备、设置禁发/禁收

**UI 设计**:
```
┌─────────────────────────────────────────────────────────────┐
│  群组管理                                    [+ 添加群组]      │
├─────────────────────────────────────────────────────────────┤
│  🔍 搜索群组名称、呼号...                                      │
├─────────────────────────────────────────────────────────────┤
│  ID  │ 名称      │ 类型 │ 呼号    │ 设备数 │ 状态 │ 操作    │
├──────┼───────────┼──────┼─────────┼────────┼──────┼─────────┤
│  1   │ 公共聊天室 │ 🔓公开 │ BG5ABC │ 5/10   │ 启用 │ ✏️ 🗑️  │
│      │           │      │         │ ▶ 展开  │      │         │
│      │ └─ 设备列表 │      │         │ - 设备1 │      │ 🔙 🚫  │
│      │           │      │         │ - 设备2 │      │ 🔙 🚫  │
│  2   │ 私密群组   │ 🔒私有 │ BG5DEF │ 3/5    │ 启用 │ ✏️ 🗑️  │
└─────────────────────────────────────────────────────────────┘
```

**任务清单**:
- [ ] 更新类型标签显示（1=🔓公开, 2=🔒私有）
- [ ] 添加群组类型选择（公开/私有单选）
- [ ] 添加密码输入框（私有群组必填，公开群组可选）
- [ ] 显示在线/总设备数（如：5/10）
- [ ] 添加设备展开列表（Accordion）
- [ ] 添加踢出设备按钮（确认对话框）
- [ ] 添加禁发/禁收开关（每行设备）

### 5.2 用户群组列表页面

**文件**: `www/src/pages/groups/GroupsPage.tsx`

**功能需求**:
- 只显示公开群组 + 用户已加入的私有群组
- 添加"搜索/加入私有群组"按钮
- 显示群组类型图标
- 显示在线/总设备数
- 显示"已加入"状态标识
- 支持创建自己的群组

**UI 设计**:
```
┌─────────────────────────────────────────────────────────────┐
│  我的群组                                    [+ 新建] [🔍加入]│
├─────────────────────────────────────────────────────────────┤
│  🔓 公开群组                                                │
│  ┌─────────────────────────────────────────────────────────┐│
│  │ 公共聊天室                    👥 5/10  [进入]            ││
│  │ BG5ABC · 10个设备                                       ││
│  └─────────────────────────────────────────────────────────┘│
│  🔒 已加入的私有群组                                        │
│  ┌─────────────────────────────────────────────────────────┐│
│  │ 业余无线电交流群 ✓              👥 3/5  [进入] [退出]    ││
│  │ 由 BG5ABC 创建                                          ││
│  └─────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────┘
```

**任务清单**:
- [ ] 区分公开群组和已加入私有群组显示
- [ ] 添加"搜索/加入私有群组"按钮
- [ ] 显示群组类型图标（🔓/🔒）
- [ ] 显示在线/总设备数（如：👥 5/10）
- [ ] 显示"已加入"标识（✓）
- [ ] 添加进入群组详情按钮
- [ ] 添加退出群组按钮（仅私有群组）

### 5.3 群组搜索对话框

**文件**: `www/src/pages/groups/GroupSearchDialog.tsx`

**功能需求**:
- 支持通过群组ID或名称搜索
- 显示搜索结果（包含私有群组）
- 点击加入时弹出密码输入框
- 验证密码成功后添加到"已加入"列表

**UI 设计**:
```
┌──────────────────────────────────────┐
│  搜索群组                    [×]     │
├──────────────────────────────────────┤
│  输入群组ID或名称                     │
│  ┌─────────────────────────────────┐ │
│  │ 🔍 123 或 业余无线电...          │ │
│  └─────────────────────────────────┘ │
│                                      │
│  搜索结果                            │
│  ┌─────────────────────────────────┐ │
│  │ 🔒 业余无线电交流群               │ │
│  │    ID: 123  创建者: BG5ABC       │ │
│  │    [加入群组]                    │ │
│  └─────────────────────────────────┘ │
└──────────────────────────────────────┘
         ↓ 点击加入
┌──────────────────────────────────────┐
│  验证密码                    [×]     │
├──────────────────────────────────────┤
│  请输入"业余无线电交流群"的密码       │
│  ┌─────────────────────────────────┐ │
│  │ ••••••••                        │ │
│  └─────────────────────────────────┘ │
│                                      │
│  [取消]              [确认加入]      │
└──────────────────────────────────────┘
```

**任务清单**:
- [ ] 创建对话框组件
- [ ] 添加搜索输入框（支持ID/名称）
- [ ] 实现搜索结果列表
- [ ] 添加"加入"按钮
- [ ] 添加密码输入对话框
- [ ] 处理验证成功后的状态更新
- [ ] 添加错误提示（密码错误、群组不存在等）

### 5.4 群组详情页面

**文件**: `www/src/pages/groups/GroupDetailPage.tsx`

**功能需求**:
- 显示群组基本信息
- 显示设备列表
- 创建者可操作：踢出设备、设置禁发/禁收
- 普通用户只能查看
- 添加"离开群组"按钮（仅私有群组）

**UI 设计（创建者视图）**:
```
┌─────────────────────────────────────────────────────────────┐
│  ← 业余无线电交流群 (🔒私有)                    [退出群组]    │
├─────────────────────────────────────────────────────────────┤
│  创建者: BG5ABC                                             │
│  设备: 3/5 在线                                             │
├─────────────────────────────────────────────────────────────┤
│  群组设备                                                    │
│  ┌─────────────────────────────────────────────────────────┐│
│  │ 🟢 我的设备1 - BG5ABC-0                    [禁发] [禁收] ││
│  │    最后在线: 2分钟前                                   ││
│  ├─────────────────────────────────────────────────────────┤│
│  │ ⚪ 我的设备2 - BG5ABC-1                    [禁发] [禁收] ││
│  │    最后在线: 1小时前                                   ││
│  └─────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────┘
```

**任务清单**:
- [ ] 创建群组详情页面
- [ ] 显示群组基本信息（名称、类型、创建者）
- [ ] 显示设备列表（区分在线/离线）
- [ ] 权限判断：创建者/普通用户
- [ ] 创建者视图：添加踢出按钮
- [ ] 创建者视图：添加禁发/禁收开关
- [ ] 添加"离开群组"按钮（仅私有群组）

### 5.5 设备管理页面优化

**文件**: `www/src/pages/devices/DevicesPage.tsx`

**功能需求**:
- 显示设备所在群组
- 添加"切换群组"按钮
- 显示设备禁发/禁收状态
- 创建者可编辑设备禁发/禁收状态

**UI 设计**:
```
┌─────────────────────────────────────────────────────────────┐
│  设备管理                                    [+ 添加设备]    │
├─────────────────────────────────────────────────────────────┤
│  状态 │ 名称      │ 呼号        │ 群组        │ 禁发禁收 │ 操作│
├──────┼───────────┼────────────┼─────────────┼─────────┼─────┤
│  🟢   │ 我的设备1 │ BG5ABC-0   │ 公共聊天室  │ 🟢🟢    │ ✏️  │
│  ⚪   │ 我的设备2 │ BG5ABC-1   │ 私密群组 🔒 │ 🟢🔴    │ ✏️  │
└─────────────────────────────────────────────────────────────┘
```

**任务清单**:
- [ ] 显示设备所在群组名称
- [ ] 私有群组显示 🔒 图标
- [ ] 添加"切换群组"按钮
- [ ] 显示禁发/禁收状态（🟢正常 / 🔴禁用）
- [ ] 编辑设备时可修改禁发/禁收

### 5.6 设备切换群组对话框

**文件**: `www/src/pages/devices/DeviceSwitchGroupDialog.tsx`

**功能需求**:
- 显示已验证的群组列表（无需密码）
- 显示公开群组列表
- 支持"搜索并加入新群组"
- 确认后更新设备群组

**UI 设计**:
```
┌──────────────────────────────────────┐
│  切换设备群组                [×]     │
├──────────────────────────────────────┤
│  当前群组: 公共聊天室                 │
│                                      │
│  已验证的群组（无需密码）             │
│  ┌─────────────────────────────────┐ │
│  │ 🔒 业余无线电交流群 ✓             │ │
│  └─────────────────────────────────┘ │
│                                      │
│  公开群组                            │
│  ┌─────────────────────────────────┐ │
│  │ 🔓 公共聊天室                    │ │
│  │ 🔓 另一个公开群组                │ │
│  └───���─────────────────────────────┘ │
│                                      │
│  [+ 搜索并加入其他群组]               │
│                                      │
│              [取消] [确认切换]       │
└──────────────────────────────────────┘
```

**任务清单**:
- [ ] 创建切换群组对话框
- [ ] 显示已验证的群组（🔒 + ✓）
- [ ] 显示公开群组（🔓）
- [ ] 标记当前所在群组
- [ ] 添加"搜索并加入其他群组"按钮
- [ ] 确认后调用切换 API
- [ ] 处理成功/失败提示

---

## 六、权限逻辑实现

### 6.1 前端权限判断

**文件**: 新建 `www/src/utils/permissions.ts`

```typescript
import { Group } from '../types'
import { useAuthStore } from '../stores/auth'

/**
 * 检查是否可以编辑群组
 */
export const canEditGroup = (
  group: Group,
  currentUserId: number | undefined,
  isAdmin: boolean
): boolean => {
  if (isAdmin) return true
  if (!currentUserId) return false
  return group.ower_id === currentUserId
}

/**
 * 检查是否可以删除群组
 */
export const canDeleteGroup = (
  group: Group,
  currentUserId: number | undefined,
  isAdmin: boolean
): boolean => {
  return canEditGroup(group, currentUserId, isAdmin)
}

/**
 * 检查是否可以踢出设备
 */
export const canKickDevice = (
  group: Group,
  currentUserId: number | undefined,
  isAdmin: boolean
): boolean => {
  return canEditGroup(group, currentUserId, isAdmin)
}

/**
 * 检查是否可以设置设备禁发/禁收
 */
export const canSetDeviceStatus = (
  group: Group,
  currentUserId: number | undefined,
  isAdmin: boolean
): boolean => {
  return canEditGroup(group, currentUserId, isAdmin)
}

/**
 * 检查是否可以查看群组设备
 */
export const canViewDevices = (group: Group): boolean => {
  // 公开群组所有人可查看，私有群组需要已加入
  return group.type === 1 || group.is_joined === true
}

/**
 * 检查是否可以离开群组
 */
export const canLeaveGroup = (group: Group): boolean => {
  // 只有私有群组且已加入的可以离开
  return group.type === 2 && group.is_joined === true
}

/**
 * 检查是否可以直接加入群组（无需密码）
 */
export const canJoinDirectly = (group: Group): boolean => {
  // 公开群组或已验证的私有群组
  return group.type === 1 || group.is_joined === true
}
```

**任务清单**:
- [ ] 创建权限判断工具文件
- [ ] 实现 `canEditGroup` 函数
- [ ] 实现 `canDeleteGroup` 函数
- [ ] 实现 `canKickDevice` 函数
- [ ] 实现 `canSetDeviceStatus` 函数
- [ ] 实现 `canViewDevices` 函数
- [ ] 实现 `canLeaveGroup` 函数
- [ ] 实现 `canJoinDirectly` 函数
- [ ] 在各页面应用权限判断
- [ ] 根据权限显示/隐藏操作按钮

### 6.2 后端权限实现

**文件**: `internal/service/group_permission.go`

```go
// 检查是否是群组创建者
func IsGroupOwner(group *Group, userID int) bool {
    return group.OwerID == userID
}

// 检查是否已验证加入群组
func IsVerifiedMember(groupID int, userID int) bool {
    var member GroupMember
    return db.Where("group_id = ? AND user_id = ? AND is_verified = ?",
        groupID, userID, true).First(&member).Error == nil
}

// 检查是否可以查看群组
func CanViewGroup(group *Group, userID int, isAdmin bool) bool {
    if isAdmin {
        return true
    }
    // 公开群组所有人可查看
    if group.Type == 1 {
        return true
    }
    // 私有群组需要已验证
    return IsVerifiedMember(group.ID, userID)
}

// 检查是否可以编辑群组
func CanEditGroup(group *Group, userID int, isAdmin bool) bool {
    if isAdmin {
        return true
    }
    return IsGroupOwner(group, userID)
}
```

**任务清单**:
- [ ] 实现群组创建者验证
- [ ] 实现已验证用户验证
- [ ] 实现管理员权限验证
- [ ] 在各个 API 接口中应用权限检查
- [ ] 添加权限日志记录

---

## 七、UI/UX 优化

### 7.1 图标和标识

**任务清单**:
- [ ] 公开群组显示 🔓 图标（LockOpen 图标）
- [ ] 私有群组显示 🔒 图标（Lock 图标）
- [ ] 已加入群组显示 ✓ 标识（绿色对勾）
- [ ] 设备在线状态：🟢 在线 / ⚪ 离线
- [ ] 禁发状态显示 🚫（红色）或 📭（灰色）
- [ ] 禁收状态显示 🚫（红色）或 📥（灰色）
- [ ] 设备数显示：👥 5/10（在线/总数）

### 7.2 提示和引导

**任务清单**:
- [ ] 私有群组加入流程引导提示
- [ ] 密码输入错误提示（如："密码错误，请重试"）
- [ ] 踢出设备确认对话框（如："确定要将设备 xxx 踢出群组吗？"）
- [ ] 空状态提示（如："暂无设备"、"暂无群组"）
- [ ] 加载状态提示（骨架屏或 Loading 动画）
- [ ] 成功操作提示（如："已加入群组"、"设备已切换群组"）

### 7.3 交互优化

**任务清单**:
- [ ] 群组列表支持下拉刷新
- [ ] 搜索支持防抖（500ms）
- [ ] 密码输入框支持显示/隐藏切换
- [ ] 禁发/禁收开关支持实时切换（无需保存按钮）
- [ ] 设备切换群组后自动返回列表
- [ ] 错误提示支持自动消失（3秒）

---

## 八、测试

### 8.1 功能测试

**公开群组测试**:
- [ ] 创建公开群组（无需密码）
- [ ] 在列表中查看公开群组
- [ ] 将设备加入公开群组（无需验证）
- [ ] 查看群组设备列表
- [ ] 创建者编辑群组信息
- [ ] 创建者删除群组

**私有群组测试**:
- [ ] 创建私有群组（设置密码）
- [ ] 创建者在列表中看到私有群组
- [ ] 其他用户在列表中看不到私有群组
- [ ] 通过ID搜索私有群组
- [ ] 通过名称搜索私有群组
- [ ] 输入正确密码加入群组
- [ ] 输入错误密码提示失败
- [ ] 加入后在列表中看到私有群组
- [ ] 将设备加入已验证的私有群组

**设备管理测试**:
- [ ] 切换设备到公开群组
- [ ] 切换设备到已验证的私有群组
- [ ] 切换设备到未验证的私有群组（需密码）
- [ ] 创建者踢出设备
- [ ] 创建者设置设备禁发
- [ ] 创建者设置设备禁收
- [ ] 设备所有者设置自己设备禁发/禁收
- [ ] 踢出后重新加入需要验证密码

**管理员测试**:
- [ ] 查看所有群组（包括私有）
- [ ] 编辑任意群组
- [ ] 删除任意群组
- [ ] 管理任意群组的设备

### 8.2 权限测试

- [ ] 普通用户只能编辑/删除自己的群组
- [ ] 普通用户只能看到公开群组和已加入的私有群组
- [ ] 管理员可以查看所有群组
- [ ] 管理员可以编辑/删除任意群组
- [ ] 未验证用户无法查看私有群组设备
- [ ] 群组创建者可以管理设备
- [ ] 非创建者无法管理设备（无操作按钮）

### 8.3 边界测试

- [ ] 设备注册默认进入 id=1 群组
- [ ] 已验证用户切换设备无需再次输入密码
- [ ] 踢出后重新加入需要验证密码
- [ ] 禁用群组无法加入设备
- [ ] 删除群组后设备自动移到默认群组
- [ ] 密码为空的私有群组无法创建
- [ ] 群组名称长度限制
- [ ] 密码长度限制

### 8.4 性能测试

- [ ] 群组列表分页加载性能
- [ ] 搜索响应时间（< 500ms）
- [ ] 设备列表展开/折叠性能
- [ ] 大量设备（100+）时的渲染性能

---

## 九、文档

### 9.1 API 接口文档

**任务清单**:
- [ ] 群组相关 API 文档
- [ ] 设备相关 API 文档
- [ ] 权限说明文档
- [ ] 错误码说明文档

### 9.2 用户使用手册

**任务清单**:
- [ ] 如何创建公开群组
- [ ] 如何创建私有群组
- [ ] 如何加入私有群组
- [ ] 如何切换设备群组
- [ ] 群组创建者如何管理设备

### 9.3 开发者文档

**任务清单**:
- [ ] 数据模型说明
- [ ] 权限系统说明
- [ ] 前端组件说明
- [ ] 后端服务说明

---

## 实施优先级

### P0 - 核心功能（第一阶段）

**后端**:
1. 创建 GroupMember 表和数据迁移
2. 实现群组列表 API（区分公开/私有）
3. 实现搜索群组 API
4. 实现加入群组 API（密码验证）

**前端**:
1. 更新 Group 类型定义
2. 更新 groupService（search, join 方法）
3. 修改群组列表页面（区分公开/私有显示）
4. 创建群组搜索对话框组件

### P1 - 重要功能（第二阶段）

**后端**:
1. 实现切换设备群组 API
2. 实现获取群组设备列表 API
3. 实现设置设备禁发/禁收 API
4. 实现踢出设备 API
5. 实现权限中间件

**前端**:
1. 更新 deviceService（switchGroup 方法）
2. 修改设备管理页面（添加切换群组）
3. 创建设备切换群组对话框
4. 优化管理员页面（设备管理功能）
5. 创建权限工具函数

### P2 - 增强功能（第三阶段）

**后端**:
1. 实现离开群组 API
2. 实现获取群组成员列表 API
3. 添加设备级别的禁发/禁收字段

**前端**:
1. 创建群组详情页面
2. 添加离开群组功能
3. 完善统计数据展示
4. UI/UX 优化（图标、提示、引导）

---

## 备注

### 业务规则总结

1. **设备注册默认行为**: 新注册的设备自动加入 `id=1` 的默认公开群组

2. **私有群组验证**: Type=2 的私有群组需要密码验证，验证后记录在 GroupMember 表

3. **密码缓存**: 已验证过的群组，用户切换设备时无需再次验证密码

4. **群组创建者权限**: 群组创建者（OwnerID）可以查看、踢出设备，设置设备禁发/禁收

5. **设备唯一群组**: 一个设备同时只能属于一个活跃群组

6. **广播单位**: 设备的收发广播以群组为单位进行

7. **禁发/禁收优先级**: 设备级别设置 > 群组成员级别设置

### 数据一致性

- 设备 `GroupID` 变更时，同步更新 GroupMember 记录
- 群组删除时，将所有设备移到默认群组
- 用户被踢出时，将其设备移到默认群组

### 安全考虑

- 密码传输使用 HTTPS
- 密码不在前端明文存储
- 后端验证所有群组操作权限
- 敏感操作（踢出、删除）需要二次确认
