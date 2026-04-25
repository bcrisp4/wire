<script lang="ts">
	import { api } from '$lib/api';
	import type { Entry } from '$lib/types';

	type Props = { data: { entry: Entry } };
	let { data }: Props = $props();

	let entry = $derived(data.entry);

	// `published_at` is Unix seconds (or null) per the backend contract.
	let publishedDate = $derived(
		entry.published_at !== null ? new Date(entry.published_at * 1000) : null
	);
	let publishedLabel = $derived(publishedDate !== null ? publishedDate.toLocaleString() : '');
	let publishedISO = $derived(publishedDate !== null ? publishedDate.toISOString() : '');

	// Auto-mark-read when the entry id changes. SvelteKit reuses this
	// component when navigating between /entry/[id] URLs, so onMount only
	// fires for the first entry visited; an effect keyed on entry.id covers
	// subsequent navigations. The `marked` set guards against duplicate PUTs
	// for the same id (e.g. rapid back/forward). Failure is logged, not
	// surfaced — the read flag is incidental to the reading experience.
	const marked = new Set<number>();
	$effect(() => {
		const id = entry.id;
		if (entry.read || marked.has(id)) return;
		marked.add(id);
		api.put(`/entries/${id}`, { read: true }).catch((err) => {
			marked.delete(id);
			console.warn('failed to mark entry read', err);
		});
	});
</script>

<svelte:head>
	<title>{entry.title} — Wire</title>
</svelte:head>

<article class="reader">
	<header>
		<h1>{entry.title}</h1>
		<div class="meta">
			{#if entry.author}
				<span class="author">{entry.author}</span>
			{/if}
			{#if publishedLabel}
				<time datetime={publishedISO}>{publishedLabel}</time>
			{/if}
			{#if entry.reading_time > 0}
				<span class="reading-time">{entry.reading_time} min read</span>
			{/if}
		</div>
		{#if entry.url || entry.comments_url}
			<div class="links">
				{#if entry.url}
					<a class="external" href={entry.url} target="_blank" rel="noopener noreferrer">
						View original
					</a>
				{/if}
				{#if entry.comments_url}
					<a
						class="external"
						href={entry.comments_url}
						target="_blank"
						rel="noopener noreferrer"
					>
						Comments
					</a>
				{/if}
			</div>
		{/if}
	</header>

	<!--
		Content is sanitized server-side by internal/extract (allowlist-based
		HTML sanitizer adapted from miniflux). `{@html}` is safe here because
		that pipeline strips <script>/<style>, blocks unknown attributes, and
		rewrites external resource URLs.
	-->
	<div class="content">
		{#if entry.content}
			{@html entry.content}
		{:else if entry.summary}
			<p class="summary">{entry.summary}</p>
		{:else}
			<p class="empty">No content extracted for this entry.</p>
		{/if}
	</div>
</article>

<style>
	.reader {
		max-width: 42rem; /* ~672px — comfortable line length for prose */
		margin: 2.5rem auto;
		padding: 0 1.25rem;
		line-height: 1.6;
	}
	header {
		margin-bottom: 2rem;
	}
	h1 {
		margin: 0 0 0.75rem;
		font-size: 2rem;
		line-height: 1.2;
		letter-spacing: -0.02em;
	}
	.meta {
		display: flex;
		flex-wrap: wrap;
		gap: 0.75rem;
		font-size: 0.875rem;
		color: var(--fg-muted);
	}
	.links {
		margin-top: 0.75rem;
		display: flex;
		flex-wrap: wrap;
		gap: 0.75rem;
	}
	.links .external {
		font-size: 0.875rem;
		text-decoration: none;
		padding: 0.25rem 0.6rem;
		border: 1px solid var(--border);
		border-radius: 4px;
	}
	.links .external:hover {
		background: var(--surface);
	}
	.content {
		font-size: 1.0625rem;
	}
	.content :global(p) {
		margin: 0 0 1.1em;
	}
	.content :global(h2),
	.content :global(h3),
	.content :global(h4) {
		margin: 1.6em 0 0.5em;
		line-height: 1.25;
	}
	.content :global(blockquote) {
		margin: 1em 0;
		padding: 0.25em 1em;
		border-left: 3px solid var(--border);
		color: var(--fg-muted);
	}
	.content :global(pre) {
		overflow-x: auto;
		padding: 0.75em 1em;
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: 4px;
		font-size: 0.9em;
		line-height: 1.45;
	}
	.content :global(code) {
		font-size: 0.9em;
		background: var(--surface);
		padding: 0.1em 0.3em;
		border-radius: 3px;
		border: 1px solid var(--border);
	}
	.content :global(pre code) {
		background: transparent;
		padding: 0;
		border: 0;
	}
	.content :global(img),
	.content :global(video),
	.content :global(iframe) {
		max-width: 100%;
		height: auto;
	}
	.content :global(figure) {
		margin: 1.25em 0;
	}
	.content :global(figcaption) {
		font-size: 0.875rem;
		color: var(--fg-muted);
		margin-top: 0.4em;
	}
	.content :global(hr) {
		border: 0;
		border-top: 1px solid var(--border);
		margin: 2em 0;
	}
	.content :global(table) {
		border-collapse: collapse;
		width: 100%;
		margin: 1em 0;
	}
	.content :global(th),
	.content :global(td) {
		border: 1px solid var(--border);
		padding: 0.4em 0.6em;
		text-align: left;
	}
	.summary {
		color: var(--fg-muted);
	}
	.empty {
		color: var(--fg-muted);
		font-style: italic;
	}
</style>
