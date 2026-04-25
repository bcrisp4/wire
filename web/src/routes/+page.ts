import { api } from '$lib/api';
import type { EntryListResponse } from '$lib/types';
import type { PageLoad } from './$types';

export const PAGE_SIZE = 50;

export const load: PageLoad = async () => {
	const params = new URLSearchParams({ status: 'unread', limit: String(PAGE_SIZE) });
	const initial = await api.get<EntryListResponse>(`/entries?${params.toString()}`);
	return { initial };
};
