import type { ConnSnapshot, LogEntry } from '@bindings/services/models'

/**
 * Payload of the "stats" event (services/connection_service.go's
 * StatsSnapshot, emitted every second while a tunnel is up). Not sourced
 * from the generated bindings: see the note in connection_service.go on why
 * application.RegisterEvent isn't used for this project's events.
 */
export interface StatsPayload {
  rxBytes: number
  txBytes: number
  since: number
}

declare module '@wailsio/runtime' {
  namespace Events {
    interface CustomEvents {
      'conn-state': ConnSnapshot
      stats: StatsPayload
      log: LogEntry
    }
  }
}
