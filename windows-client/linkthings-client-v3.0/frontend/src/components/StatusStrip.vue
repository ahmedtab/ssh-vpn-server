<script setup lang="ts">
import { formatBytes } from '../lib/format'
import { ConnState } from '@bindings/services/models'

const props = withDefaults(
  defineProps<{
    state: ConnState
    profile?: string
    elevated?: boolean
    rxBytes?: number
    txBytes?: number
  }>(),
  {
    profile: '',
    elevated: true,
    rxBytes: 0,
    txBytes: 0,
  },
)

const dotClass: Partial<Record<ConnState, string>> = {
  [ConnState.StateIdle]: 'bg-ghost',
  [ConnState.StateConnecting]: 'bg-accent animate-pulse-soft',
  [ConnState.StateConnected]: 'bg-accent',
  [ConnState.StateDisconnecting]: 'bg-accent animate-pulse-soft',
}

const context: Partial<Record<ConnState, string>> = {
  [ConnState.StateIdle]: 'No active tunnel',
  [ConnState.StateConnecting]: 'Dialing gateway…',
  [ConnState.StateConnected]: props.profile,
  [ConnState.StateDisconnecting]: 'Tearing down…',
}
</script>

<template>
  <div class="shrink-0 flex items-center justify-between px-3.5 py-1 border-t border-line text-[10.5px] font-mono text-faint">
    <span class="flex items-center gap-1.5 truncate">
      <span class="w-1.5 h-1.5 rounded-full shrink-0" :class="dotClass[state]" />
      <span class="truncate">{{ context[state] }}</span>
    </span>
    <span class="flex items-center gap-2 shrink-0">
      <span v-if="state === ConnState.StateConnected">↓ {{ formatBytes(rxBytes) }} · ↑ {{ formatBytes(txBytes) }}</span>
      <span v-if="elevated" class="text-faint">admin</span>
    </span>
  </div>
</template>
