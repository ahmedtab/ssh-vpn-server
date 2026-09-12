import type { ConnState } from '@bindings/services/models'

/** Which of App.vue's four screens is showing (mirrors TabBar's tab ids). */
export type ScreenName = 'connect' | 'profiles' | 'keys' | 'logs'

/** The subset of useConnection()'s unwrapped state ConnectView renders. */
export interface ConnDisplay {
  state: ConnState
  profile: string
  error: string
  rxBytes: number
  txBytes: number
  elapsedSeconds: number
}

/**
 * TrustHostKeyModal only reads `name`/`gateway`, so it accepts either a full
 * saved ServerConfig or the ad-hoc `{name, gateway}` literal ProfilesView
 * builds for an unsaved form before a host-key trust probe.
 */
export interface TrustableProfile {
  name: string
  gateway: string
}
