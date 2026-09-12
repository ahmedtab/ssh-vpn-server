<script setup lang="ts">
import { ref, onMounted, nextTick } from 'vue'
import TopBar from './components/TopBar.vue'
import TabBar from './components/TabBar.vue'
import StatusStrip from './components/StatusStrip.vue'
import ConnectView from './components/ConnectView.vue'
import ProfilesView from './components/ProfilesView.vue'
import SshKeyView from './components/SshKeyView.vue'
import LogsView from './components/LogsView.vue'

import { useConnection } from './composables/useConnection'
import { useProfiles } from './composables/useProfiles'
import { useKey } from './composables/useKey'
import { useAuth } from './composables/useAuth'
import { useLogs } from './composables/useLogs'
import { useElevation } from './composables/useElevation'
import type { ScreenName } from './types'

const screen = ref<ScreenName>('connect')

const conn = useConnection()
const profiles = useProfiles()
const key = useKey()
const auth = useAuth()
const logs = useLogs()
const { elevated } = useElevation()

const profilesViewRef = ref<InstanceType<typeof ProfilesView> | null>(null)

onMounted(() => {
  profiles.refresh()
  key.refresh()
})

function goNewProfile() {
  screen.value = 'profiles'
  nextTick(() => profilesViewRef.value?.newProfile())
}
</script>

<template>
  <div class="relative w-[412px] h-[660px] rounded-lg overflow-hidden bg-bg text-ink font-sans shadow-edge flex flex-col mx-auto">
    <TopBar :state="conn.state.value" />

    <main class="flex-1 min-h-0 overflow-y-auto">
      <ConnectView
        v-if="screen === 'connect'"
        :profiles="profiles.profiles.value"
        :conn="{
          state: conn.state.value,
          profile: conn.profile.value,
          error: conn.error.value,
          rxBytes: conn.rxBytes.value,
          txBytes: conn.txBytes.value,
          elapsedSeconds: conn.elapsedSeconds.value,
        }"
        :probe-host-key="profiles.probeHostKey"
        :trust-host-key="profiles.trustHostKey"
        @connect="conn.connect"
        @disconnect="conn.disconnect"
        @new-profile="goNewProfile"
      />
      <ProfilesView
        v-else-if="screen === 'profiles'"
        ref="profilesViewRef"
        :profiles="profiles.profiles.value"
        :on-save="profiles.save"
        :on-delete="profiles.remove"
        :on-duplicate="profiles.duplicate"
        :on-test="profiles.test"
        :probe-host-key="profiles.probeHostKey"
        :trust-host-key="profiles.trustHostKey"
        :forget-host-key="profiles.forgetHostKey"
      />
      <SshKeyView
        v-else-if="screen === 'keys'"
        :profiles="profiles.profiles.value"
        :key-info="key.keyInfo.value"
        :authorized-entry="key.authorizedEntry"
        :regenerate="key.regenerate"
        :import-key="key.importKey"
        :pick-private-key-file="key.pickPrivateKeyFile"
        :authorize="auth.authorize"
        :probe-host-key="profiles.probeHostKey"
        :trust-host-key="profiles.trustHostKey"
      />
      <LogsView v-else-if="screen === 'logs'" :entries="logs.entries.value" :path="logs.path.value" />
    </main>

    <TabBar :screen="screen" @update:screen="screen = $event" />
    <StatusStrip
      :state="conn.state.value"
      :profile="conn.profile.value"
      :elevated="elevated"
      :rx-bytes="conn.rxBytes.value"
      :tx-bytes="conn.txBytes.value"
    />
  </div>
</template>
