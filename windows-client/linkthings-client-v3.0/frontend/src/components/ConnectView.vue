<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { PhStack, PhCaretUpDown, PhDesktopTower, PhHardDrives, PhBuildings, PhPlay, PhPower, PhCircleNotch } from '@phosphor-icons/vue'
import ProfilePicker from './ProfilePicker.vue'
import TrustHostKeyModal from './TrustHostKeyModal.vue'
import { formatDuration, formatBytes, stripCIDR } from '../lib/format'
import { ConnState } from '@bindings/services/models'
import type { ServerConfig } from '@bindings/config/models'
import type { ConnDisplay } from '../types'

const props = defineProps<{
  profiles: ServerConfig[]
  conn: ConnDisplay
  probeHostKey: (gateway: string) => Promise<string>
  trustHostKey: (name: string, fingerprint: string) => Promise<void>
}>()
const emit = defineEmits<{
  connect: [name: string]
  disconnect: []
  'new-profile': []
}>()

const trustTarget = ref<ServerConfig | null>(null)

const pickerOpen = ref(false)
const selectedName = ref('')

watch(
  () => props.profiles,
  (list) => {
    if (!selectedName.value && list.length) selectedName.value = list[0].name
  },
  { immediate: true },
)
watch(
  () => props.conn.profile,
  (name) => {
    if (name) selectedName.value = name
  },
)

const selected = computed(() => props.profiles.find((p) => p.name === selectedName.value) || null)

const busy = computed(() => props.conn.state === ConnState.StateConnecting || props.conn.state === ConnState.StateDisconnecting)

const statusHeadline = computed((): string | undefined => ({
  [ConnState.StateIdle]: 'No active tunnel',
  [ConnState.StateConnecting]: 'Dialing gateway…',
  [ConnState.StateConnected]: `Tunnel up · ${formatDuration(props.conn.elapsedSeconds)}`,
  [ConnState.StateDisconnecting]: 'Tearing down…',
} as Partial<Record<ConnState, string>>)[props.conn.state])

const statusDetail = computed((): string | undefined => ({
  [ConnState.StateIdle]: 'Select a profile and connect to start the tunnel.',
  [ConnState.StateConnecting]: 'Establishing the SSH tunnel channel.',
  [ConnState.StateConnected]: `Routed via ${selected.value?.gateway || 'gateway'}.`,
  [ConnState.StateDisconnecting]: 'Removing routes and closing the tunnel.',
} as Partial<Record<ConnState, string>>)[props.conn.state])

const connectorOpacity = computed(() => {
  if (props.conn.state === ConnState.StateConnected) return 'opacity-100'
  if (busy.value) return 'opacity-40'
  return 'opacity-0'
})

function selectProfile(name: string) {
  selectedName.value = name
  pickerOpen.value = false
}

function primaryAction() {
  if (props.conn.state === ConnState.StateIdle) {
    if (!selected.value) return
    if (!selected.value.hostKeyFingerprint) {
      trustTarget.value = selected.value
      return
    }
    emit('connect', selected.value.name)
  } else if (props.conn.state === ConnState.StateConnected) {
    emit('disconnect')
  }
  // "Cancel" during connecting is cosmetic in v1 — matches the TUI's
  // documented behavior (see docs/TUI_REFERENCE.md's gotcha list): the
  // in-flight SSH dial can't actually be interrupted from here.
}

function onTrusted() {
  const name = trustTarget.value?.name
  trustTarget.value = null
  if (name) emit('connect', name)
}
</script>

