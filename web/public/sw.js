/* The shell only. An installed copy should open without a network; uploads,
   previews and slices always come from the server, so nothing under /api is read
   from or written to this cache. */
const SHELL = 'tuqie-shell-v1';

// The one key the page itself is kept under, so a load is answered from it no
// matter which of the two spellings of the address was used.
const SHELL_URL = '/';

// The shell is the whole app offline: the page, the manifest it links, and the
// icons a home-screen launch draws before any of the rest is asked for.
const SHELL_FILES = [SHELL_URL, '/manifest.json', '/icons/icon-192.png', '/icons/maskable-192.png'];

// The probe below must reach the network rather than the copy this worker keeps.
const REVISION_QUERY = 'sw-revision';

self.addEventListener('install', (event) => {
  event.waitUntil(caches.open(SHELL).then(precache).then(() => self.skipWaiting()));
});

/**
 * Fill the cache before this worker takes over, so the first launch after an
 * install — which may be the one with no signal — has something to serve.
 * Nothing here is fatal: an update checked while offline keeps whatever the
 * previous copy left behind, and the files are re-stored on the next load
 * anyway.
 */
async function precache(cache) {
  await Promise.all(
    SHELL_FILES.map(async (path) => {
      try {
        const res = await fetch(path, { cache: 'no-store' });
        // A 401 from an instance that asks for a password, or a 404, must not be
        // mistaken for the shell and served in its place.
        if (res.ok) await cache.put(path, res);
      } catch {
        /* unreachable: leave the stored copy alone */
      }
    }),
  );
}

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== SHELL).map((k) => caches.delete(k))))
      .then(() => self.clients.claim())
      .then(syncFromServer),
  );
});

/**
 * Ask the server which files the page names now. Used on activation, where there
 * is no page load to read it off; a page load syncs from its own response.
 */
async function syncFromServer() {
  let html;
  try {
    const res = await fetch(`/?${REVISION_QUERY}=1`, { cache: 'no-store' });
    if (!res.ok) return;
    html = await res.text();
  } catch {
    return;
  }
  await syncBuildFiles(html);
}

/**
 * Bring the cache to exactly the build this page was served from.
 *
 * The install precache cannot name these files — a hash is only known once vite
 * has run — and a first visit does not reach them through this worker either,
 * since the page that installs it is the one the network just loaded. So without
 * this step the bundle is only stored on the second visit, and an app installed
 * the day before a train is offline still has nothing to boot from.
 *
 * The build files are content-hashed, so an old one is never wrong; but nothing
 * names them obsolete either, and every deploy would leave another copy behind in
 * CacheStorage for good. Hence the pass runs on every load rather than only when
 * this worker changes: the ordinary deploy touches no line of sw.js, so an
 * activation is exactly the event that does not come.
 *
 * A failed fetch leaves the cache alone: keeping files that are no longer used is
 * harmless, losing the offline shell is not.
 */
async function syncBuildFiles(html) {
  const named = [...html.matchAll(/\/assets\/[^"'\s]+/g)].map((m) => m[0]);
  if (named.length === 0) return;
  const used = new Set(named);

  const cache = await caches.open(SHELL);
  const stored = await cache.keys();
  await Promise.all([
    ...named.map(async (path) => {
      if (await cache.match(path)) return;
      try {
        const res = await fetch(path, { cache: 'no-store' });
        if (res.ok) await cache.put(path, res);
      } catch {
        /* no network to fetch it with: what this device has is what it has */
      }
    }),
    ...stored
      .filter((req) => new URL(req.url).pathname.startsWith('/assets/') && !used.has(new URL(req.url).pathname))
      .map((req) => cache.delete(req)),
  ]);
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
  // instance is reachable.
  if (req.mode === 'navigate' || req.destination === 'document') {
    // Only the page, though: a document that happens to be a picture has nothing
    // to do in the shell's slot.
    if (url.pathname !== SHELL_URL && url.pathname !== '/index.html') return;
    event.respondWith(loadShell(req, event));
    return;
  }

  // The build files carry their own identity in their names, so a stored copy
  // cannot be the wrong one and the network would only add a wait.
  if (url.pathname.startsWith('/assets/')) {
    event.respondWith(
      caches.match(req).then((hit) => hit || fetchAndStore(req, event)),
    );
    return;
  }

  // Everything else — the manifest, the icons — keeps the same name across
  // builds, so the server gets the first chance at it and only a load that cannot
  // reach one falls back to the stored copy.
  event.respondWith(fetchAndStore(req, event).catch(() => orOffline(req)));
});

/**
 * The page from the server if it answers with the page, otherwise the copy this
 * install is keeping.
 *
 * Anything the server answers that is not the page falls back too. A password
 * protected instance replies 401 before a browser has carried its credentials
 * into an installed window, and a worker-served response never raises the
 * browser's own password prompt — so serving the stored shell is what leaves the
 * app able to ask: the first call it makes to /api does bring the prompt up.
 */
async function loadShell(req, event) {
  let res;
  try {
    res = await fetch(req);
  } catch {
    res = null;
  }
  if (res && res.ok) {
    // The page that just arrived names the build it wants, which is the same
    // information the activation probe has to go and fetch: the new files are put
    // in the cache and the ones nobody names any more come out. Kept off the
    // response itself so the load is not waiting on it.
    const html = await res.clone().text();
    event.waitUntil(store(SHELL_URL, res.clone()).then(() => syncBuildFiles(html)));
    return res;
  }
  const stored = await caches.match(SHELL_URL);
  if (stored) return stored;
  if (res) return res;
  // Nothing to fall back to: say so as a failed load rather than answering with
  // an empty response, which is a blank page that looks like the app broke.
  throw new Error('图切没有可离线的副本');
}

async function fetchAndStore(req, event) {
  const res = await fetch(req);
  if (res.ok) {
    const copy = res.clone();
    // Handed to waitUntil: the worker is free to stop the moment the response has
    // been delivered, and a put left in the air is one that never happened.
    event.waitUntil(store(req, copy));
  }
  return res;
}

async function orOffline(req) {
  const stored = await caches.match(req);
  if (stored) return stored;
  throw new Error('图切没有可离线的副本');
}

/**
 * Putting through here keeps every write in the one promise chain the browser is
 * made to wait for, instead of a floating then nobody holds on to.
 */
async function store(key, res) {
  const cache = await caches.open(SHELL);
  await cache.put(key, res);
}
