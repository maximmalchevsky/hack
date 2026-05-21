// Saved views — пользовательские пресеты фильтров.
import { api } from './client';

export type ViewPage = 'analytics' | 'diagnostics';

export interface ViewPreset {
	id: string;
	page: ViewPage;
	name: string;
	filters: Record<string, unknown>;
	created_at: string;
}

export const listViewPresets = (page: ViewPage) =>
	api.get<{ presets: ViewPreset[] }>(`/api/v1/view-presets?page=${page}`);

export const createViewPreset = (page: ViewPage, name: string, filters: Record<string, unknown>) =>
	api.post<ViewPreset>('/api/v1/view-presets', { page, name, filters });

export const deleteViewPreset = (id: string) => api.delete<void>(`/api/v1/view-presets/${id}`);
