import { Box } from '@mui/material'

interface TabPanelProps {
  children?: React.ReactNode
  value: number
  index: number
  py?: number
}

export function TabPanel({ children, value, index, py = 3 }: TabPanelProps) {
  return (
    <div role="tabpanel" hidden={value !== index}>
      {value === index && <Box sx={{ py }}>{children}</Box>}
    </div>
  )
}
