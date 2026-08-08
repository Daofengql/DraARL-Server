// UTC 转 BJT：UTC + 8小时 = BJT
export const utcToBjt = (utcTime: string): string => {
  if (!utcTime) return ''
  try {
    // 添加 'Z' 后缀强制解析为 UTC 时间
    const date = new Date(utcTime.replace(' ', 'T') + 'Z')
    // BJT = UTC + 8
    const bjtDate = new Date(date.getTime() + 8 * 60 * 60 * 1000)
    return bjtDate.toISOString().slice(0, 19).replace('T', ' ')
  } catch {
    return utcTime
  }
}

// BJT 转 UTC：BJT - 8小时 = UTC
// 输入的 BJT 时间会被当作本地时间解析（因为 datetime-local 返回的就是本地时间格式）
// 如果用户在 UTC+8 时区，本地时间就是 BJT，直接 toISOString() 就得到 UTC
export const bjtToUtc = (bjtTime: string): string => {
  if (!bjtTime) return ''
  try {
    const bjtDate = new Date(bjtTime.replace(' ', 'T') + 'Z')
    const utcDate = new Date(bjtDate.getTime() - 8 * 60 * 60 * 1000)
    return utcDate.toISOString().slice(0, 19).replace('T', ' ')
  } catch {
    return bjtTime
  }
}

// 获取当前UTC时间
export const getCurrentUtcTime = (): string => {
  // 返回带秒的格式：YYYY-MM-DD HH:MM:SS
  return new Date().toISOString().slice(0, 19).replace('T', ' ')
}

// 通信模式选项
export const MODE_OPTIONS = [
  'FM', 'AM', 'SSB', 'USB', 'LSB', 'CW', 'FT8', 'FT4', 'RTTY', 'PSK31',
  'DMR', 'D-Star', 'YSF', 'P25', 'NXDN', 'AX.25', 'SSTV', 'DV'
]
