# 前端模块化重构计划

## 一、现状分析

### 1.1 重复代码统计

#### 核心重复（设备/群组管理）

| 文件 | 行数 | 主要重复内容 |
|------|------|------------|
| [DevicePage.tsx](www/src/pages/admin/DevicePage.tsx) | 633 | 设备表格、状态控制、群组切换 |
| [DevicesPage.tsx](www/src/pages/devices/DevicesPage.tsx) | 617 | 设备表格、状态控制、群组切换 |
| [GroupPage.tsx](www/src/pages/admin/GroupPage.tsx) | 761 | 群组表格、状态切换、设备展开 |
| [GroupsPage.tsx](www/src/pages/groups/GroupsPage.tsx) | 644 | 群组表格、状态切换、搜索加入 |
| [SwitchGroupDialog.tsx](www/src/pages/devices/SwitchGroupDialog.tsx) | 321 | 群组选择对话框 |
| [GroupSelector.tsx](www/src/pages/radio/components/GroupSelector.tsx) | 87 | 群组下拉选择器 |
| **小计** | **3,063** | - |

#### 额外发现的重复（审批/通信记录）

| 文件 | 行数 | 重复内容 |
|------|------|----------|
| [ApprovalsPage.tsx](www/src/pages/users/ApprovalsPage.tsx) | 868 | TabPanel、图片预览对话框 |
| [CertificateApprovalsPage.tsx](www/src/pages/certificates/CertificateApprovalsPage.tsx) | 667 | TabPanel、图片预览对话框（100%重复） |
| [CommRecordsPage.tsx](www/src/pages/comm-records/CommRecordsPage.tsx) | 556 | 设备型号图标/名称映射 |
| **小计** | **2,091** | - |

| **总计** | **5,154** | - |

---

### 1.2 重复模式识别

#### 模式A：设备表格
- **重复度**：~70%
- **共同点**：在线状态、名称、型号、呼号-SSID、群组、收发控制、操作按钮
- **差异点**：管理员页面多"所有者"列、后台分页

#### 模式B：群组表格
- **重复度**：~60%
- **共同点**：ID、名称、类型图标、呼号、拥有者、设备数、状态、备注、操作
- **差异点**：管理员页面有设备展开详情、用户页面有加入/退出功能

#### 模式C：群组选择器
- **重复度**：~80%（核心逻辑）
- **共同点**：群组分类、类型图标、在线人数
- **差异点**：Dialog形式 vs Select形式

#### 模式D：状态控制按钮
- **重复度**：100%
- **共同点**：发送/接收双按钮、点击切换、颜色状态
- **出��位置**：DevicePage、DevicesPage、GroupPage(设备展开)

#### 模式E：图片预览对话框（🔴 新发现）
- **重复度**：100%
- **出现位置**：ApprovalsPage.tsx、CertificateApprovalsPage.tsx
- **功能**：滚轮缩放、放大/缩小/重置按钮、黑色背景遮罩
- **代码量**：~95行 × 2 = ~190行

#### 模式F：TabPanel 组件（🔴 新发现）
- **重复度**：100%
- **出现位置**：ApprovalsPage.tsx、CertificateApprovalsPage.tsx
- **代码量**：~10行 × 2 = ~20行

#### 模式G：设备型号映射（🔴 新发现）
- **重复度**：~90%
- **出现位置**：CommRecordsPage.tsx（getDevModelIcon/getDevModelName）、DevicePage.tsx（DEVICE_MODELS）
- **功能**：型号ID → 图标/名称

---

## 二、目标组件架构

