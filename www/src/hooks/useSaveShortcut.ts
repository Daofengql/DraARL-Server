import { useEffect } from 'react'

// Only the active configuration dialog handles save. Normal text editing,
// unrelated pages and disabled/busy dialogs retain their browser shortcuts.
export function useSaveShortcut(enabled: boolean, save: () => void) {
  useEffect(() => {
    if (!enabled) return
    const onKeyDown = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 's' && !event.repeat) {
        event.preventDefault()
        save()
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [enabled, save])
}
