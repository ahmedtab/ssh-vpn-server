/** hh:mm:ss for the Connect screen's uptime, per the design's stats row. */
export function formatDuration(totalSeconds: number): string {
  const s = Math.max(0, Math.floor(totalSeconds))
  const hh = String(Math.floor(s / 3600)).padStart(2, '0')
  const mm = String(Math.floor((s % 3600) / 60)).padStart(2, '0')
  const ss = String(s % 60).padStart(2, '0')
  return `${hh}:${mm}:${ss}`
}

/** Compact byte count (e.g. "1.2 MB"), used for the In/Out stat columns. */
export function formatBytes(bytes: number): string {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = bytes
  let i = 0
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024
    i++
  }
  return `${value.toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}

/** Strips the CIDR mask off an address (e.g. "10.10.11.1/30" -> "10.10.11.1"). */
export function stripCIDR(cidr: string | undefined | null): string {
  return (cidr || '').split('/')[0]
}
