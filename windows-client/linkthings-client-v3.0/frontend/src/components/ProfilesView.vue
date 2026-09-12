<script setup lang="ts">
import { ref, reactive, computed, watch } from 'vue'
import { PhPlus, PhFloppyDisk, PhPlugs, PhCopy, PhTrash, PhCircleNotch } from '@phosphor-icons/vue'
import TrustHostKeyModal from './TrustHostKeyModal.vue'
import type { ServerConfig } from '@bindings/config/models'
import type { TrustableProfile } from '../types'

const props = defineProps<{
  profiles: ServerConfig[]
  onSave: (originalName: string, server: ServerConfig) => Promise<void>
  onDelete: (name: string) => Promise<void>
  onDuplicate: (name: string) => Promise<ServerConfig | null>
  onTest: (server: ServerConfig) => Promise<void>
  probeHostKey: (gateway: string) => Promise<string>
  trustHostKey: (name: string, fingerprint: string) => Promise<void>
  forgetHostKey: (name: string) => Promise<void>
}>()

const trustTarget = ref<TrustableProfile | null>(null)

function blankForm(): ServerConfig {
  return {
    name: '',
    gateway: '',
    localIP: '',
    remoteIP: '',
    lanSubnet: '',
    sshTunnel: 0,
    mtu: '1340',
    fullTunnel: true,
    dns: '',
  }
}

const selectedName = ref('')
const form = reactive<ServerConfig>(blankForm())
const toast = ref('')
const testing = ref(false)
const confirmDelete = ref(false)

watch(
  () => props.profiles,
  (list) => {
    if (!selectedName.value && list.length) selectRow(list[0].name)
  },
  { immediate: true },
)

function selectRow(name: string) {
  const src = props.profiles.find((p) => p.name === name)
  if (!src) return
  selectedName.value = name
  Object.assign(form, src)
}

function newProfile() {
  selectedName.value = ''
  Object.assign(form, blankForm())
}

function clampSlot() {
  form.sshTunnel = Math.min(255, Math.max(0, Number(form.sshTunnel) || 0))
}

async function save() {
  if (!form.mtu) form.mtu = '1340'
  try {
    await props.onSave(selectedName.value, { ...form })
    toast.value = 'Validated · written to servers.json'
    selectedName.value = form.name
  } catch (err) {
    toast.value = `Save failed: ${err instanceof Error ? err.message : err}`
  } finally {
    setTimeout(() => (toast.value = ''), 2500)
  }
}

async function test() {
  if (!selectedName.value) {
    toast.value = 'Save the profile before testing'
    setTimeout(() => (toast.value = ''), 2500)
    return
  }
  if (!form.hostKeyFingerprint) {
    trustTarget.value = { name: selectedName.value, gateway: form.gateway }
    return
  }
  await runTest()
}

async function runTest() {
  testing.value = true
  toast.value = ''
  try {
    await props.onTest({ ...form })
    toast.value = 'Reachable · SSH auth OK'
  } catch (err) {
    toast.value = `Test failed: ${err instanceof Error ? err.message : err}`
  } finally {
    testing.value = false
    setTimeout(() => (toast.value = ''), 3000)
  }
}

async function onTestTrusted() {
  const target = trustTarget.value
  trustTarget.value = null
  // The profile list refresh from trustHostKey already picked up the pinned
  // fingerprint; re-select so `form` reflects it before testing.
  if (target) selectRow(target.name)
  await runTest()
}

async function doForgetHostKey() {
  if (!selectedName.value) return
  await props.forgetHostKey(selectedName.value)
  selectRow(selectedName.value)
}

async function duplicate() {
  if (!selectedName.value) return
  await props.onDuplicate(selectedName.value)
}

function requestDelete() {
  confirmDelete.value = true
}

async function confirmDeleteYes() {
  try {
    await props.onDelete(selectedName.value)
    newProfile()
  } catch (err) {
    toast.value = `Delete failed: ${err instanceof Error ? err.message : err}`
    setTimeout(() => (toast.value = ''), 3000)
  } finally {
    confirmDelete.value = false
  }
}

const canDelete = computed(() => props.profiles.length > 1)

defineExpose({ newProfile })
</script>

