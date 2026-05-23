<script lang="ts">
	import { onMount } from 'svelte';
	import Card from '$lib/components/Card.svelte';
	import Stat from '$lib/components/Stat.svelte';
	import Badge from '$lib/components/Badge.svelte';
	import Heatmap, { type HeatmapRow } from '$lib/components/Heatmap.svelte';
	import { me as fetchMe, type MeResponse } from '$lib/api/profile';
	import { getEmployeeMetrics, type EmployeeMetrics } from '$lib/api/metrics';
	import { getAvailability, listTeams, type TeamAvailability } from '$lib/api/teams';
	import { ApiError } from '$lib/api/client';
	import { user } from '$lib/stores/user';

	let me = $state<MeResponse | null>(null);
	let metrics = $state<EmployeeMetrics | null>(null);
	let availability = $state<TeamAvailability | null>(null);
	let loading = $state(true);
	let error = $state<string | null>(null);

	onMount(async () => {
		try {
			me = await fetchMe();
			if (me?.employee?.id) {
				metrics = await getEmployeeMetrics(me.employee.id);

				// Найдём команду, где сотрудник состоит, и подтянем availability — чтобы
				// показать "мою" строку в неделе.
				const teams = await listTeams();
				for (const t of teams.teams ?? []) {
					const av = await getAvailability(t.id, $user?.timezone || 'Europe/Moscow');
					if (av.rows.some((r) => r.employee_id === me!.employee!.id)) {
						availability = av;
						break;
					}
				}
			}
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		} finally {
			loading = false;
		}
	});

	function myRow(): HeatmapRow[] {
		if (!availability || !me?.employee?.id) return [];
		const m = availability.rows.find((r) => r.employee_id === me!.employee!.id);
		if (!m) return [];
		const rows: HeatmapRow[] = [];
		for (let d = 0; d < availability.days.length; d++) {
			const cells = m.cells.slice(d * availability.hours.length, (d + 1) * availability.hours.length);
			rows.push({
				label: availability.days[d],
				cells: cells as ('free' | 'busy' | 'conflict' | 'off')[]
			});
		}
		return rows;
	}

	// Текст всплывашки по ячейке heatmap'а. Первая строка — заголовок (жирная),
	// остальные — описание. В тултипе Heatmap.svelte \n превращается в перенос.
	//
	// Достаём ИЗ availability.rows[me].details конкретные события/исключения
	// для этой ячейки — название встречи, время, причину.
	function cellTooltipText(ri: number, ci: number, state: string): string {
		if (!availability) return '';
		const day = availability.days[ri] ?? '';
		const hour = availability.hours[ci] ?? '';
		const hourStart = String(hour).padStart(2, '0');
		const hourEnd = String(Number(hour) + 1).padStart(2, '0');
		const head = `${day}, ${hourStart}:00–${hourEnd}:00`;

		// Найти detail для этой ячейки.
		const m = me?.employee?.id
			? availability.rows.find((r) => r.employee_id === me!.employee!.id)
			: null;
		const idx = ri * availability.hours.length + ci;
		const det = m?.details?.[idx];

		const lines: string[] = [head];

		if (det?.events && det.events.length > 0) {
			// «Занято» с конкретикой.
			for (const ev of det.events) {
				const title = ev.title?.trim() || 'Без названия';
				const range = `${fmtTime(ev.start_at)}–${fmtTime(ev.end_at)}`;
				lines.push(`• ${title} (${range})`);
			}
			if (state === 'conflict') {
				// Различаем double-booking от события вне графика.
				lines.push(eventsOverlap(det.events) ? '⚠️ несколько встреч одновременно' : '⚠️ вне рабочего графика');
			}
			return lines.join('\n');
		}

		if (det?.exception) {
			const kind = excKindLabel(det.exception.kind);
			const range = `${fmtDate(det.exception.start_at)} — ${fmtDate(det.exception.end_at)}`;
			lines.push(`${kind}: ${range}`);
			if (det.exception.comment) {
				lines.push(`«${det.exception.comment}»`);
			}
			return lines.join('\n');
		}

		// Фолбэк — общее описание состояния.
		const desc = stateDescription(state, det?.note);
		if (desc) lines.push(desc);
		return lines.join('\n');
	}

	function eventsOverlap(evs: { start_at: string; end_at: string }[]): boolean {
		if (evs.length < 2) return false;
		for (let i = 0; i < evs.length; i++) {
			for (let j = i + 1; j < evs.length; j++) {
				const aStart = new Date(evs[i].start_at).getTime();
				const aEnd = new Date(evs[i].end_at).getTime();
				const bStart = new Date(evs[j].start_at).getTime();
				const bEnd = new Date(evs[j].end_at).getTime();
				if (aStart < bEnd && bStart < aEnd) return true;
			}
		}
		return false;
	}

	function stateDescription(state: string, note?: string): string {
		switch (state) {
			case 'free':
				return 'Свободно — можно ставить встречу';
			case 'busy':
				return 'Занято встречей';
			case 'conflict':
				return 'Конфликт — событие вне рабочего графика';
			case 'off':
				switch (note) {
					case 'before_work':
						return 'До начала рабочего дня';
					case 'after_work':
						return 'После окончания рабочего дня';
					case 'day_off':
						return 'Выходной';
					case 'no_profile':
						return 'Нет активного графика';
					default:
						return 'Вне рабочего графика';
				}
			case 'focus':
				return 'Фокус-время (без встреч)';
			default:
				return '';
		}
	}

	function fmtTime(iso: string): string {
		try {
			return new Date(iso).toLocaleTimeString('ru', {
				hour: '2-digit',
				minute: '2-digit'
			});
		} catch {
			return iso;
		}
	}
	function fmtDate(iso: string): string {
		try {
			return new Date(iso).toLocaleDateString('ru', { day: 'numeric', month: 'short' });
		} catch {
			return iso;
		}
	}
	function excKindLabel(k: string): string {
		switch (k) {
			case 'vacation':
				return 'Отпуск';
			case 'sick_leave':
				return 'Больничный';
			case 'business_trip':
				return 'Командировка';
			case 'personal_hours':
				return 'Личные часы';
			default:
				return 'Отсутствие';
		}
	}

	function loadVariant(L: number): 'success' | 'warning' | 'danger' {
		if (L > 0.95) return 'danger';
		if (L > 0.8) return 'warning';
		return 'success';
	}

	function loadLabel(L: number): string {
		const pct = Math.round(L * 100);
		return `${pct}%`;
	}

	function riskVariant(R: number): 'success' | 'warning' | 'danger' {
		if (R > 0.6) return 'danger';
		if (R > 0.3) return 'warning';
		return 'success';
	}
