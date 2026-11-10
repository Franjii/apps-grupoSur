const CACHE_NAME = 'inventory-v2';

const PRECACHE_URLS = [
  '/static/css/styles.css',
  '/static/js/htmx.min.js',
  '/static/images/logo.png',
  '/static/images/simple-logo.png',
  '/static/images/icons/icon-192x192.png',
  '/static/images/icons/icon-512x512.png',
  '/static/offline.html'
];

// Install: pre-cache static assets
self.addEventListener('install', function(event) {
  event.waitUntil(
    caches.open(CACHE_NAME).then(function(cache) {
      return cache.addAll(PRECACHE_URLS);
    }).then(function() {
      return self.skipWaiting();
    })
  );
});

// Activate: clean up old caches
self.addEventListener('activate', function(event) {
  event.waitUntil(
    caches.keys().then(function(names) {
      return Promise.all(
        names.filter(function(name) {
          return name !== CACHE_NAME;
        }).map(function(name) {
          return caches.delete(name);
        })
      );
    }).then(function() {
      return self.clients.claim();
    })
  );
});

// Fetch: routing strategy
self.addEventListener('fetch', function(event) {
  var request = event.request;

  // 1. Non-GET requests (POST, PUT, DELETE): pass through
  if (request.method !== 'GET') {
    return;
  }

  // 2. HTMX requests: always fresh from network (fragments with CSRF)
  if (request.headers.get('HX-Request') === 'true') {
    return;
  }

  // 3. Navigation requests: network-first, offline fallback
  if (request.mode === 'navigate') {
    event.respondWith(
      fetch(request).catch(function() {
        return caches.match('/static/offline.html');
      })
    );
    return;
  }

  // 4. Static assets (/static/*): cache-first with runtime caching
  var url = new URL(request.url);
  if (url.pathname.startsWith('/static/')) {
    event.respondWith(
      caches.match(request).then(function(cached) {
        if (cached) {
          return cached;
        }
        return fetch(request).then(function(response) {
          if (response.ok) {
            var clone = response.clone();
            caches.open(CACHE_NAME).then(function(cache) {
              cache.put(request, clone);
            });
          }
          return response;
        });
      })
    );
    return;
  }

  // 5. Everything else: pass through to network
});
