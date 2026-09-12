import { ref } from 'vue'
import { AuthorizeViaSSH } from '@bindings/services/authservice'
import type { AuthorizeRequest } from '@bindings/services/models'
import type { AuthorizeResult } from '@bindings/sshauth/models'

/** The direct-SSH key-authorization flow (replaces the old HTTP provisioning). */
export function useAuth() {
  const busy = ref(false)
  const result = ref<AuthorizeResult | null>(null)
  const error = ref('')

  async function authorize(request: AuthorizeRequest): Promise<AuthorizeResult | null> {
    busy.value = true
    error.value = ''
    result.value = null
    try {
      result.value = await AuthorizeViaSSH(request)
      return result.value
    } catch (err) {
      error.value = err instanceof Error ? err.message : String(err)
      throw err
    } finally {
      busy.value = false
    }
  }

  function reset() {
    result.value = null
    error.value = ''
  }

  return { busy, result, error, authorize, reset }
}
