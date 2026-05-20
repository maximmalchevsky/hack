import { writable, get } from 'svelte/store';
import { browser } from '$app/environment';
import { env } from '$env/dynamic/public';
import {
	listNotifications,
	countUnread,
	markRead as apiMarkRead,
	markAllRead as apiMarkAllRead,
	type Notification
} from '$lib/api/notifications';
import { getAccessToken } from '$lib/api/client';
import { toasts, type ToastVariant } from './toasts';

interface State {
	items: Notification[];
	unread: number;
	connected: boolean;
}

function createNotifications() {
	const { subscribe, set, update } = writable<State>({
		items: [],
		unread: 0,
		connected: false
	});

	let source: EventSource | null = null;

	async function reload() {
		try {
			const [list, count] = await Promise.all([listNotifications(), countUnread()]);
			update((s) => ({
				...s,
				items: list.notifications ?? [],
				unread: count.unread
			}));
		} catch {
			// silent
		}
	}

	function start() {
		if (!browser) return;
		if (source) return;
		const token = getAccessToken();
		if (!token) return;

		// EventSource не поддерживает кастомные заголовки. Передаём токен в query —
		// бэк проверяет access_token middleware'ом, который читает и Query.
		const apiBase = env.PUBLIC_API_URL || '';
		const url = `${apiBase}/api/v1/notifications/stream?token=${encodeURIComponent(token)}`;
		source = new EventSource(url, { withCredentials: false });

		source.addEventListener('ready', () => {
			update((s) => ({ ...s, connected: true }));
		});

		source.addEventListener('notification', (e: MessageEvent) => {
			try {
				const data = JSON.parse(e.data);
				if (data?.notification) {
					const n = {
						id: data.notification.id,
						kind: data.notification.kind,
						title: data.notification.title,
						body: data.notification.body,
						link: data.notification.link,
						read: false,
						created_at: data.notification.created_at
					} as Notification;
					update((s) => ({
						items: [n, ...s.items],
						unread: s.unread + 1,
						connected: s.connected
					}));
					// Подкидываем toast для важных типов.
					showToastFor(n);
				}
			} catch {
				// ignore parse errors
			}
		});

		source.onerror = () => {
			update((s) => ({ ...s, connected: false }));
			// EventSource сам перезапустится, не закрываем.
		};
	}

	function stop() {
		source?.close();
		source = null;
		set({ items: [], unread: 0, connected: false });
	}

	async function markRead(id: string) {
		await apiMarkRead(id);
		update((s) => ({
			items: s.items.map((n) => (n.id === id ? { ...n, read: true } : n)),
			unread: Math.max(0, s.unread - 1),
			connected: s.connected
		}));
	}

	async function markAllRead() {
		await apiMarkAllRead();
		update((s) => ({
			items: s.items.map((n) => ({ ...n, read: true })),
			unread: 0,
			connected: s.connected
		}));
	}

	function snapshot() {
		return get({ subscribe });
	}

	return {
		subscribe,
		start,
		stop,
		reload,
		markRead,
		markAllRead,
		snapshot
	};
}

export const notifications = createNotifications();

// showToastFor — превращает входящую нотификацию в toast.
// Не для всех видов имеет смысл — например, request_update показывается
// только в колокольчике (это длинный список). Для коротких событийных
// уведомлений (напоминание/отмена/новая встреча) — даём всплывашку.
function showToastFor(n: Notification) {
	const mapping: Record<string, { variant: ToastVariant; icon: string }> = {
		event_reminder:    { variant: 'info',    icon: 'ti-bell-ringing' },
		meeting_proposal:  { variant: 'success', icon: 'ti-calendar-plus' },
		meeting_cancelled: { variant: 'warning', icon: 'ti-calendar-cancel' },
		meeting_updated:   { variant: 'info',    icon: 'ti-calendar-event' }
	};
	const m = mapping[n.kind];
	if (!m) return;
	toasts.push({
		variant: m.variant,
		icon: m.icon,
		title: n.title,
		body: n.body,
		timeoutMs: 6000
	});
}
