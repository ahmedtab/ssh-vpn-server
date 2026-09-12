<script setup lang="ts">
import { PhShieldCheck } from '@phosphor-icons/vue'
import { ConnState } from '@bindings/services/models'

defineProps<{
  state: ConnState
}>()

const dotClass: Partial<Record<ConnState, string>> = {
  [ConnState.StateIdle]: 'bg-ghost',
  [ConnState.StateConnecting]: 'bg-accent animate-pulse-soft',
  [ConnState.StateConnected]: 'bg-accent',
  [ConnState.StateDisconnecting]: 'bg-accent animate-pulse-soft',
}

const label: Partial<Record<ConnState, string>> = {
  [ConnState.StateIdle]: 'Idle',
  [ConnState.StateConnecting]: 'Connecting',
  [ConnState.StateConnected]: 'Connected',
  [ConnState.StateDisconnecting]: 'Disconnecting',
}
</script>

<template>
  <header class="flex shrink-0 items-center justify-between px-3.5 h-[38px] border-b border-line">
    <div class="flex items-center gap-1.5">
      <PhShieldCheck :size="16" weight="fill" class="text-accent" />
      <span class="text-[13px] font-medium text-ink">LinkThings</span>
    </div>
    <div class="flex items-center gap-1.5 rounded-md bg-surface px-2 py-0.5">
      <span class="w-1.5 h-1.5 rounded-full" :class="dotClass[state]" />
      <span class="text-[10px] uppercase tracking-[0.08em] text-faint">{{ label[state] }}</span>
    </div>
  </header>
</template>
