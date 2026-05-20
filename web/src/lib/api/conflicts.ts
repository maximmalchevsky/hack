import { api } from './client';

export interface ConflictRow {
	employee_id: string;
	full_name: string;
	department?: string;
	event_id: string;
	title: string;
	start_at: string;
	end_at: string;
	reason: 'outside_hours' | 'weekend' | 'within_exception' | 'no_profile';
	severity: 'low' | 'medium' | 'high';
}

export const listConflicts = (opts?: { from?: string; to?: string }) => {
	const q = new URLSearchParams();
	if (opts?.from) q.set('from', opts.from);
	if (opts?.to) q.set('to', opts.to);
	const suffix = q.toString() ? `?${q.toString()}` : '';
	return api.get<{ conflicts: ConflictRow[] }>(`/api/v1/conflicts${suffix}`);
};

export const listEmployeeConflicts = (employeeID: string, opts?: { from?: string; to?: string }) => {
	const q = new URLSearchParams();
	if (opts?.from) q.set('from', opts.from);
	if (opts?.to) q.set('to', opts.to);
	const suffix = q.toString() ? `?${q.toString()}` : '';
	return api.get<{ conflicts: ConflictRow[] }>(`/api/v1/conflicts/employee/${employeeID}${suffix}`);
};
