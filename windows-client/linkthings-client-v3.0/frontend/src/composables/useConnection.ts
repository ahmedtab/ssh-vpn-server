import { ref, onMounted, onUnmounted } from 'vue'
import { Events } from '@wailsio/runtime'
import { Connect, Disconnect, CurrentState } from '@bindings/services/connectionservice'
import { ConnState, type ConnSnapshot } from '@bindings/services/models'

/** Mirrors the design's `conn` state enum + the Connect screen's stats row. */
export function useConnection() {
  const state = ref<ConnState>(ConnState.StateIdle)
  const profile = ref('')
  const connectedAt = ref(0)
  const error = ref('')
  const rxBytes = ref(0)
  const txBytes = ref(0)
  const elapsedSeconds = ref(0)

  let elapsedTimer: ReturnType<typeof setInterval> | null = null
  function stopElapsed() {
    if (elapsedTimer) {
      clearInterval(elapsedTimer)
      elapsedTimer = null
    }
  }
  function startElapsed() {
    stopElapsed()
    elapsedTimer = setInterval(() => {
      elapsedSeconds.value = Math.max(0, Math.floor(Date.now() / 1000 - connectedAt.value))
    }, 1000)
  }

  function applySnapshot(snap: ConnSnapshot) {
    state.value = snap.state
    profile.value = snap.profile
    connectedAt.value = snap.connectedAt || 0
    error.value = snap.error || ''
    if (state.value === ConnState.StateConnected) {
      elapsedSeconds.value = Math.max(0, Math.floor(Date.now() / 1000 - connectedAt.value))
      startElapsed()
    } else {
      stopElapsed()
      elapsedSeconds.value = 0
      rxBytes.value = 0
      txBytes.value = 0
    }
  }

  let offState: (() => void) | null = null
  let offStats: (() => void) | null = null

  onMounted(async () => {
    applySnapshot(await CurrentState())
    // Events.On's callback receives a WailsEvent wrapper ({name, data}),
    // unlike a direct binding call's return value - the payload is under
    // .data, not the event object itself.
    offState = Events.On('conn-state', (event) => applySnapshot(event.data))
    offStats = Events.On('stats', (event) => {
      rxBytes.value = event.data.rxBytes
      txBytes.value = event.data.txBytes
    })
  })

  onUnmounted(() => {
    stopElapsed()
    if (offState) offState()
    if (offStats) offStats()
  })

  async function connect(name: string) {
    error.value = ''
    try {
      await Connect(name)
    } catch (err) {
      error.value = err instanceof Error ? err.message : String(err)
    }
  }

  async function disconnect() {
    try {
      await Disconnect()
    } catch (err) {
      error.value = err instanceof Error ? err.message : String(err)
    }
  }

  return { state, profile, connectedAt, error, rxBytes, txBytes, elapsedSeconds, connect, disconnect }
}
