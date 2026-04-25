// Sidebar navigation list. Subsequent units should add a single line to
// `navItems` rather than editing `+layout.svelte` — this keeps eleven
// parallel SPA units from colliding on adjacent insertions in the layout.

export interface NavItem {
	href: string;
	label: string;
	icon?: string;
}

export const navItems: NavItem[] = [
	{ href: '/', label: 'River' },
	{ href: '/all', label: 'All' },
	{ href: '/saved', label: 'Saved' },
	{ href: '/settings', label: 'Settings' },
	{ href: '/feeds/add', label: 'Add Feed' }
];
