<script lang="ts">
	import { goto } from '$app/navigation';
	import { api, ApiError } from '$lib/api';
	import type { DiscoverCandidate, DiscoverResponse, Feed } from '$lib/types';

	let { data } = $props();

	// Discovery flow: the user types a site URL, we POST it to /feeds/discover,
	// the backend follows <link rel="alternate"> tags and returns a list of feed
	// candidates the site exposes. Empty `candidates` is a real outcome we
	// surface as "no feeds found" rather than treating as an error.
	let url = $state('');
	let discovering = $state(false);
	let discoverError = $state<string | null>(null);
	let candidates = $state<DiscoverCandidate[]>([]);
	let didDiscover = $state(false); // distinguishes "not searched yet" from "no results"

	// Subscribe flow: once we have candidates the user picks one + an optional
	// category, then we POST /feeds. 409 on duplicate is shown inline so the
	// user can pick a different candidate without losing their work.
	let selectedURL = $state<string | null>(null);
	let selectedCategoryID = $state<string>(''); // empty = uncategorised
	let subscribing = $state(false);
	let subscribeError = $state<string | null>(null);
	let subscribed = $state<Feed | null>(null);

	async function handleDiscover(event: SubmitEvent) {
		event.preventDefault();
		// Reset prior discovery state up front so a validation failure on a new
		// submission doesn't leave stale candidates from an earlier search visible
		// alongside the new error message.
		discoverError = null;
		subscribeError = null;
		subscribed = null;
		candidates = [];
		selectedURL = null;
		didDiscover = false;

		const trimmed = url.trim();
		if (trimmed === '') {
			discoverError = 'Enter a URL.';
			return;
		}
		// Client-side URL parse so an obvious typo doesn't round-trip to the
		// backend just to come back as a 400. The backend still validates.
		try {
			new URL(trimmed);
		} catch {
			discoverError = 'That does not look like a valid URL.';
			return;
		}

		discovering = true;
		try {
			const res = await api.post<DiscoverResponse>('/feeds/discover', { url: trimmed });
			candidates = res.candidates;
			didDiscover = true;
			if (candidates.length === 1) {
				selectedURL = candidates[0].url;
			}
		} catch (e) {
			didDiscover = false;
			if (e instanceof ApiError) {
				if (e.status === 400) {
					discoverError = 'That URL was rejected by the server.';
				} else if (e.status === 502) {
					discoverError = 'Could not reach that site. Try again later.';
				} else {
					discoverError = `Discovery failed (${e.status}).`;
				}
			} else {
				discoverError = 'Network error. Check your connection and try again.';
			}
		} finally {
			discovering = false;
		}
	}

	async function handleSubscribe(event: SubmitEvent) {
		event.preventDefault();
		if (selectedURL === null) {
			subscribeError = 'Pick a feed to subscribe to.';
			return;
		}
		subscribing = true;
		subscribeError = null;
		try {
			const body: { feed_url: string; category_id?: number } = { feed_url: selectedURL };
			if (selectedCategoryID !== '') {
				body.category_id = Number(selectedCategoryID);
			}
			const feed = await api.post<Feed>('/feeds', body);
			subscribed = feed;
			// Set the inline confirmation first so a goto failure (e.g. router
			// teardown during a hot reload) leaves the user with a working link.
			goto('/').catch((navError) => {
				console.error('Navigation to "/" failed after subscribing.', navError);
			});
		} catch (e) {
			if (e instanceof ApiError) {
				if (e.status === 409) {
					subscribeError = 'You are already subscribed to that feed.';
				} else if (e.status === 400) {
					subscribeError = 'The server rejected that request.';
				} else {
					subscribeError = `Subscribe failed (${e.status}).`;
				}
			} else {
				subscribeError = 'Network error. Check your connection and try again.';
			}
		} finally {
			subscribing = false;
		}
	}
</script>

<svelte:head>
	<title>Add Feed · Wire</title>
</svelte:head>

