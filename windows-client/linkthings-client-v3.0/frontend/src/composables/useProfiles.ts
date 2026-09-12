import { ref } from 'vue'
import {
  List, Save, Delete, Duplicate, Test,
  ProbeHostKey, TrustHostKey, ForgetHostKey,
} from '@bindings/services/profileservice'
import type { ServerConfig } from '@bindings/config/models'

/** Loads and mutates the servers.json-backed profile list. */
export function useProfiles() {
  const profiles = ref<ServerConfig[]>([])
  const loading = ref(false)

  async function refresh() {
    loading.value = true
    try {
      profiles.value = await List()
    } finally {
      loading.value = false
    }
  }

  async function save(originalName: string | null | undefined, server: ServerConfig) {
    await Save(originalName || '', server)
    await refresh()
  }

  async function remove(name: string) {
    await Delete(name)
    await refresh()
  }

  async function duplicate(name: string): Promise<ServerConfig | null> {
    const clone = await Duplicate(name)
    await refresh()
    return clone
  }

  async function test(server: ServerConfig) {
    await Test(server)
  }

  // Trust-on-first-use host-key pinning: probe returns the fingerprint
  // presented by the gateway without authenticating; trust persists it to
  // the profile once the user confirms; forget clears it (e.g. after a
  // legitimate gateway reinstall/host-key rotation).
  async function probeHostKey(gateway: string): Promise<string> {
    return ProbeHostKey(gateway)
  }
  async function trustHostKey(name: string, fingerprint: string) {
    await TrustHostKey(name, fingerprint)
    await refresh()
  }
  async function forgetHostKey(name: string) {
    await ForgetHostKey(name)
    await refresh()
  }

  return { profiles, loading, refresh, save, remove, duplicate, test, probeHostKey, trustHostKey, forgetHostKey }
}