<template>
  <div class="flex flex-col gap-3 p-3.5 h-[514px]">
    <!-- Profile trigger + picker overlay -->
    <div class="relative shrink-0">
      <button
        type="button"
        class="w-full flex items-center gap-2 rounded-[10px] bg-surface shadow-edge px-3 py-2 text-left focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2"
        :class="pickerOpen ? 'shadow-edge-strong' : ''"
        @click="pickerOpen = !pickerOpen"
      >
        <PhStack :size="16" class="text-faint shrink-0" />
        <span class="flex-1 min-w-0">
          <span class="block text-[13px] font-medium text-ink truncate">{{ selected?.name || 'No profiles yet' }}</span>
          <span v-if="selected" class="block font-mono text-[10.5px] text-faint truncate">
            {{ selected.gateway }} · tun{{ selected.sshTunnel }} · {{ selected.fullTunnel ? 'full tunnel' : 'split' }}
          </span>
        </span>
        <PhCaretUpDown :size="14" class="text-faint shrink-0" />
      </button>
      <ProfilePicker
        v-if="pickerOpen"
        :profiles="profiles"
        :selected="selectedName"
        @select="selectProfile"
        @new="emit('new-profile')"
        @close="pickerOpen = false"
      />
    </div>

    <!-- Route chain -->
    <div
      class="rounded-[11px] shadow-edge p-3 flex flex-col"
      style="background: radial-gradient(circle at top, #20233a, #191b29)"
    >
      <div class="flex items-center gap-2">
        <div
          class="w-[38px] h-[38px] rounded-lg bg-surface flex items-center justify-center shrink-0"
          :class="conn.state === ConnState.StateConnected ? 'shadow-glow ring-1 ring-accent' : ''"
        >
          <PhDesktopTower :size="18" class="text-ink" />
        </div>
        <div class="flex-1 min-w-0">
          <div class="text-[12px] text-ink">This PC</div>
          <div class="font-mono text-[10.5px] text-faint truncate">{{ selected ? stripCIDR(selected.localIP) : '—' }}</div>
        </div>
        <span class="text-[10px] uppercase tracking-[0.08em] text-faint">local</span>
      </div>

      <div class="flex items-center gap-2 pl-[18px] h-[34px]">
        <div class="w-px h-full relative" :class="connectorOpacity">
          <div class="absolute inset-0 bg-[repeating-linear-gradient(to_bottom,theme(colors.accent.DEFAULT)_0_7px,transparent_7px_16px)] animate-flow-down" />
        </div>
        <span class="text-[10px] font-mono text-faint">ssh channel · tun@openssh.com</span>
      </div>

      <div class="flex items-center gap-2">
        <div
          class="w-[38px] h-[38px] rounded-lg bg-surface flex items-center justify-center shrink-0"
          :class="conn.state === ConnState.StateConnected ? 'shadow-glow ring-1 ring-accent' : ''"
        >
          <PhHardDrives :size="18" class="text-ink" />
        </div>
        <div class="flex-1 min-w-0">
          <div class="text-[12px] text-ink">Gateway</div>
          <div class="font-mono text-[10.5px] text-faint truncate">{{ selected?.gateway || '—' }}</div>
        </div>
        <span class="text-[10px] uppercase tracking-[0.08em] text-faint">tun{{ selected?.sshTunnel ?? '—' }}</span>
      </div>

      <div class="flex items-center gap-2 pl-[18px] h-[34px]">
        <div class="w-px h-full relative" :class="connectorOpacity">
          <div class="absolute inset-0 bg-[repeating-linear-gradient(to_bottom,theme(colors.accent.DEFAULT)_0_7px,transparent_7px_16px)] animate-flow-down" />
        </div>
        <span class="text-[10px] font-mono text-faint">{{ selected?.fullTunnel ? 'full tunnel' : 'remote LAN only' }}</span>
      </div>

      <div class="flex items-center gap-2">
        <div
          class="w-[38px] h-[38px] rounded-lg bg-surface flex items-center justify-center shrink-0"
          :class="conn.state === ConnState.StateConnected ? 'shadow-glow ring-1 ring-accent' : ''"
        >
          <PhBuildings :size="18" class="text-ink" />
        </div>
        <div class="flex-1 min-w-0">
          <div class="text-[12px] text-ink">Remote LAN</div>
          <div class="font-mono text-[10.5px] text-faint truncate">{{ selected?.lanSubnet || '—' }}</div>
        </div>
        <span class="text-[10px] uppercase tracking-[0.08em] text-faint">remote</span>
      </div>
    </div>

    <!-- Status block -->
    <div class="shrink-0">
      <div class="text-[18px] font-medium text-ink">{{ statusHeadline }}</div>
      <div class="text-[12px] text-faint">{{ statusDetail }}</div>
    </div>

    <!-- Primary action -->
    <button
      type="button"
      class="shrink-0 w-full h-[42px] rounded-md border flex items-center justify-center gap-2 text-[13px] font-medium focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2"
      :class="conn.state === ConnState.StateConnected
        ? 'border-line text-ink hover:bg-ink/5 active:bg-ink/10'
        : 'border-accent bg-accent/10 text-accent-300 hover:bg-accent/20 active:bg-accent/25'"
      :disabled="conn.state === ConnState.StateIdle && !selected"
      @click="primaryAction"
    >
      <PhCircleNotch v-if="busy" :size="16" class="animate-spin" />
      <PhPlay v-else-if="conn.state === ConnState.StateIdle" :size="16" />
      <PhPower v-else :size="16" />
      {{ conn.state === ConnState.StateIdle ? 'Connect' : conn.state === ConnState.StateConnecting ? 'Cancel' : conn.state === ConnState.StateConnected ? 'Disconnect' : 'Tearing down…' }}
    </button>

    <p v-if="conn.error" class="text-[11px] text-danger">{{ conn.error }}</p>

    <!-- Stats row -->
    <div class="grid grid-cols-4 gap-2 mt-auto">
      <div v-for="stat in [
        { label: 'Uptime', value: conn.state === ConnState.StateConnected ? formatDuration(conn.elapsedSeconds) : '—' },
        { label: 'In', value: conn.state === ConnState.StateConnected ? formatBytes(conn.rxBytes) : '—' },
        { label: 'Out', value: conn.state === ConnState.StateConnected ? formatBytes(conn.txBytes) : '—' },
        { label: 'MTU', value: selected?.mtu || '—' },
      ]" :key="stat.label" class="rounded-md bg-surface shadow-edge px-2 py-1.5 text-center">
        <div class="text-[10px] uppercase tracking-[0.08em] text-faint">{{ stat.label }}</div>
        <div class="font-mono text-[12px] text-ink">{{ stat.value }}</div>
      </div>
    </div>

    <TrustHostKeyModal
      v-if="trustTarget"
      :profile="trustTarget"
      :probe-host-key="probeHostKey"
      :trust-host-key="trustHostKey"
      @trusted="onTrusted"
      @cancel="trustTarget = null"
    />
  </div>
</template>