<main>
	<header>
		<h1>Add Feed</h1>
		<p class="muted">Enter a site URL — Wire will look up the feeds it publishes.</p>
	</header>

	{#if data.categoriesError}
		<p class="status warn" role="status">
			Categories failed to load ({data.categoriesError}). You can still subscribe without a
			category.
		</p>
	{/if}

	<section class="card">
		<form onsubmit={handleDiscover} novalidate>
			<label for="site-url">Site or feed URL</label>
			<div class="row">
				<input
					id="site-url"
					name="url"
					type="url"
					inputmode="url"
					autocomplete="url"
					placeholder="https://example.com"
					bind:value={url}
					disabled={discovering}
					required
				/>
				<button type="submit" disabled={discovering}>
					{discovering ? 'Searching…' : 'Find feeds'}
				</button>
			</div>
			{#if discoverError}
				<p class="status error" role="alert">{discoverError}</p>
			{/if}
		</form>
	</section>

	{#if didDiscover && candidates.length === 0 && !discovering}
		<p class="status" role="status">No feeds were found at that URL.</p>
	{/if}

	{#if candidates.length > 0}
		<section class="card">
			<h2>Choose a feed</h2>
			<form onsubmit={handleSubscribe}>
				<ul class="candidates">
					{#each candidates as candidate (candidate.url)}
						<li>
							<label class="candidate">
								<input
									type="radio"
									name="candidate"
									value={candidate.url}
									checked={selectedURL === candidate.url}
									onchange={() => (selectedURL = candidate.url)}
									disabled={subscribing}
								/>
								<span class="candidate-body">
									<span class="candidate-title">{candidate.title || candidate.url}</span>
									<span class="candidate-meta">
										<code>{candidate.url}</code>
										<span class="badge">{candidate.type}</span>
									</span>
								</span>
							</label>
						</li>
					{/each}
				</ul>

				<label for="category">Category (optional)</label>
				<select
					id="category"
					name="category"
					bind:value={selectedCategoryID}
					disabled={subscribing || data.categories.length === 0}
				>
					<option value="">Uncategorised</option>
					{#each data.categories as category (category.id)}
						<option value={String(category.id)}>{category.name}</option>
					{/each}
				</select>

				<div class="actions">
					<button type="submit" disabled={subscribing || selectedURL === null}>
						{subscribing ? 'Subscribing…' : 'Subscribe'}
					</button>
				</div>

				{#if subscribeError}
					<p class="status error" role="alert">{subscribeError}</p>
				{/if}
			</form>
		</section>
	{/if}

	{#if subscribed}
		<p class="status success" role="status">
			Subscribed to <strong>{subscribed.title}</strong>. <a href="/">Back to river →</a>
		</p>
	{/if}
</main>

<style>
	main {
		max-width: 40rem;
		margin: 4rem auto;
		padding: 0 1rem;
	}
	header h1 {
		margin: 0 0 0.25rem;
		font-size: 2rem;
		letter-spacing: -0.02em;
	}
	.muted {
		color: var(--fg-muted);
		margin: 0;
	}
	.card {
		margin-top: 2rem;
		padding: 1.25rem 1.5rem;
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: 8px;
	}
	.card h2 {
		margin: 0 0 0.75rem;
		font-size: 1rem;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--fg-muted);
	}
	form label {
		display: block;
		margin-top: 0.5rem;
		margin-bottom: 0.35rem;
		font-size: 0.85rem;
		color: var(--fg-muted);
	}
	form label:first-child {
		margin-top: 0;
	}
	.row {
		display: flex;
		gap: 0.5rem;
	}
	.row input {
		flex: 1;
		min-width: 0;
	}
	input[type='url'],
	select {
		padding: 0.4rem 0.5rem;
		border: 1px solid var(--border);
		border-radius: 4px;
		background: var(--bg);
		color: var(--fg);
		font: inherit;
		width: 100%;
	}
	button {
		padding: 0.4rem 0.9rem;
		border: 1px solid var(--border);
		border-radius: 4px;
		background: var(--bg);
		color: var(--fg);
		font: inherit;
		cursor: pointer;
	}
	button:disabled {
		opacity: 0.6;
		cursor: not-allowed;
	}
	.candidates {
		list-style: none;
		padding: 0;
		margin: 0 0 1rem;
		display: flex;
		flex-direction: column;
		gap: 0.4rem;
	}
	.candidate {
		display: flex;
		align-items: flex-start;
		gap: 0.6rem;
		padding: 0.6rem 0.75rem;
		border: 1px solid var(--border);
		border-radius: 4px;
		cursor: pointer;
	}
	.candidate:hover {
		background: var(--bg);
	}
	.candidate-body {
		display: flex;
		flex-direction: column;
		gap: 0.15rem;
		min-width: 0;
	}
	.candidate-title {
		font-weight: 500;
	}
	.candidate-meta {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		font-size: 0.8rem;
		color: var(--fg-muted);
	}
	.candidate-meta code {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		max-width: 24rem;
		background: var(--bg);
		padding: 0.05em 0.35em;
		border-radius: 3px;
		border: 1px solid var(--border);
	}
	.badge {
		flex-shrink: 0;
		padding: 0.05rem 0.4rem;
		border-radius: 999px;
		background: var(--bg);
		border: 1px solid var(--border);
		font-size: 0.7rem;
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}
	.actions {
		margin-top: 1rem;
	}
	.status {
		margin: 1rem 0 0;
		font-size: 0.9rem;
		color: var(--fg-muted);
	}
	.status.error {
		color: #b91c1c;
	}
	.status.warn {
		color: #b45309;
	}
	.status.success {
		color: #047857;
	}
</style>
