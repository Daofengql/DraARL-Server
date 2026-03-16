import { useState } from 'react'
import {
  Box,
  Typography,
  Paper,
  Stack,
  Collapse,
  IconButton,
  Chip,
  Divider,
  Tabs,
  Tab,
  useTheme,
  useMediaQuery,
} from '@mui/material'
import {
  ExpandMore,
  ExpandLess,
  SettingsEthernet,
  AccountTree,
  Security,
  Code,
  CloudSync,
  MenuBook,
  Build,
  CloudDownload,
} from '@mui/icons-material'

// 导入 SVG 图像
import deviceAccessFlow from '../../assets/device-access-flow.svg'
import authSequence from '../../assets/auth-sequence.svg'

// 文档分区类型
interface DocSection {
  id: string
  label: string
  icon: React.ReactNode
  content: React.ReactNode
}

// 折叠面板组件
function CollapsibleSection({
  title,
  icon,
  children,
  defaultExpanded = false,
}: {
  title: string
  icon: React.ReactNode
  children: React.ReactNode
  defaultExpanded?: boolean
}) {
  const [expanded, setExpanded] = useState(defaultExpanded)

  return (
    <Paper variant="outlined" sx={{ mb: 2 }}>
      <Box
        sx={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          p: 2,
          cursor: 'pointer',
          '&:hover': { bgcolor: 'action.hover' },
        }}
        onClick={() => setExpanded(!expanded)}
      >
        <Stack direction="row" alignItems="center" spacing={1.5}>
          {icon}
          <Typography variant="h6" fontWeight={600}>
            {title}
          </Typography>
        </Stack>
        <IconButton size="small">
          {expanded ? <ExpandLess /> : <ExpandMore />}
        </IconButton>
      </Box>
      <Collapse in={expanded}>
        <Divider />
        <Box sx={{ p: 2 }}>{children}</Box>
      </Collapse>
    </Paper>
  )
}

// 代码块组件
function CodeBlock({ children, title }: { children: string; title?: string }) {
  return (
    <Box sx={{ position: 'relative' }}>
      {title && (
        <Typography
          variant="caption"
          sx={{
            position: 'absolute',
            top: 8,
            right: 12,
            color: 'text.secondary',
            bgcolor: 'grey.100',
            px: 1,
            borderRadius: 0.5,
          }}
        >
          {title}
        </Typography>
      )}
      <Box
        component="pre"
        sx={{
          bgcolor: 'grey.900',
          color: 'grey.100',
          p: 2,
          borderRadius: 1,
          overflow: 'auto',
          fontSize: '0.85rem',
          fontFamily: 'monospace',
          lineHeight: 1.6,
          '&::-webkit-scrollbar': {
            height: 6,
          },
          '&::-webkit-scrollbar-thumb': {
            bgcolor: 'grey.700',
            borderRadius: 3,
          },
        }}
      >
        {children}
      </Box>
    </Box>
  )
}

