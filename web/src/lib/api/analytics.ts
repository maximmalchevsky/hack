import { api } from './client';

// --- Company (Admin/HR/PM/Analyst) ---

export interface OverviewKPI {
	employees: number;
	avg_a: number;
	avg_r: number;
	avg_l: number;
	conflicts_7d: number;
	stale_profiles: number;
	needs_confirm: number;
	on_vacation_now: number;
}

export interface TeamRisk {
	team_id: string;
	team_name: string;
	avg_r: number;
	avg_a: number;
	members: number;
}

export interface WeekdayConflicts {
	weekday: number; // 1=Mon..7=Sun
	count: number;
}

export interface WeekFreshness {
	week_start: string;
	avg_a: number;
}

export interface GroupSlice {
	group: 'fresh' | 'needs_confirm' | 'stale' | 'unknown';
	count: number;
}

export const getOverview = () => api.get<OverviewKPI>('/api/v1/analytics/overview');
export const getRiskByTeam = () =>
	api.get<{ teams: TeamRisk[] }>('/api/v1/analytics/risk-by-team');
export const getConflictsByWeekday = () =>
	api.get<{ days: WeekdayConflicts[] }>('/api/v1/analytics/conflicts-by-weekday');
export const getFreshnessTrend = () =>
	api.get<{ weeks: WeekFreshness[] }>('/api/v1/analytics/freshness-trend');
export const getGroupsDistribution = () =>
	api.get<{ groups: GroupSlice[] }>('/api/v1/analytics/groups-distribution');

export interface TeamScore {
	team_id: string;
	team_name: string;
	members: number;
	avg_a: number;
	avg_r: number;
	score: number;
	rank: number;
}

export const getLeaderboard = () =>
	api.get<{ teams: TeamScore[] }>('/api/v1/analytics/leaderboard');

export interface Anomaly {
	employee_id: string;
	full_name: string;
	department?: string;
	day: string;
	events: number;
	mean: number;
	std_dev: number;
	z_score: number;
	times_mean: number;
}

export const getAnomalies = () =>
	api.get<{ anomalies: Anomaly[] }>('/api/v1/analytics/anomalies');

export interface ConflictForecast {
	employee_id: string;
	full_name: string;
	department?: string;
	weeks: number[];
	trend: number;
	current_rate: number;
	risk: 'low' | 'medium' | 'high';
	reason: string;
}

export const getForecast = () =>
	api.get<{ forecast: ConflictForecast[] }>('/api/v1/analytics/forecast');

// --- Me (любой авторизованный) ---

export interface MeOverview {
	avg_a: number;
	avg_r: number;
	avg_l: number;
	days_since_update: number; // -1 если ни разу
	events_7d: number;
	hours_7d: number;
	conflicts_30d: number;
}

export interface MeTrendPoint {
	week_start: string;
	avg_a: number;
	avg_l: number;
}

export interface MeHoursWeek {
	week_start: string;
	hours: number;
}

export const getMeOverview = () => api.get<MeOverview>('/api/v1/analytics/me/overview');
export const getMeTrend = () =>
	api.get<{ weeks: MeTrendPoint[] }>('/api/v1/analytics/me/trend');
export const getMeConflictsByWeekday = () =>
	api.get<{ days: WeekdayConflicts[] }>('/api/v1/analytics/me/conflicts-by-weekday');
export const getMeHoursByWeek = () =>
	api.get<{ weeks: MeHoursWeek[] }>('/api/v1/analytics/me/hours-by-week');

// --- Teams (Manager/PM/HR/Admin) ---

export interface TeamRef {
	id: string;
	name: string;
	members: number;
}

export interface TeamScopeOverview {
	employees: number;
	avg_a: number;
	avg_r: number;
	avg_l: number;
	conflicts_7d: number;
	stale_profiles: number;
	needs_confirm: number;
	on_vacation_now: number;
}

const scopeQuery = (teamId?: string) =>
	teamId ? `?team_id=${encodeURIComponent(teamId)}` : '';

export const getTeamsMy = () =>
	api.get<{ teams: TeamRef[] }>('/api/v1/analytics/teams/my');
export const getTeamsOverview = (teamId?: string) =>
	api.get<TeamScopeOverview>(`/api/v1/analytics/teams/overview${scopeQuery(teamId)}`);
export const getTeamsRiskByTeam = () =>
	api.get<{ teams: TeamRisk[] }>('/api/v1/analytics/teams/risk-by-team');
export const getTeamsConflictsByWeekday = (teamId?: string) =>
	api.get<{ days: WeekdayConflicts[] }>(
		`/api/v1/analytics/teams/conflicts-by-weekday${scopeQuery(teamId)}`
	);
export const getTeamsFreshnessTrend = (teamId?: string) =>
	api.get<{ weeks: WeekFreshness[] }>(
		`/api/v1/analytics/teams/freshness-trend${scopeQuery(teamId)}`
	);
export const getTeamsGroupsDistribution = (teamId?: string) =>
	api.get<{ groups: GroupSlice[] }>(
		`/api/v1/analytics/teams/groups-distribution${scopeQuery(teamId)}`
	);

// recomputeAllMetrics — admin: пересчитать метрики всем сотрудникам.
// Возвращает {queued, total}.
export const recomputeAllMetrics = () =>
	api.post<{ queued: number; total: number }>('/api/v1/admin/metrics/recompute-all', {});
