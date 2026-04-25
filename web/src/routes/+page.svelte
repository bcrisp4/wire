<script lang="ts">
	import { untrack } from 'svelte';
	import EntryList from '$lib/components/EntryList.svelte';
	import { api } from '$lib/api';
	import type { Entry, EntryListResponse } from '$lib/types';
	import type { PageProps } from './$types';
	import { PAGE_SIZE } from './+page';

	let { data }: PageProps = $props();

	let entries = $state<Entry[]>(untrack(() => data.initial.entries.slice()));
	let total = $state(untrack(() => data.initial.total));
	let nextOffset = $state(
		untrack(() => data.initial.offset + data.initial.entries.length)
	);
	let loading = $state(false);

	let hasMore = $derived(nextOffset < total);

	// Per-entry generation counter so a stale rollback can't clobber a newer
	// successful change when two updates overlap.
	const updateGen = new Map<number, number>();

	async function loadMore() {
		if (loading || !hasMore) return;
		loading = true;
		try {
			const res = await api.get<EntryListResponse>(
				`/entries?status=unread&limit=${PAGE_SIZE}&offset=${nextOffset}`
			);
			entries = [...entries, ...res.entries];
			total = res.total;
			nextOffset += res.entries.length;
		} catch (e) {
			console.error('loadMore failed', e);
		} finally {
			loading = false;
		}
	}

	async function update(entry: Entry, patch: { read?: boolean; saved?: boolean }) {
		const before = { read: entry.read, saved: entry.saved };
		const gen = (updateGen.get(entry.id) ?? 0) + 1;
		updateGen.set(entry.id, gen);
		Object.assign(entry, patch);
		try {
			const updated = await api.put<Entry>(`/entries/${entry.id}`, patch);
			if (updateGen.get(entry.id) === gen) {
				Object.assign(entry, updated);
			}
		} catch (e) {
			if (updateGen.get(entry.id) === gen) {
				Object.assign(entry, before);
			}
			console.error('entry update failed', e);
		}
	}

	function markRead(entry: Entry) {
		if (!entry.read) update(entry, { read: true });
	}
	const toggleRead = (entry: Entry) => update(entry, { read: !entry.read });
	const toggleSaved = (entry: Entry) => update(entry, { saved: !entry.saved });
</script>

<section class="river">
	<header>
		<h1>River</h1>
		<p class="muted">{total} unread</p>
	</header>

	<EntryList
		{entries}
		{loading}
		{hasMore}
		onLoadMore={loadMore}
		onexpand={markRead}
		onmarkread={toggleRead}
		ontogglesaved={toggleSaved}
	/>
</section>

<style>
	.river {
		max-width: 48rem;
		margin: 2rem auto;
		padding: 0 1rem;
	}
	header {
		display: flex;
		align-items: baseline;
		justify-content: space-between;
		margin-bottom: 1rem;
	}
	h1 {
		margin: 0;
		font-size: 1.5rem;
	}
	.muted {
		color: var(--fg-muted);
		margin: 0;
		font-size: 0.9rem;
	}
</style>
