<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { PhCircleNotch } from '@phosphor-icons/vue'
import type { TrustableProfile } from '../types'

const props = defineProps<{
  profile: TrustableProfile
  probeHostKey: (gateway: string) => Promise<string>
  trustHostKey: (name: string, fingerprint: string) => Promise<void>
}>()
const emit = defineEmits<{
  trusted: []
  cancel: []
}>()

const loading = ref(true)
const busy = ref(false)
const fingerprint = ref('')
const error = ref('')

onMounted(async () => {
  try {
    fingerprint.value = await props.probeHostKey(props.profile.gateway)
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    loading.value = false
  }
})

async function trust() {
  busy.value = true
  try {
    await props.trustHostKey(props.profile.name, fingerprint.value)
    emit('trusted')
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
    busy.value = false
  }
}
</script>

<template>
  <div class="fixed inset-0 z-40 flex items-center justify-center bg-black/50" @click.self="emit('cancel')">
    <div class="w-[340px] rounded-lg bg-surface-raised shadow-lg p-4 flex flex-col gap-3">
      <div class="text-[13px] font-medium text-ink">New host — verify fingerprint</div>

      <div v-if="loading" class="flex items-center gap-2 text-[12px] text-faint">
        <PhCircleNotch :size="14" class="animate-spin" /> Contacting {{ profile.gateway }}…
      </div>

      <template v-else-if="!error">
        <p class="text-[11.5px] text-faint">
          The authenticity of <span class="text-ink">{{ profile.gateway }}</span> can't be established
          from a prior connection. Its SSH host key fingerprint is:
        </p>
        <div class="rounded-md bg-sunken ring-1 ring-line px-2.5 py-2 font-mono text-[11px] break-all text-neutral-300">
          {{ fingerprint }}
        </div>
        <p class="text-[11px] text-faint">
          Verify this out-of-band with the gateway operator before trusting it. Once trusted, this
          fingerprint is pinned — a later mismatch (host key changed, or a possible interception) will
          be rejected rather than silently accepted.
        </p>
      </template>

      <p v-if="error" class="text-[11px] text-danger">{{ error }}</p>

      <div class="flex justify-end gap-2 pt-1">
        <button type="button" class="rounded-md border border-line text-ink hover:bg-ink/5 px-3 py-1.5 text-[12px]" @click="emit('cancel')">Cancel</button>
        <button
          v-if="!loading && !error"
          type="button"
          class="inline-flex items-center gap-1.5 rounded-md border border-accent bg-accent/10 text-accent-300 hover:bg-accent/20 px-3 py-1.5 text-[12px] disabled:opacity-50"
          :disabled="busy"
          @click="trust"
        >
          <PhCircleNotch v-if="busy" :size="14" class="animate-spin" />
          {{ busy ? 'Trusting…' : 'Trust and continue' }}
        </button>
      </div>
    </div>
  </div>
</template>