```
www/src/
├── utils/
│   └── deviceModel.ts           # 🔴 新增：设备型号映射
│
├── components/
│   ├── common/
│   │   ├── SearchBar/           # 搜索栏
│   │   │   ├── SearchBar.tsx
│   │   │   └── index.ts
│   │   ├── StatusControl/       # 状态控制
│   │   │   ├── ToggleButton.tsx
│   │   │   ├── SendRecvControl.tsx
│   │   │   └── index.ts
│   │   ├── OnlineIndicator/     # 在线状态
│   │   │   └── index.tsx
│   │   ├── PageHeader/          # 页面头部
│   │   │   └── index.tsx
│   │   ├── AutoRefresh/         # 自动刷新
│   │   │   └── index.tsx
│   │   ├── TabPanel/            # 🔴 新增：标签页面板
│   │   │   └── index.tsx
│   │   └── ImagePreviewDialog/  # 🔴 新增：图片预览对话框
│   │       └── index.tsx
│   │
│   ├── devices/
│   │   ├── DeviceTable/         # 设备表格
│   │   │   ├── DeviceTable.tsx
│   │   │   ├── DeviceRow.tsx
│   │   │   ├── DeviceDialog.tsx
│   │   │   ├── types.ts
│   │   │   └── index.ts
│   │   ├── DeviceModelIcon.tsx  # 🔴 新增：设备型号图标组件
│   │   └── ParamConfigDialog.tsx
│   │
│   ├── groups/
│   │   ├── GroupPicker/         # 群组选择器
│   │   │   ├── GroupPicker.tsx
│   │   │   ├── GroupPickerDialog.tsx
│   │   │   ├── GroupPickerSelect.tsx
│   │   │   ├── GroupListItem.tsx
│   │   │   ├── types.ts
│   │   │   └── index.ts
│   │   ├── GroupTable/          # 群组表格
│   │   │   ├── GroupTable.tsx
│   │   │   ├── GroupRow.tsx
│   │   │   ├── GroupDialog.tsx
│   │   │   ├── types.ts
│   │   │   └── index.ts
│   │   └── GroupTypeIcon.tsx
│   │
│   └── users/
│       └── UserDetailPopover.tsx
```

---

## 三、实施阶段

### 阶段1：基础组件（2天）

#### 1.1 StatusControl 组件
**文件**：`components/common/StatusControl/`

```tsx
// SendRecvControl.tsx
interface SendRecvControlProps {
  disableSend: boolean
  disableRecv: boolean
  onToggleSend: () => void
  onToggleRecv: () => void
  disabled?: boolean
  size?: 'small' | 'medium'
}

// 使用示例
<SendRecvControl
  disableSend={device.disable_send}
  disableRecv={device.disable_recv}
  onToggleSend={() => handleToggleSend(device)}
  onToggleRecv={() => handleToggleRecv(device)}
/>
```

**影响文件**：
- DevicePage.tsx（~30行 → ~5行）
- DevicesPage.tsx（~30行 → ~5行）
- GroupPage.tsx（~20行 → ~5行）

**预估节省**：~70行

#### 1.2 OnlineIndicator 组件
**文件**：`components/common/OnlineIndicator/`

```tsx
interface OnlineIndicatorProps {
  online: boolean
  size?: number
}
```

**影响文件**：DevicePage、DevicesPage、GroupPage

**预估节省**：~20行

#### 1.3 SearchBar 组件
**文件**：`components/common/SearchBar/`

```tsx
interface SearchBarProps {
  value: string
  onChange: (value: string) => void
  onSearch: () => void
  placeholder?: string
  loading?: boolean
}
```

**影响文件**：DevicePage、DevicesPage、GroupPage、GroupsPage、UsersPage、RelaysPage、ServersPage

**预估节省**：~100行

#### 1.4 AutoRefresh 组件
**文件**：`components/common/AutoRefresh/`

```tsx
interface AutoRefreshProps {
  value: number // 0=关闭, n=秒数
  onChange: (seconds: number) => void
  onRefresh: () => void
  loading?: boolean
}
```

**影响文件**：DevicePage、DevicesPage

**预估节省**：~40行

#### 1.5 TabPanel 组件 🔴 新增
**文件**：`components/common/TabPanel/`

```tsx
interface TabPanelProps {
  children?: React.ReactNode
  value: number
  index: number
  py?: number
}

function TabPanel({ children, value, index, py = 3 }: TabPanelProps) {
  return (
    <div role="tabpanel" hidden={value !== index}>
      {value === index && <Box sx={{ py }}>{children}</Box>}
    </div>
  )
}
```

**影响文件**：ApprovalsPage.tsx、CertificateApprovalsPage.tsx

**预估节省**：~20行

#### 1.6 ImagePreviewDialog 组件 🔴 新增
**文件**：`components/common/ImagePreviewDialog/`

```tsx
interface ImagePreviewDialogProps {
  open: boolean
  onClose: () => void
  imageUrl: string | null
  title?: string
}
```

