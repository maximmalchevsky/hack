import { api } from './client';

export type IntegrationProvider =
	| 'ical'
	| 'caldav'
	| 'google_calendar'
	| 'ms365'
	| 'jira'
	| 'yandex_tracker';

export type IntegrationStatus = 'connected' | 'error' | 'disabled' | 'pending';

export interface Integration {
	id: string;
	employee_id: string;
	provider: IntegrationProvider;
	account_email?: string;
	account_label?: string;
	status: IntegrationStatus;
	last_sync_at?: string;
	last_error?: string;
	created_at: string;
}

export const listIntegrations = () =>
	api.get<{ integrations: Integration[] }>('/api/v1/integrations');

export const connectICal = (body: { feed_url?: string; label?: string }) =>
	api.post<Integration>('/api/v1/integrations/ical', body);

export const connectCalDAV = (body: {
	endpoint: string;
	username: string;
	password: string;
	cal_path?: string;
	label?: string;
}) => api.post<Integration>('/api/v1/integrations/caldav', body);

export const triggerSync = (id: string) =>
	api.post<{ queued: boolean }>(`/api/v1/integrations/${id}/sync`);

export const removeIntegration = (id: string) =>
	api.delete<void>(`/api/v1/integrations/${id}`);
