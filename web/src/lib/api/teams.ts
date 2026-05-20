import { api } from './client';

export interface Team {
	id: string;
	name: string;
	owner_id?: string;
	created_at: string;
}

export interface TeamMember {
	employee_id: string;
	full_name: string;
	role: UserRole;
	department?: string;
	timezone?: string;
	work_format?: string;
	last_profile_update_at?: string;
}

export type CellState = 'free' | 'busy' | 'conflict' | 'off';

export interface CellEventRef {
	title: string;
	start_at: string;
	end_at: string;
}

export interface CellExceptionRef {
	kind: string;
	comment?: string;
	start_at: string;
	end_at: string;
}

export type OffNote = 'before_work' | 'after_work' | 'day_off' | 'no_profile' | '';

export interface CellDetail {
	events?: CellEventRef[];
	exception?: CellExceptionRef;
	note?: OffNote;
}

export interface MemberAvailability {
	employee_id: string;
	full_name: string;
	timezone?: string;
	cells: CellState[];
	details: CellDetail[];
}

export interface TeamAvailability {
	team_id: string;
	from: string;
	to: string;
	hours: number[];
	days: string[];
	rows: MemberAvailability[];
	timezone: string;
}

export const listTeams = () => api.get<{ teams: Team[] }>('/api/v1/teams');
export const getTeam = (id: string) => api.get<Team>(`/api/v1/teams/${id}`);
export const listMembers = (id: string) =>
	api.get<{ members: TeamMember[] }>(`/api/v1/teams/${id}/members`);
export const getAvailability = (id: string, tz?: string) => {
	const q = tz ? `?tz=${encodeURIComponent(tz)}` : '';
	return api.get<TeamAvailability>(`/api/v1/teams/${id}/availability${q}`);
};

export type UnavailableReason = 'busy' | 'in_exception' | 'outside_hours' | 'no_profile';

export interface MeetingParticipant {
	employee_id: string;
	full_name: string;
	reason?: UnavailableReason;
	// Локальное окно встречи в TZ участника, например "07:00–08:00".
	local_time?: string;
	// Рабочие часы участника в этот день недели в его TZ, например "10:00–18:00".
	work_hours?: string;
	// TZ профиля участника, например "Europe/Lisbon".
	timezone?: string;
}

export interface MeetingWindow {
	start_at: string;
	end_at: string;
	available_count: number;
	total_count: number;
	available: MeetingParticipant[];
	unavailable: MeetingParticipant[];
}

export const findWindow = (
	id: string,
	body: { duration_min?: number; days?: number; tz?: string; top_n?: number }
) => api.post<{ windows: MeetingWindow[] }>(`/api/v1/teams/${id}/find-window`, body);

// --- управление командой ---

export const createTeam = (body: { name: string; owner_employee_id?: string }) =>
	api.post<Team>('/api/v1/teams', body);

export const updateTeam = (id: string, body: { name?: string; owner_employee_id?: string | null }) =>
	api.patch<Team>(`/api/v1/teams/${id}`, body);

export const deleteTeam = (id: string) => api.delete<void>(`/api/v1/teams/${id}`);

export const addMember = (id: string, employeeID: string) =>
	api.post<void>(`/api/v1/teams/${id}/members`, { employee_id: employeeID });

export const removeMember = (id: string, employeeID: string) =>
	api.delete<void>(`/api/v1/teams/${id}/members/${employeeID}`);

export const setTeamManager = (id: string, managerEmployeeID: string) =>
	api.post<void>(`/api/v1/teams/${id}/manager`, { manager_employee_id: managerEmployeeID });

export interface ProposeMeetingResult {
	sent: number;
	team_name: string;
	start_at: string;
	end_at: string;
	yandex_event_uid?: string;
	yandex_pushed?: number;
}

export const proposeMeeting = (
	teamID: string,
	body: { start_at: string; end_at: string; title?: string }
) => api.post<ProposeMeetingResult>(`/api/v1/teams/${teamID}/propose-meeting`, body);
