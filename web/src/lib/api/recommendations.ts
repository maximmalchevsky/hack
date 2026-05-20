import { api } from './client';

export type RecommendationScope = 'mine' | 'team' | 'all';

export interface EmployeeRef {
	id: string;
	full_name: string;
	role?: string;
	department?: string;
}

export interface Recommendation {
	id: string;
	employee_id?: string;
	employee?: EmployeeRef;
	kind: string;
	priority: 'low' | 'medium' | 'high' | 'critical';
	title: string;
	explanation: string;
	status: 'new' | 'seen' | 'applied' | 'dismissed';
	generated_by: string;
	evidence?: Record<string, unknown>;
	created_at: string;
}

export const listRecommendations = (scope: RecommendationScope = 'mine') => {
	const q = scope && scope !== 'mine' ? `?scope=${scope}` : '';
	return api.get<{ recommendations: Recommendation[]; scope: string }>(
		`/api/v1/recommendations${q}`
	);
};

export const generateRecommendations = () =>
	api.post<{ recommendations: Recommendation[] }>('/api/v1/recommendations/generate');

export const applyRecommendation = (id: string) =>
	api.post<void>(`/api/v1/recommendations/${id}/apply`);

export const dismissRecommendation = (id: string) =>
	api.post<void>(`/api/v1/recommendations/${id}/dismiss`);
