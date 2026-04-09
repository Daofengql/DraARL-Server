export type ToneMode = 'off' | 'ctcss' | 'cdcss_n' | 'cdcss_i'
export type PowerOption = 'low' | 'high'
export type BandwidthOption = 'wide' | 'narrow'
export type FrequencyCardId = 'sa818-radio-v1'

export interface ToneSelection {
  mode: ToneMode
  value: string
}

export interface RadioConfigForm {
  txFreq: string
  rxFreq: string
  txTone: ToneSelection
  rxTone: ToneSelection
  squelch: number
  sameFreq: boolean
  power: PowerOption
  bandwidth: BandwidthOption
}

export interface FrequencyCardCapabilities {
  moduleType: 'sa818'
  supportedToneModes: ToneMode[]
  sqlRange: readonly number[]
  powerLevels: readonly PowerOption[]
  configKeys: readonly string[]
}

export interface FrequencyCardProfile {
  cardId: FrequencyCardId
  moduleType: 'sa818'
  capabilities: FrequencyCardCapabilities
}

export const DEFAULT_FREQUENCY_CARD_ID: FrequencyCardId = 'sa818-radio-v1'
export const TONE_MODE_OPTIONS: Array<{ value: ToneMode; label: string }> = [
  { value: 'off', label: 'OFF' },
  { value: 'ctcss', label: 'CTCSS' },
  { value: 'cdcss_n', label: 'CDCSS_N' },
  { value: 'cdcss_i', label: 'CDCSS_I' },
]

export const CTCSS_OPTIONS = [
  '67.0', '69.3', '71.9', '74.4', '77.0', '79.7', '82.5', '85.4', '88.5', '91.5',
  '94.8', '97.4', '100.0', '103.5', '107.2', '110.9', '114.8', '118.8', '123.0', '127.3',
  '131.8', '136.5', '141.3', '146.2', '151.4', '156.7', '159.8', '162.2', '165.5', '167.9',
  '171.3', '173.8', '177.3', '179.9', '183.5', '186.2', '189.9', '192.8',
] as const

export const DCS_OPTIONS = [
  '023', '025', '026', '031', '032', '036', '043', '047', '051', '053',
  '054', '065', '071', '072', '073', '074', '114', '115', '116', '122',
  '125', '131', '132', '134', '143', '145', '152', '155', '156', '162',
  '165', '172', '174', '205', '212', '223', '225', '226', '243', '244',
  '245', '246', '251', '252', '255', '261', '263', '265', '266', '271',
  '274', '306', '311', '315', '325', '331', '332', '343', '346', '351',
  '356', '364', '365', '371', '411', '412', '413', '423', '431', '432',
  '445', '446', '452', '454', '455', '462', '464', '465', '466', '503',
  '506', '516', '523', '526', '532', '546', '565', '606', '612', '624',
  '627', '631', '632', '654', '662', '664', '703', '712', '723', '731',
  '732', '734', '743', '754',
] as const

export const SQL_LEVEL_OPTIONS = Array.from({ length: 9 }, (_, index) => index)
export const POWER_OPTIONS: Array<{ value: PowerOption; label: string }> = [
  { value: 'low', label: '低功率' },
  { value: 'high', label: '高功率' },
]

const SA818_PROFILE: FrequencyCardProfile = {
  cardId: DEFAULT_FREQUENCY_CARD_ID,
  moduleType: 'sa818',
  capabilities: {
    moduleType: 'sa818',
    supportedToneModes: ['off', 'ctcss', 'cdcss_n', 'cdcss_i'],
    sqlRange: SQL_LEVEL_OPTIONS,
    powerLevels: ['low', 'high'],
    configKeys: [
      'tx_freq',
      'rx_freq',
      'tx_ctcss',
      'rx_ctcss',
      'tx_tone_mode',
      'tx_tone_value',
      'rx_tone_mode',
      'rx_tone_value',
      'sql_level',
      'power_level',
      'tx_bandwidth',
    ],
  },
}

export function getDefaultRadioConfig(): RadioConfigForm {
  return {
    txFreq: '',
    rxFreq: '',
    txTone: { mode: 'off', value: '0' },
    rxTone: { mode: 'off', value: '0' },
    squelch: 0,
    sameFreq: true,
    power: 'high',
    bandwidth: 'wide',
  }
}

export function resolveFrequencyCardProfile(devModel: number, cardId?: string | null): FrequencyCardProfile | null {
  const resolvedCardId = cardId || (devModel === 1 || devModel === 106 || devModel === 107 ? DEFAULT_FREQUENCY_CARD_ID : null)
  if (resolvedCardId === DEFAULT_FREQUENCY_CARD_ID) {
    return SA818_PROFILE
  }
  return null
}

export function getToneValueOptions(mode: ToneMode): readonly string[] {
  switch (mode) {
    case 'ctcss':
      return CTCSS_OPTIONS
    case 'cdcss_n':
    case 'cdcss_i':
      return DCS_OPTIONS
    default:
      return []
  }
}

