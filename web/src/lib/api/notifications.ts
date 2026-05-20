import { api } from './client';

export interface Notification {
	id: string;
	kind: string;
	title: string;
	body?: string;
	link?: string;
	read: boolean;
	created_at: string;
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
