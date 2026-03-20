import { useState, useEffect } from 'react'
import {
  Box,
  Typography,
  Paper,
  Stack,
  Tabs,
  Tab,
  Collapse,
  useTheme,
  useMediaQuery,
  styled,
} from '@mui/material'
import SettingsEthernet from '@mui/icons-material/SettingsEthernet'
import Build from '@mui/icons-material/Build'
import CloudDownload from '@mui/icons-material/CloudDownload'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

// 导入图片
import deviceAccessFlowImg from '../../assets/device-access-flow.svg'
import authSequenceImg from '../../assets/auth-sequence.svg'
// 导入下载中心组件
import { DownloadCenterPage } from '../download/DownloadCenterPage'

// Markdown 样式容器
const MarkdownContent = styled(Box)(({ theme }) => ({
  lineHeight: 1.8,
  '& h3': {
    fontSize: '1.1rem',
    fontWeight: 600,
    marginTop: theme.spacing(1.5),
    marginBottom: theme.spacing(1),
    color: theme.palette.primary.main,
  },
  '& p': {
    marginBottom: theme.spacing(1.5),
  },
  '& ul, & ol': {
    paddingLeft: theme.spacing(3),
    marginBottom: theme.spacing(1.5),
  },
  '& li': {
    marginBottom: theme.spacing(0.5),
  },
  '& code': {
    fontFamily: 'monospace',
    backgroundColor: theme.palette.mode === 'dark' ? theme.palette.grey[800] : theme.palette.grey[100],
    padding: '2px 6px',
    borderRadius: 4,
    fontSize: '0.9em',
  },
  '& pre': {
    backgroundColor: theme.palette.grey[900],
    color: theme.palette.grey[100],
    padding: theme.spacing(2),
    borderRadius: theme.shape.borderRadius,
    overflow: 'auto',
    fontSize: '0.85rem',
    lineHeight: 1.6,
    marginBottom: theme.spacing(2),
    '& code': {
      backgroundColor: 'transparent',
      padding: 0,
    },
  },
  '& table': {
    width: '100%',
    borderCollapse: 'collapse',
    marginBottom: theme.spacing(2),
    fontSize: '0.9rem',
    '& th, & td': {
      border: `1px solid ${theme.palette.divider}`,
      padding: theme.spacing(1.5),
      textAlign: 'left',
    },
    '& th': {
      backgroundColor: theme.palette.mode === 'dark' ? theme.palette.grey[800] : theme.palette.grey[50],
      fontWeight: 600,
      whiteSpace: 'nowrap',
    },
  },
  '& blockquote': {
    borderLeft: `4px solid ${theme.palette.info.main}`,
    backgroundColor: theme.palette.mode === 'dark' ? 'rgba(25, 118, 210, 0.1)' : theme.palette.info.light,
    padding: theme.spacing(2),
    borderRadius: theme.shape.borderRadius,
    marginBottom: theme.spacing(2),
    '& p:last-child': {
      marginBottom: 0,
    },
  },
  '& img': {
    maxWidth: '100%',
    height: 'auto',
    display: 'block',
    margin: `${theme.spacing(2)} auto`,
    backgroundColor: theme.palette.mode === 'dark' ? theme.palette.grey[800] : theme.palette.grey[50],
    borderRadius: theme.shape.borderRadius,
    padding: theme.spacing(2),
  },
}))

// 解析 Markdown，按 ## 标题分割
function parseMarkdownSections(md: string) {
  const lines = md.split('\n')
  const sections: { title: string | null; content: string }[] = []
  let currentSection: { title: string | null; content: string[] } = { title: null, content: [] }

  for (const line of lines) {
    if (line.startsWith('## ')) {
      if (currentSection.content.length > 0 || currentSection.title !== null) {
        sections.push({
          title: currentSection.title,
          content: currentSection.content.join('\n').trim(),
        })
      }
      currentSection = {
        title: line.slice(3).trim(),
        content: [],
      }
    } else if (line.startsWith('# ')) {
      // h1 标题，作为第一个无折叠的部分
      if (currentSection.content.length > 0 || currentSection.title !== null) {
        sections.push({
          title: currentSection.title,
          content: currentSection.content.join('\n').trim(),
        })
      }
      currentSection = {
        title: null, // null 表示不折叠
        content: [line],
      }
    } else {
      currentSection.content.push(line)
    }
  }

  // 添加最后一个 section
  if (currentSection.content.length > 0 || currentSection.title !== null) {
    sections.push({
      title: currentSection.title,
      content: currentSection.content.join('\n').trim(),
    })
  }

  return sections
}

