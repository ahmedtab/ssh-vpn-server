<script setup lang="ts">
import { ref, reactive, computed, watch } from 'vue'
import {
  PhCopy, PhCheck, PhArrowsClockwise, PhUploadSimple, PhPaperPlaneTilt,
  PhCheckCircle, PhClockCountdown, PhArrowLeft, PhCircleNotch,
} from '@phosphor-icons/vue'
import TrustHostKeyModal from './TrustHostKeyModal.vue'
import type { ServerConfig } from '@bindings/config/models'
import type { KeyInfo, AuthorizeRequest } from '@bindings/services/models'
import type { AuthorizeResult } from '@bindings/sshauth/models'
import type { TrustableProfile } from '../types'

const props = defineProps<{
  profiles: ServerConfig[]
  keyInfo: KeyInfo | null
  authorizedEntry: (sshTunnel: number) => Promise<string>
  regenerate: () => Promise<void>
  importKey: (path: string, passphrase?: string) => Promise<void>
  pickPrivateKeyFile: () => Promise<string>
  authorize: (request: AuthorizeRequest) => Promise<AuthorizeResult | null>
  probeHostKey: (gateway: string) => Promise<string>
  trustHostKey: (name: string, fingerprint: string) => Promise<void>
}>()

const trustTarget = ref<TrustableProfile | null>(null)

type KeyTab = 'authorized_keys' | 'public'
const keyTab = ref<KeyTab>('authorized_keys')
const previewProfileName = ref('')
const previewEntry = ref('')
const copied = ref(false)

watch(
  () => props.profiles,
  (list) => {
    if (!previewProfileName.value && list.length) previewProfileName.value = list[0].name
  },
  { immediate: true },
)

const previewProfile = computed(() => props.profiles.find((p) => p.name === previewProfileName.value) || null)

watch([previewProfileName, () => props.keyInfo], async () => {
  if (previewProfile.value) {
    previewEntry.value = await props.authorizedEntry(previewProfile.value.sshTunnel)
  }
}, { immediate: true })

async function copyEntry() {
  const text = keyTab.value === 'authorized_keys' ? previewEntry.value : (props.keyInfo?.line || '')
  await navigator.clipboard.writeText(text)
  copied.value = true
  setTimeout(() => (copied.value = false), 1800)
}

const regenBusy = ref(false)
async function doRegenerate() {
  regenBusy.value = true
  try {
    await props.regenerate()
  } finally {
    regenBusy.value = false
  }
}

const importBusy = ref(false)
async function doImport() {
  const path = await props.pickPrivateKeyFile()
  if (!path) return
  importBusy.value = true
  try {
    await props.importKey(path, '')
  } finally {
    importBusy.value = false
  }
}

// --- Register flow (two modals) ---
type RegMode = 'password' | 'privatekey'
type RegStep = 'profile' | 'creds' | null

const regStep = ref<RegStep>(null)
const regPick = ref<ServerConfig | null>(null)
const regForm = reactive<{
  username: string
  mode: RegMode
  password: string
  privateKeyPath: string
  passphrase: string
}>({ username: 'root', mode: 'password', password: '', privateKeyPath: '', passphrase: '' })
const regBusy = ref(false)
const regResult = ref<AuthorizeResult | null>(null)
const regError = ref('')
// Session-only record of profiles this machine's key has been sent to via
// the SSH auth flow — there's no persistent server-verified "authorized"
// signal to poll (the old HTTP provisioning response is gone), so this is
// honestly scoped to "this session", not a durable authorization ledger.
const authorizedThisSession = reactive(new Set<string>())

function openRegister() {
  regStep.value = 'profile'
  regResult.value = null
  regError.value = ''
}
function pickProfile(p: ServerConfig) {
  regPick.value = p
  regStep.value = 'creds'
}
function backToProfiles() {
  regStep.value = 'profile'
}
function closeRegister() {
  regStep.value = null
}

