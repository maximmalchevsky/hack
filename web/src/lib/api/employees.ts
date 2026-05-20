import { api } from './client';
import type { TimeException, WorkProfile } from './profile';

export interface EmployeeListRow {
	employee_id: string;
	user_id: string;
	email: string;
	full_name: string;
	role: UserRole;
	department?: string;
	position?: string;
	timezone?: string;
	hr_work_format?: string;
	last_profile_update_at?: string;
}

export interface IntegrationListRow {
	id: string;
	provider: string;
	account_email?: string;
	account_label?: string;
	status: string;
	last_sync_at?: string;
	last_error?: string;
}

export interface EmployeeDetail {
	employee: EmployeeListRow;
	work_profile?: WorkProfile;
	exceptions?: TimeException[];
	integrations?: IntegrationListRow[];
	upcoming_events_count?: number;
}

export const listEmployees = () =>
	api.get<{ employees: EmployeeListRow[] }>('/api/v1/employees');

export const getEmployeeDetail = (id: string) =>
	api.get<EmployeeDetail>(`/api/v1/employees/${id}`);
