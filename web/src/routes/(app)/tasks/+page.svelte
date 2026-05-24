<script lang="ts">
	// /tasks — планировщик задач (Jira). Сверху — таблица задач со всеми
	// характеристиками + inline edit estimate. Снизу — Gantt-bar: 14 дней
	// горизонтально, цветные блоки задач по приоритету.
	//
	// Реальный sync задач из Jira идёт фоном (cron раз в 5 минут + при
	// подключении). Пересчёт плана — по кнопке «Пересчитать план» или
	// автоматически раз в час.
	import { onMount } from 'svelte';
	import Card from '$lib/components/Card.svelte';
	import Button from '$lib/components/Button.svelte';
	import Badge from '$lib/components/Badge.svelte';
	import {
		listMyTasks,
		replanTasks,
		setTaskEstimate,
		type TrackerTask,
		type TaskSlot
	} from '$lib/api/tasks';
	import { ApiError } from '$lib/api/client';

	let tasks = $state<TrackerTask[]>([]);
	let horizonEnd = $state<string>('');
	let loading = $state(true);
	let replanning = $state(false);
	let error = $state<string | null>(null);
	let success = $state<string | null>(null);

	// Локальный буфер ручной правки estimate. ID задачи → строка часов.
	let estimateDraft = $state<Record<string, string>>({});

	onMount(async () => {
		await load();
	});

	async function load() {
		loading = true;
		error = null;
		try {
			const r = await listMyTasks();
			tasks = r.tasks ?? [];
			horizonEnd = r.horizon_end;
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		} finally {
			loading = false;
		}
	}

	async function replan() {
		replanning = true;
		error = null;
		success = null;
		try {
			const r = await replanTasks();
			success = `План обновлён: ${pluralTasks(r.planned_tasks)}, всего ${r.total_hours} ч.${r.ai_calls > 0 ? ` AI оценил ${pluralTasks(r.ai_calls)}.` : ''}`;
			await load();
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		} finally {
			replanning = false;
		}
	}

	async function saveEstimate(t: TrackerTask) {
		const raw = estimateDraft[t.id];
		if (!raw) return;
		const hours = parseFloat(raw.replace(',', '.'));
		if (!isFinite(hours) || hours <= 0) {
			error = 'Неверное значение часов';
			return;
		}
		try {
			await setTaskEstimate(t.id, hours);
			delete estimateDraft[t.id];
			estimateDraft = { ...estimateDraft };
			await load();
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		}
	}

	// --- Gantt-helpers ---

	// 14 дней от сегодня (today = индекс 0).
	const HORIZON_DAYS = 14;
	const days = $derived.by(() => {
		const today = new Date();
		today.setHours(0, 0, 0, 0);
		const out: { date: Date; key: string; label: string; isWeekend: boolean }[] = [];
		for (let i = 0; i < HORIZON_DAYS; i++) {
			const d = new Date(today);
			d.setDate(today.getDate() + i);
			out.push({
				date: d,
				key: dateKey(d),
				label: d.toLocaleDateString('ru', { day: 'numeric', month: 'short' }),
				isWeekend: d.getDay() === 0 || d.getDay() === 6
			});
		}
		return out;
	});

	function dateKey(d: Date): string {
		const y = d.getFullYear();
		const m = String(d.getMonth() + 1).padStart(2, '0');
		const dd = String(d.getDate()).padStart(2, '0');
		return `${y}-${m}-${dd}`;
	}

	function priorityColor(p?: string): string {
		switch (p) {
			case 'highest':
				return '#ef4444';
			case 'high':
				return '#f59e0b';
			case 'medium':
				return '#6366f1';
			case 'low':
				return '#22c55e';
			case 'lowest':
				return '#94a3b8';
			default:
				return '#6366f1';
		}
	}

	function priorityLabel(p?: string): string {
		switch (p) {
			case 'highest':
				return 'Highest';
			case 'high':
				return 'High';
			case 'medium':
				return 'Medium';
			case 'low':
				return 'Low';
			case 'lowest':
				return 'Lowest';
			default:
				return '—';
		}
	}

	function priorityBadge(p?: string): 'danger' | 'warning' | 'info' | 'success' | 'neutral' {
		switch (p) {
			case 'highest':
				return 'danger';
			case 'high':
				return 'warning';
			case 'medium':
				return 'info';
			case 'low':
				return 'success';
			default:
				return 'neutral';
		}
	}

	function effectiveEstimate(t: TrackerTask): { hours: number; source: 'manual' | 'ai' | 'default' } {
		if (t.estimated_hours && t.estimated_hours > 0) {
			return { hours: t.estimated_hours, source: 'manual' };
		}
		if (t.ai_estimated_hours && t.ai_estimated_hours > 0) {
			return { hours: t.ai_estimated_hours, source: 'ai' };
		}
		return { hours: 4, source: 'default' };
	}

	function slotForDay(t: TrackerTask, key: string): TaskSlot | undefined {
		return t.slots?.find((s) => s.date === key);
	}

	// Сумма часов на день (по всем задачам) — для footer-строки «Загрузка дня».
	function dayLoad(key: string): number {
		let h = 0;
		for (const t of tasks) {
			const s = slotForDay(t, key);
			if (s) h += s.hours;
		}
		return Math.round(h * 10) / 10;
	}

	function dueLabel(t: TrackerTask): string {
		if (!t.due_at) return 'без срока';
		const d = new Date(t.due_at);
		return d.toLocaleDateString('ru', { day: 'numeric', month: 'short' });
	}

	function dueOverdue(t: TrackerTask): boolean {
		if (!t.due_at) return false;
		return new Date(t.due_at).getTime() < Date.now();
	}

	// pluralTasks(2) → '2 задачи', pluralTasks(5) → '5 задач'
	function pluralTasks(n: number): string {
		const m10 = n % 10;
		const m100 = n % 100;
		if (m10 === 1 && m100 !== 11) return `${n} задача`;
		if (m10 >= 2 && m10 <= 4 && (m100 < 10 || m100 >= 20)) return `${n} задачи`;
		return `${n} задач`;
	}

	// fmtPlanDate("2026-06-06") → "6 июня"
	function fmtPlanDate(iso: string): string {
		const d = new Date(iso);
		return d.toLocaleDateString('ru', { day: 'numeric', month: 'long' });
	}
