<script lang="ts">
	import type { Entry } from '$lib/types';

	type Props = {
		entry: Entry;
		expanded: boolean;
		ontoggle?: () => void;
		onmarkread?: () => void;
		ontogglesaved?: () => void;
	};

	let { entry, expanded, ontoggle, onmarkread, ontogglesaved }: Props = $props();

	// `published_at` is Unix seconds (or null). Render as locale string.
	let publishedLabel = $derived(
		entry.published_at !== null ? new Date(entry.published_at * 1000).toLocaleString() : ''
	);
</script>

<article class="entry" class:read={entry.read} class:expanded>
	<header>
		<button
			type="button"
			class="title"
			onclick={() => ontoggle?.()}
			aria-expanded={expanded}
			aria-controls="entry-{entry.id}-body"
		>
			<h3>{entry.title}</h3>
		</button>
		<div class="meta">
			{#if publishedLabel}
				<time datetime={publishedLabel}>{publishedLabel}</time>
			{/if}
			{#if entry.author}
				<span class="author">{entry.author}</span>
			{/if}
			{#if entry.reading_time > 0}
				<span class="reading-time">{entry.reading_time} min</span>
			{/if}
		</div>
	</header>

	{#if expanded}
		<div id="entry-{entry.id}-body" class="body">
			{#if entry.summary}
				<p class="summary">{entry.summary}</p>
			{/if}
			<div class="actions">
				{#if entry.url}
					<a href={entry.url} target="_blank" rel="noopener noreferrer">View original</a>
				{/if}
				<button type="button" onclick={() => onmarkread?.()}>
					{entry.read ? 'Mark unread' : 'Mark read'}
				</button>
				<button type="button" onclick={() => ontogglesaved?.()}>
					{entry.saved ? 'Unsave' : 'Save'}
				</button>
			</div>
		</div>
	{/if}
</article>

<style>
	.entry {
		border: 1px solid var(--border);
		border-radius: 6px;
		background: var(--surface);
		padding: 0.75rem 1rem;
		margin: 0.5rem 0;
	}
	.entry.read h3 {
		color: var(--fg-muted);
		font-weight: 400;
	}
	.title {
		all: unset;
		cursor: pointer;
		display: block;
		width: 100%;
	}
	.title:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	h3 {
		margin: 0;
		font-size: 1rem;
		font-weight: 600;
	}
	.meta {
		display: flex;
		gap: 0.75rem;
		font-size: 0.8rem;
		color: var(--fg-muted);
		margin-top: 0.25rem;
	}
	.body {
		margin-top: 0.5rem;
		padding-top: 0.5rem;
		border-top: 1px solid var(--border);
	}
	.summary {
		margin: 0 0 0.75rem;
		color: var(--fg-muted);
	}
	.actions {
		display: flex;
		gap: 0.5rem;
		flex-wrap: wrap;
	}
	.actions button {
		font: inherit;
		padding: 0.25rem 0.6rem;
		border: 1px solid var(--border);
		background: var(--bg);
		color: var(--fg);
		border-radius: 4px;
		cursor: pointer;
	}
	.actions a {
		padding: 0.25rem 0.6rem;
		border: 1px solid var(--border);
		border-radius: 4px;
		text-decoration: none;
	}
</style>