async function submitRegister() {
  if (!regPick.value) return
  if (!regPick.value.hostKeyFingerprint) {
    trustTarget.value = regPick.value
    return
  }
  regBusy.value = true
  regError.value = ''
  try {
    const result = await props.authorize({
      profileName: regPick.value.name,
      username: regForm.username,
      mode: regForm.mode,
      password: regForm.mode === 'password' ? regForm.password : '',
      privateKeyPath: regForm.mode === 'privatekey' ? regForm.privateKeyPath : '',
      passphrase: regForm.mode === 'privatekey' ? regForm.passphrase : '',
    })
    regResult.value = result
    authorizedThisSession.add(regPick.value.name)
    regStep.value = null
  } catch (err) {
    regError.value = err instanceof Error ? err.message : String(err)
  } finally {
    regBusy.value = false
  }
}

async function onRegisterTrusted() {
  const name = trustTarget.value?.name
  trustTarget.value = null
  // Re-lookup so regPick reflects the freshly pinned fingerprint — it was
  // captured as a snapshot when the profile-picker modal was opened.
  if (name) regPick.value = props.profiles.find((p) => p.name === name) || regPick.value
  await submitRegister()
}

async function browseRegisterKey() {
  const path = await props.pickPrivateKeyFile()
  if (path) regForm.privateKeyPath = path
}
</script>

