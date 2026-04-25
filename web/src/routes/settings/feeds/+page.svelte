<script lang="ts">
	import { api, ApiError } from '$lib/api';
	import type { Category, Feed } from '$lib/types';

	const { data } = $props();

	// CRUD actions mutate the local state below. Capturing the load() snapshot
	// once is intentional — there is no invalidate() path that would refresh
	// `data`, so the state_referenced_locally warning is a false positive here.
	// svelte-ignore state_referenced_locally
	let feeds = $state<Feed[]>(data.feeds);
	// svelte-ignore state_referenced_locally
	let categories = $state<Category[]>(data.categories);
	// svelte-ignore state_referenced_locally
	let loadError = $state<string | null>(data.error);
	let actionError = $state<string | null>(null);

	// Track which feed has an in-flight action so the row's buttons can
	// disable themselves without locking the whole page.
	let busyFeedID = $state<number | null>(null);

	function categoryName(id: number | null): string {
		if (id === null) return 'Uncategorized';
		const c = categories.find((c) => c.id === id);
		return c?.name ?? 'Uncategorized';
	}

	function formatTimestamp(secs: number | null): string {
		if (secs === null) return 'never';
		return new Date(secs * 1000).toLocaleString();
	}

	function reportError(prefix: string, err: unknown) {
		const message = err instanceof ApiError ? `${err.status}: ${err.message}` : String(err);
		actionError = `${prefix}: ${message}`;
	}

	async function rename(feed: Feed) {
		const next = window.prompt('New title', feed.title);
		if (next === null) return;
		const title = next.trim();
		if (title === '' || title === feed.title) return;
		busyFeedID = feed.id;
		actionError = null;
		try {
			const updated = await api.put<Feed>(`/feeds/${feed.id}`, { title });
			feeds = feeds.map((f) => (f.id === feed.id ? { ...f, ...updated } : f));
		} catch (err) {
			reportError(`rename feed ${feed.id}`, err);
		} finally {
			busyFeedID = null;
		}
	}

	async function recategorize(feed: Feed, raw: string) {
		// `raw` is the <select>'s string value; "" means uncategorized. Avoid
		// firing the request if the choice didn't actually change.
		const nextID = raw === '' ? null : Number(raw);
		if (nextID === feed.category_id) return;
		busyFeedID = feed.id;
		actionError = null;
		try {
			const updated = await api.put<Feed>(`/feeds/${feed.id}`, { category_id: nextID });
			feeds = feeds.map((f) => (f.id === feed.id ? { ...f, ...updated } : f));
		} catch (err) {
			reportError(`recategorize feed ${feed.id}`, err);
		} finally {
			busyFeedID = null;
		}
	}

	async function remove(feed: Feed) {
		if (!window.confirm(`Delete feed "${feed.title}"? Entries will be removed.`)) return;
		busyFeedID = feed.id;
		actionError = null;
		try {
			await api.delete<void>(`/feeds/${feed.id}`);
			feeds = feeds.filter((f) => f.id !== feed.id);
		} catch (err) {
			reportError(`delete feed ${feed.id}`, err);
		} finally {
			busyFeedID = null;
		}
	}

	async function refresh(feed: Feed) {
		busyFeedID = feed.id;
		actionError = null;
		try {
			// 202 Accepted with no body — bypass api.post because the JSON
			// client tries to res.json() any non-204 response, which throws on
			// an empty body. Use fetch directly and check status manually.
			const res = await fetch(`/api/v1/feeds/${feed.id}/refresh`, { method: 'POST' });
			if (!res.ok) {
				throw new ApiError(res.status, res.statusText);
			}
		} catch (err) {
			reportError(`refresh feed ${feed.id}`, err);
		} finally {
			busyFeedID = null;
		}
	}
</script>

