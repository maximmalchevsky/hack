import { api } from './client';

export interface NotificationPrefs {
	email_notifications: boolean;
	telegram_notifications: boolean;
	telegram_linked: boolean;
}

export interface TelegramStatus {
	linked: boolean;
	bot_username?: string;
	deep_link?: string;
}

export const getNotificationPrefs = () =>
	api.get<NotificationPrefs>('/api/v1/me/notification-prefs');

export const updateNotificationPrefs = (body: {
	email_notifications?: boolean;
	telegram_notifications?: boolean;
}) => api.patch<NotificationPrefs>('/api/v1/me/notification-prefs', body);

export const getTelegramStatus = () => api.get<TelegramStatus>('/api/v1/me/telegram');

export const unlinkTelegram = () => api.delete<{ ok: boolean }>('/api/v1/me/telegram');
