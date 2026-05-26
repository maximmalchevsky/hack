import { api } from './client';

export interface Notification {
	id: string;
	kind: string;
	title: string;
	body?: string;
	link?: string;
	read: boolean;
	created_at: string;
	payload?: Record<string, unknown>;
}

export const listNotifications = (opts?: { unreadOnly?: boolean }) => {
	const q = opts?.unreadOnly ? '?unread=true' : '';
	return api.get<{ notifications: Notification[] }>(`/api/v1/notifications${q}`);
};

export const countUnread = () =>
	api.get<{ unread: number }>('/api/v1/notifications/count');

export const markRead = (id: string) =>
	api.post<void>(`/api/v1/notifications/${id}/read`);

export const markAllRead = () => api.post<void>('/api/v1/notifications/read-all');

// broadcastNotifications — массовая рассылка по списку emp_ids с шаблоном по kind.
// Бэк сам определяет title/body, дедуп 24ч. Доступно только manager/pm/hr/admin.
export type BroadcastKind = 'burnout' | 'overload' | 'anomaly' | 'stale_profile';

export interface BroadcastResult {
	sent: number;
	skipped: number;
	targeted: number;
	emails?: string[];
}

export const broadcastNotifications = (kind: BroadcastKind, employee_ids: string[]) =>
	api.post<BroadcastResult>('/api/v1/notifications/broadcast', {
		kind,
		employee_ids
	});
