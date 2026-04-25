import { api } from '$lib/api';
import type { Entry, EntryListResponse } from '$lib/types';

export const PAGE_LIMIT = 50;

export interface EntryListPage {
	readonly entries: Entry[];
	readonly total: number;
	readonly loading: boolean;
	readonly hasMore: boolean;
	loadMore: () => Promise<void>;
	markReadOnExpand: (id: number) => Promise<void>;
	toggleRead: (id: number) => Promise<void>;
	toggleSaved: (id: number) => Promise<void>;
}

// createEntryListPage owns the offset-paginated state for an entries view.
// `baseQuery` is the query string passed to /entries (without limit/offset),
// e.g. "status=all" or "saved=true". Initial data comes from a +page.ts
// load() so the first paint already has rows.
//
// `matches` is an optional predicate identifying which entries belong in the
// view. When a mutation flips an entry such that `matches(updated)` is false
// (e.g. unsaving on the Saved view), the entry is dropped from `entries` and
// `total` is decremented so it stops occupying a slot until refresh.
export function createEntryListPage(
	baseQuery: string,
	initial: EntryListResponse,
	matches?: (e: Entry) => boolean
): EntryListPage {
	let entries = $state<Entry[]>(initial.entries);
	let total = $state<number>(initial.total);
	let offset = $state<number>(initial.offset + initial.entries.length);
	let loading = $state<boolean>(false);

	function buildPath(off: number): string {
		const q = baseQuery ? `${baseQuery}&` : '';
		return `/entries?${q}limit=${PAGE_LIMIT}&offset=${off}`;
	}

	async function loadMore(): Promise<void> {
		if (loading) return;
		if (entries.length >= total) return;
		loading = true;
		try {
			const r = await api.get<EntryListResponse>(buildPath(offset));
			entries = [...entries, ...r.entries];
			total = r.total;
			offset = r.offset + r.entries.length;
		} finally {
			loading = false;
		}
	}

	async function patch(id: number, body: { read?: boolean; saved?: boolean }): Promise<void> {
		const updated = await api.put<Entry>(`/entries/${id}`, body);
		const i = entries.findIndex((e) => e.id === id);
		if (i < 0) return;
		if (matches && !matches(updated)) {
			entries = [...entries.slice(0, i), ...entries.slice(i + 1)];
			if (total > 0) total -= 1;
			if (offset > 0) offset -= 1;
			return;
		}
		entries[i] = updated;
	}

	async function markReadOnExpand(id: number): Promise<void> {
		const e = entries.find((x) => x.id === id);
		if (!e || e.read) return;
		await patch(id, { read: true });
	}

	async function toggleRead(id: number): Promise<void> {
		const e = entries.find((x) => x.id === id);
		if (!e) return;
		await patch(id, { read: !e.read });
	}

	async function toggleSaved(id: number): Promise<void> {
		const e = entries.find((x) => x.id === id);
		if (!e) return;
		await patch(id, { saved: !e.saved });
	}

	return {
		get entries() {
			return entries;
		},
		get total() {
			return total;
		},
		get loading() {
			return loading;
		},
		get hasMore() {
			return entries.length < total;
		},
		loadMore,
		markReadOnExpand,
		toggleRead,
		toggleSaved
	};
}
