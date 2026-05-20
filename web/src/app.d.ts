// See https://kit.svelte.dev/docs/types#app

declare global {
	namespace App {
		// interface Error {}
		interface Locals {
			user?: {
				id: string;
				email: string;
				role: UserRole;
				fullName: string;
			};
		}
		// interface PageData {}
		// interface PageState {}
		// interface Platform {}
	}

	type UserRole = 'admin' | 'employee' | 'manager' | 'hr' | 'pm' | 'analyst';
	type Theme = 'light' | 'dark';
}

export {};