**功能**：
- 滚轮缩放
- 放大/缩小/重置按钮
- 黑色背景遮罩
- 显示当前缩放比例

**影响文件**：ApprovalsPage.tsx、CertificateApprovalsPage.tsx

**预估节省**：~190行（95行 × 2）

---

### 阶段2：设备组件（2天）

#### 2.1 DeviceModelIcon 工具函数 🔴 新增
**文件**：`utils/deviceModel.ts`

```tsx
// 设备型号常量
export const DEVICE_MODELS = [
  { value: 0, label: '未知设备', icon: Devices },
  { value: 100, label: '微信小程序', icon: ChatBubble },
  { value: 101, label: 'Android 客户端', icon: Android },
  { value: 102, label: 'iOS 客户端', icon: PhoneIphone },
  { value: 103, label: 'Windows 客户端', icon: DesktopWindows },
  { value: 104, label: 'macOS 客户端', icon: LaptopMac },
  { value: 105, label: '浏览器客户端', icon: Language },
] as const

// 获取设备型号信息
export function getDeviceInfo(devModel: number) {
  return DEVICE_MODELS.find(m => m.value === devModel) || DEVICE_MODELS[0]
}

// 获取设备型号名称
export function getDevModelName(devModel: number): string

// 获取设备型号图标组件
export function getDevModelIcon(devModel: number): React.ReactNode

// 格式化设备显示名称（幽灵设备特殊处理）
export function formatDeviceDisplayName(deviceName: string, devModel: number): string
```

**影响文件**：
- CommRecordsPage.tsx（~60行 → ~5行）
- DevicePage.tsx（~15行 → ~2行）

**预估节省**：~68行

#### 2.2 DeviceTable 组件
**文件**：`components/devices/DeviceTable/`

```tsx
interface DeviceTableProps {
  devices: Device[]
  groups: Group[]
  loading?: boolean

  // 配置选项
  showOwner?: boolean          // 管理员模式显示所有者
  paginationMode?: 'client' | 'server' | 'none'
  showSearch?: boolean
  showAutoRefresh?: boolean

  // 事件回调
  onEdit?: (device: Device) => void
  onDelete?: (device: Device) => void
  onSwitchGroup?: (device: Device) => void
  onToggleSend?: (device: Device) => void
  onToggleRecv?: (device: Device) => void
  onOpenParams?: (device: Device) => void
  onRefresh?: () => void

  // 分页（服务端模式）
  page?: number
  rowsPerPage?: number
  onPageChange?: (page: number) => void
  onRowsPerPageChange?: (rows: number) => void
}
```

**重构后使用示例**：

```tsx
// admin/DevicePage.tsx - 管理员页面
export function AdminDevicePage() {
  const [devices, setDevices] = useState<Device[]>([])
  const [groups, setGroups] = useState<Group[]>([])

  const loadDevices = async () => { /* ... */ }

  return (
    <Box>
      <PageHeader title="设备管理">
        <AutoRefresh onRefresh={loadDevices} />
      </PageHeader>

      <DeviceTable
        devices={devices}
        groups={groups}
        showOwner              // 管理员显示所有者
        paginationMode="server"
        onEdit={handleEdit}
        onDelete={handleDelete}
        onSwitchGroup={handleSwitchGroup}
        onToggleSend={handleToggleSend}
        onToggleRecv={handleToggleRecv}
        onRefresh={loadDevices}
      />
    </Box>
  )
}

// devices/DevicesPage.tsx - 用户页面
export function DevicesPage() {
  const [devices, setDevices] = useState<Device[]>([])

  return (
    <Box>
      <PageHeader title="我的设备" />

      <DeviceTable
        devices={devices}
        groups={groups}
        showOwner={false}      // 用户页面不显示所有者
        paginationMode="client"
        showAutoRefresh
        onEdit={handleEdit}
        onDelete={handleDelete}
        onSwitchGroup={handleSwitchGroup}
      />
    </Box>
  )
}
```

**代码变化**：
- `DevicePage.tsx`：633行 → ~150行
- `DevicesPage.tsx`：617行 → ~180行

**预估节省**：~900行

---

### 阶段3：群组组件（2天）

