<script lang="ts">
	// Settings overview: links to the per-area pages (feeds, categories) plus
	// OPML import/export controls. Theme/font controls are intentionally NOT
	// rendered here — Unit 12b owns them inside the sidebar so they're
	// reachable from every page, not just /settings.
	import { api, ApiError } from '$lib/api';
	import type { OpmlImportResult } from '$lib/types';

	type ImportStatus =
		| { kind: 'idle' }
		| { kind: 'uploading' }
		| { kind: 'ok'; result: OpmlImportResult }
		| { kind: 'error'; message: string };

	let importStatus = $state<ImportStatus>({ kind: 'idle' });
	let fileInput: HTMLInputElement | undefined = $state();

	async function onFileSelected(e: Event) {
		const input = e.currentTarget as HTMLInputElement;
		const file = input.files?.[0];
		if (!file) return;
		importStatus = { kind: 'uploading' };
		const fd = new FormData();
		// Field name "file" matches r.FormFile("file") in internal/api/opml.go.
		fd.append('file', file);
		try {
			const result = await api.postFormData<OpmlImportResult>('/opml/import', fd);
			importStatus = { kind: 'ok', result };
		} catch (err) {
			const message = err instanceof ApiError ? `${err.status}: ${err.message}` : String(err);
			importStatus = { kind: 'error', message };
		} finally {
			// Clear the input so selecting the same file again fires `change`.
			if (fileInput) fileInput.value = '';
		}
	}
</script>

<section class="settings">
	<header>
		<h1>Settings</h1>
		<p class="muted">Manage feeds, categories, and subscriptions.</p>
	</header>

	<nav class="tabs" aria-label="Settings sections">
		<a href="/settings/feeds">Feeds</a>
		<a href="/settings/categories">Categories</a>
	</nav>

	<section class="card">
		<h2>OPML</h2>
		<p class="muted">
			Import or export your subscriptions as OPML. Existing duplicates are skipped.
		</p>

		<div class="opml-actions">
			<label class="upload">
				<span>Import OPML</span>
				<input
					bind:this={fileInput}
					type="file"
					accept=".opml,.xml,application/xml,text/xml"
					onchange={onFileSelected}
					disabled={importStatus.kind === 'uploading'}
				/>
			</label>

			<a href="/api/v1/opml/export" download="wire-subscriptions.opml" class="download">
				Export OPML
			</a>
		</div>

		{#if importStatus.kind === 'uploading'}
			<p role="status" class="status">Uploading…</p>
		{:else if importStatus.kind === 'ok'}
			<p role="status" class="status ok">
				Imported {importStatus.result.imported}, skipped
				{importStatus.result.skipped_duplicates} duplicates,
				created {importStatus.result.categories_created} categories.
			</p>
		{:else if importStatus.kind === 'error'}
			<p role="alert" class="status err">Import failed: {importStatus.message}</p>
		{/if}
	</section>
</section>

<style>
	.settings {
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
		margin: 0;
	}
	.tabs {
		display: flex;
		gap: 0.5rem;
		margin: 1rem 0 1.5rem;
		flex-wrap: wrap;
	}
	.tabs a {
		padding: 0.4rem 0.8rem;
		border: 1px solid var(--border);
		border-radius: 4px;
		text-decoration: none;
		color: var(--fg);
		background: var(--surface);
	}
	.tabs a:hover {
		background: var(--bg);
	}
	.card {
		padding: 1.25rem 1.5rem;
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: 8px;
		margin-bottom: 1rem;
	}
	.card h2 {
		margin: 0 0 0.5rem;
		font-size: 1rem;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--fg-muted);
	}
	.opml-actions {
		display: flex;
		gap: 1rem;
		margin-top: 1rem;
		flex-wrap: wrap;
		align-items: center;
	}
	.upload {
		display: inline-flex;
		flex-direction: column;
		gap: 0.25rem;
		font-size: 0.85rem;
	}
	.download {
		padding: 0.4rem 0.8rem;
		border: 1px solid var(--border);
		border-radius: 4px;
		text-decoration: none;
		background: var(--bg);
		color: var(--fg);
	}
	.status {
		margin: 0.75rem 0 0;
		font-size: 0.9rem;
	}
	.status.ok {
		color: var(--accent);
	}
	.status.err {
		color: #b91c1c;
	}
</style>