// 图片映射
const imageMap: Record<string, string> = {
  '../../assets/device-access-flow.svg': deviceAccessFlowImg,
  '../../assets/auth-sequence.svg': authSequenceImg,
}

// 文档分区类型
interface DocSection {
  id: string
  label: string
  icon: React.ReactNode
  content: React.ReactNode
}

// 折叠卡片组件
function CollapsibleSection({
  title,
  defaultOpen = false,
  children,
}: {
  title: string
  defaultOpen?: boolean
  children: React.ReactNode
}) {
  const [open, setOpen] = useState(defaultOpen)

  return (
    <Paper variant="outlined" sx={{ mb: 2, overflow: 'hidden' }}>
      <Box
        sx={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          p: 2,
          cursor: 'pointer',
          '&:hover': { bgcolor: 'action.hover' },
        }}
        onClick={() => setOpen(!open)}
      >
        <Typography variant="h6" fontWeight={600}>
          {title}
        </Typography>
        <Box
          component="span"
          sx={{
            transform: open ? 'rotate(180deg)' : 'rotate(0deg)',
            transition: 'transform 0.2s',
            fontSize: '0.8em',
            color: 'text.secondary',
          }}
        >
          ▼
        </Box>
      </Box>
      <Collapse in={open}>
        <Box sx={{ p: 2, borderTop: 1, borderColor: 'divider' }}>{children}</Box>
      </Collapse>
    </Paper>
  )
}

// Markdown 渲染组件
function MarkdownRenderer({ content }: { content: string }) {
  return (
    <MarkdownContent>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          img: ({ src, alt }) => {
            const mappedSrc = src ? imageMap[src] || src : src
            return (
              <Box
                component="img"
                src={mappedSrc}
                alt={alt || ''}
                sx={{
                  maxWidth: '100%',
                  height: 'auto',
                  display: 'block',
                  margin: '16px auto',
                  bgcolor: 'grey.50',
                  borderRadius: 1,
                  p: 2,
                }}
              />
            )
          },
        }}
      >
        {content}
      </ReactMarkdown>
    </MarkdownContent>
  )
}

// 设备接入指南内容
function DeviceProtocolContent() {
  const [mdContent, setMdContent] = useState<string | null>(null)
  const [error, setError] = useState(false)

  useEffect(() => {
    fetch('/docs/device-protocol.md')
      .then((res) => {
        if (!res.ok) throw new Error('Failed to load')
        return res.text()
      })
      .then(setMdContent)
      .catch(() => setError(true))
  }, [])

  if (error) {
    return (
      <Paper variant="outlined" sx={{ p: 3, textAlign: 'center' }}>
        <Typography color="error">文档加载失败，请刷新页面重试</Typography>
      </Paper>
    )
  }

  if (!mdContent) {
    return (
      <Paper variant="outlined" sx={{ p: 3, textAlign: 'center' }}>
        <Typography color="text.secondary">加载中...</Typography>
      </Paper>
    )
  }

  const sections = parseMarkdownSections(mdContent)

  return (
    <Box>
      {sections.map((section, index) => {
        // 没有标题的部分（h1 开头），不折叠
        if (section.title === null) {
          return <MarkdownRenderer key={index} content={section.content} />
        }

        // 有标题的部分，使用折叠卡片
        return (
          <CollapsibleSection key={index} title={section.title} defaultOpen={index === 1}>
            <MarkdownRenderer content={section.content} />
          </CollapsibleSection>
        )
      })}
    </Box>
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
      id: 'dev-guide',
      label: '开发指南',
      icon: <Build fontSize="small" />,
      content: <DevGuideContent />,
    },
    {
      id: 'download-center',
      label: '下载中心',
      icon: <CloudDownload fontSize="small" />,
      content: <DownloadCenterPage />,
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
