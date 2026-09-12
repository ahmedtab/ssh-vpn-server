<script setup lang="ts">
import { ref, computed } from 'vue'
import type { LogEntry } from '@bindings/services/models'

const props = withDefaults(
  defineProps<{
    entries: LogEntry[]
    path?: string
  }>(),
  { path: '' },
)

type LogFilter = 'All' | 'Info' | 'Error'
const filter = ref<LogFilter>('All')

const filtered = computed(() => {
  if (filter.value === 'All') return props.entries
  const level = filter.value === 'Info' ? 'INFO' : 'ERROR'
  return props.entries.filter((e) => e.level === level)
})

function formatTime(unixSeconds: number): string {
  return new Date(unixSeconds * 1000).toLocaleTimeString()
}
</script>

<template>
  <div class="flex flex-col gap-3 p-3.5 h-[514px]">
    <div class="shrink-0">
      <div class="text-[15px] font-medium text-ink">Logs</div>
      <div class="flex items-center gap-1.5 mt-1.5">
        <button
          v-for="f in (['All', 'Info', 'Error'] as LogFilter[])"
          :key="f"
          type="button"
          class="rounded-md border px-2.5 py-1 text-[11px]"
          :class="filter === f ? 'border-accent bg-accent/10 text-accent-300' : 'border-line text-faint hover:bg-ink/5'"
          @click="filter = f"
        >{{ f }}</button>
      </div>
      <div class="font-mono text-[10px] text-faint truncate mt-1">{{ path }}</div>
    </div>

    <div class="flex-1 min-h-0 rounded-md bg-sunken ring-1 ring-line overflow-y-auto p-2 flex flex-col gap-1.5">
      <div v-for="(entry, i) in filtered" :key="i" class="px-1.5 py-1">
        <div class="font-mono text-[10px]">
          <span class="text-faint">{{ formatTime(entry.time) }}</span>
          <span class="ml-1.5" :class="entry.level === 'ERROR' ? 'text-danger' : 'text-[#8b83b5]'">{{ entry.level }}</span>
        </div>
        <div class="text-[11px] text-ink">{{ entry.msg }}</div>
      </div>
      <p v-if="!filtered.length" class="text-[11px] text-faint px-1.5">No log entries.</p>
    </div>
  </div>
</template>
