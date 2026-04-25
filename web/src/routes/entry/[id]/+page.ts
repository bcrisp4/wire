import { error } from '@sveltejs/kit';
import { ApiError, api } from '$lib/api';
import type { Entry } from '$lib/types';

export const load = async ({ params }: { params: { id: string } }) => {
	const id = Number(params.id);
	if (!Number.isInteger(id) || id <= 0) {
		error(400, 'invalid entry id');
	}
	try {
		const entry = await api.get<Entry>(`/entries/${id}`);
		return { entry };
	} catch (e) {
		if (e instanceof ApiError) {
			error(e.status, e.message || 'failed to load entry');
		}
		throw e;
	}
};
