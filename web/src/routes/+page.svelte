<script lang="ts">
	import { api, ApiError } from '$lib/api';

	let status = $state('checking…');

	$effect(() => {
		api
			.health()
			.then((r) => (status = r.status))
			.catch((e: unknown) => {
				if (e instanceof ApiError) status = `error ${e.status}: ${e.message}`;
				else status = `error: ${String(e)}`;
			});
	});
</script>

<main>
	<header>
		<h1>Wire</h1>
		<p class="muted">A self-hosted feed reader.</p>
	</header>

	<section class="card">
		<h2>Foundation health</h2>
		<p>API status: <code>{status}</code></p>
		<p class="muted">
			Phase 0 foundation. Feed reading, search, and offline lands in Phase 1.
		</p>
	</section>
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
	code {
		background: var(--bg);
		padding: 0.1em 0.4em;
		border-radius: 3px;
		border: 1px solid var(--border);
		font-size: 0.9em;
	}
</style>
