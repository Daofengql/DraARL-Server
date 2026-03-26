import { Typography, Paper, Box } from '@mui/material'
import { PublicPageLayout } from '../../components/layout'

export function AboutPage() {
  return (
    <PublicPageLayout maxWidth="md">
      <Paper elevation={3} sx={{ p: 4 }}>
        <Box sx={{ textAlign: 'center', py: 8 }}>
          <Typography variant="h4" gutterBottom>
            关于我们
          </Typography>
          <Typography variant="body1" color="text.secondary">
            关于页面内容即将上线，敬请期待...
          </Typography>
        </Box>
      </Paper>
    </PublicPageLayout>
  )
}