<template>
  <div class="flex flex-col gap-3 p-3.5 h-[514px]">
    <div class="shrink-0">
      <div class="text-[15px] font-medium text-ink">SSH key</div>
      <div class="text-[11px] text-faint">Authorize this machine on a gateway to bring its tunnel up.</div>
    </div>

    <!-- Key card -->
    <div class="shrink-0 rounded-[11px] bg-surface shadow-edge p-3.5 flex flex-col gap-2">
      <div class="flex rounded-md bg-sunken ring-1 ring-line p-0.5 w-fit">
        <button
          type="button"
          class="px-2.5 py-1 rounded text-[11px]"
          :class="keyTab === 'authorized_keys' ? 'bg-accent/15 text-accent-300' : 'text-faint'"
          @click="keyTab = 'authorized_keys'"
        >authorized_keys</button>
        <button
          type="button"
          class="px-2.5 py-1 rounded text-[11px]"
          :class="keyTab === 'public' ? 'bg-accent/15 text-accent-300' : 'text-faint'"
          @click="keyTab = 'public'"
        >public key</button>
      </div>

      <div v-if="keyTab === 'authorized_keys'" class="flex items-center gap-1.5 text-[10.5px] text-faint">
        <span>restricted to slot {{ previewProfile?.sshTunnel ?? '—' }} · for</span>
        <select v-model="previewProfileName" class="bg-transparent border-none text-accent-300 text-[10.5px] focus-visible:outline-2 focus-visible:outline-accent">
          <option v-for="p in profiles" :key="p.name" :value="p.name">{{ p.name }}</option>
        </select>
      </div>

      <div class="rounded-md bg-sunken ring-1 ring-line px-2.5 py-2 font-mono text-[10.5px] break-all text-neutral-300">
        {{ keyTab === 'authorized_keys' ? previewEntry : (keyInfo?.line || '') }}
      </div>

      <button
        type="button"
        class="w-full inline-flex items-center justify-center gap-1.5 rounded-md border border-line text-ink hover:bg-ink/5 active:bg-ink/10 px-3 py-1.5 text-[12px] focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2"
        @click="copyEntry"
      >
        <PhCheck v-if="copied" :size="14" />
        <PhCopy v-else :size="14" />
        {{ copied ? 'Copied to clipboard' : 'Copy' }}
      </button>

      <div class="flex items-center gap-2">
        <button type="button" class="inline-flex items-center gap-1.5 rounded-md border border-line text-ink hover:bg-ink/5 active:bg-ink/10 px-2.5 py-1.5 text-[12px]" @click="doRegenerate">
          <PhCircleNotch v-if="regenBusy" :size="14" class="animate-spin" />
          <PhArrowsClockwise v-else :size="14" />
          Regenerate
        </button>
        <button type="button" class="inline-flex items-center gap-1.5 rounded-md border border-line text-ink hover:bg-ink/5 active:bg-ink/10 px-2.5 py-1.5 text-[12px]" @click="doImport">
          <PhCircleNotch v-if="importBusy" :size="14" class="animate-spin" />
          <PhUploadSimple v-else :size="14" />
          Import
        </button>
        <button type="button" class="ml-auto inline-flex items-center justify-center rounded-md border border-accent bg-accent/10 text-accent-300 hover:bg-accent/20 w-8 h-8" title="Register key over SSH" @click="openRegister">
          <PhPaperPlaneTilt :size="14" />
        </button>
      </div>

      <p v-if="regResult" class="text-[11px] text-faint">
        Registered on {{ regPick?.name }} · tun_num {{ regResult.TunNum }} · server_ip {{ regResult.ServerIP }} · client_ip {{ regResult.ClientIP }}
      </p>
    </div>

    <!-- Authorized on (session-scoped, see comment above) -->
    <div class="flex-1 min-h-0 rounded-[11px] bg-surface shadow-edge p-3.5 flex flex-col gap-2">
      <div class="shrink-0 flex items-center justify-between text-[10px] uppercase tracking-[0.08em] text-faint">
        <span>Authorized this session</span>
        <span>{{ authorizedThisSession.size }}/{{ profiles.length }}</span>
      </div>
      <div class="flex-1 min-h-0 overflow-y-auto flex flex-col gap-1">
        <div v-for="p in profiles" :key="p.name" class="flex items-center gap-2 px-1 py-1 text-[12px]">
          <PhCheckCircle v-if="authorizedThisSession.has(p.name)" :size="14" class="text-accent shrink-0" weight="fill" />
          <PhClockCountdown v-else :size="14" class="text-danger shrink-0" />
          <span class="flex-1 min-w-0 truncate text-ink">{{ p.name }}</span>
          <span class="text-[10.5px] text-faint">{{ authorizedThisSession.has(p.name) ? `slot ${p.sshTunnel}` : 'pending' }}</span>
        </div>
      </div>
    </div>

    <!-- Key details -->
    <div class="shrink-0 rounded-[11px] bg-surface shadow-edge p-3.5 grid grid-cols-1 gap-1 font-mono text-[10.5px] text-faint">
      <div>Fingerprint: {{ keyInfo?.fingerprint || '—' }}</div>
      <div class="truncate">Path: {{ keyInfo?.path || '—' }}</div>
      <div>Created: {{ keyInfo?.createdAt ? new Date(keyInfo.createdAt * 1000).toLocaleString() : '—' }}</div>
    </div>

    <!-- Modal 1: pick profile -->
    <div v-if="regStep === 'profile'" class="fixed inset-0 z-30 flex items-center justify-center bg-black/50" @click.self="closeRegister">
      <div class="w-[320px] rounded-lg bg-surface-raised shadow-lg p-4 flex flex-col gap-2">
        <div class="text-[13px] font-medium text-ink">Register this key on…</div>
        <button
          v-for="p in profiles"
          :key="p.name"
          type="button"
          class="w-full flex items-center justify-between rounded-md px-2.5 py-2 text-left hover:bg-ink/5 text-[12.5px] text-ink"
          @click="pickProfile(p)"
        >
          <span>{{ p.name }}</span>
          <span class="font-mono text-[10.5px] text-faint">slot {{ p.sshTunnel }}</span>
        </button>
        <button type="button" class="mt-1 self-end rounded-md border border-line text-ink hover:bg-ink/5 px-3 py-1.5 text-[12px]" @click="closeRegister">Cancel</button>
      </div>
    </div>

    <!-- Modal 2: sign in to register -->
    <div v-if="regStep === 'creds'" class="fixed inset-0 z-30 flex items-center justify-center bg-black/50" @click.self="closeRegister">
      <form class="w-[320px] rounded-lg bg-surface-raised shadow-lg p-4 flex flex-col gap-2.5" @submit.prevent="submitRegister">
        <div class="flex items-center gap-2">
          <button type="button" class="text-faint hover:text-ink" @click="backToProfiles"><PhArrowLeft :size="14" /></button>
          <div class="text-[13px] font-medium text-ink">Sign in to register — {{ regPick?.name }}</div>
        </div>

        <label class="flex flex-col gap-1">
          <span class="text-[10px] uppercase tracking-[0.08em] text-faint">Username</span>
          <input v-model="regForm.username" required class="w-full min-h-8 rounded-md bg-bg border border-line px-2.5 text-[13px] focus-visible:border-accent" />
        </label>

        <div class="flex rounded-md bg-sunken ring-1 ring-line p-0.5 w-fit">
          <button type="button" class="px-2.5 py-1 rounded text-[11px]" :class="regForm.mode === 'password' ? 'bg-accent/15 text-accent-300' : 'text-faint'" @click="regForm.mode = 'password'">Password</button>
          <button type="button" class="px-2.5 py-1 rounded text-[11px]" :class="regForm.mode === 'privatekey' ? 'bg-accent/15 text-accent-300' : 'text-faint'" @click="regForm.mode = 'privatekey'">Private key</button>
        </div>

        <label v-if="regForm.mode === 'password'" class="flex flex-col gap-1">
          <span class="text-[10px] uppercase tracking-[0.08em] text-faint">Password</span>
          <input v-model="regForm.password" type="password" required class="w-full min-h-8 rounded-md bg-bg border border-line px-2.5 text-[13px] focus-visible:border-accent" />
        </label>
        <template v-else>
          <label class="flex flex-col gap-1">
            <span class="text-[10px] uppercase tracking-[0.08em] text-faint">Private key file</span>
            <div class="flex gap-1.5">
              <input v-model="regForm.privateKeyPath" readonly class="flex-1 min-h-8 rounded-md bg-bg border border-line px-2.5 text-[12px] font-mono truncate" />
              <button type="button" class="rounded-md border border-line text-ink hover:bg-ink/5 px-2.5 text-[12px]" @click="browseRegisterKey">Browse</button>
            </div>
          </label>
          <label class="flex flex-col gap-1">
            <span class="text-[10px] uppercase tracking-[0.08em] text-faint">Passphrase (optional)</span>
            <input v-model="regForm.passphrase" type="password" class="w-full min-h-8 rounded-md bg-bg border border-line px-2.5 text-[13px] focus-visible:border-accent" />
          </label>
        </template>

        <p class="text-[10.5px] text-faint">Credentials are used once for this SSH session and not stored.</p>
        <p v-if="regError" class="text-[11px] text-danger">{{ regError }}</p>

        <div class="flex justify-end gap-2 pt-1">
          <button type="button" class="rounded-md border border-line text-ink hover:bg-ink/5 px-3 py-1.5 text-[12px]" @click="closeRegister">Cancel</button>
          <button type="submit" class="inline-flex items-center gap-1.5 rounded-md border border-accent bg-accent/10 text-accent-300 hover:bg-accent/20 px-3 py-1.5 text-[12px]" :disabled="regBusy">
            <PhCircleNotch v-if="regBusy" :size="14" class="animate-spin" />
            {{ regBusy ? 'Registering…' : 'Register key' }}
          </button>
        </div>
      </form>
    </div>

    <TrustHostKeyModal
      v-if="trustTarget"
      :profile="trustTarget"
      :probe-host-key="probeHostKey"
      :trust-host-key="trustHostKey"
      @trusted="onRegisterTrusted"
      @cancel="trustTarget = null"
    />
  </div>
</template>
