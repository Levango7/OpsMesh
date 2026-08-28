import { ref } from 'vue'

const isOnline = ref(navigator.onLine)
const isUpdateAvailable = ref(false)
const registrationInstance = ref(null)

let resolveReady
const ready = new Promise((resolve) => { resolveReady = resolve })

export function useServiceWorker() {
  return {
    isOnline,
    isUpdateAvailable,
    registration: registrationInstance,
    ready,
    reload: () => window.location.reload(),
    activateUpdate: () => {
      if (registrationInstance.value?.waiting) {
        registrationInstance.value.waiting.postMessage('skipWaiting')
      }
    }
  }
}

export function registerSW() {
  if (!('serviceWorker' in navigator)) {
    resolveReady()
    return
  }

  window.addEventListener('load', () => {
    navigator.serviceWorker
      .register('/enterprise/sw.js', { scope: '/enterprise/' })
      .then((reg) => {
        registrationInstance.value = reg

        if (reg.waiting) {
          isUpdateAvailable.value = true
        }

        reg.addEventListener('updatefound', () => {
          const newWorker = reg.installing
          newWorker.addEventListener('statechange', () => {
            if (newWorker.state === 'installed' && navigator.serviceWorker.controller) {
              isUpdateAvailable.value = true
            }
          })
        })

        resolveReady()
      })
      .catch(() => {
        resolveReady()
      })
  })

  navigator.serviceWorker.addEventListener('controllerchange', () => {
    window.location.reload()
  })

  window.addEventListener('online', () => { isOnline.value = true })
  window.addEventListener('offline', () => { isOnline.value = false })
}