#### 3.1 GroupPicker 组件
**文件**：`components/groups/GroupPicker/`

```tsx
interface GroupPickerProps {
  groups: Group[]
  currentGroupId?: number

  // 模式选择
  mode: 'dialog' | 'select'

  // Dialog 模式配置
  device?: Device              // 切换设备群组时需要
  showSearchTab?: boolean      // 是否显示搜索标签页

  // Select 模式配置
  size?: 'small' | 'medium'
  placeholder?: string

  // 事件
  onSelect: (groupId: number, password?: string) => void
  onClose?: () => void

  // Dialog 控制
  open?: boolean
}
```

**使用示例**：

```tsx
// 对话框模式（设备群组切换）
<GroupPicker
  mode="dialog"
  open={switchDialogOpen}
  groups={groups}
  device={switchingDevice}
  currentGroupId={switchingDevice?.group_id}
  onSelect={handleSwitchGroup}
  onClose={() => setSwitchDialogOpen(false)}
/>

// 下拉模式（Radio页面）
<GroupPicker
  mode="select"
  groups={groups}
  currentGroupId={currentGroupId}
  onSelect={handleGroupChange}
  size="small"
/>
```

**重构后文件变化**：
- `SwitchGroupDialog.tsx`：删除（321行）
- `GroupSelector.tsx`：删除（87行）
- 新增 `GroupPicker/`：~250行

**预估节省**：~150行

#### 3.2 GroupTable 组件
**文件**：`components/groups/GroupTable/`

```tsx
interface GroupTableProps {
  groups: Group[]

  // 配置
  mode: 'user' | 'admin'       // 用户模式/管理员模式
  groupType?: 'public' | 'private' | 'all'
  showSearch?: boolean

  // 用户模式特有
  onJoin?: (group: Group) => void
  onLeave?: (group: Group) => void

  // 管理员模式特有
  showDeviceList?: boolean     // 展开设备列表

  // 通用事件
  onEdit?: (group: Group) => void
  onDelete?: (group: Group) => void
  onToggleStatus?: (group: Group) => void

  // 分页
  pagination?: boolean
}
```

**代码变化**：
- `GroupPage.tsx`：761行 → ~200行
- `GroupsPage.tsx`：644行 → ~180行

**预估节省**：~1,000行

---

## 四、文件变更清单

### 4.1 新增文件

| 文件路径 | 预估行数 | 说明 |
|----------|----------|------|
| `utils/deviceModel.ts` | ~50 | 设备型号映射 |
| `components/common/StatusControl/SendRecvControl.tsx` | ~50 | 收发控制 |
| `components/common/StatusControl/ToggleButton.tsx` | ~30 | 通用切换按钮 |
| `components/common/StatusControl/index.ts` | ~5 | 导出 |
| `components/common/OnlineIndicator/index.tsx` | ~20 | 在线状态 |
| `components/common/SearchBar/SearchBar.tsx` | ~40 | 搜索栏 |
| `components/common/AutoRefresh/index.tsx` | ~50 | 自动刷新 |
| `components/common/PageHeader/index.tsx` | ~30 | 页面头部 |
| `components/common/TabPanel/index.tsx` | ~15 | 🔴 标签页面板 |
| `components/common/ImagePreviewDialog/index.tsx` | ~100 | 🔴 图片预览 |
| `components/devices/DeviceTable/DeviceTable.tsx` | ~150 | 设备表格主组件 |
| `components/devices/DeviceTable/DeviceRow.tsx` | ~100 | 设备行 |
| `components/devices/DeviceTable/DeviceDialog.tsx` | ~80 | 设备编辑对话框 |
| `components/devices/DeviceTable/types.ts` | ~30 | 类型定义 |
| `components/devices/DeviceTable/index.ts` | ~5 | 导出 |
| `components/groups/GroupPicker/GroupPicker.tsx` | ~40 | 群组选择器主组件 |
| `components/groups/GroupPicker/GroupPickerDialog.tsx` | ~150 | 对话框模式 |
| `components/groups/GroupPicker/GroupPickerSelect.tsx` | ~60 | 下拉模式 |
| `components/groups/GroupPicker/GroupListItem.tsx` | ~50 | 群组列表项 |
| `components/groups/GroupPicker/types.ts` | ~20 | 类型定义 |
| `components/groups/GroupPicker/index.ts` | ~5 | 导出 |
| `components/groups/GroupTable/GroupTable.tsx` | ~120 | 群组表格主组件 |
| `components/groups/GroupTable/GroupRow.tsx` | ~100 | 群组行 |
| `components/groups/GroupTable/GroupDialog.tsx` | ~80 | 群组编辑对话框 |
| `components/groups/GroupTable/types.ts` | ~30 | 类型定义 |
| `components/groups/GroupTable/index.ts` | ~5 | 导出 |
| `components/groups/GroupTypeIcon.tsx` | ~20 | 群组类型图标 |
| **新增总计** | **~1,485行** | - |

