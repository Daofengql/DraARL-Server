const RADIO_TAB_LEASE_KEY = 'draarl-radio-active-tab'
const RADIO_TAB_CHANNEL = 'draarl-radio-tabs'

interface TabClaim {
  tabId: string
  claimedAt: number
}

function createTabId(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID()
  }
  return `${Date.now()}-${Math.random().toString(36).slice(2)}`
}

function parseClaim(value: string | null): TabClaim | null {
  if (!value) return null
  try {
    const parsed = JSON.parse(value) as Partial<TabClaim>
    if (typeof parsed.tabId !== 'string' || !Number.isFinite(parsed.claimedAt)) return null
    return { tabId: parsed.tabId, claimedAt: Number(parsed.claimedAt) }
  } catch {
    return null
  }
}

function isLaterClaim(candidate: TabClaim, current: TabClaim): boolean {
  return candidate.claimedAt > current.claimedAt ||
    (candidate.claimedAt === current.claimedAt && candidate.tabId > current.tabId)
}

/** 同一浏览器 profile 只允许一个 Radio 页面持有收发会话。 */
export class RadioTabCoordinator {
  private readonly tabId = createTabId()
  private claim: TabClaim = { tabId: this.tabId, claimedAt: 0 }
  private channel: BroadcastChannel | null = null
  private onTakenOver: (() => void) | null = null
  private active = false

  start(onTakenOver: () => void): void {
    this.onTakenOver = onTakenOver
    this.claim = { tabId: this.tabId, claimedAt: Date.now() }
    this.active = true

    window.addEventListener('storage', this.handleStorage)
    if (typeof BroadcastChannel !== 'undefined') {
      this.channel = new BroadcastChannel(RADIO_TAB_CHANNEL)
      this.channel.addEventListener('message', this.handleMessage)
    }

    const encoded = JSON.stringify(this.claim)
    localStorage.setItem(RADIO_TAB_LEASE_KEY, encoded)
    this.channel?.postMessage({ type: 'claim', claim: this.claim })
  }

  stop(): void {
    this.active = false
    window.removeEventListener('storage', this.handleStorage)
    this.channel?.removeEventListener('message', this.handleMessage)
    this.channel?.close()
    this.channel = null
    this.onTakenOver = null
  }

  private handleStorage = (event: StorageEvent): void => {
    if (event.key !== RADIO_TAB_LEASE_KEY) return
    this.considerClaim(parseClaim(event.newValue))
  }

  private handleMessage = (event: MessageEvent<{ type?: string; claim?: TabClaim }>): void => {
    if (event.data?.type !== 'claim') return
    this.considerClaim(event.data.claim || null)
  }

  private considerClaim(candidate: TabClaim | null): void {
    if (!this.active || !candidate || candidate.tabId === this.claim.tabId) return
    if (!isLaterClaim(candidate, this.claim)) return

    this.active = false
    this.onTakenOver?.()
  }
}
