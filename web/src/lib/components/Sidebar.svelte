<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { page } from '$app/stores';
	import { user } from '$lib/stores/user';
	import { sidebarCounts } from '$lib/stores/sidebar-counts';

	type BadgeKind = 'diagnostics' | 'conflicts' | 'hrRoadmap' | 'notifications';

	interface NavItem {
		page: string;
		href: string;
		label: string;
		icon: string;
		roles: UserRole[];
		badgeKind?: BadgeKind;
		badgeVariant?: 'danger';
	}

	interface NavGroup {
		title: string;
		items: NavItem[];
		// если задан — группа видна только для этих ролей
		roles?: UserRole[];
	}

	// Структура навигации 1:1 из прототипа.
	const groups: NavGroup[] = [
		{
			title: 'Основное',
			items: [
				{
					page: 'dashboard',
					href: '/dashboard',
					label: 'Дашборд',
					icon: 'ti-layout-dashboard',
					roles: ['admin', 'employee', 'manager', 'pm', 'hr', 'analyst']
				},
				{
					page: 'profile',
					href: '/profile',
					label: 'Мой профиль',
					icon: 'ti-user',
					roles: ['employee', 'manager', 'pm', 'hr', 'analyst', 'admin']
				},
				{
					page: 'team-map',
					href: '/team-map',
					label: 'Карта команды',
					icon: 'ti-calendar-stats',
					roles: ['manager', 'pm', 'hr', 'analyst', 'admin']
				},
				{
					page: 'workload',
					href: '/workload',
					label: 'Загрузка',
					icon: 'ti-gauge',
					roles: ['employee', 'manager', 'hr', 'admin']
				}
			]
		},
		{
			title: 'Управление',
			items: [
				{
					page: 'diagnostics',
					href: '/diagnostics',
					label: 'Диагностика',
					icon: 'ti-stethoscope',
					roles: ['manager', 'pm', 'hr', 'admin'],
					badgeKind: 'diagnostics',
					badgeVariant: 'danger'
				},
				{
					page: 'conflicts',
					href: '/conflicts',
					label: 'Конфликты',
					icon: 'ti-alert-triangle',
					roles: ['manager', 'pm', 'hr', 'admin'],
					badgeKind: 'conflicts',
					badgeVariant: 'danger'
				},
				{
					page: 'recommendations',
					href: '/recommendations',
					label: 'Рекомендации',
					icon: 'ti-bulb',
					roles: ['employee', 'manager', 'pm', 'hr', 'analyst', 'admin']
				},
				{
					page: 'scheduler',
					href: '/scheduler',
					label: 'Планировщик',
					icon: 'ti-calendar-plus',
					// Открыт всем: employee/analyst видят только «Входящие приглашения»
					// (без блока поиска окон). Манагерам — полный функционал.
					roles: ['employee', 'manager', 'pm', 'hr', 'analyst', 'admin']
				},
				{
					page: 'teams',
					href: '/teams',
					label: 'Команды',
					icon: 'ti-users-group',
					roles: ['admin', 'hr', 'pm', 'manager']
				},
				{
					page: 'hr-roadmap',
					href: '/hr-roadmap',
					label: 'Дорожная карта HR',
					icon: 'ti-flag-3',
					roles: ['hr', 'admin'],
					badgeKind: 'hrRoadmap',
					badgeVariant: 'danger'
				}
			]
		},
		{
			title: 'Информация',
			items: [
				{
					page: 'analytics',
					href: '/analytics',
					label: 'Аналитика',
					icon: 'ti-chart-line',
					roles: ['manager', 'pm', 'hr', 'analyst', 'admin']
				},
				{
					page: 'reports',
					href: '/reports',
					label: 'Отчёты',
					icon: 'ti-file-export',
					roles: ['manager', 'pm', 'hr', 'analyst', 'admin']
				},
				{
					page: 'ai-chat',
					href: '/ai-chat',
					label: 'AI-ассистент',
					icon: 'ti-sparkles',
					roles: ['employee', 'manager', 'pm', 'hr', 'analyst', 'admin']
				},
				{
					page: 'notifications',
					href: '/notifications',
					label: 'Уведомления',
					icon: 'ti-bell',
					roles: ['employee', 'manager', 'pm', 'hr', 'analyst', 'admin'],
					badgeKind: 'notifications'
				},
				{
					page: 'integrations',
					href: '/integrations',
					label: 'Интеграции',
					icon: 'ti-plug',
					roles: ['employee', 'manager', 'pm', 'hr', 'admin']
				}
			]
		},
		{
			title: 'Администрирование',
			roles: ['admin'],
			items: [
				{
					page: 'admin-users',
					href: '/admin/users',
					label: 'Пользователи',
					icon: 'ti-users',
					roles: ['admin']
				},
				{
					page: 'admin-sources',
					href: '/admin/sources',
					label: 'Источники',
					icon: 'ti-database',
					roles: ['admin']
				},
				{
					page: 'admin-rules',
					href: '/admin/rules',
					label: 'Правила метрик',
					icon: 'ti-adjustments',
					roles: ['admin']
				},
				{
					page: 'admin-audit',
					href: '/admin/audit',
					label: 'Журнал изменений',
					icon: 'ti-history',
					roles: ['admin']
				}
			]
		}
	];

	const role = $derived<UserRole>($user?.role ?? 'employee');

	onMount(() => sidebarCounts.start());
	onDestroy(() => sidebarCounts.stop());

	function isActive(href: string): boolean {
		return $page.url.pathname === href || $page.url.pathname.startsWith(href + '/');
	}

	function badgeFor(kind: BadgeKind, counts: typeof $sidebarCounts): number | null {
		const v = counts[kind];
		return typeof v === 'number' && v > 0 ? v : null;
	}

	const visibleGroups = $derived(
		groups
			.filter((g) => !g.roles || g.roles.includes(role))
			.map((g) => ({
				...g,
				items: g.items.filter((it) => it.roles.includes(role))
			}))
			.filter((g) => g.items.length > 0)
	);
</script>

<aside class="sidebar">
	{#each visibleGroups as group (group.title)}
		<div class="sidebar__group">
			<div class="sidebar__group-title">{group.title}</div>
			{#each group.items as item (item.page)}
				{@const badgeValue = item.badgeKind ? badgeFor(item.badgeKind, $sidebarCounts) : null}
				<a class="nav-item" class:active={isActive(item.href)} href={item.href}>
					<i class="ti {item.icon}"></i>{item.label}
					{#if badgeValue !== null}
						<span
							class="nav-item__badge {item.badgeVariant === 'danger'
								? 'nav-item__badge--danger'
								: ''}"
						>
							{badgeValue}
						</span>
					{/if}
				</a>
			{/each}
		</div>
	{/each}
</aside>
