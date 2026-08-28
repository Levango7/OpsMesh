import { ref } from 'vue'

const pendingRequests = new Map()
const etagStore = new Map()
const memoryCache = new Map()

const DEFAULT_TTL = 30 * 1000
const MAX_CACHE_SIZE = 200

export function useFetch() {
  const loading = ref(false)
  const error = ref(null)
  const data = ref(null)

  async function fetchWithCache(url, options = {}) {
    const {
      ttl = DEFAULT_TTL,
      deduplicate = true,
      useETag = true,
      staleWhileRevalidate = false,
      signal,
      headers = {},
      ...fetchOptions
    } = options

    const cacheKey = `${options.method || 'GET'}:${url}`

    if (deduplicate && pendingRequests.has(cacheKey)) {
      return pendingRequests.get(cacheKey)
    }

    const cached = memoryCache.get(cacheKey)
    const now = Date.now()

    if (cached && now - cached.timestamp < ttl) {
      if (staleWhileRevalidate && now - cached.timestamp > ttl * 0.8) {
        revalidateInBackground(url, cacheKey, { ...options, headers })
      }
      return Promise.resolve(cached.data)
    }

    loading.value = true
    error.value = null

    const requestHeaders = { ...headers }
    if (useETag && etagStore.has(cacheKey)) {
      requestHeaders['If-None-Match'] = etagStore.get(cacheKey)
    }

    const requestPromise = fetch(url, {
      ...fetchOptions,
      headers: requestHeaders,
      signal
    })
      .then(async (response) => {
        if (response.status === 304 && cached) {
          cached.timestamp = now
          return cached.data
        }

        if (!response.ok) {
          throw new Error(`HTTP ${response.status}: ${response.statusText}`)
        }

        const etag = response.headers.get('etag')
        if (etag && useETag) {
          etagStore.set(cacheKey, etag)
        }

        const contentType = response.headers.get('content-type') || ''
        const result = contentType.includes('application/json') ? await response.json() : await response.text()

        memoryCache.set(cacheKey, { data: result, timestamp: now })
        trimCache(MAX_CACHE_SIZE)

        data.value = result
        return result
      })
      .catch((err) => {
        if (cached && staleWhileRevalidate) {
          return cached.data
        }
        error.value = err
        throw err
      })
      .finally(() => {
        pendingRequests.delete(cacheKey)
        loading.value = false
      })

    if (deduplicate) {
      pendingRequests.set(cacheKey, requestPromise)
    }

    return requestPromise
  }

  function invalidateCache(url, method = 'GET') {
    const cacheKey = `${method}:${url}`
    memoryCache.delete(cacheKey)
    etagStore.delete(cacheKey)
  }

  function clearAllCache() {
    memoryCache.clear()
    etagStore.clear()
  }

  return {
    loading,
    error,
    data,
    fetchWithCache,
    invalidateCache,
    clearAllCache
  }
}

function revalidateInBackground(url, cacheKey, options) {
  const { ttl, useETag, headers = {}, ...fetchOptions } = options
  const requestHeaders = { ...headers }

  if (useETag && etagStore.has(cacheKey)) {
    requestHeaders['If-None-Match'] = etagStore.get(cacheKey)
  }

  fetch(url, { ...fetchOptions, headers: requestHeaders })
    .then(async (response) => {
      if (response.status === 304) {
        const cached = memoryCache.get(cacheKey)
        if (cached) cached.timestamp = Date.now()
        return
      }
      if (!response.ok) return

      const etag = response.headers.get('etag')
      if (etag) etagStore.set(cacheKey, etag)

      const contentType = response.headers.get('content-type') || ''
      const result = contentType.includes('application/json') ? await response.json() : await response.text()
      memoryCache.set(cacheKey, { data: result, timestamp: Date.now() })
    })
    .catch(() => {})
}

function trimCache(maxSize) {
  if (memoryCache.size <= maxSize) return
  const entries = [...memoryCache.entries()]
  const toDelete = entries.slice(0, entries.length - maxSize)
  for (const [key] of toDelete) {
    memoryCache.delete(key)
    etagStore.delete(key)
  }
}
