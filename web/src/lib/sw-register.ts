// registerServiceWorker installs `web/static/sw.js` once in the browser. Call
// from a layout's `onMount` so we don't run during SSR / prerender — the
// `static` adapter runs the page code at build time and `navigator` is
// undefined there.
//
// The function is idempotent: calling it twice resolves to the same
// registration without issuing a second `register()` call. It also wires up
// (once) a `window 'online'` listener that pokes the active SW to drain its
// mutation queue. Service workers are commonly terminated while idle and
// don't reliably observe `'online'` themselves; the page-side nudge is what
// guarantees queued mutations replay after connectivity returns.

let registered: Promise<ServiceWorkerRegistration | null> | null = null;
let onlineHookInstalled = false;

function installOnlineHook(): void {
	if (onlineHookInstalled) return;
	if (typeof window === 'undefined' || typeof navigator === 'undefined') return;
	if (!('serviceWorker' in navigator)) return;
	onlineHookInstalled = true;
	const nudge = () => {
		// `controller` is null for the very first load after registration; in
		// that case there's nothing to drain anyway. `ready` resolves to the
		// active registration regardless.
		navigator.serviceWorker.ready
			.then((reg) => reg.active?.postMessage('wire:replay-mutations'))
			.catch(() => {
				/* SW unavailable — nothing to do */
			});
	};
	window.addEventListener('online', nudge);
	// Fire once on registration so a queue accumulated while the page was
	// loading drains as soon as the SW activates.
	if (navigator.onLine) nudge();
}

export function registerServiceWorker(): Promise<ServiceWorkerRegistration | null> {
	if (registered) return registered;
	if (typeof navigator === 'undefined' || !('serviceWorker' in navigator)) {
		registered = Promise.resolve(null);
		return registered;
	}
	registered = navigator.serviceWorker
		.register('/sw.js')
		.then((reg) => {
			console.log('[wire] service worker registered', reg.scope);
			installOnlineHook();
			return reg;
		})
		.catch((err: unknown) => {
			console.error('[wire] service worker registration failed', err);
			return null;
		});
	return registered;
}
