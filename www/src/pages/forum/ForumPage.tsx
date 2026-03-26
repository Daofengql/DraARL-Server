import { Typography, Paper, Box } from '@mui/material'
import { PublicPageLayout } from '../../components/layout'

export function ForumPage() {
  return (
    <PublicPageLayout maxWidth="md">
      <Paper elevation={3} sx={{ p: 4 }}>
        <Box sx={{ textAlign: 'center', py: 8 }}>
          <Typography variant="h4" gutterBottom>
            社区论坛
          </Typography>
          <Typography variant="body1" color="text.secondary">
            论坛功能即将上线，敬请期待...
          </Typography>
        </Box>
      </Paper>
    </PublicPageLayout>
  )
}
