import { api, setTokens, clearTokens } from './client';
import { user, type User } from '$lib/stores/user';

export interface TokenPair {
	access: string;
	refresh: string;
}

export interface AuthUser {
	id: string;
	email: string;
	role: UserRole;
	full_name: string;
	timezone: string;
	locale: string;
	avatar_url?: string;
	created_at: string;
}

export interface AuthResponse {
	tokens: TokenPair;
	user: AuthUser;
	employee?: { id: string };
}

function applyAuth(res: AuthResponse) {
	setTokens(res.tokens.access, res.tokens.refresh);
	const u: User = {
		id: res.user.id,
		email: res.user.email,
		role: res.user.role,
		fullName: res.user.full_name,
		timezone: res.user.timezone,
		avatarUrl: res.user.avatar_url
	};
	user.set(u);
}

export async function register(input: {
	email: string;
	password: string;
	full_name: string;
	timezone?: string;
}): Promise<AuthResponse> {
	const res = await api.post<AuthResponse>('/api/v1/auth/register', input, { auth: false });
	applyAuth(res);
	return res;
}

export async function login(email: string, password: string): Promise<AuthResponse> {
	const res = await api.post<AuthResponse>(
		'/api/v1/auth/login',
		{ email, password },
		{ auth: false }
	);
	applyAuth(res);
	return res;
}

export function logout() {
	clearTokens();
	user.set(null);
}