<section class="page">
	<header>
		<h1>Feeds</h1>
		<p class="muted">
			<a href="/settings">← Back to settings</a>
		</p>
	</header>

	{#if loadError}
		<p role="alert" class="status err">Failed to load: {loadError}</p>
	{/if}
	{#if actionError}
		<p role="alert" class="status err">{actionError}</p>
	{/if}

	{#if feeds.length === 0 && !loadError}
		<p class="status">No feeds yet.</p>
	{:else}
		<div class="table-wrap">
			<table>
				<thead>
					<tr>
						<th scope="col">Title</th>
						<th scope="col">Category</th>
						<th scope="col">Last polled</th>
						<th scope="col">Errors</th>
						<th scope="col">Actions</th>
					</tr>
				</thead>
				<tbody>
					{#each feeds as feed (feed.id)}
						{@const busy = busyFeedID === feed.id}
						<tr class:disabled={feed.disabled}>
							<td>
								<div class="title">{feed.title}</div>
								<div class="url muted">
									{#if feed.site_url}
										<a href={feed.site_url} target="_blank" rel="noopener noreferrer">
											{feed.site_url}
										</a>
									{:else}
										<span>{feed.feed_url}</span>
									{/if}
								</div>
							</td>
							<td>
								<select
									aria-label="Category for {feed.title}"
									value={feed.category_id === null ? '' : String(feed.category_id)}
									onchange={(e) => recategorize(feed, (e.currentTarget as HTMLSelectElement).value)}
									disabled={busy}
								>
									<option value="">{categoryName(null)}</option>
									{#each categories as c (c.id)}
										<option value={String(c.id)}>{c.name}</option>
									{/each}
								</select>
							</td>
							<td>{formatTimestamp(feed.last_polled_at)}</td>
							<td>
								<span class:err-count={feed.error_count > 0}>{feed.error_count}</span>
								{#if feed.last_error}
									<div class="last-error muted" title={feed.last_error}>
										{feed.last_error}
									</div>
								{/if}
							</td>
							<td class="actions">
								<button type="button" onclick={() => rename(feed)} disabled={busy}>
									Rename
								</button>
								<button type="button" onclick={() => refresh(feed)} disabled={busy}>
									Refresh
								</button>
								<button type="button" class="danger" onclick={() => remove(feed)} disabled={busy}>
									Delete
								</button>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
</section>

<style>
	.page {
		max-width: 64rem;
		margin: 2rem auto;
		padding: 0 1rem;
	}
	header h1 {
		margin: 0 0 0.25rem;
		font-size: 1.75rem;
		letter-spacing: -0.02em;
	}
	.muted {
		color: var(--fg-muted);
	}
	.muted a {
		color: inherit;
	}
	.status {
		margin: 0.75rem 0;
		font-size: 0.9rem;
		color: var(--fg-muted);
	}
	.status.err {
		color: #b91c1c;
	}
	.table-wrap {
		overflow-x: auto;
		margin-top: 1rem;
	}
	table {
		width: 100%;
		border-collapse: collapse;
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: 6px;
	}
	th,
	td {
		text-align: left;
		padding: 0.6rem 0.75rem;
		border-bottom: 1px solid var(--border);
		vertical-align: top;
	}
	th {
		font-size: 0.8rem;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--fg-muted);
		font-weight: 600;
	}
	tbody tr:last-child td {
		border-bottom: none;
	}
	tr.disabled .title {
		color: var(--fg-muted);
		text-decoration: line-through;
	}
	.title {
		font-weight: 500;
	}
	.url {
		font-size: 0.8rem;
		max-width: 24rem;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.err-count {
		color: #b91c1c;
		font-weight: 600;
	}
	.last-error {
		font-size: 0.8rem;
		max-width: 18rem;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.actions {
		white-space: nowrap;
	}
	.actions button {
		font: inherit;
		padding: 0.25rem 0.6rem;
		margin-right: 0.25rem;
		border: 1px solid var(--border);
		background: var(--bg);
		color: var(--fg);
		border-radius: 4px;
		cursor: pointer;
	}
	.actions button:disabled {
		opacity: 0.5;
		cursor: progress;
	}
	.actions button.danger {
		border-color: #b91c1c;
		color: #b91c1c;
	}
	select {
		font: inherit;
		padding: 0.25rem 0.4rem;
		border: 1px solid var(--border);
		background: var(--bg);
		color: var(--fg);
		border-radius: 4px;
	}
</style>
