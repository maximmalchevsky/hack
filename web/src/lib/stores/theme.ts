import { writable } from 'svelte/store';
import { browser } from '$app/environment';

const STORAGE_KEY = 'wts-theme';

function readInitial(): Theme {
	if (!browser) return 'light';
	const saved = localStorage.getItem(STORAGE_KEY) as Theme | null;
	if (saved === 'light' || saved === 'dark') return saved;
	const prefersDark = window.matchMedia?.('(prefers-color-scheme: dark)').matches;
	return prefersDark ? 'dark' : 'light';
}

function createThemeStore() {
	const { subscribe, set, update } = writable<Theme>(readInitial());

	return {
		subscribe,
		set: (t: Theme) => {
			set(t);
			if (browser) {
				localStorage.setItem(STORAGE_KEY, t);
				document.documentElement.setAttribute('data-theme', t);
			}
		},
		toggle: () =>
			update((t) => {
				const next: Theme = t === 'light' ? 'dark' : 'light';
				if (browser) {
					localStorage.setItem(STORAGE_KEY, next);
					document.documentElement.setAttribute('data-theme', next);
				}
				return next;
			})
	};
}

export const theme = createThemeStore();
