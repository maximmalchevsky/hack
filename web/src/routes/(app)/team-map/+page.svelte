<script lang="ts">
	import { onMount } from 'svelte';
	import Card from '$lib/components/Card.svelte';
	import Badge from '$lib/components/Badge.svelte';
	import Tabs from '$lib/components/Tabs.svelte';
	import Heatmap, { type HeatmapRow, type HeatmapCellState } from '$lib/components/Heatmap.svelte';
	import TimeBreakdownCard from '$lib/components/TimeBreakdownCard.svelte';
	import {
		listTeams,
		getAvailability,
		type Team,
		type TeamAvailability,
		type CellState,
		type CellDetail
	} from '$lib/api/teams';
	import { ApiError } from '$lib/api/client';
	import { user } from '$lib/stores/user';

	let teams = $state<Team[]>([]);
	let selectedTeamID = $state<string | null>(null);
	let availability = $state<TeamAvailability | null>(null);
	let loading = $state(true);
	let loadingAv = $state(false);
	let error = $state<string | null>(null);
	let dayIndex = $state(0); // 0..4 — выбранный день недели

	const viewerTZ = $derived($user?.timezone || 'Europe/Moscow');

	onMount(async () => {
		try {
			const r = await listTeams();
			teams = r.teams ?? [];
			if (teams.length > 0) {
				selectedTeamID = teams[0].id;
				await loadAvailability(teams[0].id);
			}
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		} finally {
			loading = false;
		}
	});

	async function loadAvailability(id: string) {
		loadingAv = true;
		error = null;
		try {
			availability = await getAvailability(id, viewerTZ);
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		} finally {
			loadingAv = false;
		}
	}

	async function selectTeam(id: string) {
		selectedTeamID = id;
		await loadAvailability(id);
	}

	function initials(name: string): string {
		const parts = name.trim().split(/\s+/);
		return ((parts[0]?.[0] ?? '') + (parts[1]?.[0] ?? '')).toUpperCase() || '??';
	}

	// Одна строка = один сотрудник, ячейки = часы выбранного дня.
	function memberRows(av: TeamAvailability, dIdx: number): HeatmapRow[] {
		return av.rows.map((m) => {
			const cells = m.cells.slice(dIdx * av.hours.length, (dIdx + 1) * av.hours.length);
			return {
				label: m.full_name,
				sub: m.timezone,
				cells: cells as HeatmapCellState[],
				avatar: { initials: initials(m.full_name), variant: 'purple' },
				href: `/employees/${m.employee_id}`
			};
		});
	}

	const dayTabs = $derived(
		availability ? availability.days.map((d, i) => ({ id: String(i), label: d })) : []
	);

	// --- Tooltip для ячейки ---

	const RU_EXC: Record<string, string> = {
		vacation: 'отпуск',
		sick_leave: 'больничный',
		business_trip: 'командировка',
		personal_hours: 'личные часы',
		custom: 'отсутствие'
	};

	const RU_OFF: Record<string, string> = {
		before_work: 'до начала рабочего дня',
		after_work: 'после окончания рабочего дня',
		day_off: 'нерабочий день',
		no_profile: 'у сотрудника нет графика'
	};

	function fmtTime(iso: string): string {
		try {
			return new Date(iso).toLocaleTimeString('ru', { hour: '2-digit', minute: '2-digit' });
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

	// Строим текст для одной ячейки. Используется в Heatmap callback.
	function cellTooltipText(
		av: TeamAvailability,
		dIdx: number,
		ri: number,
		ci: number,
		state: CellState
	): string {
		const member = av.rows[ri];
		if (!member) return '';
		const hour = av.hours[ci];
		const hourLabel = `${hour}:00–${Number(hour) + 1}:00`;
		const detailIdx = dIdx * av.hours.length + ci;
		const detail: CellDetail | undefined = member.details?.[detailIdx];

		const header = `${member.full_name} · ${hourLabel}`;

		if (state === 'off' && detail?.exception) {
			const exc = detail.exception;
			const kind = RU_EXC[exc.kind] ?? exc.kind;
			const range = `${fmtDate(exc.start_at)} ${fmtTime(exc.start_at)} → ${fmtDate(exc.end_at)} ${fmtTime(exc.end_at)}`;
			let text = `${header}\nОтсутствие: ${kind}\n${range}`;
			if (exc.comment) text += `\n«${exc.comment}»`;
			return text;
		}

		if (state === 'off') {
			const note = detail?.note ? RU_OFF[detail.note] ?? '' : '';
			return note ? `${header}\nВне графика: ${note}` : `${header}\nВне графика`;
		}

		if (state === 'free') {
			return `${header}\nСвободен`;
		}

		// busy / conflict — показываем события
		const evs = detail?.events ?? [];
		if (evs.length === 0) {
			return state === 'conflict'
				? `${header}\nКонфликт (событие вне графика)`
				: `${header}\nЗанят`;
		}
		const evLines = evs
			.map((e) => `• ${e.title || 'без названия'} · ${fmtTime(e.start_at)}–${fmtTime(e.end_at)}`)
			.join('\n');
		if (state === 'conflict') {
			// Различаем два кейса: double-booking (события пересекаются между собой)
			// vs событие вне рабочих часов.
			const reason = eventsOverlap(evs)
				? 'Пересечение встреч (одновременно):'
				: 'Конфликт — событие вне рабочих часов:';
			return `${header}\n${reason}\n${evLines}`;
		}
		return `${header}\nЗанят:\n${evLines}`;
	}

	// Реально пересекаются ли события между собой по времени.
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

	const tooltipFn = $derived((ri: number, ci: number, state: HeatmapCellState) => {
		if (!availability) return null;
		return cellTooltipText(availability, dayIndex, ri, ci, state as CellState);
	});
</script>

<div class="page-header">
	<div>
		<h1>Карта команды</h1>
		<div class="page-header__subtitle">
			Доступность сотрудников по часам · TZ просмотра: {viewerTZ}
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
{:else if teams.length === 0}
	<Card>
		<div class="text-text-3 text-sm" style="padding: 16px; text-align: center;">
			Команды не созданы
		</div>
	</Card>
{:else}
	<div class="section">
		<div class="flex flex-wrap gap-2">
			{#each teams as t (t.id)}
				<button
					class="btn {selectedTeamID === t.id ? 'btn--primary' : ''}"
					onclick={() => selectTeam(t.id)}
				>
					{t.name}
				</button>
			{/each}
		</div>
	</div>

	{#if loadingAv}
		<div class="text-text-3 text-sm">Считаем…</div>
	{:else if availability}
		<div class="section">
			<Tabs
				tabs={dayTabs}
				value={String(dayIndex)}
				onChange={(id) => (dayIndex = Number(id))}
			/>
		</div>
		<Card>
			<Heatmap
				rows={memberRows(availability, dayIndex)}
				hours={availability.hours}
				cellTooltip={tooltipFn}
			/>
		</Card>

		{#if selectedTeamID}
			<div class="section" style="margin-top: 16px;">
				<TimeBreakdownCard teamID={selectedTeamID} titleOverride="Куда уходит время команды" />
			</div>
		{/if}
	{/if}
{/if}
