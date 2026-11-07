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
        pb: 2.5,
        borderBottom: '1px solid',
        borderColor: 'divider',
        ...sx,
      }}
    >
      <Box sx={{ minWidth: 0, pl: 2, borderLeft: '2px solid', borderColor: 'secondary.main' }}>
        <Typography variant="h5" component="h1" sx={{ overflowWrap: 'anywhere' }}>
          {title}
        </Typography>
        {subtitle && (
          <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
            {subtitle}
          </Typography>
        )}
      </Box>
      {actions && <Box sx={{ display: 'flex', gap: 1, alignItems: 'center', flexWrap: 'wrap', width: { xs: '100%', sm: 'auto' } }}>{actions}</Box>}
    </Box>
  )
}
