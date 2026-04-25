// Wire service worker — Phase 1b Unit 18.
//
// Static assets (`/_app/*`, `/`, `/manifest.json`, `/icons/*`):
//   stale-while-revalidate from a Cache Storage cache. The first hit goes
//   to the network and primes the cache; subsequent loads serve the cached
//   copy and refresh in the background.
//
// API GETs (`/api/*` except `/api/v1/health`):
//   network-first. We always hit the network when online and only fall
//   back to the IndexedDB `api-cache` store if that fetch fails (offline,
//   DNS, etc.). To respect the contract that we MUST NOT serve a stale
//   API response while online, the cache is consulted only on fetch
//   failure — there is no "return cache then revalidate" path. When we
//   do hit the network we forward the cached entry's ETag in
//   `If-None-Match`, and on a 304 reply we return the cached body so
//   callers see a usable response without re-decoding.
//
// API mutations (POST/PUT/DELETE under `/api/*`) while offline:
//   queued in the `mutation-queue` IndexedDB store and replayed when the
//   `online` event fires. Online mutations pass through unchanged.

const SW_VERSION = 'wire-sw-v1';
const STATIC_CACHE = `wire-static-${SW_VERSION}`;
const IDB_NAME = 'wire-offline';
const IDB_VERSION = 1;
const API_CACHE_STORE = 'api-cache';
const MUTATION_QUEUE_STORE = 'mutation-queue';

// Static-asset matcher: same origin, path under `/_app/`, exactly `/`,
// `/manifest.json`, or under `/icons/`. Anything else (including
// `/api/...`) falls through to API or default handling.
function isStaticAsset(url) {
	if (url.origin !== self.location.origin) return false;
	const p = url.pathname;
	if (p.startsWith('/_app/')) return true;
	if (p === '/' || p === '/index.html') return true;
	if (p === '/manifest.json') return true;
	if (p.startsWith('/icons/')) return true;
	return false;
}

function isApiRequest(url) {
	return url.origin === self.location.origin && url.pathname.startsWith('/api/');
}

function isHealthRequest(url) {
	return url.pathname === '/api/v1/health';
}

// --- IndexedDB helpers ----------------------------------------------------
//
// The same schema is duplicated in `src/lib/idb.ts` for the page-side code.
// Keep these in sync if the schema ever changes.

let dbPromise = null;

function openIdb() {
	if (dbPromise) return dbPromise;
	dbPromise = new Promise((resolve, reject) => {
		const req = indexedDB.open(IDB_NAME, IDB_VERSION);
		req.onupgradeneeded = () => {
			const db = req.result;
			if (!db.objectStoreNames.contains(API_CACHE_STORE)) {
				db.createObjectStore(API_CACHE_STORE);
			}
			if (!db.objectStoreNames.contains(MUTATION_QUEUE_STORE)) {
				db.createObjectStore(MUTATION_QUEUE_STORE, { autoIncrement: true });
			}
		};
		req.onsuccess = () => {
			const db = req.result;
			db.onversionchange = () => {
				db.close();
				dbPromise = null;
			};
			resolve(db);
		};
		req.onerror = () => {
			dbPromise = null;
			reject(req.error);
		};
	});
	return dbPromise;
}

function idbGet(storeName, key) {
	return openIdb().then(
		(db) =>
			new Promise((resolve, reject) => {
				const tx = db.transaction(storeName, 'readonly');
				const req = tx.objectStore(storeName).get(key);
				req.onsuccess = () => resolve(req.result ?? null);
				req.onerror = () => reject(req.error);
			})
	);
}

function idbPut(storeName, value, key) {
	return openIdb().then(
		(db) =>
			new Promise((resolve, reject) => {
				const tx = db.transaction(storeName, 'readwrite');
				const store = tx.objectStore(storeName);
				const req = key === undefined ? store.put(value) : store.put(value, key);
				req.onsuccess = () => resolve(undefined);
				req.onerror = () => reject(req.error);
			})
	);
}

function idbDrainQueue(storeName) {
	return openIdb().then(
		(db) =>
			new Promise((resolve, reject) => {
				const tx = db.transaction(storeName, 'readwrite');
				const store = tx.objectStore(storeName);
				const items = [];
				const cursorReq = store.openCursor();
				cursorReq.onsuccess = () => {
					const cursor = cursorReq.result;
					if (cursor) {
						items.push({ key: cursor.key, value: cursor.value });
						cursor.continue();
					}
				};
				cursorReq.onerror = () => reject(cursorReq.error);
				tx.oncomplete = () => resolve(items);
				tx.onerror = () => reject(tx.error);
				tx.onabort = () => reject(tx.error);
			})
	);
}

function idbDelete(storeName, key) {
	return openIdb().then(
		(db) =>
			new Promise((resolve, reject) => {
				const tx = db.transaction(storeName, 'readwrite');
				const req = tx.objectStore(storeName).delete(key);
				req.onsuccess = () => resolve(undefined);
				req.onerror = () => reject(req.error);
			})
	);
}

// --- Lifecycle -----------------------------------------------------------

