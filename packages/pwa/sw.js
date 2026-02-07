const CACHE_NAME = "voice-relay-v2";

self.addEventListener("install", (event) => {
  self.skipWaiting();
});

self.addEventListener("fetch", (event) => {
  // Only cache GET requests for static assets
  if (event.request.method !== "GET") {
    return;
  }

  // Don't cache API, WebSocket, or dynamic requests
  const url = new URL(event.request.url);
  if (url.pathname.startsWith("/machines") ||
      url.pathname.startsWith("/transcribe") ||
      url.pathname.startsWith("/tts-") ||
      url.pathname.startsWith("/send-text") ||
      url.pathname.startsWith("/ws") ||
      url.pathname.startsWith("/health")) {
    return;
  }

  // Network-first: always try fresh, fall back to cache for offline
  event.respondWith(
    fetch(event.request)
      .then((response) => {
        const clone = response.clone();
        caches.open(CACHE_NAME).then((cache) => cache.put(event.request, clone));
        return response;
      })
      .catch(() => caches.match(event.request))
  );
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches.keys().then((keys) => {
      return Promise.all(
        keys
          .filter((key) => key !== CACHE_NAME)
          .map((key) => caches.delete(key))
      );
    }).then(() => self.clients.claim())
  );
});
