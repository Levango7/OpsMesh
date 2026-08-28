const CACHE_VERSION = 'v1'
const STATIC_CACHE = `opsmesh-static-${CACHE_VERSION}`
const DYNAMIC_CACHE = `opsmesh-dynamic-${CACHE_VERSION}`
const API_CACHE = `opsmesh-api-${CACHE_VERSION}`

const STATIC_ASSETS = [
  '/enterprise/',
  '/enterprise/index.html',
  '/enterprise/offline.html',
  '/enterprise/manifest.json'
]

const STATIC_EXTENSIONS = /\.(js|css|woff2?|ttf|otf|svg|png|jpe?g|webp|avif|gif)$/

const API_TTL = 5 * 60 * 1000
const MAX_API_ENTRIES = 100

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(STATIC_CACHE).then((cache) => cache.addAll(STATIC_ASSETS)).then(() => self.skipWaiting())
  )
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(
        keys
          .filter((k) => k.startsWith('opsmesh-') && k !== STATIC_CACHE && k !== DYNAMIC_CACHE && k !== API_CACHE)
          .map((k) => caches.delete(k))
      )
    ).then(() => self.clients.claim())
  )
})

self.addEventListener('fetch', (event) => {
  const { request } = event
  if (request.method !== 'GET') return

  const url = new URL(request.url)
  if (url.origin !== self.location.origin) return

  if (isAPIRequest(url)) {
    event.respondWith(networkFirstWithTTL(request))
    return
  }

  if (isStaticAsset(url)) {
    event.respondWith(cacheFirst(request, STATIC_CACHE))
    return
  }

  event.respondWith(cacheFirst(request, DYNAMIC_CACHE))
})

function isAPIRequest(url) {
  return url.pathname.startsWith('/api/')
}

function isStaticAsset(url) {
  return STATIC_EXTENSIONS.test(url.pathname)
}

async function cacheFirst(request, cacheName) {
  const cached = await caches.match(request)
  if (cached) return cached

  try {
    const response = await fetch(request)
    if (response.ok) {
      const cache = await caches.open(cacheName)
      cache.put(request, response.clone())
    }
    return response
  } catch {
    if (request.mode === 'navigate') {
      const offline = await caches.match('/enterprise/offline.html')
      if (offline) return offline
    }
    return new Response('Offline', { status: 503, statusText: 'Service Unavailable' })
  }
}

async function networkFirstWithTTL(request) {
  const cache = await caches.open(API_CACHE)
  const cached = await cache.match(request)

  if (cached) {
    const dateHeader = cached.headers.get('sw-cached-at')
    const age = dateHeader ? Date.now() - parseInt(dateHeader, 10) : Infinity
    if (age < API_TTL) {
      return cached
    }
  }

  try {
    const response = await fetch(request)
    if (response.ok) {
      const headers = new Headers(response.headers)
      headers.set('sw-cached-at', Date.now().toString())
      const modified = new Response(response.body, {
        status: response.status,
        statusText: response.statusText,
        headers
      })
      await cache.put(request, modified)
      trimCache(cache, MAX_API_ENTRIES)
    }
    return response
  } catch {
    if (cached) return cached
    return new Response(JSON.stringify({ error: 'offline' }), {
      status: 503,
      headers: { 'Content-Type': 'application/json' }
    })
  }
}

async function trimCache(cache, maxEntries) {
  const keys = await cache.keys()
  if (keys.length <= maxEntries) return
  const toDelete = keys.slice(0, keys.length - maxEntries)
  await Promise.all(toDelete.map((k) => cache.delete(k)))
}

self.addEventListener('message', (event) => {
  if (event.data === 'skipWaiting') {
    self.skipWaiting()
  }
})

self.addEventListener('sync', (event) => {
  if (event.tag === 'sync-queued-requests') {
    event.waitUntil(syncQueuedRequests())
  }
})

async function syncQueuedRequests() {
  const db = await openQueueDB()
  const requests = await getAllQueued(db)
  for (const req of requests) {
    try {
      await fetch(req.url, {
        method: req.method,
        headers: req.headers,
        body: req.body
      })
      await deleteQueued(db, req.id)
    } catch {
      // keep in queue for next sync
    }
  }
}

function openQueueDB() {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open('opsmesh-queue', 1)
    req.onerror = () => reject(req.error)
    req.onsuccess = () => resolve(req.result)
    req.onupgradeneeded = () => {
      req.result.createObjectStore('requests', { keyPath: 'id', autoIncrement: true })
    }
  })
}

function getAllQueued(db) {
  return new Promise((resolve, reject) => {
    const tx = db.transaction('requests', 'readonly')
    const store = tx.objectStore('requests')
    const req = store.getAll()
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => reject(req.error)
  })
}

function deleteQueued(db, id) {
  return new Promise((resolve, reject) => {
    const tx = db.transaction('requests', 'readwrite')
    const store = tx.objectStore('requests')
    const req = store.delete(id)
    req.onsuccess = () => resolve()
    req.onerror = () => reject(req.error)
  })
}

self.addEventListener('push', (event) => {
  if (!event.data) return
  const data = event.data.json()
  event.waitUntil(
    self.registration.showNotification(data.title || 'OpsMesh', {
      body: data.body || '',
      icon: '/enterprise/icon-192.png',
      badge: '/enterprise/badge-72.png',
      data: data.url || '/enterprise/'
    })
  )
})

self.addEventListener('notificationclick', (event) => {
  event.notification.close()
  event.waitUntil(
    self.clients.matchAll({ type: 'window' }).then((clientList) => {
      for (const client of clientList) {
        if (client.url.includes('/enterprise/') && 'focus' in client) {
          return client.focus()
        }
      }
      return self.clients.openWindow(event.notification.data)
    })
  )
})