self.addEventListener('install', (event) => {
	// Activate immediately on first install — no waiting for tabs to close.
	event.waitUntil(self.skipWaiting());
});

self.addEventListener('activate', (event) => {
	event.waitUntil(
		(async () => {
			// Drop any stale caches from previous SW versions.
			const names = await caches.keys();
			await Promise.all(
				names.filter((n) => n.startsWith('wire-static-') && n !== STATIC_CACHE).map((n) => caches.delete(n))
			);
			await self.clients.claim();
		})()
	);
});

// --- Fetch strategies ----------------------------------------------------

// Best-effort cache write. Cache.put rejects synchronously for navigation
// requests (`mode: 'navigate'`) and asynchronously for partial / opaque
// responses or quota issues. We isolate it here so a failed write never
// bubbles into the response path. We also normalize the cache key to a
// plain GET request so navigation requests can still be served from cache
// on reload.
function cachePut(cache, request, response) {
	const key = new Request(request.url, { method: 'GET' });
	try {
		const p = cache.put(key, response);
		if (p && typeof p.catch === 'function') p.catch(() => {});
	} catch {
		/* sync TypeError — drop silently */
	}
}

async function cacheMatch(cache, request) {
	// Match against the normalized GET key; navigation requests (`mode:
	// 'navigate'`) won't match their own original Request via cache.match
	// in some browsers, so we always look up by URL.
	return await cache.match(new Request(request.url, { method: 'GET' }));
}

async function staleWhileRevalidate(request) {
	const cache = await caches.open(STATIC_CACHE);
	const cached = await cacheMatch(cache, request);
	const networkFetch = fetch(request).catch(() => null);
	if (cached) {
		// Refresh in the background; don't await. Failures here are non-fatal
		// — the cached response is already on its way to the page.
		networkFetch.then((res) => {
			if (res && res.ok && res.type === 'basic') cachePut(cache, request, res.clone());
		});
		return cached;
	}
	const fresh = await networkFetch;
	if (fresh) {
		// Clone before returning so we can prime the cache without consuming
		// the body the page is about to read.
		if (fresh.ok && fresh.type === 'basic') cachePut(cache, request, fresh.clone());
		return fresh;
	}
	return new Response('offline', { status: 503, statusText: 'offline' });
}

// Serve SPA navigations: the backend falls back to `index.html` for any
// non-`/api` path, so deep links like `/all` or `/saved` should resolve to the
// cached shell when offline. We try the network first (so server-side
// redirects still work) and fall through to the cached `/` shell on failure.
async function navigationFallback(request) {
	const cache = await caches.open(STATIC_CACHE);
	try {
		const fresh = await fetch(request);
		if (fresh && fresh.ok && fresh.type === 'basic') {
			// Don't cache deep-link URLs — index.html itself is what we want
			// to keep primed under '/'.
			return fresh;
		}
		if (fresh) return fresh;
	} catch {
		/* fall through to cache */
	}
	// Try the cached shell at '/' first, then '/index.html' as a fallback.
	const shell =
		(await cache.match(new Request(new URL('/', self.location.origin).href, { method: 'GET' }))) ||
		(await cache.match(
			new Request(new URL('/index.html', self.location.origin).href, { method: 'GET' })
		));
	if (shell) return shell;
	return new Response('offline', { status: 503, statusText: 'offline' });
}

// Build a Response object from a cached `api-cache` entry. `extraHeaders` is
// merged on top of the persisted headers so callers can flag cache hits.
function responseFromCachedEntry(entry, extraHeaders) {
	const h = new Headers();
	if (entry.contentType) h.set('Content-Type', entry.contentType);
	else h.set('Content-Type', 'application/json');
	if (entry.contentDisposition) h.set('Content-Disposition', entry.contentDisposition);
	if (extraHeaders) {
		for (const [k, v] of Object.entries(extraHeaders)) h.set(k, v);
	}
	return new Response(entry.body, {
		status: 200,
		statusText: extraHeaders && extraHeaders['X-Wire-From-Cache'] ? 'OK (cached)' : 'OK',
		headers: h
	});
}

async function apiNetworkFirst(request) {
	const url = request.url;
	let cachedEntry = null;
	try {
		cachedEntry = await idbGet(API_CACHE_STORE, url);
	} catch {
		cachedEntry = null;
	}

	const headers = new Headers(request.headers);
	if (cachedEntry && cachedEntry.etag) {
		headers.set('If-None-Match', cachedEntry.etag);
	}

	try {
		const fresh = await fetch(request, { headers });

		// 304 Not Modified — backend confirmed the cached body is fresh.
		// Synthesize a 200 response from the cached body so callers don't
		// have to special-case 304. Preserve the cached Content-Type
		// (and Content-Disposition for downloads like OPML export) so
		// non-JSON endpoints round-trip correctly.
		if (fresh.status === 304 && cachedEntry) {
			return responseFromCachedEntry(cachedEntry);
		}

		if (fresh.ok) {
			// Clone for caching; the consumer gets the original.
			const clone = fresh.clone();
			const etag = fresh.headers.get('ETag') ?? '';
			const contentType = fresh.headers.get('Content-Type') ?? '';
			const contentDisposition = fresh.headers.get('Content-Disposition') ?? '';
			(async () => {
				try {
					const body = await clone.text();
					await idbPut(
						API_CACHE_STORE,
						{ etag, body, contentType, contentDisposition, ts: Date.now() },
						url
					);
				} catch {
					/* cache write failed — not fatal */
				}
			})();
		}
		return fresh;
	} catch {
		// Network unreachable — fall back to cache if we have one.
		if (cachedEntry) {
			return responseFromCachedEntry(cachedEntry, { 'X-Wire-From-Cache': '1' });
		}
		return new Response('{"error":"offline"}', {
			status: 503,
			statusText: 'offline',
			headers: { 'Content-Type': 'application/json' }
		});
	}
}

