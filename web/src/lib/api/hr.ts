import { api } from './client';

export interface HRRoadmapItem {
	employee_id: string;
	full_name: string;
	department?: string;
	email: string;
	role: UserRole;
	last_profile_update_at?: string;
	days_since_update: number;
	action: 'request_confirm' | 'request_update' | 'check_hr' | 'review_format';
	priority: 'low' | 'medium' | 'high' | 'critical';
	reason: string;
}

export const getHRRoadmap = () => api.get<{ items: HRRoadmapItem[] }>('/api/v1/hr/roadmap');

export interface NotifyStaleResult {
	sent: number;
	skipped: number;
	targeted: number;
	emails?: string[];
}

export const notifyStaleEmployees = (minDaysSince = 60) =>
	api.post<NotifyStaleResult>('/api/v1/hr/notify-stale', { min_days_since: minDaysSince });
