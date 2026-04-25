<script lang="ts">
	import '../app.css';
	import { navItems } from '$lib/nav';

	let { children } = $props();
</script>

<div class="shell">
	<aside class="sidebar">
		<nav>
			<ul class="nav-list">
				{#each navItems as item (item.href)}
					<li><a href={item.href}>{item.label}</a></li>
				{/each}
			</ul>
		</nav>

		<!-- Unit 12b: sidebar-content -->
		<!-- Unit 12b: end -->
	</aside>

	<main>{@render children()}</main>
</div>

<style>
	.shell {
		display: grid;
		grid-template-columns: 16rem 1fr;
		min-height: 100vh;
	}
	.sidebar {
		border-right: 1px solid var(--border);
		background: var(--surface);
		padding: 1rem;
	}
	.nav-list {
		list-style: none;
		padding: 0;
		margin: 0 0 1rem;
		display: flex;
		flex-direction: column;
		gap: 0.25rem;
	}
	.nav-list a {
		display: block;
		padding: 0.4rem 0.6rem;
		border-radius: 4px;
		text-decoration: none;
		color: var(--fg);
	}
	.nav-list a:hover {
		background: var(--bg);
	}
	main {
		min-width: 0;
	}
	@media (max-width: 48rem) {
		.shell {
			grid-template-columns: 1fr;
		}
		.sidebar {
			border-right: none;
			border-bottom: 1px solid var(--border);
		}
	}
</style>
