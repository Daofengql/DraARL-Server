let databaseTimezone = ''

export function setAPITimezone(timezone: unknown) {
  if (typeof timezone === 'string' && timezone) databaseTimezone = timezone
}

// Legacy DATETIME responses have no offset. Interpret them in the server's
// advertised database timezone, then let Intl display the user's local time.
// Explicit ISO offsets and logbook's separate UTC/BJT contract remain intact.
export function parseAPIDate(value: string | number | Date, timezone = databaseTimezone): Date {
  if (value instanceof Date) return new Date(value.getTime())
  if (typeof value !== 'string' || !timezone || !/^\d{4}-\d\d-\d\d[ T]\d\d:\d\d:\d\d$/.test(value)) return new Date(value)
  const iso = value.replace(' ', 'T')
  if (/^[+-]\d\d:\d\d$/.test(timezone)) return new Date(iso + timezone)
  if (timezone === 'UTC') return new Date(iso + 'Z')
  try {
    const formatter = new Intl.DateTimeFormat('en-CA', { timeZone: timezone, year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23' })
    const target = Date.parse(iso + 'Z')
    let result = target
    for (let i = 0; i < 3; i++) {
      const parts = Object.fromEntries(formatter.formatToParts(new Date(result)).map((part) => [part.type, part.value]))
      const observed = Date.UTC(+parts.year, +parts.month - 1, +parts.day, +parts.hour, +parts.minute, +parts.second)
      const difference = target - observed
      if (!difference) break
      result += difference
    }
    return new Date(result)
  } catch { return new Date(iso) }
}
