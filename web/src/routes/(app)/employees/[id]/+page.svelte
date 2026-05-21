<script lang="ts">
	import { page } from '$app/stores';
	import Card from '$lib/components/Card.svelte';
	import Badge from '$lib/components/Badge.svelte';
	import Stat from '$lib/components/Stat.svelte';
	import Avatar from '$lib/components/Avatar.svelte';
	import Button from '$lib/components/Button.svelte';
	import { getEmployeeDetail, type EmployeeDetail } from '$lib/api/employees';
	import { getEmployeeMetrics, type EmployeeMetrics } from '$lib/api/metrics';
	import ProfileHistory from '$lib/components/ProfileHistory.svelte';
	import { ApiError } from '$lib/api/client';
	import { user } from '$lib/stores/user';

	let detail = $state<EmployeeDetail | null>(null);
	let metrics = $state<EmployeeMetrics | null>(null);
	let loading = $state(true);
	let error = $state<string | null>(null);

	// Интеграции — приватная информация: показываем только себе или admin/hr.
	const canSeeIntegrations = $derived(
		detail !== null &&
			$user !== null &&
			($user.id === detail.employee.user_id ||
				$user.role === 'admin' ||
				$user.role === 'hr')
	);

	const employeeID = $derived($page.params.id);

	// При смене id (или первой загрузке) запускаем перезагрузку данных.
	// Используем явный $effect.pre чтобы реакция на смену id была чистой,
	// без эффектов от других $state внутри (detail/metrics — write-only).
	$effect(() => {
		const id = employeeID;
		if (!id) return;
		void loadEmployee(id);
	});

	async function loadEmployee(id: string) {
		loading = true;
		error = null;
		try {
			const [d, m] = await Promise.all([
				getEmployeeDetail(id),
				getEmployeeMetrics(id).catch(() => null)
			]);
			detail = d;
			metrics = m;
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		} finally {
			loading = false;
		}
	}

	function initials(name: string): string {
		const parts = name.trim().split(/\s+/);
		return ((parts[0]?.[0] ?? '') + (parts[1]?.[0] ?? '')).toUpperCase() || '??';
	}

	function fmtDate(iso?: string): string {
		if (!iso) return '—';
		return new Date(iso).toLocaleDateString('ru', {
			day: 'numeric',
			month: 'short',
			year: 'numeric'
		});
	}

	function aVariant(a: number): 'success' | 'warning' | 'danger' {
		if (a < 0.5) return 'danger';
		if (a < 0.7) return 'warning';
		return 'success';
	}

	// Расписание по дням недели — для блока графика.
	type DayKey = 'mon' | 'tue' | 'wed' | 'thu' | 'fri' | 'sat' | 'sun';
	const DAY_LABELS: { key: DayKey; label: string }[] = [
		{ key: 'mon', label: 'Пн' },
		{ key: 'tue', label: 'Вт' },
		{ key: 'wed', label: 'Ср' },
		{ key: 'thu', label: 'Чт' },
		{ key: 'fri', label: 'Пт' },
		{ key: 'sat', label: 'Сб' },
		{ key: 'sun', label: 'Вс' }
	];

	function workFormatLabel(f?: string): string {
		switch (f) {
			case 'office':
				return 'Офис';
			case 'remote':
				return 'Удалёнка';
			case 'hybrid':
				return 'Гибрид';
			default:
				return f ?? '—';
		}
	}
</script>

