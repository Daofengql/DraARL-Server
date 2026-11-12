import { useTheme } from '@mui/material/styles'
import { useConfig } from '../contexts/ConfigContext'
import { selectSiteLogo } from '../utils/siteBranding'

export function useSiteLogo() {
  const { config } = useConfig()
  const theme = useTheme()
  return selectSiteLogo(config.systemInfo, theme.palette.mode)
}
