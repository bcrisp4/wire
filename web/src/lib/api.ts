const BASE = '/api/v1';

export class ApiError extends Error {
	constructor(
		public status: number,
		message: string,
		public body?: unknown
	) {
		super(message);
		this.name = 'ApiError';
	}
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
	const headers: Record<string, string> = { ...((init.headers as Record<string, string>) ?? {}) };
	if (init.body !== undefined && headers['Content-Type'] === undefined) {
		headers['Content-Type'] = 'application/json';
	}
	return send<T>(path, { ...init, headers });
}

// send performs the fetch and uniform error handling shared by request() and
// postFormData(). Caller is responsible for any Content-Type headers; this
// function does NOT inject defaults so multipart bodies keep their boundary.
async function send<T>(path: string, init: RequestInit): Promise<T> {
	const res = await fetch(`${BASE}${path}`, init);
	if (!res.ok) {
		let message = res.statusText;
		let body: unknown;
		try {
			body = await res.json();
			if (typeof body === 'object' && body !== null && 'error' in body) {
				const e = (body as { error?: unknown }).error;
				if (typeof e === 'string') message = e;
			}
		} catch {
			/* not JSON */
		}
		throw new ApiError(res.status, message, body);
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
	delete: <T>(path: string) => request<T>(path, { method: 'DELETE' }),
	// postFormData submits multipart/form-data. Do NOT set Content-Type — the
	// browser fills in the boundary automatically when fetch sees a FormData
	// body. Setting it explicitly here would strip the boundary and the server
	// would reject the body as malformed.
	postFormData: <T>(path: string, fd: FormData) => send<T>(path, { method: 'POST', body: fd })
};