### 4.2 修改文件

| 文件路径 | 当前行数 | 重构后 | 节省 |
|----------|----------|--------|------|
| `pages/admin/DevicePage.tsx` | 633 | ~150 | 483 |
| `pages/devices/DevicesPage.tsx` | 617 | ~180 | 437 |
| `pages/admin/GroupPage.tsx` | 761 | ~200 | 561 |
| `pages/groups/GroupsPage.tsx` | 644 | ~180 | 464 |
| `pages/radio/components/GroupSelector.tsx` | 87 | 删除 | 87 |
| `pages/devices/SwitchGroupDialog.tsx` | 321 | 删除 | 321 |
| `pages/users/ApprovalsPage.tsx` | 868 | ~780 | 88 |
| `pages/certificates/CertificateApprovalsPage.tsx` | 667 | ~580 | 87 |
| `pages/comm-records/CommRecordsPage.tsx` | 556 | ~490 | 66 |
| **修改总计** | **5,154** | **~2,540** | **~2,594** |

### 4.3 删除文件

| 文件路径 | 行数 |
|----------|------|
| `pages/devices/SwitchGroupDialog.tsx` | 321 |
| `pages/radio/components/GroupSelector.tsx` | 87 |
| **删除总计** | **408** |

---

## 五、代码量变化总结

| 指标 | 数值 |
|------|------|
| 原始代码（涉及文件） | 5,154 行 |
| 新增公共组件 | +1,485 行 |
| 重构后页面代码 | 2,540 行 |
| **净减少** | **~1,129 行** |
| **减少比例** | **22%** |

### 打包体积预估

| 指标 | 当前 | 重构后 | 变化 |
|------|------|--------|------|
| 源代码 | 5,154 行 | 4,025 行 | -22% |
| 打包后 JS (gzip) | ~619 KB | ~585 KB | -~35 KB |
| 实际传输 | ~180 KB | ~165 KB | -~15 KB |

---

## 六、实施时间表

| 阶段 | 任务 | 预估时间 | 优先级 |
|------|------|----------|--------|
| 1.1 | StatusControl 组件 | 0.5 天 | 高 |
| 1.2 | OnlineIndicator 组件 | 0.5 天 | 高 |
| 1.3 | SearchBar 组件 | 0.5 天 | 高 |
| 1.4 | AutoRefresh 组件 | 0.5 天 | 中 |
| 1.5 | TabPanel 组件 | 0.25 天 | 中 |
| 1.6 | ImagePreviewDialog 组件 | 0.5 天 | 中 |
| 1.7 | deviceModel 工具函数 | 0.25 天 | 中 |
| 2 | DeviceTable 组件 | 2 天 | 高 |
| 3.1 | GroupPicker 组件 | 1 天 | 高 |
| 3.2 | GroupTable 组件 | 1 天 | 中 |
| **总计** | - | **7-8 天** | - |

---

## 七、不重构的部分

以下页面暂时不重构，保留现有代码：

| 页面 | 原因 |
|------|------|
| `pages/relays/RelaysPage.tsx` | 空功能，预留位置 |
| `pages/servers/ServersPage.tsx` | 空功能，预留位置 |
| `pages/users/UsersPage.tsx` | 用户管理页面，功能独立，后续单独处理 |

---

## 八、风险与注意事项

### 8.1 兼容性
- 保持现有 API 调用方式不变
- 组件 props 设计需兼容现有使用场景
- 渐进式迁移，可逐个页面替换

### 8.2 测试要点
- 各组件的单元测试
- 页面集成测试
- 打包体积验证

