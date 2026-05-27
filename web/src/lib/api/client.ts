// HTTP-клиент к backend Fiber: JWT с авто-refresh, обёртки для GET/POST/PUT/PATCH/DELETE.

import { browser } from '$app/environment';
import { env } from '$env/dynamic/public';

const STORAGE_ACCESS = 'wts-access';
const STORAGE_REFRESH = 'wts-refresh';

function baseURL(): string {
	return env.PUBLIC_API_URL || (browser ? '' : 'http://localhost:8080');
}

export function getAccessToken(): string | null {
	if (!browser) return null;
	return localStorage.getItem(STORAGE_ACCESS);
}

export function getRefreshToken(): string | null {
	if (!browser) return null;
	return localStorage.getItem(STORAGE_REFRESH);
}

export function setTokens(access: string, refresh: string) {
	if (!browser) return;
	localStorage.setItem(STORAGE_ACCESS, access);
	localStorage.setItem(STORAGE_REFRESH, refresh);
}

export function clearTokens() {
	if (!browser) return;
	localStorage.removeItem(STORAGE_ACCESS);
	localStorage.removeItem(STORAGE_REFRESH);
}

export class ApiError extends Error {
	constructor(
		public status: number,
		message: string,
		// payload — JSON-тело ответа, если оно было parseable. Используется
		// в кейсах вроде 409 от /propose-meeting, где сервер возвращает
		// {error: "overload", overload: [...]} и фронту нужны эти данные
		// чтобы показать confirm-диалог.
		public payload?: unknown
	) {
		super(message);
	}
}

interface RequestOptions extends RequestInit {
	auth?: boolean;
	retryOn401?: boolean;
	timeoutMs?: number;
}

const DEFAULT_TIMEOUT_MS = 15000;

async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
	const {
		auth = true,
		retryOn401 = true,
		headers = {},
		timeoutMs = DEFAULT_TIMEOUT_MS,
		signal,
		...rest
	} = opts;

	const h = new Headers(headers as HeadersInit);
	if (!h.has('Content-Type') && rest.body) h.set('Content-Type', 'application/json');
	if (auth) {
		const token = getAccessToken();
		if (token) h.set('Authorization', `Bearer ${token}`);
	}

	// Таймаут через AbortController. Если внешний signal задан — комбинируем.
	const controller = new AbortController();
	const timer = setTimeout(() => controller.abort(), timeoutMs);
	if (signal) {
		signal.addEventListener('abort', () => controller.abort(), { once: true });
	}

	let res: Response;
	try {
		res = await fetch(`${baseURL()}${path}`, {
			...rest,
			headers: h,
			signal: controller.signal
		});
	} catch (err) {
		clearTimeout(timer);
		if (err instanceof DOMException && err.name === 'AbortError') {
			throw new ApiError(0, `request timeout (${timeoutMs}ms)`);
		}
		throw new ApiError(0, err instanceof Error ? err.message : 'network error');
	}
	clearTimeout(timer);

	if (res.status === 401 && auth && retryOn401) {
		const refreshed = await tryRefresh();
		if (refreshed) {
			return request<T>(path, { ...opts, retryOn401: false });
		}
		clearTokens();
		throw new ApiError(401, 'unauthorized');
	}

	if (!res.ok) {
		let msg = `request failed: ${res.status}`;
		let payload: unknown;
		try {
			const body = await res.json();
			payload = body;
			if (body && typeof body.error === 'string') msg = body.error;
		} catch {
			// ignore
		}
		throw new ApiError(res.status, msg, payload);
	}

	if (res.status === 204) return undefined as T;
	return (await res.json()) as T;
}

async function tryRefresh(): Promise<boolean> {
	const refresh = getRefreshToken();
	if (!refresh) return false;
	try {
		const res = await fetch(`${baseURL()}/api/v1/auth/refresh`, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ refresh })
		});
		if (!res.ok) return false;
		const data = (await res.json()) as { access: string; refresh: string };
		setTokens(data.access, data.refresh);
		return true;
	} catch {
		return false;
	}
}

export const api = {
	get: <T>(path: string, opts?: RequestOptions) => request<T>(path, { ...opts, method: 'GET' }),
	post: <T>(path: string, body?: unknown, opts?: RequestOptions) =>
		request<T>(path, { ...opts, method: 'POST', body: body ? JSON.stringify(body) : undefined }),
	put: <T>(path: string, body?: unknown, opts?: RequestOptions) =>
		request<T>(path, { ...opts, method: 'PUT', body: body ? JSON.stringify(body) : undefined }),
	patch: <T>(path: string, body?: unknown, opts?: RequestOptions) =>
		request<T>(path, { ...opts, method: 'PATCH', body: body ? JSON.stringify(body) : undefined }),
	delete: <T>(path: string, opts?: RequestOptions) =>
		request<T>(path, { ...opts, method: 'DELETE' })
};
