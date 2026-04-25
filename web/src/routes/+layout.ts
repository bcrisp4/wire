// SPA mode — no SSR, no prerendering. The Go server serves index.html for all
// non-/api paths and the client router takes over.
export const ssr = false;
export const prerender = false;