// 表格组件
function InfoTable({
  headers,
  rows,
}: {
  headers: string[]
  rows: (string | React.ReactNode)[][]
}) {
  return (
    <Box
      component="table"
      sx={{
        width: '100%',
        borderCollapse: 'collapse',
        fontSize: '0.9rem',
        '& th, & td': {
          border: '1px solid',
          borderColor: 'divider',
          p: 1.5,
          textAlign: 'left',
        },
        '& th': {
          bgcolor: 'grey.50',
          fontWeight: 600,
          whiteSpace: 'nowrap',
        },
        '& td': {
          verticalAlign: 'top',
        },
      }}
    >
      <thead>
        <tr>
          {headers.map((h, i) => (
            <th key={i}>{h}</th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map((row, i) => (
          <tr key={i}>
            {row.map((cell, j) => (
              <td key={j}>{cell}</td>
            ))}
          </tr>
        ))}
      </tbody>
    </Box>
  )
}

// 提示框组件
function AlertBox({
  type,
  children,
}: {
  type: 'info' | 'warning' | 'error'
  children: React.ReactNode
}) {
  const colors = {
    info: { bg: 'info.50', border: 'info.main' },
    warning: { bg: 'warning.50', border: 'warning.main' },
    error: { bg: 'error.50', border: 'error.main' },
  }
  const config = colors[type]

  return (
    <Box
      sx={{
        bgcolor: config.bg,
        borderLeft: 4,
        borderColor: config.border,
        p: 2,
        borderRadius: 1,
      }}
    >
      {children}
    </Box>
  )
}

// 设备接入指南内容
function DeviceProtocolContent() {
  return (
    <Stack spacing={2}>
      {/* 快速开始 */}
      <Paper
        variant="outlined"
        sx={{
          background: 'linear-gradient(135deg, #667eea 0%, #764ba2 100%)',
          color: 'white',
          border: 'none',
        }}
      >
        <Stack spacing={1.5} sx={{ p: 2 }}>
          <Typography variant="subtitle1" fontWeight={600}>
            快速开始
          </Typography>
          <Typography variant="body2" sx={{ opacity: 0.9 }}>
            本文档介绍如何将设备接入 DraARL 平台。接入前请确保您已完成以下准备工作：
          </Typography>
          <Stack direction="row" spacing={1} sx={{ flexWrap: 'wrap' }}>
            <Chip
              size="small"
              label="已完成注册并审核通过"
              sx={{ bgcolor: 'rgba(255,255,255,0.2)', color: 'white' }}
            />
            <Chip
              size="small"
              label="已获取设备密码"
              sx={{ bgcolor: 'rgba(255,255,255,0.2)', color: 'white' }}
            />
            <Chip
              size="small"
              label="拥有有效的业余电台呼号"
              sx={{ bgcolor: 'rgba(255,255,255,0.2)', color: 'white' }}
            />
          </Stack>
        </Stack>
      </Paper>

      {/* 设备接入流程 */}
      <CollapsibleSection title="设备接入流程" icon={<AccountTree color="primary" />}>
        <Stack spacing={3}>
          {/* 流程图 SVG */}
          <Box
            sx={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              py: 2,
              px: 2,
              bgcolor: 'grey.50',
              borderRadius: 2,
            }}
          >
            <Box
              component="img"
              src={deviceAccessFlow}
              alt="设备接入流程"
              sx={{ maxWidth: '100%', height: 'auto' }}
            />
          </Box>

          <Divider />

          {/* 详细步骤说明 */}
          <Stack spacing={2}>
            <Typography variant="subtitle1" fontWeight={600}>
              详细步骤
            </Typography>

            <Paper variant="outlined" sx={{ p: 2 }}>
              <Typography variant="subtitle2" color="primary" gutterBottom>
                步骤 1-2：注册与审核
              </Typography>
              <Typography variant="body2" color="text.secondary">
                在平台完成注册，填写真实的业余电台呼号。管理员审核通过后，您的账号状态将变为"已激活"。
              </Typography>
            </Paper>

            <Paper variant="outlined" sx={{ p: 2 }}>
              <Typography variant="subtitle2" color="primary" gutterBottom>
                步骤 3：获取设备密码
              </Typography>
              <Typography variant="body2" color="text.secondary">
                登录后进入「个人中心」页面，您可以查看设备密码（脱敏显示）。如需查看完整密码，可点击显示按钮。
                您也可以自定义修改设备密码，长度为 6-10 位字母和数字组合。
              </Typography>
            </Paper>

            <Paper variant="outlined" sx={{ p: 2 }}>
              <Typography variant="subtitle2" color="primary" gutterBottom>
                步骤 4：配置设备
              </Typography>
              <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
                在您的设备客户端中填入以下认证信息：
              </Typography>
              <Box component="ul" sx={{ m: 0, pl: 3 }}>
                <li>
                  <Typography variant="body2">
                    <strong>用户名</strong>：您的平台用户名
                  </Typography>
                </li>
                <li>
                  <Typography variant="body2">
                    <strong>设备密码</strong>：个人中心获取的设备准入密码
                  </Typography>
                </li>
                <li>
                  <Typography variant="body2">
                    <strong>服务器地址</strong>：平台提供的 UDP 服务器地址和端口
                  </Typography>
                </li>
              </Box>
            </Paper>

            <Paper variant="outlined" sx={{ p: 2 }}>
              <Typography variant="subtitle2" color="primary" gutterBottom>
                步骤 5：开始通信
              </Typography>
              <Typography variant="body2" color="text.secondary">
                设备连接成功后，会自动进行认证。认证通过后，您的呼号将由服务器自动填充。
                之后即可在同一群组内与其他设备进行语音和文本通信。
              </Typography>
            </Paper>
          </Stack>
        </Stack>
      </CollapsibleSection>

      {/* 协议概述 */}
      <CollapsibleSection title="协议概述" icon={<SettingsEthernet color="primary" />}>
        <Stack spacing={2}>
          <Typography variant="body1">
            <strong>DraARLv1</strong> (Dra Amateur Radio Link v1，麟链v1)
            是 DraARL 平台的设备通信协议，用于设备与服务器之间的实时通信。
          </Typography>

          <Typography variant="body2" color="text.secondary">
            该协议专为业余无线电通信场景设计，采用 UDP 作为传输层协议，
            具有低延迟、高效率的特点。协议支持语音通联、文本消息、位置共享等功能，
            适用于各种客户端设备（移动端、桌面端、浏览器等）。
          </Typography>

          <Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap' }}>
            <Chip label="UDP 传输" size="small" />
            <Chip label="大端序" size="small" />
            <Chip label="90字节固定头部" size="small" />
            <Chip label="变长负载" size="small" />
            <Chip label="Opus 16K 语音编码" size="small" />
          </Box>

          <Paper variant="outlined" sx={{ p: 2, bgcolor: 'grey.50' }}>
            <Typography variant="subtitle2" gutterBottom fontWeight={600}>
              协议特点
            </Typography>
            <Box component="ul" sx={{ m: 0, pl: 2 }}>
              <li>
                <Typography variant="body2">
                  <strong>轻量高效</strong>：固定 90 字节头部，解析快速，适合嵌入式设备
                </Typography>
              </li>
              <li>
                <Typography variant="body2">
                  <strong>安全认证</strong>：设备密码 + 阶梯封禁机制，防止暴力破解
                </Typography>
              </li>
              <li>
                <Typography variant="body2">
                  <strong>语音优化</strong>：统一采用 Opus 16kHz 编码，音质与带宽平衡
                </Typography>
              </li>
              <li>
                <Typography variant="body2">
                  <strong>群组通信</strong>：支持多群组切换，同一群组内设备可互相通信
                </Typography>
              </li>
              <li>
                <Typography variant="body2">
                  <strong>服务器互联</strong>：支持多服务器部署，跨服务器语音转发
                </Typography>
              </li>
            </Box>
          </Paper>

          <AlertBox type="info">
            <Typography variant="body2">
              本协议为业余无线电通信设计，请遵守当地无线电管理法规。
            </Typography>
          </AlertBox>
        </Stack>
      </CollapsibleSection>

      {/* 报文结构 */}
      <CollapsibleSection title="报文结构" icon={<Code color="primary" />}>
        <Stack spacing={2}>
          <Typography variant="body1">
            协议报文由固定头部（90字节）和变长数据区组成。所有多字节字段使用大端序（Big-Endian）。
          </Typography>

          <CodeBlock title="报文布局">
{`+--------+--------+--------+--------+--------+--------+--------+--------+
| 0-3    | 4-5    | 6-37   | 38-47  | 48     | 49     | 50     | 51-53  |
| Ver    | Length |Username|DevPass | Type   |DevModel| SSID   | DMRID  |
| 4B     | 2B     | 32B    | 10B    | 1B     | 1B     | 1B     | 3B     |
+--------+--------+--------+--------+--------+--------+--------+--------+
| 54-85  | 86-89  | 90+                                              |
|CallSign| Rsv    | DATA                                             |
| 32B    | 4B     | 变长                                             |
+--------+--------+--------+-----------------------------------------+

固定头部：90 字节
最小报文：90 字节`}
          </CodeBlock>

          <Typography variant="subtitle1" fontWeight={600} sx={{ mt: 2 }}>
            字段说明
          </Typography>
          <Box sx={{ overflowX: 'auto' }}>
            <InfoTable
              headers={['偏移', '长度', '字段名', '类型', '说明']}
              rows={[
                ['0', '4B', 'Version', 'string', '协议版本标识，固定为 "DraA"'],
                ['4', '2B', 'Length', 'uint16 BE', '报文总长度（包含头部和数据）'],
                ['6', '32B', 'Username', 'string', '用户名，UTF-8 编码，\\0 填充'],
                ['38', '10B', 'DevicePassword', 'string', '设备准入密码，ASCII 字母数字'],
                ['48', '1B', 'Type', 'byte', '数据包类型'],
                ['49', '1B', 'DevModel', 'byte', '设备型号'],
                ['50', '1B', 'SSID', 'byte', '设备子号 (0-255)'],
                ['51', '3B', 'DMRID', 'uint24 BE', 'DMR ID'],
                ['54', '32B', 'CallSign', 'string', '业余电台呼号，服务器填充'],
                ['86', '4B', 'Reserved', '-', '保留字段，填 0'],
                ['90', '变长', 'DATA', '[]byte', '负载数据'],
              ]}
            />
          </Box>
        </Stack>
      </CollapsibleSection>

      {/* 数据包类型 */}
      <CollapsibleSection title="数据包类型" icon={<CloudSync color="primary" />}>
        <Stack spacing={2}>
          <Typography variant="body1">
            协议定义了以下数据包类型，通过 Type 字段区分：
          </Typography>
          <Box sx={{ overflowX: 'auto' }}>
            <InfoTable
              headers={['值', '常量名', '说明', 'DATA 内容']}
              rows={[
                ['0', 'TypeControl', '控制指令', '控制命令数据'],
                ['1', '-', '保留', '-'],
                ['2', 'TypeHeartbeat', '心跳包', '可选携带 GPS 位置信息'],
                ['3', 'TypeConfig', '设备配置', '配置参数或查询响应'],
                ['4', 'TypeTextMessage', '文本消息', 'UTF-8 编码文本'],
                ['5', 'TypeOpus16K', 'Opus 16K 语音', 'Opus 16kHz 编码语音帧'],
                ['6', 'TypeServerVoice', '服务器互联语音', '服务器间转发的语音数据'],
                ['7', 'TypeATPassThrough', 'AT 透传', 'AT 命令透传'],
              ]}
            />
          </Box>
        </Stack>
      </CollapsibleSection>

      {/* 设备型号 */}
      <CollapsibleSection title="设备型号" icon={<SettingsEthernet color="primary" />}>
        <Stack spacing={2}>
          <Typography variant="body1">
            DevModel 字段用于标识设备类型，服务器可根据设备型号进行不同的处理策略：
          </Typography>
          <Box sx={{ overflowX: 'auto' }}>
            <InfoTable
              headers={['值', '常量名', '说明']}
              rows={[
                ['0', 'DevModelUnknown', '未知设备'],
                ['100', 'DevModelWeChatMini', '微信小程序'],
                ['101', 'DevModelAndroid', 'Android 客户端'],
                ['102', 'DevModelIOS', 'iOS 客户端'],
                ['103', 'DevModelWindows', 'Windows 客户端'],
                ['105', 'DevModelBrowser', '浏览器客户端'],
                ['106', 'DevModelInterconnect', '互联设备'],
              ]}
            />
          </Box>
        </Stack>
      </CollapsibleSection>

      {/* 认证流程 */}
      <CollapsibleSection title="设备认证流程" icon={<Security color="primary" />}>
        <Stack spacing={2}>
          <Typography variant="body1">
            设备通过发送心跳包（Type=2）进行认证。认证成功后，服务器会在响应中填充用户的呼号。
          </Typography>

          {/* 认证时序图 SVG */}
          <Box
            sx={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              py: 2,
              px: 2,
              bgcolor: 'grey.50',
              borderRadius: 2,
            }}
          >
            <Box
              component="img"
              src={authSequence}
              alt="认证流程时序图"
              sx={{ maxWidth: '100%', height: 'auto' }}
            />
          </Box>

          <AlertBox type="warning">
            <Typography variant="subtitle2" gutterBottom>
              安全机制
            </Typography>
            <Typography variant="body2">
              为防止暴力破解，平台采用阶梯封禁机制：
            </Typography>
            <Box component="ul" sx={{ m: 0, pl: 2, mt: 1 }}>
              <li>连续失败 3 次：封禁 10 秒</li>
              <li>再次失败：封禁 30 秒</li>
              <li>继续失败：封禁 60 秒、300 秒（递增）</li>
            </Box>
            <Typography variant="body2" sx={{ mt: 1 }}>
              封禁 Key：IP + Username
            </Typography>
          </AlertBox>
        </Stack>
      </CollapsibleSection>

      {/* 字符编码规则 */}
      <CollapsibleSection title="字符编码规则" icon={<Code color="primary" />}>
        <Stack spacing={2}>
          <Typography variant="body1">
            各字段使用不同的编码规则，请确保设备端正确处理：
          </Typography>
          <Box sx={{ overflowX: 'auto' }}>
            <InfoTable
              headers={['字段', '编码', '字符集', '说明']}
              rows={[
                ['Username', 'UTF-8', '字母、数字、下划线', '用户名'],
                ['DevicePassword', 'ASCII', '大小写字母、数字', '设备准入密码，6-10 位'],
                ['CallSign', 'ASCII', '大写字母、数字', '业余电台呼号'],
                ['TextMessage', 'UTF-8', '任意 Unicode', '文本消息'],
              ]}
            />
          </Box>
        </Stack>
      </CollapsibleSection>

      {/* 常见问题 */}
      <CollapsibleSection title="常见问题" icon={<MenuBook color="primary" />}>
        <Stack spacing={2}>
          <Paper variant="outlined" sx={{ p: 2 }}>
            <Typography variant="subtitle2" color="primary" gutterBottom>
              Q: 设备显示认证失败怎么办？
            </Typography>
            <Typography variant="body2" color="text.secondary">
              请检查：1) 用户名是否正确；2) 设备密码是否正确（注意大小写）；3) 账号是否已审核通过；
              4) 是否因多次失败被封禁（等待封禁时间过期）。
            </Typography>
          </Paper>

          <Paper variant="outlined" sx={{ p: 2 }}>
            <Typography variant="subtitle2" color="primary" gutterBottom>
              Q: 设备多久没有数据会被判定离线？
            </Typography>
            <Typography variant="body2" color="text.secondary">
              服务器检测到设备 20 秒内无任何数据包（包括心跳和语音数据），将判定设备离线。
              建议设备端每 10-15 秒发送一次心跳包保持在线状态。
            </Typography>
          </Paper>

          <Paper variant="outlined" sx={{ p: 2 }}>
            <Typography variant="subtitle2" color="primary" gutterBottom>
              Q: 支持哪些语音编码格式？
            </Typography>
            <Typography variant="body2" color="text.secondary">
              平台统一使用 Opus 16kHz 编码格式。这是目前业余无线电网络中音质与带宽平衡的最佳选择。
            </Typography>
          </Paper>

          <Paper variant="outlined" sx={{ p: 2 }}>
            <Typography variant="subtitle2" color="primary" gutterBottom>
              Q: 如何切换通信群组？
            </Typography>
            <Typography variant="body2" color="text.secondary">
              登录平台后，在「设备管理」页面选择要操作的设备，点击「切换群组」按钮，
              选择目标群组后保存即可。设备会在下次心跳时获取新的群组配置。
            </Typography>
          </Paper>
        </Stack>
      </CollapsibleSection>

      {/* 版本历史 */}
      <CollapsibleSection title="协议版本历史" icon={<Code color="primary" />}>
        <Stack spacing={2}>
          <Box sx={{ overflowX: 'auto' }}>
            <InfoTable
              headers={['版本', '日期', '说明']}
              rows={[
                ['v1.0', '2026-03', '初始版本，替代 NRL2 协议'],
                ['v1.1', '2026-03', '移除 G.711 编解码支持，统一使用 Opus 16K 格式'],
                ['v1.2', '2026-03', '简化协议头，移除 Status 和 SeqNum 字段，头部从 93 字节简化为 90 字节'],
              ]}
            />
          </Box>
        </Stack>
      </CollapsibleSection>
    </Stack>
  )
}

