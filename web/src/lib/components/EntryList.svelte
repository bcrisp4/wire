<script lang="ts">
	import type { Entry } from '$lib/types';
	import EntryCard from './EntryCard.svelte';

	type Props = {
		entries: Entry[];
		loading: boolean;
		hasMore: boolean;
		onLoadMore?: () => void;
	};

	let { entries, loading, hasMore, onLoadMore }: Props = $props();

	let expandedID = $state<number | null>(null);

	// `IntersectionObserver` is wired via an attachment so the observer is
	// torn down when the sentinel unmounts (e.g. when `hasMore` becomes false).
	// Guard against re-firing while a load is already in flight: the observer
	// keeps emitting `isIntersecting` for as long as the sentinel stays in
	// view, so without this we'd fire `onLoadMore` once per scroll frame.
	function sentinel(node: Element) {
		const io = new IntersectionObserver((entries) => {
			for (const entry of entries) {
				if (entry.isIntersecting && !loading && hasMore) {
					onLoadMore?.();
				}
			}
		});
		io.observe(node);
		return () => io.disconnect();
	}
</script>

<div class="entry-list">
	{#each entries as entry (entry.id)}
		<EntryCard
			{entry}
			expanded={expandedID === entry.id}
			ontoggle={() => (expandedID = expandedID === entry.id ? null : entry.id)}
		/>
	{/each}

	{#if hasMore}
		<div class="sentinel" {@attach sentinel} aria-hidden="true"></div>
	{/if}

	{#if loading}
		<p class="status" role="status">Loading…</p>
	{:else if entries.length === 0}
		<p class="status">No entries.</p>
	{/if}
</div>

<style>
	.entry-list {
		display: flex;
		flex-direction: column;
	}
	.sentinel {
		height: 1px;
		width: 100%;
	}
	.status {
		text-align: center;
		color: var(--fg-muted);
		padding: 1rem;
	}
</style>
