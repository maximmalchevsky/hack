import { api } from './client';

export interface EmployeeMetrics {
	employee_id: string;
	A: number;
	C: number;
	L: number;
	Z: number;
	H: number;
	R: number;
}

export const getEmployeeMetrics = (id: string) =>
	api.get<EmployeeMetrics>(`/api/v1/metrics/employee/${id}`);
