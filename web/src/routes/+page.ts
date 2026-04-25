import { api, ApiError } from '$lib/api';

export const load = async () => {
	try {
		const r = await api.health();
		return { status: r.status };
	} catch (e) {
		if (e instanceof ApiError) return { status: `error ${e.status}: ${e.message}` };
		return { status: `error: ${String(e)}` };
	}
};
