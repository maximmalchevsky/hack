import { api } from './client';

export interface AdminUser {
	id: string;
	email: string;
	role: UserRole;
	full_name: string;
	timezone: string;
	created_at: string;
}

export interface AdminSource {
	id: string;
	employee_id: string;
	employee_name: string;
	provider: string;
	status: string;
	account_label?: string;
	account_email?: string;
	last_sync_at?: string;
	last_error?: string;
	created_at: string;
}

export interface AnalyticsWeights {
	w1: number;
	w2: number;
	w3: number;
	w4: number;
	w5: number;
	freshness_d_days: number;
	updated_at?: string;
}

export interface SystemHealth {
	users_count: number;
	employees_count: number;
	teams_count: number;
	integrations_active: number;
	integrations_error: number;
	unread_notifications: number;
	webhook_inbox_queued: number;
}

export const listAdminUsers = () =>
	api.get<{ users: AdminUser[] }>('/api/v1/admin/users');

export const updateUserRole = (id: string, role: UserRole) =>
	api.patch<void>(`/api/v1/admin/users/${id}/role`, { role });

export const listAdminSources = () =>
	api.get<{ sources: AdminSource[] }>('/api/v1/admin/sources');

export const getRules = () => api.get<AnalyticsWeights>('/api/v1/admin/rules');
export const updateRules = (w: AnalyticsWeights) => api.put<void>('/api/v1/admin/rules', w);

export const systemHealth = () => api.get<SystemHealth>('/api/v1/admin/system/health');

export interface AuditRecord {
	id: string;
	actor_user_id?: string;
	action: string;
	entity: string;
	entity_id?: string;
	before?: unknown;
	after?: unknown;
	created_at: string;
}

export const listAudit = (opts?: { entity?: string; entity_id?: string; limit?: number }) => {
	const q = new URLSearchParams();
	if (opts?.entity) q.set('entity', opts.entity);
	if (opts?.entity_id) q.set('entity_id', opts.entity_id);
	if (opts?.limit) q.set('limit', String(opts.limit));
	const suffix = q.toString() ? `?${q.toString()}` : '';
	return api.get<{ records: AuditRecord[] }>(`/api/v1/admin/audit${suffix}`);
};
