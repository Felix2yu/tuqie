/* The shell only. An installed copy should open without a network; uploads,
   previews and slices always come from the server, so nothing under /api is read
   from or written to this cache. */
const SHELL = 'tuqie-shell-v1';

// The probe below must reach the network rather than the copy this worker keeps.
const REVISION_QUERY = 'sw-revision';

self.addEventListener('install', () => self.skipWaiting());

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== SHELL).map((k) => caches.delete(k))))
      .then(() => self.clients.claim())
      .then(dropUnreferencedAssets),
  );
});

/**
 * The build files are content-hashed, so an old one is never wrong — but nothing
 * names them obsolete either, and every deploy would leave another copy behind in
 * CacheStorage for good. Ask the server which files the page uses now and drop
 * the rest. A failed request leaves the cache alone: keeping files that are no
 * longer used is harmless, losing the offline shell is not.
 */
async function dropUnreferencedAssets() {
  let html;
  try {
    const res = await fetch(`/?${REVISION_QUERY}=1`, { cache: 'no-store' });
    if (!res.ok) return;
    html = await res.text();
  } catch {
    return;
  }

  const used = new Set([...html.matchAll(/\/assets\/[^"'\s]+/g)].map((m) => m[0]));
  if (used.size === 0) return;

  const cache = await caches.open(SHELL);
  const stored = await cache.keys();
  await Promise.all(
    stored
      .filter((req) => new URL(req.url).pathname.startsWith('/assets/') && !used.has(new URL(req.url).pathname))
      .map((req) => cache.delete(req)),
  );
}

self.addEventListener('fetch', (event) => {
  const req = event.request;
  const url = new URL(req.url);
  if (req.method !== 'GET' || url.origin !== self.location.origin || url.pathname.startsWith('/api/')) {
    return;
  }
  if (url.searchParams.has(REVISION_QUERY)) {
    return;
  }

  // A page load asks the server first so a new deploy is picked up as soon as the
  // instance is reachable; the stored copy is the offline fallback.
  if (req.mode === 'navigate' || req.destination === 'document') {
    event.respondWith(
      fetch(req)
        .then((res) => {
          if (res.ok) {
            const copy = res.clone();
            caches.open(SHELL).then((c) => c.put('/index.html', copy));
          }
          return res;
        })
        .catch(() => caches.match('/index.html')),
    );
    return;
  }

  // The rest are content-hashed build files, so a cached copy cannot go stale.
  event.respondWith(
    caches.match(req).then(
      (hit) =>
        hit ||
        fetch(req).then((res) => {
          if (res.ok) {
            const copy = res.clone();
            caches.open(SHELL).then((c) => c.put(req, copy));
          }
          return res;
        }),
    ),
  );
});
