import { ref, onMounted } from 'vue'
import { IsElevated } from '@bindings/services/elevationservice'

/** Footer admin/root badge. */
export function useElevation() {
  const elevated = ref(true)

  onMounted(async () => {
    elevated.value = await IsElevated()
  })

  return { elevated }
}
