import { useEffect } from 'react'
import { Box, Link, Typography } from '@mui/material'
import { PublicPageLayout } from '../../components/layout'
import { SITE_CONFIG } from '../../config/site'
import { usePageTitle } from '../../hooks/usePageTitle'

export function DocsRedirectPage() {
  usePageTitle()

  useEffect(() => {
    window.location.replace(SITE_CONFIG.DOCS_URL)
  }, [])

  return (
    <PublicPageLayout maxWidth="sm">
      <Box sx={{ border: '1px solid', borderColor: 'divider', p: { xs: 3, sm: 5 }, textAlign: 'center' }}>
        <Typography variant="h5" component="h1" gutterBottom>
          正在打开在线文档
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          如果页面没有自动跳转，请使用下面的链接。
        </Typography>
        <Link href={SITE_CONFIG.DOCS_URL} target="_blank" rel="noopener noreferrer" color="secondary.main" sx={{ overflowWrap: 'anywhere' }}>
          {SITE_CONFIG.DOCS_URL}
        </Link>
      </Box>
    </PublicPageLayout>
  )
}
