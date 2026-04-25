// Loader for /feeds/add — preloads the category list so the Subscribe form
// can offer an "(uncategorised)" + dropdown choice without an in-component
// fetch waterfall. A failure here is non-fatal: we surface a load error to
// the page and let the user continue with category_id unset.

import { api, ApiError } from '$lib/api';
import type { Category } from '$lib/types';

export const load = async (): Promise<{
	categories: Category[];
	categoriesError: string | null;
}> => {
	try {
		const categories = await api.get<Category[]>('/categories');
		return { categories, categoriesError: null };
	} catch (e) {
		const message =
			e instanceof ApiError ? `error ${e.status}: ${e.message}` : `error: ${String(e)}`;
		return { categories: [], categoriesError: message };
	}
};
