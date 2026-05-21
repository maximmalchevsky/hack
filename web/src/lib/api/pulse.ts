// Pulse-check API: короткий опрос «как ты сейчас» раз в 2 недели.
import { api } from './client';

export interface PulseEntry {
	id: string;
	score: number;        // 1..5
	comment?: string;
	created_at: string;
}

export interface PulseMe {
	should_ask: boolean;
	days_since?: number;
	last?: PulseEntry;
	history: PulseEntry[];   // последние 6 от нового к старому
}

export interface PulseTeamMember {
	employee_id: string;
	full_name: string;
	department?: string;
	last_score?: number;
	last_at?: string;
	days_since?: number;
	comment?: string;
	trend: number[];   // от старого к новому, 0..4 чисел
}

export interface PulseTeamSummary {
	members: PulseTeamMember[];
	avg_last: number;
	red_zone: number;
	no_data: number;
}

export const getPulseMe = () => api.get<PulseMe>('/api/v1/pulse/me');

export const submitPulse = (score: number, comment?: string) =>
	api.post<PulseEntry>('/api/v1/pulse', { score, comment: comment ?? '' });

export const getPulseTeam = () => api.get<PulseTeamSummary>('/api/v1/pulse/team');