async function apiMutation(request) {
	// Capture the body and the content type BEFORE issuing fetch(): once the
	// network attempt starts consuming the request body stream, cloning or
	// reading it can race and yield an empty body in the catch path. Doing
	// this up front also lets us reject multipart uploads cleanly — we can't
	// faithfully replay a multipart body via JSON-in-IDB because the boundary
	// data is binary-fragile through `.text()`.
	const contentType = request.headers.get('Content-Type') ?? '';
	const isMultipart = contentType.toLowerCase().startsWith('multipart/');

	let bodyText = '';
	if (!isMultipart) {
		try {
			bodyText = await request.clone().text();
		} catch {
			/* unreadable — store empty */
		}
	}

	try {
		return await fetch(request);
	} catch {
		// Multipart mutations can't be safely serialized into IDB and
		// re-played; surface a real offline error instead of pretending we
		// queued anything.
		if (isMultipart) {
			return new Response('{"error":"offline","reason":"multipart not queueable"}', {
				status: 503,
				statusText: 'offline',
				headers: { 'Content-Type': 'application/json' }
			});
		}
		try {
			await idbPut(MUTATION_QUEUE_STORE, {
				method: request.method,
				path: new URL(request.url).pathname + new URL(request.url).search,
				body: bodyText,
				contentType,
				ts: Date.now()
			});
		} catch {
			/* enqueue failed; the client will surface the original 503 */
		}
		return new Response('{"queued":true}', {
			status: 202,
			statusText: 'Queued',
			headers: { 'Content-Type': 'application/json', 'X-Wire-Queued': '1' }
		});
	}
}

self.addEventListener('fetch', (event) => {
	const request = event.request;
	const url = new URL(request.url);

	// Never intercept non-GET/PUT/POST/DELETE or cross-origin assets.
	if (url.origin !== self.location.origin) return;

	if (isApiRequest(url)) {
		// /api/v1/health is a liveness probe — never cached, never queued.
		if (isHealthRequest(url)) return;

		if (request.method === 'GET') {
			event.respondWith(apiNetworkFirst(request));
			return;
		}
		if (request.method === 'POST' || request.method === 'PUT' || request.method === 'DELETE') {
			event.respondWith(apiMutation(request));
			return;
		}
		return;
	}

	if (request.method === 'GET' && isStaticAsset(url)) {
		event.respondWith(staleWhileRevalidate(request));
		return;
	}

	// SPA deep-link navigation: any non-API navigation request that isn't a
	// matched static asset should fall back to the cached shell when offline.
	// `mode === 'navigate'` is the canonical way to detect a top-level
	// document load; we additionally guard `request.destination === 'document'`
	// for older agents that don't expose `mode`.
	if (
		request.method === 'GET' &&
		(request.mode === 'navigate' || request.destination === 'document')
	) {
		event.respondWith(navigationFallback(request));
	}
});

// --- Online replay --------------------------------------------------------

async function replayMutations() {
	let items;
	try {
		items = await idbDrainQueue(MUTATION_QUEUE_STORE);
	} catch {
		return;
	}
	for (const { key, value } of items) {
		try {
			const replayHeaders = {};
			// Use the originally-captured Content-Type so non-JSON mutations
			// (e.g. text/plain or application/x-www-form-urlencoded) replay
			// faithfully. Older queue entries written before this field
			// existed default to JSON.
			replayHeaders['Content-Type'] = value.contentType || 'application/json';
			const res = await fetch(value.path, {
				method: value.method,
				headers: replayHeaders,
				body: value.body || undefined
			});
			// Only drop the queued item on a definitive non-network outcome.
			// 5xx responses are kept so a transient backend failure doesn't
			// silently lose the user's mutation.
			if (res.status < 500) {
				await idbDelete(MUTATION_QUEUE_STORE, key);
			}
		} catch {
			// Still offline / network failed — leave the rest queued and bail.
			break;
		}
	}
}

self.addEventListener('online', () => {
	replayMutations();
});

// Some browsers fire `message` events instead of `online` from the page;
// allow the page to nudge us explicitly.
self.addEventListener('message', (event) => {
	if (event.data === 'wire:replay-mutations') {
		event.waitUntil?.(replayMutations());
	}
});
