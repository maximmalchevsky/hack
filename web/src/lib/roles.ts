// Локализация ролей пользователей.
//
// На бэке роли хранятся как enum-строки на английском (employee/manager/hr/...),
// а в UI везде показываем на русском. Один источник правды — этот файл,
// чтобы не плодить дубли подписей в каждой странице.

export type RoleSlug = 'admin' | 'employee' | 'manager' | 'hr' | 'pm' | 'analyst';

const ROLE_LABELS: Record<string, string> = {
	admin: 'Администратор',
	employee: 'Сотрудник',
	manager: 'Руководитель',
	hr: 'HR',
	pm: 'Проектный менеджер',
	analyst: 'Аналитик'
};

// roleLabel — возвращает русскую подпись роли. Если роль неизвестна (новая,
// сторонняя система, и т.п.) — отдаём как есть, без падения.
export function roleLabel(role: string | null | undefined): string {
	if (!role) return '';
	return ROLE_LABELS[role] ?? role;
}

// ROLES — упорядоченный список для <select> в админке.
export const ROLES: { value: RoleSlug; label: string }[] = [
	{ value: 'admin', label: ROLE_LABELS.admin },
	{ value: 'employee', label: ROLE_LABELS.employee },
	{ value: 'manager', label: ROLE_LABELS.manager },
	{ value: 'hr', label: ROLE_LABELS.hr },
	{ value: 'pm', label: ROLE_LABELS.pm },
	{ value: 'analyst', label: ROLE_LABELS.analyst }
];