// 开发指南内容（占位）
function DevGuideContent() {
  return (
    <Stack spacing={3}>
      <Paper variant="outlined" sx={{ p: 3, textAlign: 'center' }}>
        <Build sx={{ fontSize: 48, color: 'text.secondary', mb: 2 }} />
        <Typography variant="h6" color="text.secondary" gutterBottom>
          开发指南
        </Typography>
        <Typography variant="body2" color="text.secondary">
          内容建设中，敬请期待...
        </Typography>
      </Paper>
    </Stack>
  )
}

// 下载中心内容
function DownloadCenterContent() {
  return (
    <Stack spacing={3}>
      <Paper variant="outlined" sx={{ p: 3, textAlign: 'center' }}>
        <CloudDownload sx={{ fontSize: 48, color: 'text.secondary', mb: 2 }} />
        <Typography variant="h6" color="text.secondary" gutterBottom>
          下载中心
        </Typography>
        <Typography variant="body2" color="text.secondary">
          功能开发中，敬请期待...
        </Typography>
      </Paper>
    </Stack>
  )
}

// 主页面组件
export function DocsPage() {
  const theme = useTheme()
  const isMobile = useMediaQuery(theme.breakpoints.down('sm'))
  const [currentSection, setCurrentSection] = useState(0)

  // 文档分区配置
  const sections: DocSection[] = [
    {
      id: 'device-protocol',
      label: '设备接入指南',
      icon: <SettingsEthernet fontSize="small" />,
      content: <DeviceProtocolContent />,
    },
    {
      id: 'download-center',
      label: '下载中心',
      icon: <CloudDownload fontSize="small" />,
      content: <DownloadCenterContent />,
    },
    {
      id: 'dev-guide',
      label: '开发指南',
      icon: <Build fontSize="small" />,
      content: <DevGuideContent />,
    },
  ]

  return (
    <Stack spacing={3}>
      {/* 页面标题 */}
      <Box>
        <Typography variant="h4" fontWeight={600} gutterBottom>
          技术文档
        </Typography>
        <Typography variant="body2" color="text.secondary">
          DraARL 平台技术文档中心
        </Typography>
      </Box>

      {/* 文档分区导航 */}
      <Paper variant="outlined" sx={{ mb: 2 }}>
        <Tabs
          value={currentSection}
          onChange={(_, newValue) => setCurrentSection(newValue)}
          variant={isMobile ? 'scrollable' : 'standard'}
          scrollButtons={isMobile ? 'auto' : false}
          sx={{
            borderBottom: 1,
            borderColor: 'divider',
            '& .MuiTab-root': {
              minHeight: 56,
            },
          }}
        >
          {sections.map((section, index) => (
            <Tab
              key={section.id}
              label={
                <Stack direction="row" alignItems="center" spacing={1}>
                  {section.icon}
                  <span>{section.label}</span>
                </Stack>
              }
              id={`doc-tab-${index}`}
              aria-controls={`doc-tabpanel-${index}`}
            />
          ))}
        </Tabs>
      </Paper>

      {/* 文档内容 */}
      <Box
        role="tabpanel"
        id={`doc-tabpanel-${currentSection}`}
        aria-labelledby={`doc-tab-${currentSection}`}
      >
        {sections[currentSection].content}
      </Box>
    </Stack>
  )
}
