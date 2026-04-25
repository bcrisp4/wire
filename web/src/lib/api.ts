const BASE = '/api/v1';

export class ApiError extends Error {
	constructor(
		public status: number,
		message: string
	) {
		super(message);
		this.name = 'ApiError';
	}
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
	const res = await fetch(`${BASE}${path}`, {
		...init,
		headers: {
			'Content-Type': 'application/json',
			...(init.headers ?? {})
		}
	});
	if (!res.ok) {
		let msg = res.statusText;
		try {
			const body = await res.json();
			if (body?.error) msg = body.error;
		} catch {
			/* ignore */
		}
		throw new ApiError(res.status, msg);
	}
	if (res.status === 204) return undefined as T;
	return res.json() as Promise<T>;
}

export const api = {
	health: () => request<{ status: string }>('/health'),
	get: <T>(path: string) => request<T>(path),
	post: <T>(path: string, body: unknown) =>
		request<T>(path, { method: 'POST', body: JSON.stringify(body) }),
	put: <T>(path: string, body: unknown) =>
		request<T>(path, { method: 'PUT', body: JSON.stringify(body) }),
	delete: <T>(path: string) => request<T>(path, { method: 'DELETE' })
};
