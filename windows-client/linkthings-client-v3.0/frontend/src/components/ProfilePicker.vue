<script setup lang="ts">
import { PhStack, PhPlus } from '@phosphor-icons/vue'
import type { ServerConfig } from '@bindings/config/models'

withDefaults(
  defineProps<{
    profiles: ServerConfig[]
    selected?: string
  }>(),
  { selected: '' },
)
const emit = defineEmits<{
  select: [name: string]
  new: []
  close: []
}>()
</script>

<template>
  <div class="absolute inset-0 z-10" @click="emit('close')" />
  <div
    class="absolute top-[calc(100%+6px)] inset-x-0 z-20 rounded-lg bg-surface-raised shadow-lg overflow-y-auto"
    style="max-height: 236px"
  >
    <button
      v-for="p in profiles"
      :key="p.name"
      type="button"
      class="w-full flex items-center gap-2 px-3 py-2 text-left hover:bg-ink/5 focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2"
      :class="p.name === selected ? 'bg-accent/10 ring-1 ring-accent/40' : ''"
      @click="emit('select', p.name)"
    >
      <PhStack :size="14" class="text-faint shrink-0" />
      <span class="flex-1 min-w-0">
        <span class="block text-[13px] font-medium text-ink truncate">{{ p.name }}</span>
        <span class="block font-mono text-[10.5px] text-faint truncate">
          {{ p.gateway }} · tun{{ p.sshTunnel }} · {{ p.fullTunnel ? 'full tunnel' : 'split' }}
        </span>
      </span>
    </button>
    <button
      type="button"
      class="w-full flex items-center gap-2 px-3 py-2 text-left text-accent-300 hover:bg-ink/5 focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2"
      @click="emit('new')"
    >
      <PhPlus :size="14" />
      <span class="text-[13px]">New profile</span>
    </button>
  </div>
</template>
