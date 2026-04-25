<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import type { Category, Feed } from '$lib/types';
	import ThemeSwitcher from './ThemeSwitcher.svelte';
	import FontToggle from './FontToggle.svelte';

	let categories = $state<Category[]>([]);
	let feeds = $state<Feed[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);

	onMount(async () => {
		try {
			const [cats, fds] = await Promise.all([
				api.get<Category[]>('/categories'),
				api.get<Feed[]>('/feeds')
			]);
			categories = cats;
			feeds = fds;
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load sidebar';
		} finally {
			loading = false;
		}
	});
</script>

<div class="sidebar-content">
	{#if loading}
		<p class="status" role="status">Loading…</p>
	{:else if error}
		<p class="status error" role="alert">{error}</p>
	{:else}
		<section aria-label="Categories">
			<h2>Categories</h2>
			{#if categories.length === 0}
				<p class="empty">No categories.</p>
			{:else}
				<ul>
					{#each categories as category (category.id)}
						<li>
							<a href="/categories/{category.id}">
								<span class="label">{category.name}</span>
								<span class="badge" aria-label="{category.unread_count} unread"
									>{category.unread_count}</span
								>
							</a>
						</li>
					{/each}
				</ul>
			{/if}
		</section>

		<section aria-label="Feeds">
			<h2>Feeds</h2>
			{#if feeds.length === 0}
				<p class="empty">No feeds.</p>
			{:else}
				<ul>
					{#each feeds as feed (feed.id)}
						<li>
							<a href="/feeds/{feed.id}">
								<span class="label">{feed.title}</span>
								<span class="badge" aria-label="{feed.unread_count} unread"
									>{feed.unread_count}</span
								>
							</a>
						</li>
					{/each}
				</ul>
			{/if}
		</section>
	{/if}

	<section class="prefs" aria-label="Display preferences">
		<ThemeSwitcher />
		<FontToggle />
	</section>
</div>

<style>
	.sidebar-content {
		display: flex;
		flex-direction: column;
		gap: 1rem;
	}
	h2 {
		margin: 0 0 0.4rem;
		font-size: 0.75rem;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--fg-muted);
	}
	ul {
		list-style: none;
		padding: 0;
		margin: 0;
		display: flex;
		flex-direction: column;
		gap: 0.15rem;
	}
	li a {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.5rem;
		padding: 0.3rem 0.5rem;
		border-radius: 4px;
		text-decoration: none;
		color: var(--fg);
	}
	li a:hover {
		background: var(--bg);
	}
	.label {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.badge {
		flex-shrink: 0;
		min-width: 1.5rem;
		text-align: center;
		padding: 0.05rem 0.4rem;
		border-radius: 999px;
		background: var(--bg);
		border: 1px solid var(--border);
		font-size: 0.75rem;
		color: var(--fg-muted);
	}
	.status,
	.empty {
		margin: 0;
		font-size: 0.85rem;
		color: var(--fg-muted);
	}
	.status.error {
		color: #b91c1c;
	}
	.prefs {
		margin-top: auto;
		display: flex;
		flex-direction: column;
		gap: 0.5rem;
		padding-top: 0.75rem;
		border-top: 1px solid var(--border);
	}
</style>
