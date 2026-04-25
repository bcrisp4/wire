<script lang="ts">
	import { getTheme, setTheme, type Theme } from '$lib/prefs';

	let theme = $state<Theme>(getTheme());

	function onChange(event: Event) {
		const value = (event.currentTarget as HTMLSelectElement).value as Theme;
		theme = value;
		setTheme(value);
	}
</script>

<label class="row">
	<span>Theme</span>
	<select value={theme} onchange={onChange}>
		<option value="system">System</option>
		<option value="light">Light</option>
		<option value="dark">Dark</option>
		<option value="sepia">Sepia</option>
	</select>
</label>

<style>
	.row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.5rem;
		font-size: 0.85rem;
		color: var(--fg-muted);
	}
	select {
		font: inherit;
		padding: 0.2rem 0.4rem;
		background: var(--bg);
		color: var(--fg);
		border: 1px solid var(--border);
		border-radius: 4px;
	}
</style>