<template>
  <div class="flex flex-col gap-3 p-3.5">
    <div class="shrink-0 flex items-center justify-between">
      <div>
        <div class="text-[15px] font-medium text-ink">Profiles</div>
        <div class="text-[11px] text-faint">saved to servers.json</div>
      </div>
      <button
        type="button"
        class="inline-flex items-center gap-1.5 rounded-md border border-line text-ink hover:bg-ink/5 active:bg-ink/10 px-3 py-1.5 text-[12px] focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2"
        @click="newProfile"
      >
        <PhPlus :size="14" /> New
      </button>
    </div>

    <!-- List card -->
    <div class="shrink-0 rounded-[11px] bg-surface shadow-edge overflow-y-auto" style="max-height: 140px">
      <button
        v-for="p in profiles"
        :key="p.name"
        type="button"
        class="w-full flex items-center gap-2 px-3 py-2 text-left hover:bg-ink/5 focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2"
        :class="p.name === selectedName ? 'bg-accent/10 ring-1 ring-accent/40' : ''"
        @click="selectRow(p.name)"
      >
        <span class="w-1.5 h-1.5 rounded-full bg-ghost shrink-0" />
        <span class="flex-1 min-w-0 text-[13px] text-ink truncate">{{ p.name }}</span>
        <span class="font-mono text-[10.5px] text-faint truncate">{{ p.gateway }}</span>
        <span class="text-[10px] uppercase tracking-[0.08em] text-faint">tun{{ p.sshTunnel }}</span>
      </button>
    </div>

    <!-- Form card -->
    <form class="rounded-[11px] bg-surface shadow-edge p-3.5 flex flex-col gap-2.5" @submit.prevent="save">
      <label class="flex flex-col gap-1">
        <span class="text-[10px] uppercase tracking-[0.08em] text-faint">Name</span>
        <input v-model="form.name" required class="w-full min-h-8 rounded-md bg-bg border border-line px-2.5 text-[13px] caret-accent hover:border-ink/40 focus-visible:border-accent" />
      </label>

      <label class="flex flex-col gap-1">
        <span class="text-[10px] uppercase tracking-[0.08em] text-faint">Gateway (host:port)</span>
        <input v-model="form.gateway" required placeholder="1.2.3.4:2255" class="w-full min-h-8 rounded-md bg-bg border border-line px-2.5 text-[13px] font-mono caret-accent hover:border-ink/40 focus-visible:border-accent" />
      </label>

      <div class="grid grid-cols-2 gap-2">
        <label class="flex flex-col gap-1">
          <span class="text-[10px] uppercase tracking-[0.08em] text-faint">Local IP (CIDR)</span>
          <input v-model="form.localIP" required placeholder="10.10.11.1/30" class="w-full min-h-8 rounded-md bg-bg border border-line px-2.5 text-[13px] font-mono caret-accent hover:border-ink/40 focus-visible:border-accent" />
        </label>
        <label class="flex flex-col gap-1">
          <span class="text-[10px] uppercase tracking-[0.08em] text-faint">Remote IP</span>
          <input v-model="form.remoteIP" required placeholder="10.10.11.2" class="w-full min-h-8 rounded-md bg-bg border border-line px-2.5 text-[13px] font-mono caret-accent hover:border-ink/40 focus-visible:border-accent" />
        </label>
      </div>

      <label class="flex flex-col gap-1">
        <span class="text-[10px] uppercase tracking-[0.08em] text-faint">LAN subnet</span>
        <input v-model="form.lanSubnet" required placeholder="192.168.4.0/24" class="w-full min-h-8 rounded-md bg-bg border border-line px-2.5 text-[13px] font-mono caret-accent hover:border-ink/40 focus-visible:border-accent" />
      </label>

      <div class="flex items-center justify-between text-[10.5px] text-faint">
        <span class="truncate">
          Host key: {{ form.hostKeyFingerprint ? form.hostKeyFingerprint : 'not yet trusted' }}
        </span>
        <button
          v-if="form.hostKeyFingerprint"
          type="button"
          class="shrink-0 text-accent-300 hover:underline"
          @click="doForgetHostKey"
        >Forget</button>
      </div>

      <div class="grid grid-cols-3 gap-2">
        <label class="flex flex-col gap-1">
          <span class="text-[10px] uppercase tracking-[0.08em] text-faint">Slot</span>
          <input v-model.number="form.sshTunnel" type="number" min="0" max="255" @blur="clampSlot" class="w-full min-h-8 rounded-md bg-bg border border-line px-2.5 text-[13px] font-mono caret-accent hover:border-ink/40 focus-visible:border-accent" />
        </label>
        <label class="flex flex-col gap-1">
          <span class="text-[10px] uppercase tracking-[0.08em] text-faint">MTU</span>
          <input v-model="form.mtu" placeholder="1340" class="w-full min-h-8 rounded-md bg-bg border border-line px-2.5 text-[13px] font-mono caret-accent hover:border-ink/40 focus-visible:border-accent" />
        </label>
        <label class="flex flex-col gap-1">
          <span class="text-[10px] uppercase tracking-[0.08em] text-faint">DNS</span>
          <input v-model="form.dns" placeholder="8.8.8.8,1.1.1.1" class="w-full min-h-8 rounded-md bg-bg border border-line px-2.5 text-[13px] font-mono caret-accent hover:border-ink/40 focus-visible:border-accent" />
        </label>
      </div>

      <button
        type="button"
        class="flex items-center justify-between rounded-md bg-sunken ring-1 ring-line px-3 py-2"
        @click="form.fullTunnel = !form.fullTunnel"
      >
        <span class="text-left">
          <span class="block text-[12px] text-ink">Full tunnel</span>
          <span class="block text-[10.5px] text-faint">
            {{ form.fullTunnel ? 'All traffic goes through the tunnel' : 'Only the remote LAN goes through the tunnel' }}
          </span>
        </span>
        <span
          class="w-9 h-5 rounded-full relative transition-colors shrink-0"
          :class="form.fullTunnel ? 'bg-accent/35 ring-1 ring-accent' : 'bg-ink/10 ring-1 ring-ink/20'"
        >
          <span
            class="absolute top-[3px] w-3.5 h-3.5 rounded-full bg-ink transition-[left] duration-150"
            :class="form.fullTunnel ? 'left-[19px]' : 'left-[3px]'"
          />
        </span>
      </button>

      <div class="flex items-center gap-2 pt-1">
        <button type="submit" class="inline-flex items-center gap-1.5 rounded-md border border-accent bg-accent/10 px-3 py-1.5 text-accent-300 hover:bg-accent/20 active:bg-accent/25 text-[12px] focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2">
          <PhFloppyDisk :size="14" /> Save
        </button>
        <button type="button" class="inline-flex items-center gap-1.5 rounded-md border border-line text-ink hover:bg-ink/5 active:bg-ink/10 px-3 py-1.5 text-[12px] focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2" @click="test">
          <PhCircleNotch v-if="testing" :size="14" class="animate-spin" />
          <PhPlugs v-else :size="14" />
          Test
        </button>
        <button
          v-if="selectedName"
          type="button"
          class="inline-flex items-center gap-1.5 rounded-md border border-line text-ink hover:bg-ink/5 active:bg-ink/10 px-2 py-1.5 text-[12px] focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2"
          title="Duplicate"
          @click="duplicate"
        >
          <PhCopy :size="14" />
        </button>
        <button
          v-if="selectedName"
          type="button"
          class="ml-auto inline-flex items-center gap-1.5 rounded-md border border-danger/40 text-danger hover:bg-danger/10 px-2 py-1.5 text-[12px] focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2 disabled:opacity-45 disabled:cursor-not-allowed"
          title="Delete"
          :disabled="!canDelete"
          @click="requestDelete"
        >
          <PhTrash :size="14" />
        </button>
      </div>

      <p v-if="toast" class="text-[11px] text-faint">{{ toast }}</p>
    </form>

    <!-- Delete confirm modal -->
    <div v-if="confirmDelete" class="fixed inset-0 z-30 flex items-center justify-center bg-black/50" @click.self="confirmDelete = false">
      <div class="w-[320px] rounded-lg bg-surface-raised shadow-lg p-4 flex flex-col gap-3">
        <div class="text-[13px] font-medium text-ink">Delete "{{ selectedName }}"?</div>
        <p class="text-[11.5px] text-faint">This can't be undone.</p>
        <div class="flex justify-end gap-2">
          <button type="button" class="rounded-md border border-line text-ink hover:bg-ink/5 px-3 py-1.5 text-[12px]" @click="confirmDelete = false">Cancel</button>
          <button type="button" class="rounded-md border border-danger/40 text-danger hover:bg-danger/10 px-3 py-1.5 text-[12px]" @click="confirmDeleteYes">Delete</button>
        </div>
      </div>
    </div>

    <TrustHostKeyModal
      v-if="trustTarget"
      :profile="trustTarget"
      :probe-host-key="probeHostKey"
      :trust-host-key="trustHostKey"
      @trusted="onTestTrusted"
      @cancel="trustTarget = null"
    />
  </div>
</template>
