import { onMounted, onUnmounted, watch } from 'vue'

export function useScrollLock(isLocked) {
  const originalStyles = new Map()

  const lock = () => {
    if (typeof document === 'undefined') return
    const body = document.body
    originalStyles.set('overflow', body.style.overflow)
    originalStyles.set('position', body.style.position)
    originalStyles.set('width', body.style.width)
    body.style.overflow = 'hidden'
    body.style.position = 'fixed'
    body.style.width = '100%'
  }

  const unlock = () => {
    if (typeof document === 'undefined') return
    const body = document.body
    body.style.overflow = originalStyles.get('overflow') || ''
    body.style.position = originalStyles.get('position') || ''
    body.style.width = originalStyles.get('width') || ''
    originalStyles.clear()
  }

  const cleanup = () => {
    if (typeof document === 'undefined') return
    const body = document.body
    body.style.overflow = ''
    body.style.position = ''
    body.style.width = ''
    originalStyles.clear()
  }

  if (isLocked) {
    watch(isLocked, (locked) => {
      if (locked) lock()
      else unlock()
    })

    onMounted(() => {
      if (isLocked.value) lock()
    })

    onUnmounted(() => {
      cleanup()
    })

    return { lock, unlock, cleanup }
  }

  return cleanup
}
