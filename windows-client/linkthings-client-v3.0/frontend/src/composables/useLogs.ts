import { ref, onMounted, onUnmounted } from 'vue'
import { Events } from '@wailsio/runtime'
import { History, Path } from '@bindings/services/logservice'
import type { LogEntry } from '@bindings/services/models'

const MAX_ENTRIES = 500

/** Log tail: history on load, then live entries via the "log" event. */
export function useLogs() {
  const entries = ref<LogEntry[]>([])
  const path = ref('')

  let off: (() => void) | null = null

  onMounted(async () => {
    const [history, logPath] = await Promise.all([History(MAX_ENTRIES), Path()])
    // Newest first, matching the design's "prepended live" behavior.
    entries.value = [...history].reverse()
    path.value = logPath

    // See useConnection.ts: Events.On's callback gets a WailsEvent wrapper
    // ({name, data}), so the actual log entry is event.data.
    off = Events.On('log', (event) => {
      entries.value = [event.data, ...entries.value].slice(0, MAX_ENTRIES)
    })
  })

  onUnmounted(() => {
    if (off) off()
  })

  return { entries, path }
}
