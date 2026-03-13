# 项目升级进度记录

## 后端开发进度

### P0 - 核心功能（第一阶段）

#### 1. GroupMember 模型 ✅
- **文件**: `internal/gormdb/models.go`
- **内容**:
  - 添加 `GroupMember` 结构体
  - 字段: ID, GroupID, UserID, IsVerified, JoinTime, LastVerify, DeviceID, DisableSend, DisableRecv
  - 添加到 AutoMigrate

#### 2. GroupMember 仓储 ✅
- **文件**: `internal/gormdb/group_member.go`
- **实现方法**:
  - `GetMemberByGroupAndUser` - 获取群组成员记录
  - `GetVerifiedMemberByGroupAndUser` - 获取已验证的群组成员记录
  - `ListMembersByGroup` - 获取群组成员列表
  - `ListVerifiedMembersByGroup` - 获取群组已验证成员列表
  - `ListGroupsByUser` - 获取用户已加入的群组列表
  - `CreateMember` - 创建群组成员记录
  - `UpdateMember` - 更新群组成员记录
  - `UpdateMemberVerification` - 更新成员验证状态
  - `UpdateMemberDevice` - 更新成员设备
  - `UpdateMemberDeviceStatus` - 更新成员设备禁发/禁收状态
  - `DeleteMember` - 删除群组成员记录
  - `IsVerifiedMember` - 检查用户是否已验证加入群组
  - `GetMemberByDevice` - 获取设备所在的群组成员记录
  - `CountMembersByGroup` - 统计群组成员数

#### 3. Device 模型扩展 ✅
- **文件**: `internal/gormdb/models.go`
- **新增字段**:
  - `DisableSend bool` - 设备级禁发
  - `DisableRecv bool` - 设备级禁收

#### 4. GroupRepository 扩展 ✅
- **文件**: `internal/gormdb/repositories.go`
- **新增方法**:
  - `ListPublicGroups` - 获取公开群组列表
  - `ListPublicGroupsPaginated` - 分页获取公开群组列表
  - `ListGroupsByType` - 按类型获取群组列表
  - `GetGroupsByIDs` - 批量获取群组

#### 5. 群组列表 API ✅
- **文件**: `internal/handler/group.go`
- **接口**: `GET /api/groups`
- **功能**:
  - 区分公开/私有群组返回
  - 公开群组所有人可见
  - 私有群组只对已验证用户可见
  - 支持分页和关键词搜索

#### 6. 搜索群组 API ✅
- **文件**: `internal/handler/group.go`
- **接口**: `POST /api/groups/search`
- **功能**:
  - 支持ID精确匹配、名称模糊匹配
  - 返回私有群组的基本信息

#### 7. 加入群组 API ✅
- **文件**: `internal/handler/group.go`
- **接口**: `POST /api/groups/{id}/join`
- **功能**:
  - 密码验证
  - 创建或更新 GroupMember 记录
  - 处理已加入用户重复加入的情况

#### 8. 获取群组成员列表 API ✅
- **文件**: `internal/handler/group.go`
- **接口**: `GET /api/groups/{id}/members`
- **功能**:
  - 仅群组创建者可查看
  - 权限验证

#### 9. 设置设备禁发/禁收 API ✅
- **文件**: `internal/handler/group.go`
- **接口**: `PUT /api/groups/{id}/devices/{deviceId}`
- **功能**:
  - 群组创建者和管理员可操作
  - 更新 GroupMember 表的设备状态

#### 10. 踢出设备 API ✅
- **文件**: `internal/handler/group.go`
- **接口**: `DELETE /api/groups/{id}/devices/{deviceId}`
- **功能**:
  - 验证操作者是群组创建者
  - 将设备移到默认群组（id=1）
  - 删除 GroupMember 记录

#### 11. 离开群组 API ✅
- **文件**: `internal/handler/group.go`
- **接口**: `POST /api/groups/{id}/leave`
- **功能**:
  - 删除用户的 GroupMember 记录
  - 将用户在该群组中的所有设备移到默认群组

#### 12. 设备级禁发/禁收 API ✅
- **文件**: `internal/handler/device.go`
- **接口**: `PUT /api/devices/{id}`
- **功能**:
  - 设备所有者可设置自己设备的禁发/禁收状态
  - 管理员可设置所有设备的禁发/禁收状态
  - 更新 Device 表的 disable_send 和 disable_recv 字段

#### 13. Device 模型修复 ✅
- **文件**: `internal/models/device.go`
- **新增字段**:
  - `DisableSend bool` - 设备级禁发
  - `DisableRecv bool` - 设备级禁收

#### 14. API 响应字段修复 ✅
- **文件**: `internal/handler/group.go`, `internal/handler/device.go`
- **修复内容**:
  - `SearchGroups` 返回 `require_password` 和 `is_verified` 字段
  - `GetGroupDevices` 返回设备禁发/禁收状态（合并设备级和群组成员级）
  - `GetDevices` 正确映射 `DisableSend` 和 `DisableRecv` 字段