</script>

<div class="page-header">
	<div>
		<h1>Задачи</h1>
		<div class="page-header__subtitle">
			Задачи из Jira, разложенные по дням с учётом встреч и приоритетов.
		</div>
	</div>
	<div class="page-header__actions">
		<Button variant="primary" icon={replanning ? 'ti-loader' : 'ti-refresh'} onclick={replan} disabled={replanning}>
			{replanning ? 'Пересчитываю…' : 'Пересчитать план'}
		</Button>
	</div>
</div>

{#if error}
	<div class="section">
		<Badge variant="danger"><i class="ti ti-alert-circle"></i>{error}</Badge>
	</div>
{/if}
{#if success}
	<div class="section">
		<Badge variant="success"><i class="ti ti-check"></i>{success}</Badge>
	</div>
{/if}

{#if loading}
	<Card>
		<div class="text-text-3 text-sm" style="padding: 16px; text-align: center;">Загрузка…</div>
	</Card>
{:else if tasks.length === 0}
	<Card>
		<div class="text-text-3 text-sm" style="padding: 24px; text-align: center;">
			Задач нет. <a href="/integrations" class="empty-link">Подключи Jira</a> и задачи подтянутся автоматически в течение нескольких минут.
		</div>
	</Card>
{:else}
	<!-- Таблица задач -->
	<Card title="Мои задачи" subtitle="Кликни в оценку чтобы поправить вручную">
		<table class="tasks-table">
			<thead>
				<tr>
					<th>Задача</th>
					<th>Приоритет</th>
					<th>Срок</th>
					<th>Оценка</th>
					<th>Статус</th>
				</tr>
			</thead>
			<tbody>
				{#each tasks as t (t.id)}
					{@const est = effectiveEstimate(t)}
					<tr>
						<td>
							<div class="t-title">
								<span class="t-key">{t.source_task_id}</span>
								<span class="t-summary">{t.title}</span>
							</div>
						</td>
						<td>
							<Badge variant={priorityBadge(t.priority)}>{priorityLabel(t.priority)}</Badge>
						</td>
						<td>
							<span class:t-due--overdue={dueOverdue(t)}>{dueLabel(t)}</span>
						</td>
						<td>
							<div class="t-estimate">
								<input
									type="text"
									class="t-estimate__input"
									value={estimateDraft[t.id] ?? est.hours}
									oninput={(e) => (estimateDraft[t.id] = (e.target as HTMLInputElement).value)}
									onblur={() => saveEstimate(t)}
								/>
								<span class="t-estimate__unit">ч</span>
								{#if est.source === 'ai'}
									<span class="t-estimate__badge t-estimate__badge--ai" title="Оценил GigaChat">
										<i class="ti ti-sparkles"></i>
									</span>
								{:else if est.source === 'default'}
									<span class="t-estimate__badge t-estimate__badge--default" title="Дефолт — нет оценки">
										?
									</span>
								{/if}
							</div>
						</td>
						<td>
							<span class="text-text-3 text-sm">{t.status || '—'}</span>
						</td>
					</tr>
				{/each}
			</tbody>
		</table>
	</Card>

	<!-- Gantt -->
	<div class="section" style="margin-top: 24px;">
		<Card title="План на 14 дней" subtitle="Каждый блок — час задачи в дне. Цвет = приоритет.">
			<div class="gantt">
				<div class="gantt__head">
					<div class="gantt__name-col"></div>
					{#each days as d (d.key)}
						<div class="gantt__day" class:gantt__day--weekend={d.isWeekend}>
							<div class="gantt__day-num">{d.date.getDate()}</div>
							<div class="gantt__day-mon">{d.date.toLocaleDateString('ru', { month: 'short' })}</div>
						</div>
					{/each}
				</div>

				{#each tasks as t (t.id)}
					<div class="gantt__row">
						<div class="gantt__name-col">
							<span class="gantt__name-key" style="background:{priorityColor(t.priority)}">
								{t.source_task_id}
							</span>
							<span class="gantt__name-title">{t.title}</span>
						</div>
						{#each days as d (d.key)}
							{@const slot = slotForDay(t, d.key)}
							<div class="gantt__cell" class:gantt__cell--weekend={d.isWeekend}>
								{#if slot}
									<div
										class="gantt__bar"
										style="background:{priorityColor(t.priority)}; opacity:{Math.min(1, 0.4 + slot.hours / 8)}"
										title="{slot.hours} ч"
									>
										{slot.hours}
									</div>
								{/if}
							</div>
						{/each}
					</div>
				{/each}

				<!-- Footer: загрузка дня -->
				<div class="gantt__row gantt__row--footer">
					<div class="gantt__name-col gantt__footer-label">Загрузка дня, ч</div>
					{#each days as d (d.key)}
						{@const load = dayLoad(d.key)}
						<div class="gantt__cell" class:gantt__cell--weekend={d.isWeekend}>
							<span class="gantt__footer-num" class:gantt__footer-num--high={load > 6}>
								{load > 0 ? load : ''}
							</span>
						</div>
					{/each}
				</div>
			</div>
		</Card>
	</div>
{/if}

<style>
	.page-header__actions {
		display: flex;
		gap: 8px;
	}
	.empty-link {
		color: var(--info-strong);
		font-weight: 600;
		text-decoration: none;
	}
	.empty-link:hover {
		text-decoration: underline;
	}
	.tasks-table {
		width: 100%;
		border-collapse: collapse;
		font-size: 13px;
	}
	.tasks-table th {
		text-align: left;
		font-weight: 500;
		color: var(--text-3);
		font-size: 11px;
		text-transform: uppercase;
		letter-spacing: 0.4px;
		padding: 8px 10px;
		border-bottom: 0.5px solid var(--border);
	}
	.tasks-table td {
		padding: 10px;
		border-bottom: 0.5px solid var(--border);
		vertical-align: middle;
	}
	.t-title {
		display: flex;
		align-items: center;
		gap: 8px;
	}
	.t-key {
		font-family: 'JetBrains Mono', monospace;
		font-size: 11px;
		color: var(--text-3);
		padding: 2px 6px;
		background: var(--surface);
		border-radius: 4px;
		flex-shrink: 0;
	}
	.t-summary {
		color: var(--text);
		font-weight: 500;
	}
	.t-due--overdue {
		color: var(--danger-strong);
		font-weight: 600;
	}
	.t-estimate {
		display: inline-flex;
		align-items: center;
		gap: 4px;
	}
	.t-estimate__input {
		width: 50px;
		padding: 2px 6px;
		font-size: 13px;
		text-align: right;
		border: 0.5px solid var(--border);
		border-radius: 4px;
		background: var(--bg);
	}
	.t-estimate__unit {
		font-size: 11px;
		color: var(--text-3);
	}
	.t-estimate__badge {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 18px;
		height: 18px;
		border-radius: 50%;
		font-size: 11px;
	}
	.t-estimate__badge--ai {
		color: var(--info-strong);
		background: var(--info-bg);
	}
	.t-estimate__badge--default {
		color: var(--text-3);
		background: var(--surface);
		font-weight: 700;
	}

	/* --- Gantt --- */
	.gantt {
		display: flex;
		flex-direction: column;
		gap: 2px;
		font-size: 11px;
		overflow-x: auto;
	}
	.gantt__head,
	.gantt__row {
		display: grid;
		grid-template-columns: 220px repeat(14, minmax(40px, 1fr));
		gap: 1px;
	}
	.gantt__name-col {
		display: flex;
		align-items: center;
		gap: 6px;
		padding: 4px 8px;
		min-width: 0;
	}
	.gantt__name-key {
		display: inline-block;
		padding: 2px 6px;
		font-family: 'JetBrains Mono', monospace;
		font-size: 10px;
		color: white;
		border-radius: 4px;
		flex-shrink: 0;
	}
	.gantt__name-title {
		font-size: 12px;
		color: var(--text);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.gantt__day {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		padding: 6px 2px;
		font-size: 10px;
		color: var(--text-3);
		border-bottom: 0.5px solid var(--border);
	}
	.gantt__day--weekend {
		background: var(--surface-2);
	}
	.gantt__day-num {
		font-weight: 700;
		color: var(--text);
		font-size: 13px;
	}
	.gantt__day-mon {
		text-transform: uppercase;
	}
	.gantt__cell {
		min-height: 32px;
		padding: 3px;
		display: flex;
		align-items: center;
		justify-content: center;
	}
	.gantt__cell--weekend {
		background: var(--surface-2);
	}
	.gantt__bar {
		width: 100%;
		height: 22px;
		display: flex;
		align-items: center;
		justify-content: center;
		color: white;
		font-weight: 600;
		font-size: 11px;
		border-radius: 4px;
	}
	.gantt__row--footer {
		border-top: 1px solid var(--border);
		margin-top: 4px;
		padding-top: 4px;
		font-weight: 600;
	}
	.gantt__footer-label {
		color: var(--text-3);
		font-size: 11px;
		text-transform: uppercase;
		letter-spacing: 0.4px;
	}
	.gantt__footer-num {
		font-size: 12px;
		color: var(--text-2);
	}
	.gantt__footer-num--high {
		color: var(--danger-strong);
	}
</style>
