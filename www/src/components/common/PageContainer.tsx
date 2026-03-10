import { Box, type BoxProps } from '@mui/material'

interface PageContainerProps extends BoxProps {
  title?: string
  actions?: React.ReactNode
}

export function PageContainer({ title, actions, children, ...props }: PageContainerProps) {
  return (
    <Box {...props}>
      {title && (
        <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 3 }}>
          <Box>
            <Box component="h1" sx={{ fontSize: '1.5rem', fontWeight: 600, m: 0 }}>
              {title}
            </Box>
          </Box>
          {actions && <Box>{actions}</Box>}
        </Box>
      )}
      {children}
    </Box>
  )
}