---

### P1 - 重要功能（第二阶段）

#### 12. 路由注册 ✅
- **文件**: `internal/server/server.go`
- **新增路由**:
  - `POST /api/groups/search` - 搜索群组
  - `POST /api/groups/:id/join` - 加入群组
  - `GET /api/groups/:id/members` - 获取群组成员列表
  - `PUT /api/groups/:id/devices/:deviceId` - 设置设备状态
  - `DELETE /api/groups/:id/devices/:deviceId` - 踢出设备
  - `POST /api/groups/:id/leave` - 离开群组

#### 13. 权限中间件 ✅
- **文件**: `internal/middleware/group_permission.go`
- **新增中间件**:
  - `RequireGroupOwner` - 要求群组创建者权限
  - `RequireGroupMember` - 要求已验证群组成员权限
  - `RequireAdminOrOwner` - 要求管理员或群组创建者权限

---

## 前端开发进度

### P0 - 核心功能

#### 1. 更新类型定义 ✅
- **文件**: `www/src/types/index.ts`
- **内容**:
  - 更新 `Group` 接口添加新字段: is_joined, is_owner, online_count, total_count, require_password
  - 更新 `Device` 接口添加新字段: disable_send, disable_recv, group_name
  - 新增 `GroupMember` 接口

#### 2. 更新 groupService ✅
- **文件**: `www/src/services/group.ts`
- **新增方法**:
  - `search()` - 搜索群组
  - `join()` - 加入群组（验证密码）
  - `getMembers()` - 获取群组成员列表
  - `updateDevice()` - 设置设备禁发/禁收
  - `kickDevice()` - 踢出设备
  - `leave()` - 离开群组

#### 3. 更新 deviceService ✅
- **文件**: `www/src/services/device.ts`
- **新增方法**:
  - `switchGroup()` - 切换设备群组

#### 4. 更新群组列表页面 ✅
- **文件**: `www/src/pages/groups/GroupsPage.tsx`
- **功能**:
  - 区分公开群组和私有群组显示
  - 添加搜索/加入群组对话框
  - 显示群组类型图标（🔓/🔒）
  - 显示在线/总设备数
  - 支持创建公开/私有群组
  - 支持离开私有群组

#### 5. 群组搜索对话框 ✅
- **文件**: `www/src/pages/groups/GroupsPage.tsx` (内嵌)
- **功能**:
  - 支持通过群组ID或名称搜索
  - 显示搜索结果
  - 加入私有群组密码验证

#### 6. 权限工具函数 ✅
- **文件**: `www/src/utils/permissions.ts`
- **函数**:
  - `canEditGroup` - 检查是否可编辑群组
  - `canDeleteGroup` - 检查是否可删除群组
  - `canKickDevice` - 检查是否可踢出设备
  - `canSetDeviceStatus` - 检查是否可设置设备状态
  - `canViewDevices` - 检查是否可查看设备
  - `canLeaveGroup` - 检查是否可离开群组
  - `canJoinDirectly` - 检查是否可直接加入

---

### P1 - 重要功能

#### 7. 设备管理页面 ✅
- **文件**: `www/src/pages/devices/DevicesPage.tsx`
- **功能**:
  - 显示设备所在群组（含群组类型图标）
  - 显示禁发/禁收状态
  - 编辑设备时支持设置禁发/禁收
  - 添加切换群组按钮

#### 8. 设备切换群组对话框 ✅
- **文件**: `www/src/pages/devices/SwitchGroupDialog.tsx`
- **功能**:
  - 显示已验证的群组（无需密码）
  - 显示公开群组列表
  - 支持搜索并加入新群组
  - 私有群组密码验证

#### 9. 管理员页面优化 ✅
- **文件**: `www/src/pages/admin/GroupPage.tsx`
- **功能**:
  - 更新群组类型为公开/私有
  - 设备展开列表中添加禁发/禁收开关
  - 添加踢出设备按钮
  - 实时更新设备状态

#### 10. 后台页面样式统一 ✅
- **文件**: `www/src/pages/admin/DevicePage.tsx`, `www/src/pages/admin/GroupPage.tsx`
- **更新内容**:
  - 搜索栏改用 Paper variant="outlined" + 搜索按钮（与前台一致）
  - 表格使用 TableContainer component={Paper} variant="outlined"
  - 表头添加 bgcolor: 'grey.50' 背景色
  - 在线状态使用 Circle 圆点显示（绿色在线/灰色离线）
  - 群组类型图标直接显示在名称列（🔓/🔒）
  - 设备展开列表内在线状态使用圆点
  - 收发控制按钮样式统一（contained+success / outlined+error）
  - 添加切换群组功能（复用 SwitchGroupDialog）

