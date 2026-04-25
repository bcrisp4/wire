import { api, ApiError } from '$lib/api';
import type { Category } from '$lib/types';

export const load = async () => {
	try {
		const categories = await api.get<Category[]>('/categories');
		return { categories, error: null as string | null };
	} catch (e) {
		const message = e instanceof ApiError ? `${e.status}: ${e.message}` : String(e);
		return { categories: [] as Category[], error: message };
	}
};
