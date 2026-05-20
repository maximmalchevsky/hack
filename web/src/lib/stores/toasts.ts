import { writable } from 'svelte/store';

export type ToastVariant = 'info' | 'success' | 'warning' | 'danger';

export interface Toast {
	id: number;
	variant: ToastVariant;
	icon?: string;        // ti-* icon
	title: string;
	body?: string;
	timeoutMs: number;    // 0 = не закрывать автоматически
}

interface ToastInput {
	variant?: ToastVariant;
	icon?: string;
	title: string;
	body?: string;
	timeoutMs?: number;
}

function createToasts() {
	const { subscribe, update } = writable<Toast[]>([]);
	let counter = 1;

	function push(input: ToastInput): number {
		const id = counter++;
		const t: Toast = {
			id,
			variant: input.variant ?? 'info',
			icon: input.icon,
			title: input.title,
			body: input.body,
			timeoutMs: input.timeoutMs ?? 5000
		};
		update((arr) => [...arr, t]);
		if (t.timeoutMs > 0 && typeof window !== 'undefined') {
			setTimeout(() => dismiss(id), t.timeoutMs);
		}
		return id;
	}

	function dismiss(id: number) {
		update((arr) => arr.filter((t) => t.id !== id));
	}

	function clear() {
		update(() => []);
	}

	return { subscribe, push, dismiss, clear };
}

export const toasts = createToasts();
