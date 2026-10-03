import { Box, Typography } from '@mui/material'

interface PageHeaderProps {
  title: string
  subtitle?: string
  actions?: React.ReactNode
  sx?: object
}

export function PageHeader({ title, subtitle, actions, sx }: PageHeaderProps) {
  return (
    <Box
      sx={{
        display: 'flex',
        justifyContent: 'space-between',
        alignItems: { xs: 'stretch', md: 'center' },
        flexDirection: { xs: 'column', md: 'row' },
        gap: { xs: 1.5, sm: 2 },
        mb: 3,
        ...sx,
      }}
    >
      <Box>
        <Typography variant="h5" component="h1">
          {title}
        </Typography>
        {subtitle && (
          <Typography variant="body2" color="text.secondary">
            {subtitle}
          </Typography>
        )}
      </Box>
      {actions && <Box sx={{ display: 'flex', gap: 1, alignItems: 'center', flexWrap: 'wrap', width: { xs: '100%', sm: 'auto' } }}>{actions}</Box>}
    </Box>
  )
}
