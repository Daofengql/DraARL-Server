export function debugLog(...values: unknown[]): void {
  if (import.meta.env?.DEV) {
    // eslint-disable-next-line no-console -- Debug output is excluded from production behavior.
    console.debug(...values)
  }
}
