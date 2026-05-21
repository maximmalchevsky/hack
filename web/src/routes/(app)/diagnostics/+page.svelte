<script lang="ts">
	import { onMount } from 'svelte';
	import Card from '$lib/components/Card.svelte';
	import Badge from '$lib/components/Badge.svelte';
	import Tabs from '$lib/components/Tabs.svelte';
	import Avatar from '$lib/components/Avatar.svelte';
	import MetricInfo from '$lib/components/MetricInfo.svelte';
	import {
		getDiagnostics,
		getBurnout,
		type DiagnosticsGroups,
		type DiagnosticsRow,
		type BurnoutRow
	} from '$lib/api/diagnostics';
	import { ApiError } from '$lib/api/client';

	let groups = $state<DiagnosticsGroups | null>(null);
	let burnout = $state<BurnoutRow[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let activeTab = $state('all');

	onMount(async () => {
		try {
			const [g, b] = await Promise.all([
				getDiagnostics(),
				getBurnout().catch(() => ({ burnout: [] as BurnoutRow[] }))
			]);
			groups = g;
			burnout = b.burnout ?? [];
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		} finally {
			loading = false;
		}
	});

	const tabs = $derived(
		groups
			? [
					{ id: 'all', label: 'Все', count: groups.total ?? 0 },
					{ id: 'stale', label: 'Устаревшие', count: (groups.stale ?? []).length },
					{ id: 'needs_confirm', label: 'Подтвердить', count: (groups.needs_confirm ?? []).length },
					{ id: 'fresh', label: 'Актуальные', count: (groups.fresh ?? []).length },
					{ id: 'unknown', label: 'Без данных', count: (groups.unknown ?? []).length },
					{ id: 'burnout', label: 'Выгорание', count: burnout.length }
				]
			: []
	);

	const visible = $derived(visibleRows(activeTab, groups));

	function visibleRows(tab: string, g: DiagnosticsGroups | null): DiagnosticsRow[] {
		if (!g) return [];
		switch (tab) {
			case 'stale':
				return g.stale ?? [];
			case 'needs_confirm':
				return g.needs_confirm ?? [];
			case 'fresh':
				return g.fresh ?? [];
			case 'unknown':
				return g.unknown ?? [];
			default:
				return [
					...(g.stale ?? []),
					...(g.needs_confirm ?? []),
					...(g.fresh ?? []),
					...(g.unknown ?? [])
				];
		}
	}

	function initials(name: string): string {
		const parts = name.trim().split(/\s+/);
		return ((parts[0]?.[0] ?? '') + (parts[1]?.[0] ?? '')).toUpperCase() || '??';
	}

	function groupVariant(g: DiagnosticsRow['group']): 'success' | 'warning' | 'danger' | 'neutral' {
		switch (g) {
			case 'fresh':
				return 'success';
			case 'needs_confirm':
				return 'warning';
			case 'stale':
				return 'danger';
			default:
				return 'neutral';
		}
	}

	function groupLabel(g: DiagnosticsRow['group']): string {
		switch (g) {
			case 'fresh':
				return 'Обновлён недавно';
			case 'needs_confirm':
				return 'Месяц без правок';
			case 'stale':
				return '2+ месяца без правок';
			default:
				return 'Нет рабочего графика';
		}
	}

	function groupHint(g: DiagnosticsRow['group']): string {
		switch (g) {
			case 'fresh':
				return 'Рабочий график обновлялся меньше 30 дней назад — данные свежие.';
			case 'needs_confirm':
				return 'График не трогали 30–60 дней. Стоит попросить сотрудника подтвердить, что всё актуально.';
			case 'stale':
				return 'График не обновлялся больше 60 дней. Скорее всего данные неактуальны — нужно зайти и поправить.';
			default:
				return 'У сотрудника не задан рабочий график. Загрузку и конфликты считать не от чего.';
		}
	}

	function fmtUpdate(iso?: string): string {
		if (!iso) return '—';
		const d = new Date(iso);
		return d.toLocaleDateString('ru', { day: 'numeric', month: 'short', year: 'numeric' });
	}

	function exceptionLabel(kind: string): string {
		switch (kind) {
			case 'vacation':
				return 'отпуск';
			case 'sick_leave':
				return 'больничный';
			case 'business_trip':
				return 'командировка';
			case 'personal_hours':
				return 'личные часы';
			default:
				return 'отсутствие';
		}
	}

	function exceptionPhrase(kind: string, days: number): string {
		const label = exceptionLabel(kind);
		if (days === 0) return `${label} сегодня`;
		if (days === 1) return `${label} завтра`;
		const m10 = days % 10;
		const m100 = days % 100;
		let dayWord = 'дней';
		if (m10 === 1 && m100 !== 11) dayWord = 'день';
		else if ([2, 3, 4].includes(m10) && ![12, 13, 14].includes(m100)) dayWord = 'дня';
		return `${label} через ${days} ${dayWord}`;
	}
</script>

<div class="page-header">
	<div>
		<h1>Диагностика</h1>
		<div class="page-header__subtitle">
			Группировка сотрудников по актуальности рабочего графика
		</div>
	</div>
</div>

{#if loading}
	<div class="text-text-3 text-sm">Загрузка…</div>
{:else if error}
	<Badge variant="danger">
		<i class="ti ti-alert-circle"></i>
		{error}
	</Badge>
{:else if groups}
	<Tabs {tabs} bind:value={activeTab} />

	{#if activeTab === 'burnout'}
		{#if burnout.length === 0}
			<Card>
				<div class="text-text-3 text-sm" style="padding: 16px; text-align: center;">
					Никого в зоне выгорания — за последние 2 недели у всех ОК.
				</div>
			</Card>
		{:else}
			<div class="text-text-2 text-xs" style="margin: 8px 0 12px;">
				Критерий: L &gt; 0.85 (загрузка) ИЛИ C &gt; 0.3 (встречи вне графика) в обеих последних неделях подряд.
			</div>
			<div class="space-y-2">
				{#each burnout as b (b.employee_id)}
					<Card>
						<div class="flex items-center gap-3">
							<a href="/employees/{b.employee_id}" style="display: contents;">
								<Avatar initials={initials(b.full_name)} size="md" variant="purple" />
							</a>
							<div class="flex-1">
								<div class="flex items-center gap-2 mb-1">
									<a href="/employees/{b.employee_id}" class="emp-link">
										<div class="card__title">{b.full_name}</div>
									</a>
									<Badge variant="danger">риск выгорания</Badge>
								</div>
								<div class="text-text-2 text-xs">
									{b.role}
									{#if b.department} · {b.department}{/if}
								</div>
								{#if b.reasons.length > 0}
									<div class="text-text-3 text-xs" style="margin-top: 4px;">
										{b.reasons.join(' · ')}
									</div>
								{/if}
							</div>
							<div class="text-right" style="font-size: 12px; color: var(--text-2);">
								<div>L<sub>−1н</sub>: <strong>{b.l1.toFixed(2)}</strong></div>
								<div>L<sub>тек</sub>: <strong>{b.l2.toFixed(2)}</strong></div>
								<div>C<sub>тек</sub>: <strong>{b.c2.toFixed(2)}</strong></div>
							</div>
						</div>
					</Card>
				{/each}
			</div>
		{/if}
	{:else if visible.length === 0}
		<Card>
			<div class="text-text-3 text-sm" style="padding: 16px; text-align: center;">
				В этой группе никого нет.
			</div>
		</Card>
	{:else}
		<div class="space-y-2">
			{#each visible as r (r.employee_id)}
				<Card>
					<div class="flex items-center gap-3">
						<a href="/employees/{r.employee_id}" style="display: contents;">
							<Avatar initials={initials(r.full_name)} size="md" variant="purple" />
						</a>
						<div class="flex-1">
							<div class="flex items-center gap-2 mb-1">
								<a href="/employees/{r.employee_id}" class="emp-link">
									<div class="card__title">{r.full_name}</div>
								</a>
								<Badge variant={groupVariant(r.group)} title={groupHint(r.group)}>
									{groupLabel(r.group)}
								</Badge>
								{#if r.upcoming_exception && r.upcoming_exception_days !== undefined}
									<Badge variant="info">
										<i class="ti ti-beach"></i>
										{exceptionPhrase(r.upcoming_exception, r.upcoming_exception_days)}
									</Badge>
								{/if}
							</div>
							<div class="text-text-2 text-xs">
								{r.role}
								{#if r.department} · {r.department}{/if}
								{#if r.timezone} · {r.timezone}{/if}
							</div>
						</div>
						<div class="text-right">
							<div class="stat__value diag-metric" style="font-size: 16px;">
								<span class="diag-metric__letter">A</span>
								<span>=</span>
								<span>{r.group === 'unknown' ? '—' : r.freshness.toFixed(2)}</span>
								<MetricInfo letter="A" />
							</div>
							<div class="text-text-3 text-xs">
								{r.group === 'unknown' ? 'профиль не создан' : `${r.days_since_update} дн с обновления`}
							</div>
							<div class="text-text-3 text-xs">
								{fmtUpdate(r.last_profile_update_at)}
							</div>
						</div>
					</div>
				</Card>
			{/each}
		</div>
	{/if}
{/if}

<style>
	.diag-metric {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		justify-content: flex-end;
	}
	.diag-metric__letter {
		font-family: 'JetBrains Mono', ui-monospace, monospace;
	}
</style>