### 8.3 回滚策略
- Git 分支开发
- 每个 phase 完成后合并
- 保留原文件备份

---

## 九、下一步行动

1. **确认重构范围**：批准此计划后开始执行
2. **创建开发分支**：`feat/refactor-components`
3. **开始阶段1**：从 StatusControl 和 OnlineIndicator 开始
4. **优先完成**：ImagePreviewDialog（100%重复，立即收益）

---

## 十、附录：ImagePreviewDialog 详细设计

```tsx
// components/common/ImagePreviewDialog/index.tsx
import { useState } from 'react'
import {
  Dialog,
  DialogTitle,
  DialogContent,
  IconButton,
  Button,
  Box,
  Typography,
} from '@mui/material'
import Close from '@mui/icons-material/Close'

interface ImagePreviewDialogProps {
  open: boolean
  onClose: () => void
  imageUrl: string | null
  title?: string
}

export function ImagePreviewDialog({
  open,
  onClose,
  imageUrl,
  title = '图片预览',
}: ImagePreviewDialogProps) {
  const [scale, setScale] = useState(1)

  const handleWheel = (e: React.WheelEvent) => {
    e.preventDefault()
    const delta = e.deltaY > 0 ? -0.1 : 0.1
    setScale(prev => Math.max(0.1, Math.min(5, prev + delta)))
  }

  const handleReset = () => setScale(1)
  const handleZoomIn = () => setScale(prev => Math.min(5, prev + 0.2))
  const handleZoomOut = () => setScale(prev => Math.max(0.1, prev - 0.2))

  return (
    <Dialog
      open={open}
      onClose={onClose}
      maxWidth="lg"
      fullWidth
      PaperProps={{
        sx: { bgcolor: 'rgba(0, 0, 0, 0.9)' },
      }}
    >
      <DialogTitle
        sx={{
          color: 'white',
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
        }}
      >
        <Typography>{title}</Typography>
        <Box sx={{ display: 'flex', gap: 1, alignItems: 'center' }}>
          <Typography variant="caption" sx={{ color: 'grey.400' }}>
            滚轮缩放 • {Math.round(scale * 100)}%
          </Typography>
          <IconButton size="small" onClick={onClose} sx={{ color: 'white' }}>
            <Close />
          </IconButton>
        </Box>
      </DialogTitle>
      <DialogContent
        sx={{
          bgcolor: 'transparent',
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          justifyContent: 'center',
        }}
      >
        <Box
          component="img"
          src={imageUrl || ''}
          alt="preview"
          onWheel={handleWheel}
          sx={{
            maxWidth: '100%',
            maxHeight: '70vh',
            objectFit: 'contain',
            transform: `scale(${scale})`,
            transition: 'transform 0.1s',
            cursor: 'zoom-in',
          }}
        />
        <Box sx={{ display: 'flex', gap: 2, mt: 2 }}>
          <Button
            size="small"
            variant="outlined"
            sx={{ color: 'white', borderColor: 'white' }}
            onClick={handleZoomOut}
            disabled={scale <= 0.1}
          >
            缩小
          </Button>
          <Button
            size="small"
            variant="outlined"
            sx={{ color: 'white', borderColor: 'white' }}
            onClick={handleReset}
          >
            重置
          </Button>
          <Button
            size="small"
            variant="outlined"
            sx={{ color: 'white', borderColor: 'white' }}
            onClick={handleZoomIn}
            disabled={scale >= 5}
          >
            放大
          </Button>
        </Box>
      </DialogContent>
    </Dialog>
  )
}
```

**使用方式**：

```tsx
// 在 ApprovalsPage.tsx 或 CertificateApprovalsPage.tsx 中
import { ImagePreviewDialog } from '@/components/common/ImagePreviewDialog'

// 状态
const [imagePreviewOpen, setImagePreviewOpen] = useState(false)
const [previewImageUrl, setPreviewImageUrl] = useState<string | null>(null)

// 渲染
<ImagePreviewDialog
  open={imagePreviewOpen}
  onClose={() => setImagePreviewOpen(false)}
  imageUrl={previewImageUrl}
  title="操作证预览"
/>

// 点击图片时
onClick={() => {
  setPreviewImageUrl(cert.file_url)
  setImagePreviewOpen(true)
}}
```