{#if loading}
	<div class="text-text-3 text-sm">Загрузка…</div>
{:else if error}
	<Badge variant="danger">
		<i class="ti ti-alert-circle"></i>
		{error}
	</Badge>
{:else if detail}
	<div class="page-header">
		<div class="flex items-center gap-3">
			<Avatar initials={initials(detail.employee.full_name)} size="xl" variant="purple" />
			<div>
				<h1>{detail.employee.full_name}</h1>
				<div class="page-header__subtitle">
					{detail.employee.role}
					{#if detail.employee.department} · {detail.employee.department}{/if}
					{#if detail.employee.position} · {detail.employee.position}{/if}
					{#if detail.employee.timezone} · {detail.employee.timezone}{/if}
				</div>
				<div class="text-text-3 text-xs mt-1">{detail.employee.email}</div>
			</div>
		</div>
		<div class="page-header__actions">
			<a href="/hr-roadmap" class="btn">
				<i class="ti ti-arrow-left"></i>HR Roadmap
			</a>
		</div>
	</div>

	{#if metrics}
		<div class="section">
			<div class="stat-grid">
				<Stat
					label="Актуальность"
					metricLetter="A"
					value={metrics.A.toFixed(2)}
					valueVariant={aVariant(metrics.A)}
				/>
				<Stat
					label="Конфликты"
					metricLetter="C"
					value={metrics.C.toFixed(2)}
					valueVariant={metrics.C > 0.3 ? 'danger' : metrics.C > 0.15 ? 'warning' : 'success'}
				/>
				<Stat
					label="Загрузка"
					metricLetter="L"
					value={`${Math.round(metrics.L * 100)}%`}
					valueVariant={metrics.L > 0.95 ? 'danger' : metrics.L > 0.8 ? 'warning' : 'success'}
				/>
				<Stat
					label="TZ-drift"
					metricLetter="Z"
					value={metrics.Z.toFixed(2)}
					valueVariant={metrics.Z > 0.3 ? 'danger' : metrics.Z > 0.15 ? 'warning' : 'success'}
				/>
				<Stat
					label="Расхождение с HR"
					metricLetter="H"
					value={metrics.H.toFixed(2)}
					valueVariant={metrics.H > 0.5 ? 'danger' : metrics.H > 0 ? 'warning' : 'success'}
				/>
				<Stat
					label="Риск"
					metricLetter="R"
					value={metrics.R.toFixed(2)}
					valueVariant={metrics.R > 0.6 ? 'danger' : metrics.R > 0.3 ? 'warning' : 'success'}
				/>
			</div>
		</div>
	{/if}

	<div class="grid-2">
		<Card title="Рабочий профиль">
			{#if detail.work_profile}
				<div class="text-text-2 text-sm">
					<div>Часовой пояс: <strong>{detail.work_profile.timezone}</strong></div>
					<div>Формат: <strong>{workFormatLabel(detail.work_profile.work_format)}</strong></div>
					{#if detail.employee.hr_work_format && detail.employee.hr_work_format !== detail.work_profile.work_format}
						<div style="margin-top: 8px;">
							<Badge variant="warning">
								<i class="ti ti-alert-triangle"></i>
								HR-формат "{workFormatLabel(detail.employee.hr_work_format)}" не совпадает с профильным
							</Badge>
						</div>
					{/if}

					<div class="schedule">
						<div class="schedule__title">График работы</div>
						<div class="schedule__grid">
							{#each DAY_LABELS as d (d.key)}
								{@const dh = detail.work_profile.days_of_week?.[d.key]}
								<div class="schedule__day" class:schedule__day--off={!dh}>
									<div class="schedule__day-label">{d.label}</div>
									<div class="schedule__day-hours">
										{#if dh}
											{dh.start}–{dh.end}
										{:else}
											—
										{/if}
									</div>
								</div>
							{/each}
						</div>
					</div>

					<div style="margin-top: 12px;">
						Обновлён: <strong>{fmtDate(detail.work_profile.valid_from)}</strong>
					</div>
				</div>
			{:else}
				<div class="text-text-3 text-sm">Профиль не задан</div>
			{/if}
		</Card>

		{#if canSeeIntegrations}
		<Card title="Интеграции" caption="источники данных">
			{#if detail.integrations && detail.integrations.length > 0}
				<div class="space-y-2">
					{#each detail.integrations as i (i.id)}
						<div class="flex items-center gap-2">
							<i class="ti ti-plug text-text-3"></i>
							<span class="text-sm">{i.provider}</span>
							{#if i.account_label}<span class="text-text-3 text-xs">· {i.account_label}</span>{/if}
							<Badge variant={i.status === 'connected' ? 'success' : i.status === 'error' ? 'danger' : 'neutral'}>
								{i.status}
							</Badge>
						</div>
					{/each}
				</div>
			{:else}
				<div class="text-text-3 text-sm">Источники не подключены</div>
			{/if}
		</Card>
		{/if}
	</div>

	<div class="section" style="margin-top: 16px;">
		<ProfileHistory employeeID={detail.employee.employee_id} />
	</div>

	<div class="section" style="margin-top: 16px;">
		<Card title="Исключения" subtitle="Отпуска, больничные, командировки на ближайшие 90 дней">
			{#if detail.exceptions && detail.exceptions.length > 0}
				<div class="space-y-2">
					{#each detail.exceptions as e (e.id)}
						<div class="flex items-center gap-2 p-2" style="border: 0.5px solid var(--border); border-radius: var(--radius-md);">
							<Badge variant="info">{e.kind}</Badge>
							<div class="flex-1 text-sm">
								{fmtDate(e.start_at)} — {fmtDate(e.end_at)}
								{#if e.comment}<span class="text-text-3 ml-2">· {e.comment}</span>{/if}
							</div>
						</div>
					{/each}
				</div>
			{:else}
				<div class="text-text-3 text-sm">Исключений нет</div>
			{/if}
		</Card>
	</div>

	{#if detail.upcoming_events_count !== undefined && detail.upcoming_events_count > 0}
		<div class="section">
			<Card>
				<div class="flex items-center gap-3">
					<i class="ti ti-calendar text-text-3" style="font-size: 24px;"></i>
					<div>
						<div class="card__title">{detail.upcoming_events_count} событий в ближайшие 2 недели</div>
						<div class="text-text-3 text-xs">Загружено из подключённых календарей</div>
					</div>
				</div>
			</Card>
		</div>
	{/if}
{/if}

<style>
	.schedule {
		margin-top: 14px;
	}
	.schedule__title {
		font-size: 11px;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.4px;
		color: var(--text-3);
		margin-bottom: 6px;
	}
	.schedule__grid {
		display: grid;
		grid-template-columns: repeat(7, 1fr);
		gap: 6px;
	}
	.schedule__day {
		text-align: center;
		padding: 8px 4px;
		background: var(--surface);
		border: 0.5px solid var(--border);
		border-radius: 8px;
	}
	.schedule__day--off {
		background: transparent;
		border-style: dashed;
		color: var(--text-3);
	}
	.schedule__day-label {
		font-size: 11px;
		color: var(--text-2);
		text-transform: uppercase;
		letter-spacing: 0.3px;
	}
	.schedule__day-hours {
		font-size: 12px;
		font-weight: 600;
		color: var(--text);
		margin-top: 4px;
		font-variant-numeric: tabular-nums;
	}
	.schedule__day--off .schedule__day-hours {
		color: var(--text-3);
		font-weight: 400;
	}
</style>
