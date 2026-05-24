<script lang="ts">
	import { onMount } from 'svelte';
	import Card from '$lib/components/Card.svelte';
	import Badge from '$lib/components/Badge.svelte';
	import Button from '$lib/components/Button.svelte';
	import Avatar from '$lib/components/Avatar.svelte';
	import Tabs from '$lib/components/Tabs.svelte';
	import { getHRRoadmap, type HRRoadmapItem } from '$lib/api/hr';
	import { ApiError } from '$lib/api/client';
	import { roleLabel } from '$lib/roles';

	let items = $state<HRRoadmapItem[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let activeTab = $state('all');

	onMount(async () => {
		try {
			const r = await getHRRoadmap();
			items = r.items ?? [];
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		} finally {
			loading = false;
		}
	});

	const counts = $derived({
		critical: items.filter((i) => i.priority === 'critical').length,
		high: items.filter((i) => i.priority === 'high').length,
		medium: items.filter((i) => i.priority === 'medium').length
	});

	const tabs = $derived([
		{ id: 'all', label: 'Все', count: items.length },
		{ id: 'critical', label: 'Критично', count: counts.critical },
		{ id: 'high', label: 'Высокий', count: counts.high },
		{ id: 'medium', label: 'Средний', count: counts.medium }
	]);

	const visible = $derived(
		activeTab === 'all' ? items : items.filter((i) => i.priority === activeTab)
	);

	function priorityVariant(p: HRRoadmapItem['priority']): 'danger' | 'warning' | 'info' | 'neutral' {
		if (p === 'critical') return 'danger';
		if (p === 'high') return 'danger';
		if (p === 'medium') return 'warning';
		return 'info';
	}

	function actionLabel(a: HRRoadmapItem['action']): string {
		switch (a) {
			case 'request_confirm':
				return 'Запросить подтверждение';
			case 'request_update':
				return 'Запросить обновление';
			case 'check_hr':
				return 'Проверить HR-данные';
			case 'review_format':
				return 'Пересмотреть формат';
		}
	}

	function initials(name: string): string {
		const parts = name.trim().split(/\s+/);
		return ((parts[0]?.[0] ?? '') + (parts[1]?.[0] ?? '')).toUpperCase() || '??';
	}
</script>

<div class="page-header">
	<div>
		<h1>Дорожная карта HR</h1>
		<div class="page-header__subtitle">
			Приоритезированный список действий: кому из сотрудников написать, что подтвердить, что
			пересмотреть.
		</div>
	</div>
</div>

{#if error}
	<div class="section">
		<Badge variant="danger">
			<i class="ti ti-alert-circle"></i>
			{error}
		</Badge>
	</div>
{/if}

{#if loading}
	<div class="text-text-3 text-sm">Загрузка…</div>
{:else if items.length === 0}
	<Card>
		<div class="text-text-3 text-sm" style="padding: 16px; text-align: center;">
			Всё актуально — ни одного устаревшего профиля.
		</div>
	</Card>
{:else}
	<Tabs {tabs} bind:value={activeTab} />

	<div class="space-y-2">
		{#each visible as it (it.employee_id)}
			<Card>
				<div class="flex items-center gap-3">
					<a href="/employees/{it.employee_id}" style="display: contents;">
						<Avatar initials={initials(it.full_name)} size="md" variant="purple" />
					</a>
					<div class="flex-1">
						<div class="flex items-center gap-2 mb-1">
							<a href="/employees/{it.employee_id}" class="emp-link">
								<div class="card__title">{it.full_name}</div>
							</a>
							<Badge variant={priorityVariant(it.priority)}>{it.priority}</Badge>
							<Badge variant="neutral">{actionLabel(it.action)}</Badge>
						</div>
						<div class="text-text-2 text-sm">{it.reason}</div>
						<div class="text-text-3 text-xs mt-1">
							{roleLabel(it.role)}
							{#if it.department} · {it.department}{/if}
							{#if it.days_since_update < 9999}· {it.days_since_update} дн с обновления{:else}· профиль не задан{/if}
						</div>
					</div>
					<div class="flex flex-col gap-1">
						<a href={`/employees/${it.employee_id}`} class="btn btn--sm">
							<i class="ti ti-user"></i>Карточка
						</a>
						<Button size="sm" variant="primary" icon="ti-mail">Написать</Button>
					</div>
				</div>
			</Card>
		{/each}
	</div>
{/if}
