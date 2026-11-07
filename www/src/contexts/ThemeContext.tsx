import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { ThemeProvider, type PaletteMode } from '@mui/material/styles'
import CssBaseline from '@mui/material/CssBaseline'
import { createBlueprintTheme } from '../theme/blueprint'

const storageKey = 'draarl-color-mode'
const ColorModeContext = createContext<{ mode: PaletteMode; toggleMode: () => void } | null>(null)

function readPreference(): PaletteMode | null {
  try {
    const value = localStorage.getItem(storageKey)
    return value === 'light' || value === 'dark' ? value : null
  } catch {
    return null
  }
}

export function AppThemeProvider({ children }: { children: ReactNode }) {
  const [preference, setPreference] = useState(readPreference)
  useEffect(() => {
    const handleStorage = (event: StorageEvent) => {
      if (event.key === storageKey || event.key === null) setPreference(readPreference())
    }
    window.addEventListener('storage', handleStorage)
    return () => {
      window.removeEventListener('storage', handleStorage)
    }
  }, [])
  const mode = preference ?? 'dark'
  const theme = useMemo(() => createBlueprintTheme(mode), [mode])
  const value = useMemo(() => ({ mode, toggleMode: () => {
    const next = mode === 'light' ? 'dark' : 'light'
    setPreference(next)
    try { localStorage.setItem(storageKey, next) } catch { /* The current tab still switches when storage is unavailable. */ }
  } }), [mode])
  return <ColorModeContext.Provider value={value}><ThemeProvider theme={theme}><CssBaseline />{children}</ThemeProvider></ColorModeContext.Provider>
}

export function useColorMode() {
  const context = useContext(ColorModeContext)
  if (!context) throw new Error('useColorMode requires AppThemeProvider')
  return context
}
