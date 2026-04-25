import { api, ApiError } from '$lib/api';
import type { Category, Feed } from '$lib/types';

export const load = async () => {
	try {
		const [feeds, categories] = await Promise.all([
			api.get<Feed[]>('/feeds'),
			api.get<Category[]>('/categories')
		]);
		return { feeds, categories, error: null as string | null };
	} catch (e) {
		const message = e instanceof ApiError ? `${e.status}: ${e.message}` : String(e);
		return { feeds: [] as Feed[], categories: [] as Category[], error: message };
	}
};
