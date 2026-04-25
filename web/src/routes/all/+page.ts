import { api } from '$lib/api';
import { PAGE_LIMIT } from '$lib/entryListPage.svelte';
import type { EntryListResponse } from '$lib/types';

export const load = async () => {
	const initial = await api.get<EntryListResponse>(
		`/entries?status=all&limit=${PAGE_LIMIT}`
	);
	return { initial };
};
