import { api } from './client';

export type TaskPriority = 'highest' | 'high' | 'medium' | 'low' | 'lowest' | '';

export interface TaskSlot {
	date: string; // YYYY-MM-DD
	hours: number;
}

export interface TrackerTask {
	id: string;
	integration_id?: string;
	source_task_id: string;
	title: string;
	description?: string;
	status?: string;
	priority?: TaskPriority;
	type?: string;
	due_at?: string;
	estimated_hours?: number;
	actual_hours?: number;
	ai_estimated_hours?: number;
	ai_confidence?: number;
	slots?: TaskSlot[];
}

export interface TasksResponse {
	tasks: TrackerTask[];
	horizon_end: string;
}

export const listMyTasks = () => api.get<TasksResponse>('/api/v1/me/tasks');

export const replanTasks = () =>
	api.post<{ ai_calls: number; total_hours: number; horizon_end: string; planned_tasks: number }>(
		'/api/v1/me/tasks/replan',
		{}
	);

export const setTaskEstimate = (id: string, hours: number) =>
	api.patch<{ ok: boolean; hours: number }>(`/api/v1/me/tasks/${id}/estimate`, { hours });
