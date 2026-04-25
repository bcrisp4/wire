<script lang="ts">
	import { api, ApiError } from '$lib/api';
	import type { Category } from '$lib/types';

	const { data } = $props();

	// CRUD actions mutate the local state below. Capturing the load() snapshot
	// once is intentional — there is no invalidate() path that would refresh
	// `data`, so the state_referenced_locally warning is a false positive here.
	// svelte-ignore state_referenced_locally
	let categories = $state<Category[]>(data.categories);
	// svelte-ignore state_referenced_locally
	let loadError = $state<string | null>(data.error);
	let actionError = $state<string | null>(null);

	let newName = $state('');
	let creating = $state(false);
	let busyID = $state<number | null>(null);

	function reportError(prefix: string, err: unknown) {
		const message = err instanceof ApiError ? `${err.status}: ${err.message}` : String(err);
		actionError = `${prefix}: ${message}`;
	}

	async function create(e: SubmitEvent) {
		e.preventDefault();
		const name = newName.trim();
		if (name === '') return;
		creating = true;
		actionError = null;
		try {
			const created = await api.post<Category>('/categories', { name });
			// Write endpoints leave unread_count at zero (see categories.go);
			// fall back to 0 if the field is missing from the response shape.
			const fresh: Category = { ...created, unread_count: created.unread_count ?? 0 };
			categories = [...categories, fresh];
			newName = '';
		} catch (err) {
			reportError('create category', err);
		} finally {
			creating = false;
		}
	}

	async function rename(cat: Category) {
		const next = window.prompt('New name', cat.name);
		if (next === null) return;
		const name = next.trim();
		if (name === '' || name === cat.name) return;
		busyID = cat.id;
		actionError = null;
		try {
			const updated = await api.put<Category>(`/categories/${cat.id}`, { name });
			categories = categories.map((c) =>
				c.id === cat.id ? { ...c, ...updated, unread_count: c.unread_count } : c
			);
		} catch (err) {
			reportError(`rename category ${cat.id}`, err);
		} finally {
			busyID = null;
		}
	}

	async function remove(cat: Category) {
		if (!window.confirm(`Delete category "${cat.name}"?`)) return;
		busyID = cat.id;
		actionError = null;
		try {
			await api.delete<void>(`/categories/${cat.id}`);
			categories = categories.filter((c) => c.id !== cat.id);
		} catch (err) {
			reportError(`delete category ${cat.id}`, err);
		} finally {
			busyID = null;
		}
	}
</script>

<section class="page">
	<header>
		<h1>Categories</h1>
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

	<form class="create" onsubmit={create}>
		<label>
			<span class="visually-hidden">New category name</span>
			<input
				type="text"
				placeholder="New category"
				bind:value={newName}
				disabled={creating}
				required
			/>
		</label>
		<button type="submit" disabled={creating || newName.trim() === ''}>
			{creating ? 'Creating…' : 'Add'}
		</button>
	</form>

	{#if categories.length === 0 && !loadError}
		<p class="status">No categories yet.</p>
	{:else}
		<ul class="list">
			{#each categories as cat (cat.id)}
				{@const busy = busyID === cat.id}
				<li>
					<span class="name">{cat.name}</span>
					<span class="count muted">{cat.unread_count} unread</span>
					<span class="actions">
						<button type="button" onclick={() => rename(cat)} disabled={busy}>Rename</button>
						<button type="button" class="danger" onclick={() => remove(cat)} disabled={busy}>
							Delete
						</button>
					</span>
				</li>
			{/each}
		</ul>
	{/if}
</section>

<style>
	.page {
		max-width: 48rem;
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
	.create {
		display: flex;
		gap: 0.5rem;
		margin: 1rem 0 1.5rem;
	}
	.create input {
		flex: 1;
		font: inherit;
		padding: 0.4rem 0.6rem;
		border: 1px solid var(--border);
		background: var(--surface);
		color: var(--fg);
		border-radius: 4px;
	}
	.create button {
		font: inherit;
		padding: 0.4rem 0.8rem;
		border: 1px solid var(--border);
		background: var(--bg);
		color: var(--fg);
		border-radius: 4px;
		cursor: pointer;
	}
	.create button:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}
	.list {
		list-style: none;
		padding: 0;
		margin: 0;
		border: 1px solid var(--border);
		border-radius: 6px;
		background: var(--surface);
	}
	.list li {
		display: flex;
		align-items: center;
		gap: 0.75rem;
		padding: 0.6rem 0.75rem;
		border-bottom: 1px solid var(--border);
	}
	.list li:last-child {
		border-bottom: none;
	}
	.name {
		font-weight: 500;
		flex: 1;
	}
	.count {
		font-size: 0.85rem;
	}
	.actions {
		white-space: nowrap;
	}
	.actions button {
		font: inherit;
		padding: 0.25rem 0.6rem;
		margin-left: 0.25rem;
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
	.visually-hidden {
		position: absolute;
		width: 1px;
		height: 1px;
		padding: 0;
		margin: -1px;
		overflow: hidden;
		clip: rect(0, 0, 0, 0);
		white-space: nowrap;
		border: 0;
	}
</style>
