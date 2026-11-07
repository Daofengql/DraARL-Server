import { Box, Card, CardContent, Stack, Typography } from '@mui/material'
import type { ReactNode } from 'react'

interface BlueprintStatCardProps {
  title: string
  value: number | string
  icon: ReactNode
  color: 'primary' | 'success' | 'info' | 'warning'
}

export function BlueprintStatCard({ title, value, icon, color }: BlueprintStatCardProps) {
  return (
    <Card sx={{ height: '100%' }}>
      <CardContent sx={{ height: '100%' }}>
        <Stack direction="row" justifyContent="space-between" alignItems="center" spacing={2} sx={{ mb: 2 }}>
          <Typography variant="body2" color="text.secondary">{title}</Typography>
          <Box sx={{ color: color === 'success' ? 'success.main' : 'secondary.main', display: 'flex', '& svg': { fontSize: 20 } }}>{icon}</Box>
        </Stack>
        <Typography component="p" variant="h4" sx={{
          lineHeight: 1.35, minHeight: 43, overflowWrap: 'anywhere',
          fontSize: typeof value === 'string' && value.length > 8 ? '1.375rem' : '2rem',
        }}>
          {typeof value === 'number' ? value.toLocaleString() : value}
        </Typography>
      </CardContent>
    </Card>
  )
}