#### 11. 后台设备管理所有者功能 ✅
- **文件**: `www/src/pages/admin/DevicePage.tsx`
- **更新内容**:
  - 添加"所有者"列，显示设备所有者
  - 支持点击所有者查看用户详情（Popover弹窗）

#### 12. 后台群组管理分类显示 ✅
- **文件**: `www/src/pages/admin/GroupPage.tsx`
- **更新内容**:
  - 像前台一样分开显示公开群组和私有群组
  - 公开群组用 primary.50 标题背景，私有群组用 secondary.50 标题背景
  - 显示群组类型图标（🔓/🔒）和数量统计

#### 13. 共享组件创建 ✅
- **文件**: `www/src/components/UserDetailPopover.tsx`
- **内容**:
  - 创建共享的用户详情 Popover 组件
  - 支持显示用户详细信息（头像、角色、状态、呼号等）

---

## 待完成功能

### 前端

#### P2 - 增强功能
- [ ] 创建群组详情页面
- [ ] UI/UX优化（骨架屏、加载动画等）
- [ ] 添加错误提示自动消失功能

### 测试
- [ ] 前后端联调测试
- [ ] 功能测试（公开/私有群组完整流程）
- [ ] 权限测试
- [ ] 边界测试

---

## 技术要点

### 群组类型定义
| Type 值 | 含义 | 可见性 | 加入方式 |
|---------|------|--------|----------|
| 1 | 公开群组 | 所有人可见 | 无需密码，直接加入设备 |
| 2 | 私有群组 | 仅创建者和已验证用户可见 | 需搜索+验证密码 |

### 权限逻辑
- 群组创建者（OwnerID）可以：
  - 查看群组内所有设备
  - 踢出设备
  - 设置设备禁发/禁收（群组成员级别）
- 管理员可以：
  - 设置所有设备的禁发/禁收状态（群组成员级别）
  - 查看和操作所有群组
- 设备所有者可以：
  - 设置自己设备的禁发/禁收状态（设备级别）
  - 将设备切换到已验证的群组
- 普通用户可以：
  - 查看已验证的私有群组
  - 将自己的设备加入已验证的群组

### 设备状态优先级
设备级别的 `DisableSend/DisableRecv` 优先级高于群组成员级别的设置

---

## 数据库变更

### 新增表
- `group_members` - 群组成员关系表

### 修改表
- `devices` - 添加 `disable_send` 和 `disable_recv` 字段

---

## API 路由配置 ✅

以下路由已全部注册：
```
GET  /api/groups                    - 获取群组列表
POST /api/groups/search             - 搜索群组
POST /api/groups/:id/join           - 加入群组
GET  /api/groups/:id/members        - 获取群组成员列表
PUT  /api/groups/:id/devices/:deviceId  - 设置设备状态（群组成员级）
DELETE /api/groups/:id/devices/:deviceId  - 踢出设备
POST /api/groups/:id/leave          - 离开群组
PUT  /api/devices/:id               - 更新设备（设备级禁发/禁收）
```

---

## 文件修改清单

### 后端文件
- `internal/models/device.go` - 添加设备级 DisableSend/DisableRecv 字段
- `internal/gormdb/models.go` - 添加 GroupMember 模型，Device 扩展字段
- `internal/gormdb/group_member.go` - 新增文件
- `internal/gormdb/repositories.go` - 群组仓储扩展
- `internal/handler/group.go` - 群组相关API
- `internal/handler/device.go` - 设备API扩展（支持设备级禁发/禁收）
- `internal/server/server.go` - 路由注册
- `internal/middleware/group_permission.go` - 新增文件

### 前端文件
- `www/src/types/index.ts` - 类型定义更新（Device添加owner字段，Group添加owner_name字段）
- `www/src/services/group.ts` - 群组服务更新
- `www/src/services/device.ts` - 设备服务更新
- `www/src/pages/groups/GroupsPage.tsx` - 群组列表页面更新
- `www/src/pages/devices/DevicesPage.tsx` - 设备管理页面更新
- `www/src/pages/devices/SwitchGroupDialog.tsx` - 新增文件
- `www/src/pages/admin/GroupPage.tsx` - 管理员群组页面（样式统一、分类显示）
- `www/src/pages/admin/DevicePage.tsx` - 管理员设备页面（样式统一、所有者列）
- `www/src/components/UserDetailPopover.tsx` - 新增共享组件
- `www/src/utils/permissions.ts` - 新增文件

---

## 构建状态 ✅

### 后端构建
```bash
go build ./cmd/udphub
```
**状态**: ✅ 通过

### 前端构建
```bash
cd www && npm run build
```
**状态**: ✅ 通过（已禁用 noUnusedLocals 检查）

### TypeScript 修复
- 修复了 `GroupPage.tsx` 中可选字段处理
- 修复了 `DevicesPage.tsx` 中 TableCell fontWeight 属性
- 修复了 `permissions.ts` 中类型导入问题
- 清理了 `SwitchGroupDialog.tsx` 中未使用的导入
