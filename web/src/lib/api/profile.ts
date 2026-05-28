import { api } from './client';

export interface CalendarEvent {
	id: string;
	title: string;
	description?: string;
	start_at: string;
	end_at: string;
	timezone?: string;
	attendees_count?: number;
	organizer?: string;
	status: 'confirmed' | 'tentative' | 'cancelled';
	is_excluded?: boolean;
	// Тип встречи. Пусто = ещё не классифицировано.
	category?: string;
}

export const listMyEvents = (from: string, to: string) => {
	const q = new URLSearchParams({ from, to }).toString();
	return api.get<{ events: CalendarEvent[]; from: string; to: string }>(
		`/api/v1/me/events?${q}`
	);
};

// setEventCategory — выставляет/сбрасывает категорию своей встречи.
// Пустая строка = сбрасывает (тогда GigaChat пере-классифицирует при подсчёте).
export const setEventCategory = (eventID: string, category: string) =>
	api.patch<{ ok: boolean; category: string }>(
		`/api/v1/me/events/${eventID}/category`,
		{ category }
	);

// setEventTitle — переименовывает своё событие. Пустое название бэк отклонит.
export const setEventTitle = (eventID: string, title: string) =>
	api.patch<{ ok: boolean; title: string }>(
		`/api/v1/me/events/${eventID}/title`,
		{ title }
	);

export interface DayHours {
	start: string;
	end: string;
}

export interface DaysOfWeek {
	mon?: DayHours;
	tue?: DayHours;
	wed?: DayHours;
	thu?: DayHours;
	fri?: DayHours;
	sat?: DayHours;
	sun?: DayHours;
}

export type WorkFormat = 'office' | 'remote' | 'hybrid';

export interface WorkProfile {
	id: string;
	employee_id: string;
	valid_from: string;
	valid_to?: string;
	days_of_week: DaysOfWeek;
	timezone: string;
	work_format: WorkFormat;
	source: string;
	is_active: boolean;
	created_at: string;
}

export interface TimeException {
	id: string;
	employee_id: string;
	kind: 'vacation' | 'sick_leave' | 'business_trip' | 'personal_hours' | 'custom';
	start_at: string;
	end_at: string;
	comment?: string;
	source: string;
	created_at: string;
}

export interface MeResponse {
	user: {
		id: string;
		email: string;
		role: UserRole;
		full_name: string;
		timezone: string;
		locale: string;
		avatar_url?: string;
		created_at: string;
	};
	employee?: {
		id: string;
		user_id: string;
		department?: string;
		position?: string;
		hr_work_format?: string;
		last_profile_update_at?: string;
		last_confirmed_at?: string;
	};
	work_profile?: WorkProfile;
	exceptions?: TimeException[];
}

export const me = () => api.get<MeResponse>('/api/v1/me');

export interface WeeklySummary {
	week_start: string;
	week_end: string;
	events_total: number;
	hours_busy: number;
	hours_work: number;
	busy_percent: number;
	by_day: { day: string; hours_busy: number; hours_work: number; events: number }[];
	busiest_day?: string;
	freest_day?: string;
	conflicts: number;
	next_exception?: { kind: string; start_at: string; end_at: string };
	ai_text: string;
	generated_by: 'ai' | 'rule';
}

export const getWeeklySummary = () => api.get<WeeklySummary>('/api/v1/me/weekly-summary');

export const getProfileHistory = (employeeID: string) =>
	api.get<{ versions: WorkProfile[] }>(`/api/v1/profiles/${employeeID}/history`);

export const updateMyProfile = (body: {
	days_of_week: DaysOfWeek;
	timezone: string;
	work_format: WorkFormat;
}) => api.put<WorkProfile>('/api/v1/me/profile', body);

export const confirmMyProfile = () => api.post<void>('/api/v1/me/profile/confirm');

// updateMyEmail — меняет email текущего пользователя. После смены логин
// тоже становится новым (входить нужно по новой почте).
export const updateMyEmail = (email: string) =>
	api.patch<{ ok: boolean; email: string }>('/api/v1/me/email', { email });

export const listExceptions = (params?: { from?: string; to?: string }) => {
	const q = new URLSearchParams();
	if (params?.from) q.set('from', params.from);
	if (params?.to) q.set('to', params.to);
	const suffix = q.toString() ? `?${q.toString()}` : '';
	return api.get<{ exceptions: TimeException[] }>(`/api/v1/exceptions${suffix}`);
};

export const createException = (body: {
	kind: TimeException['kind'];
	start_at: string;
	end_at: string;
	comment?: string;
}) => api.post<TimeException>('/api/v1/exceptions', body);

export const deleteException = (id: string) => api.delete<void>(`/api/v1/exceptions/${id}`);
