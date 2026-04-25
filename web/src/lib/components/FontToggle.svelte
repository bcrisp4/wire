<script lang="ts">
	import { getFont, setFont, type Font } from '$lib/prefs';

	let font = $state<Font>(getFont());

	function onChange(event: Event) {
		const value = (event.currentTarget as HTMLSelectElement).value as Font;
		font = value;
		setFont(value);
	}
</script>

<label class="row">
	<span>Font</span>
	<select value={font} onchange={onChange}>
		<option value="sans">Sans</option>
		<option value="serif">Serif</option>
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
