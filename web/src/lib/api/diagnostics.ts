import { api } from './client';

export interface DiagnosticsRow {
	employee_id: string;
	full_name: string;
	department?: string;
	role: UserRole;
	timezone?: string;
	hr_work_format?: string;
	last_profile_update_at?: string;
	days_since_update: number;
	freshness: number;
	group: 'fresh' | 'stale' | 'needs_confirm' | 'unknown';
	// Ближайшее отсутствие в следующие 14 дней.
	upcoming_exception?: 'vacation' | 'sick_leave' | 'business_trip' | 'personal_hours' | 'custom';
	upcoming_exception_at?: string;
	upcoming_exception_days?: number;
}

export interface DiagnosticsGroups {
	fresh: DiagnosticsRow[];
	stale: DiagnosticsRow[];
	needs_confirm: DiagnosticsRow[];
	unknown: DiagnosticsRow[];
	total: number;
}

export const getDiagnostics = () => api.get<DiagnosticsGroups>('/api/v1/diagnostics/groups');

export interface BurnoutRow {
	employee_id: string;
	full_name: string;
	department?: string;
	role: string;
	l1: number;
	l2: number;
	c1: number;
	c2: number;
	reasons: string[];
}

export const getBurnout = () =>
	api.get<{ burnout: BurnoutRow[] }>('/api/v1/diagnostics/burnout');
