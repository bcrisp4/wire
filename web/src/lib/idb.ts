// IndexedDB wrapper used by the page-side code to inspect the offline cache
// populated by `web/static/sw.js`. The schema is shared with the service
// worker — keep this file in lock-step with the SW's IDB helpers.
//
// Database `wire-offline`:
//   - `api-cache`        key: full request URL string
//                         value: { etag, body, contentType?, contentDisposition?, ts }
//   - `mutation-queue`   key: auto-incremented number
//                         value: { method, path, body, contentType?, ts }
//
// All functions are no-ops on environments without `indexedDB` (e.g. SSR
// during prerender / vitest jsdom without IDB shim) and resolve with safe
// defaults so callers can stay free of `typeof window` checks.

const DB_NAME = 'wire-offline';
const DB_VERSION = 1;
const API_CACHE = 'api-cache';
const MUTATION_QUEUE = 'mutation-queue';

export interface CachedApiResponse {
	etag: string;
	body: string;
	contentType?: string;
	contentDisposition?: string;
	ts: number;
}

export interface QueuedMutation {
	method: string;
	path: string;
	body: string;
	contentType?: string;
	ts: number;
}

function hasIdb(): boolean {
	return typeof indexedDB !== 'undefined';
}

// Cache the open-database promise so concurrent calls share one connection.
// We only invalidate on `versionchange` (another tab upgraded the schema) so
// the next call reopens with the new version.
let dbPromise: Promise<IDBDatabase> | null = null;

function openDb(): Promise<IDBDatabase> {
	if (dbPromise) return dbPromise;
	dbPromise = new Promise<IDBDatabase>((resolve, reject) => {
		const req = indexedDB.open(DB_NAME, DB_VERSION);
		req.onupgradeneeded = () => {
			const db = req.result;
			if (!db.objectStoreNames.contains(API_CACHE)) {
				db.createObjectStore(API_CACHE);
			}
			if (!db.objectStoreNames.contains(MUTATION_QUEUE)) {
				db.createObjectStore(MUTATION_QUEUE, { autoIncrement: true });
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

export async function cacheApiResponse(
	url: string,
	etag: string,
	body: string,
	contentType?: string,
	contentDisposition?: string
): Promise<void> {
	if (!hasIdb()) return;
	const db = await openDb();
	await new Promise<void>((resolve, reject) => {
		const tx = db.transaction(API_CACHE, 'readwrite');
		const value: CachedApiResponse = { etag, body, ts: Date.now() };
		if (contentType) value.contentType = contentType;
		if (contentDisposition) value.contentDisposition = contentDisposition;
		const req = tx.objectStore(API_CACHE).put(value, url);
		req.onsuccess = () => resolve();
		req.onerror = () => reject(req.error);
	});
}

export async function getCachedApiResponse(url: string): Promise<CachedApiResponse | null> {
	if (!hasIdb()) return null;
	const db = await openDb();
	return await new Promise<CachedApiResponse | null>((resolve, reject) => {
		const tx = db.transaction(API_CACHE, 'readonly');
		const req = tx.objectStore(API_CACHE).get(url);
		req.onsuccess = () => resolve((req.result as CachedApiResponse | undefined) ?? null);
		req.onerror = () => reject(req.error);
	});
}

export async function enqueueMutation(
	method: string,
	path: string,
	body: string,
	contentType?: string
): Promise<void> {
	if (!hasIdb()) return;
	const db = await openDb();
	await new Promise<void>((resolve, reject) => {
		const tx = db.transaction(MUTATION_QUEUE, 'readwrite');
		const value: QueuedMutation = { method, path, body, ts: Date.now() };
		if (contentType) value.contentType = contentType;
		const req = tx.objectStore(MUTATION_QUEUE).add(value);
		req.onsuccess = () => resolve();
		req.onerror = () => reject(req.error);
	});
}

// dequeueMutations returns and removes every queued mutation in a single
// read/write transaction. Callers replay them in order; if replay fails the
// caller is responsible for re-enqueueing — we don't re-insert here because
// that risks a tight loop on persistent failures.
export async function dequeueMutations(): Promise<QueuedMutation[]> {
	if (!hasIdb()) return [];
	const db = await openDb();
	return await new Promise<QueuedMutation[]>((resolve, reject) => {
		const tx = db.transaction(MUTATION_QUEUE, 'readwrite');
		const store = tx.objectStore(MUTATION_QUEUE);
		const out: QueuedMutation[] = [];
		const cursorReq = store.openCursor();
		cursorReq.onsuccess = () => {
			const cursor = cursorReq.result;
			if (cursor) {
				out.push(cursor.value as QueuedMutation);
				cursor.delete();
				cursor.continue();
			}
		};
		cursorReq.onerror = () => reject(cursorReq.error);
		tx.oncomplete = () => resolve(out);
		tx.onerror = () => reject(tx.error);
		tx.onabort = () => reject(tx.error);
	});
}
