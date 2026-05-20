import { api } from './client';

export type ReportKind =
	| 'upcoming_vacations'
	| 'stale_profiles'
	| 'conflicts'
	| 'all_employees';

export interface ReportPresetFilters {
	from?: string; // ISO
	to?: string;   // ISO
	departments?: string[];
}

export interface ReportPreset {
	id: string;
	user_id: string;
	name: string;
	kind: ReportKind;
	columns: string[];
	filters: ReportPresetFilters;
	created_at: string;
	updated_at: string;
}

export interface CreatePresetBody {
	name: string;
	kind: ReportKind;
	columns: string[];
	filters: ReportPresetFilters;
}

export const listPresets = () =>
	api.get<{ presets: ReportPreset[] }>('/api/v1/report-presets/');

export const createPreset = (body: CreatePresetBody) =>
	api.post<ReportPreset>('/api/v1/report-presets/', body);

export const updatePreset = (id: string, body: CreatePresetBody) =>
	api.put<ReportPreset>(`/api/v1/report-presets/${id}`, body);

export const deletePreset = (id: string) =>
	api.delete<{ ok: boolean }>(`/api/v1/report-presets/${id}`);
