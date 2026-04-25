// Wire TypeScript types mirroring the backend JSON shapes.
//
// Conventions:
//   - All timestamps are Unix seconds (integers). Use `new Date(ts * 1000)`
//     to convert to a JS Date.
//   - Nullable Go fields (`*T`) become `T | null` here.
//   - Field names match the JSON keys produced by the Go handlers.
//
// Sources of truth:
//   - internal/api/feeds.go      (feedJSON)
//   - internal/api/categories.go (categoryDTO + Unit 12-pre unread_count)
//   - internal/api/entries.go    (model.Entry, listResponse)
//   - internal/api/discover.go   (discoverCandidate)
//   - internal/api/opml.go       (import response)
//
// `unread_count` on Feed and Category is added by Unit 12-pre (landing in
// parallel with this unit). If the API hasn't shipped it yet, it will be
// `undefined` at runtime — but the SPA hasn't shipped either, so consumers
// can rely on the field being present by the time real users see it.

export type EntryStatus = 'unread' | 'read' | 'all';

export interface Feed {
	id: number;
	feed_url: string;
	site_url: string | null;
	title: string;
	description: string | null;
	category_id: number | null;
	icon_id: number | null;
	last_polled_at: number | null;
	next_poll_at: number | null;
	poll_interval: number;
	error_count: number;
	last_error: string | null;
	crawler: boolean;
	scraper_rules: string | null;
	disabled: boolean;
	ignore_entry_updates: boolean;
	created_at: number;
	updated_at: number;
	unread_count: number;
}

export interface Category {
	id: number;
	name: string;
	unread_count: number;
}

export interface Entry {
	id: number;
	feed_id: number;
	user_id: number;
	hash: string;
	title: string;
	url: string | null;
	comments_url: string | null;
	author: string | null;
	summary: string | null;
	content: string | null;
	published_at: number | null;
	reading_time: number;
	read: boolean;
	read_at: number | null;
	saved: boolean;
	saved_at: number | null;
	created_at: number;
	changed_at: number;
}

export interface EntryListResponse {
	entries: Entry[];
	total: number;
	limit: number;
	offset: number;
}

export interface DiscoverCandidate {
	url: string;
	title: string;
	type: 'rss' | 'atom' | 'json';
}

export interface DiscoverResponse {
	candidates: DiscoverCandidate[];
}

export interface OpmlImportResult {
	imported: number;
	skipped_duplicates: number;
	categories_created: number;
}
