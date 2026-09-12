<script setup lang="ts">
import type { Component } from 'vue'
import { PhPlugsConnected, PhListDashes, PhKey, PhListDashes as PhLogs } from '@phosphor-icons/vue'
import type { ScreenName } from '../types'

defineProps<{
  screen: ScreenName
}>()
const emit = defineEmits<{
  'update:screen': [screen: ScreenName]
}>()

const tabs: { id: ScreenName; label: string; icon: Component }[] = [
  { id: 'connect', label: 'Connect', icon: PhPlugsConnected },
  { id: 'profiles', label: 'Profiles', icon: PhListDashes },
  { id: 'keys', label: 'SSH key', icon: PhKey },
  { id: 'logs', label: 'Logs', icon: PhLogs },
]
</script>

<template>
  <nav class="shrink-0 flex border-t border-line">
    <button
      v-for="tab in tabs"
      :key="tab.id"
      type="button"
      class="flex-1 flex flex-col items-center gap-0.5 py-1.5 text-[10px] focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2"
      :class="screen === tab.id ? 'text-accent-300' : 'text-faint hover:bg-ink/5'"
      @click="emit('update:screen', tab.id)"
    >
      <span
        class="flex items-center justify-center w-7 h-7 rounded-md"
        :class="screen === tab.id ? 'bg-accent/15' : ''"
      >
        <component :is="tab.icon" :size="16" :weight="screen === tab.id ? 'fill' : 'regular'" />
      </span>
      {{ tab.label }}
    </button>
  </nav>
</template>
