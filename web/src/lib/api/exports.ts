import { api } from './client';

// JSON-структура выгрузки для рендера PDF на фронте.
export interface ExportDataset {
	kind: string;
	title: string;
	headers: string[];
	rows: unknown[][];
}

export const getExportDataset = (kind: string) =>
	api.get<ExportDataset>(`/api/v1/exports/${kind}?format=json`);

export interface ExportQuery {
	from?: string;          // YYYY-MM-DD
	to?: string;            // YYYY-MM-DD
	departments?: string[];
	columns?: string[];
}

export function buildExportURL(kind: string, format: 'json' | 'xlsx', q: ExportQuery): string {
	const params = new URLSearchParams();
	params.set('format', format);
	if (q.from) params.set('from', q.from);
	if (q.to) params.set('to', q.to);
	if (q.departments && q.departments.length > 0) {
		params.set('departments', q.departments.join(','));
	}
	if (q.columns && q.columns.length > 0) {
		params.set('columns', q.columns.join(','));
	}
	return `/api/v1/exports/${kind}?${params.toString()}`;
}

export const getExportDatasetFiltered = (kind: string, q: ExportQuery) =>
	api.get<ExportDataset>(buildExportURL(kind, 'json', q));
