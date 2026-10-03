import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { alpha, createTheme, ThemeProvider, type PaletteMode } from '@mui/material/styles'
import CssBaseline from '@mui/material/CssBaseline'

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

function createAppTheme(mode: PaletteMode) {
  const dark = mode === 'dark'
  return createTheme({
    palette: {
      mode,
      primary: { main: dark ? '#60a5fa' : '#2563eb', light: '#93c5fd', dark: '#1d4ed8', contrastText: dark ? '#0b1220' : '#ffffff' },
      secondary: { main: dark ? '#a1aab9' : '#6b7280', light: '#9ca3af', dark: '#4b5563' },
      error: { main: dark ? '#f87171' : '#dc2626' },
      warning: { main: dark ? '#fbbf24' : '#ca8a04' },
      success: { main: dark ? '#4ade80' : '#16a34a' },
      info: { main: dark ? '#38bdf8' : '#0284c7' },
      background: { default: dark ? '#0f172a' : '#f9fafb', paper: dark ? '#1e293b' : '#ffffff' },
      text: { primary: dark ? '#e5edf7' : '#1f2937', secondary: dark ? '#a6b4c7' : '#6b7280' },
      divider: dark ? '#334155' : '#e5e7eb',
    },
    typography: {
      fontFamily: ['Inter', '-apple-system', 'BlinkMacSystemFont', '"Segoe UI"', 'Roboto', '"Helvetica Neue"', 'Arial', 'sans-serif', '"Apple Color Emoji"', '"Segoe UI Emoji"', '"Segoe UI Symbol"'].join(','),
      h4: { fontWeight: 600 }, h5: { fontWeight: 600 }, h6: { fontWeight: 600 },
      button: { textTransform: 'none', fontWeight: 500 },
    },
    shape: { borderRadius: 8 },
    components: {
      MuiCssBaseline: { styleOverrides: { html: { colorScheme: mode } } },
      MuiButton: { styleOverrides: {
        root: { boxShadow: 'none' },
        contained: { boxShadow: '0 2px 4px 0 rgba(0, 0, 0, 0.1)', '&:hover': { boxShadow: '0 4px 8px 0 rgba(0, 0, 0, 0.12)' } },
      } },
      MuiCard: { styleOverrides: { root: ({ theme }) => ({
        boxShadow: '0 2px 8px 0 rgba(0, 0, 0, 0.08), 0 1px 3px 0 rgba(0, 0, 0, 0.04)',
        border: `1px solid ${theme.palette.divider}`,
      }) } },
      MuiTextField: { defaultProps: { variant: 'outlined', size: 'small' } },
      MuiSelect: { defaultProps: { variant: 'outlined', size: 'small' } },
      MuiIconButton: { styleOverrides: { root: ({ theme }) => ({ '&:hover': { backgroundColor: theme.palette.action.hover } }) } },
      MuiDrawer: { styleOverrides: { paper: ({ theme }) => ({ borderRight: `1px solid ${theme.palette.divider}`, boxShadow: 'none' }) } },
      MuiAppBar: { styleOverrides: { root: ({ theme }) => ({
        boxShadow: '0 1px 3px 0 rgba(0, 0, 0, 0.08)', backgroundColor: theme.palette.background.paper, color: theme.palette.text.primary,
        backgroundImage: 'none',
      }) } },
      MuiLink: { styleOverrides: { root: ({ theme }) => ({ color: theme.palette.primary.main }) } },
      MuiTableHead: { styleOverrides: { root: ({ theme }) => ({ backgroundColor: alpha(theme.palette.text.primary, 0.04) }) } },
    },
  })
}

export function AppThemeProvider({ children }: { children: ReactNode }) {
  const [preference, setPreference] = useState(readPreference)
  const [systemDark, setSystemDark] = useState(() => window.matchMedia('(prefers-color-scheme: dark)').matches)
  useEffect(() => {
    const query = window.matchMedia('(prefers-color-scheme: dark)')
    const handleSystemChange = (event: MediaQueryListEvent) => setSystemDark(event.matches)
    const handleStorage = (event: StorageEvent) => {
      if (event.key === storageKey || event.key === null) setPreference(readPreference())
    }
    query.addEventListener('change', handleSystemChange)
    window.addEventListener('storage', handleStorage)
    return () => {
      query.removeEventListener('change', handleSystemChange)
      window.removeEventListener('storage', handleStorage)
    }
  }, [])
  const mode = preference ?? (systemDark ? 'dark' : 'light')
  const theme = useMemo(() => createAppTheme(mode), [mode])
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
