<script lang="ts">
	import EntryList from '$lib/components/EntryList.svelte';
	import { createEntryListPage } from '$lib/entryListPage.svelte';

	let { data } = $props();

	const page = $derived(createEntryListPage('status=all', data.initial));
</script>

<section class="page">
	<header>
		<h1>All</h1>
	</header>

	<EntryList
		entries={page.entries}
		loading={page.loading}
		hasMore={page.hasMore}
		onLoadMore={page.loadMore}
		onExpand={page.markReadOnExpand}
		onMarkRead={page.toggleRead}
		onToggleSaved={page.toggleSaved}
	/>
</section>

<style>
	.page {
		max-width: 48rem;
		margin: 1.5rem auto;
		padding: 0 1rem;
	}
	header h1 {
		margin: 0 0 1rem;
		font-size: 1.5rem;
		letter-spacing: -0.01em;
	}
</style>
