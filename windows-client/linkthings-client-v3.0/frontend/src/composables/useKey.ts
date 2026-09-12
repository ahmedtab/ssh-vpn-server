import { ref } from 'vue'
import {
  GetPublicKey,
  AuthorizedEntry,
  Regenerate,
  Import as ImportKey,
  PickPrivateKeyFile,
} from '@bindings/services/keyservice'
import type { KeyInfo } from '@bindings/services/models'

/** This machine's Ed25519 keypair: display info + regenerate/import. */
export function useKey() {
  const keyInfo = ref<KeyInfo | null>(null)
  const loading = ref(false)

  async function refresh() {
    loading.value = true
    try {
      keyInfo.value = await GetPublicKey()
    } finally {
      loading.value = false
    }
  }

  async function authorizedEntry(sshTunnel: number): Promise<string> {
    return AuthorizedEntry(sshTunnel)
  }

  async function regenerate() {
    await Regenerate()
    await refresh()
  }

  async function importKey(path: string, passphrase?: string) {
    await ImportKey(path, passphrase || '')
    await refresh()
  }

  async function pickPrivateKeyFile(): Promise<string> {
    return PickPrivateKeyFile()
  }

  return { keyInfo, loading, refresh, authorizedEntry, regenerate, importKey, pickPrivateKeyFile }
}
