// «Куда уходит время» — топ-категорий встреч за период.
import { api } from './client';

export interface TimeBreakdownItem {
	category: string;
	minutes: number;
	hours: number;
	count: number;
	percent: number;
}

export interface TimeBreakdown {
	from: string;
	to: string;
	total_minutes: number;
	total_hours: number;
	items: TimeBreakdownItem[];
}

export const getMyTimeBreakdown = (days = 30) =>
	api.get<TimeBreakdown>(`/api/v1/me/time-breakdown?days=${days}`);

export const getTeamTimeBreakdown = (teamID: string, days = 30) =>
	api.get<TimeBreakdown>(`/api/v1/teams/${teamID}/time-breakdown?days=${days}`);
