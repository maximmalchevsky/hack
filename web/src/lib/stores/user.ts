import { writable } from 'svelte/store';

export interface User {
	id: string;
	email: string;
	role: UserRole;
	fullName: string;
	avatarUrl?: string;
	timezone?: string;
}

export const user = writable<User | null>(null);
