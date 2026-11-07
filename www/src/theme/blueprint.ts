import { alpha, createTheme, type PaletteMode, type Theme } from '@mui/material/styles'

export const blueprintFont = '"JetBrains Mono", "SFMono-Regular", Consolas, "Noto Sans SC", "Microsoft YaHei", monospace'

export const blueprintGridSx = {
  bgcolor: 'background.default',
  backgroundImage: (theme: Theme) => {
    const line = alpha(theme.palette.secondary.main, theme.palette.mode === 'dark' ? 0.07 : 0.08)
    return 'linear-gradient(' + line + ' 1px, transparent 1px), linear-gradient(90deg, ' + line + ' 1px, transparent 1px)'
  },
  backgroundSize: '32px 32px',
}

export function createBlueprintTheme(mode: PaletteMode) {
  const dark = mode === 'dark'
  const ink = dark ? '#f0f0f0' : '#172c40'
  const muted = dark ? '#8b9bb4' : '#50657a'
  const paper = dark ? '#101f2e' : '#ffffff'
  const divider = dark ? '#314759' : '#cbd7e1'
  const accent = dark ? '#ff9f30' : '#a34b00'
  const line = dark ? '#72c8df' : '#006b82'
  return createTheme({
    palette: {
      mode,
      primary: { main: accent, light: '#ffbe70', dark: dark ? '#e88a20' : '#7d3900', contrastText: dark ? '#0b1623' : '#ffffff' },
      secondary: { main: line, contrastText: dark ? '#0b1623' : '#ffffff' },
      error: { main: dark ? '#ff7b7b' : '#ba2635' },
      warning: { main: dark ? '#ffb65c' : '#8c5200' },
      success: { main: dark ? '#00ff9d' : '#0c704b' },
      info: { main: line },
      background: { default: dark ? '#0b1623' : '#f2f6f9', paper },
      text: { primary: ink, secondary: muted },
      divider,
      action: {
        hover: alpha(line, 0.07),
        selected: alpha(accent, 0.1),
        focus: alpha(accent, 0.14),
      },
    },
    typography: {
      fontFamily: blueprintFont,
      h1: { fontSize: '2.25rem', fontWeight: 700, lineHeight: 1.25, letterSpacing: '-0.04em' },
      h2: { fontSize: '1.5rem', fontWeight: 600, lineHeight: 1.4 },
      h3: { fontSize: '1.375rem', fontWeight: 600 },
      h4: { fontSize: '2rem', fontWeight: 600, fontVariantNumeric: 'tabular-nums' },
      h5: { fontSize: '1.375rem', fontWeight: 600, lineHeight: 1.4 },
      h6: { fontSize: '1.0625rem', fontWeight: 600, lineHeight: 1.5 },
      body1: { fontSize: '0.9375rem', lineHeight: 1.7 },
      body2: { fontSize: '0.875rem', lineHeight: 1.7 },
      caption: { fontSize: '0.75rem', lineHeight: 1.6, letterSpacing: '0.025em' },
      button: { textTransform: 'none', fontWeight: 600, letterSpacing: '0.015em' },
    },
    shape: { borderRadius: 0 },
    transitions: {
      duration: { shortest: 150, shorter: 150, short: 150, standard: 150, complex: 150, enteringScreen: 150, leavingScreen: 150 },
    },
    components: {
      MuiCssBaseline: { styleOverrides: {
        html: { colorScheme: mode },
        body: { minWidth: 320 },
        '#root': { minHeight: '100dvh' },
        '::selection': { backgroundColor: alpha(accent, 0.28), color: ink },
        ':focus-visible': { outline: '2px solid ' + accent, outlineOffset: 3 },
        'svg text': { fontFamily: blueprintFont },
        '@media (prefers-reduced-motion: reduce)': {
          '*, *::before, *::after': { animationDuration: '0.01ms !important', animationIterationCount: '1 !important', transitionDuration: '0.01ms !important', scrollBehavior: 'auto !important' },
        },
      } },
      MuiButtonBase: {
        defaultProps: { disableRipple: true },
        styleOverrides: { root: { '&.Mui-focusVisible': { outline: '2px solid ' + accent, outlineOffset: 3 } } },
      },
      MuiButton: {
        defaultProps: { disableElevation: true },
        styleOverrides: {
          root: { minHeight: 38, borderRadius: 0, boxShadow: 'none', transition: 'background-color 150ms, border-color 150ms, color 150ms' },
          contained: { '&:hover': { boxShadow: 'none' } },
          containedPrimary: { backgroundColor: '#ff9f30', color: '#0b1623', '&:hover': { backgroundColor: '#eb922c' } },
          outlined: { borderColor: divider, '&:hover': { borderColor: accent, backgroundColor: alpha(accent, 0.06) } },
          sizeLarge: { minHeight: 48, paddingInline: 24 },
        },
      },
      MuiPaper: { styleOverrides: { root: { backgroundImage: 'none', boxShadow: 'none' } } },
      MuiCard: { styleOverrides: { root: { border: '1px solid ' + divider, boxShadow: 'none', overflow: 'hidden' } } },
      MuiCardContent: { styleOverrides: { root: { padding: 24, '&:last-child': { paddingBottom: 24 } } } },
      MuiTextField: { defaultProps: { variant: 'outlined', size: 'small' } },
      MuiSelect: { defaultProps: { variant: 'outlined', size: 'small' } },
      MuiInputBase: { styleOverrides: {
        input: {
          // The input container draws the focus border and leaves room for the floating label.
          '&:focus-visible': { outline: 'none' },
        },
      } },
      MuiOutlinedInput: { styleOverrides: {
        root: {
          backgroundColor: paper,
          '& .MuiOutlinedInput-notchedOutline': { borderColor: dark ? '#40566d' : '#93a6b7' },
        },
      } },
      MuiIconButton: { styleOverrides: { root: { borderRadius: 0, '&:hover': { backgroundColor: alpha(line, 0.08) } } } },
      MuiDrawer: { styleOverrides: { paper: { borderRight: '1px solid ' + divider, boxShadow: 'none' } } },
      MuiAppBar: { styleOverrides: { root: { backgroundColor: paper, color: ink, backgroundImage: 'none', boxShadow: 'none' } } },
      MuiLink: { styleOverrides: { root: { textUnderlineOffset: 4 } } },
      MuiListItemButton: { styleOverrides: {
        root: {
          minHeight: 44, borderLeft: '2px solid transparent', transition: 'background-color 150ms, color 150ms, border-color 150ms',
          '&.Mui-selected': { borderLeftColor: accent, color: accent },
        },
      } },
      MuiChip: { styleOverrides: { root: { borderRadius: 0, fontWeight: 500 }, sizeSmall: { height: 25 } } },
      MuiTableHead: { styleOverrides: { root: { backgroundColor: alpha(line, 0.045) } } },
      MuiTableCell: { styleOverrides: {
        root: { borderColor: divider },
        head: { color: muted, fontSize: '0.75rem', fontWeight: 600, letterSpacing: '0.035em', whiteSpace: 'nowrap' },
      } },
      MuiTab: { styleOverrides: { root: { minHeight: 44, fontWeight: 500 } } },
      MuiTabs: { styleOverrides: { indicator: { height: 2 } } },
      MuiAlert: { styleOverrides: { root: { border: '1px solid', borderColor: 'currentColor', backgroundImage: 'none' } } },
      MuiDialog: { styleOverrides: { paper: { border: '1px solid ' + divider, boxShadow: '0 16px 64px #00000040' } } },
      MuiMenu: { styleOverrides: { paper: { border: '1px solid ' + divider } } },
      MuiPopover: { styleOverrides: { paper: { border: '1px solid ' + divider } } },
      MuiTooltip: { styleOverrides: { tooltip: { backgroundColor: dark ? '#1c2b3a' : '#172c40', border: '1px solid ' + divider, fontSize: '0.75rem' } } },
      MuiSkeleton: { defaultProps: { animation: false } },
    },
  })
}