</script>

<div class="page-header">
	<div>
		<h1>Моя загрузка</h1>
		<div class="page-header__subtitle">
			Метрики A/C/L/R и календарь занятости за текущую неделю
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
{:else if metrics}
	<div class="section">
		<div class="stat-grid">
			<Stat
				label="Актуальность (A)"
				metricLetter="A"
				value={metrics.A.toFixed(2)}
				valueVariant={metrics.A < 0.5 ? 'danger' : metrics.A < 0.7 ? 'warning' : 'success'}
			/>
			<Stat
				label="Конфликты (C)"
				metricLetter="C"
				value={metrics.C.toFixed(2)}
				valueVariant={metrics.C > 0.3 ? 'danger' : metrics.C > 0.15 ? 'warning' : 'success'}
			/>
			<Stat
				label="Загрузка (L)"
				metricLetter="L"
				value={loadLabel(metrics.L)}
				valueVariant={loadVariant(metrics.L)}
			/>
			<Stat
				label="Риск (R)"
				metricLetter="R"
				value={metrics.R.toFixed(2)}
				valueVariant={riskVariant(metrics.R)}
			/>
		</div>
	</div>

	{#if availability && myRow().length > 0}
		<Card title="Моя неделя" subtitle="Свободно / занято / конфликт по часам. Наведи на ячейку — подробности.">
			<Heatmap
				rows={myRow()}
				hours={availability.hours}
				cellTooltip={cellTooltipText}
			/>
		</Card>
	{:else}
		<Card>
			<div class="text-text-3 text-sm" style="padding: 16px; text-align: center;">
				Нет данных
			</div>
		</Card>
	{/if}
{:else}
	<Card>
		<div class="text-text-3 text-sm" style="padding: 16px;">
			Метрики недоступны
		</div>
	</Card>
{/if}