export function buildToneSelection(params?: {
  mode?: string
  value?: string
  legacy?: string
}): ToneSelection {
  const hasExplicitTone = params?.mode !== undefined || params?.value !== undefined
  const explicit = normalizeToneSelection(params?.mode, params?.value)
  if (hasExplicitTone) {
    return explicit
  }
  return normalizeToneSelection(undefined, params?.legacy)
}

export function normalizeToneSelection(mode?: string, rawValue?: string): ToneSelection {
  const normalizedMode = normalizeToneMode(mode)
  const value = String(rawValue || '').trim().toUpperCase()

  const dcsMatch = value.match(/^(\d{3})([NI])$/)
  if (dcsMatch) {
    return {
      mode: dcsMatch[2] === 'N' ? 'cdcss_n' : 'cdcss_i',
      value: dcsMatch[1],
    }
  }

  if (normalizedMode === 'off') {
    return { mode: 'off', value: '0' }
  }

  if (normalizedMode === 'ctcss') {
    const normalizedValue = normalizeCtcssValue(value)
    return normalizedValue === '0'
      ? { mode: 'off', value: '0' }
      : { mode: 'ctcss', value: normalizedValue }
  }

  if (normalizedMode === 'cdcss_n' || normalizedMode === 'cdcss_i') {
    const normalizedValue = normalizeDcsValue(value)
    return normalizedValue
      ? { mode: normalizedMode, value: normalizedValue }
      : { mode: 'off', value: '0' }
  }

  if (!value || value === '0' || value === 'OFF') {
    return { mode: 'off', value: '0' }
  }

  const ctcssValue = normalizeCtcssValue(value)
  if (ctcssValue !== '0') {
    return { mode: 'ctcss', value: ctcssValue }
  }

  return { mode: 'off', value: '0' }
}

export function toneSelectionToLegacyValue(tone: ToneSelection): string {
  return tone.mode === 'ctcss' ? tone.value : '0'
}

export function toneSelectionToToneValue(tone: ToneSelection): string {
  return tone.mode === 'off' ? '0' : tone.value
}

export function toneSelectionToRelayValue(tone: ToneSelection): string {
  switch (tone.mode) {
    case 'off':
      return '0'
    case 'ctcss':
      return tone.value
    case 'cdcss_n':
      return `${tone.value}N`
    case 'cdcss_i':
      return `${tone.value}I`
    default:
      return '0'
  }
}

export function formatToneDisplay(rawValue?: string, mode?: string, explicitValue?: string): string {
  const selection = buildToneSelection({ legacy: rawValue, mode, value: explicitValue })
  switch (selection.mode) {
    case 'ctcss':
      return `${selection.value} Hz`
    case 'cdcss_n':
      return `${selection.value}N`
    case 'cdcss_i':
      return `${selection.value}I`
    default:
      return 'OFF'
  }
}

export function usesDigitalTone(tone: ToneSelection): boolean {
  return tone.mode === 'cdcss_n' || tone.mode === 'cdcss_i'
}

export function hzToMHz(hz?: string): string {
  if (!hz || hz === '0') return ''
  const num = parseInt(hz, 10)
  if (Number.isNaN(num)) return ''
  return (num / 1_000_000).toFixed(6).replace(/\.?0+$/, '')
}

export function mhzToHz(mhz?: string): string {
  if (!mhz) return '0'
  const num = parseFloat(mhz)
  if (Number.isNaN(num)) return '0'
  return Math.round(num * 1_000_000).toString()
}

export function normalizeSquelchLevel(level: number): number {
  if (!Number.isFinite(level)) {
    return 0
  }
  return Math.max(0, Math.min(8, Math.round(level)))
}

export function powerToLevel(power: PowerOption): '1' | '3' {
  return power === 'low' ? '1' : '3'
}

export function levelToPower(level?: string): PowerOption {
  return level === '1' ? 'low' : 'high'
}

export function bandwidthToLevel(bandwidth: BandwidthOption): '1' | '2' {
  return bandwidth === 'narrow' ? '1' : '2'
}

export function levelToBandwidth(level?: string): BandwidthOption {
  return level === '1' ? 'narrow' : 'wide'
}

function normalizeToneMode(mode?: string): ToneMode {
  switch (String(mode || '').trim().toLowerCase()) {
    case 'ctcss':
      return 'ctcss'
    case 'cdcss_n':
    case 'cdcss-n':
    case 'cdcssn':
    case 'dcsn':
      return 'cdcss_n'
    case 'cdcss_i':
    case 'cdcss-i':
    case 'cdcssi':
    case 'dcsi':
      return 'cdcss_i'
    default:
      return 'off'
  }
}

function normalizeCtcssValue(value: string): string {
  const parsed = Number.parseFloat(value)
  if (!Number.isFinite(parsed) || parsed <= 0) {
    return '0'
  }
  return parsed.toFixed(1)
}

function normalizeDcsValue(value: string): string {
  const digits = value.replace(/\D/g, '')
  if (!digits) {
    return ''
  }
  const parsed = Number.parseInt(digits, 10)
  if (!Number.isFinite(parsed) || parsed < 0 || parsed > 999) {
    return ''
  }
  return parsed.toString().padStart(3, '0')
}
