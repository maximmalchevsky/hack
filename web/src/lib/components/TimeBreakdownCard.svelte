<script lang="ts">
	// TimeBreakdownCard — «Куда уходит время» на /profile.
	// Donut + список топ-категорий встреч за период (7 / 30 / 90 дней).
	import { onMount } from 'svelte';
	import Card from './Card.svelte';
	import Badge from './Badge.svelte';
	import EChart from './EChart.svelte';
	import {
		getMyTimeBreakdown,
		getTeamTimeBreakdown,
		type TimeBreakdown
	} from '$lib/api/time-breakdown';

	interface Props {
		// Если задан — рисуем агрегат по команде. Иначе — личный.
		teamID?: string;
		// Заголовок переопределить (для team-режима).
		titleOverride?: string;
	}
	let { teamID, titleOverride }: Props = $props();

	let data = $state<TimeBreakdown | null>(null);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let days = $state(30);

	// Палитра под категории. Берём с разворотом для контраста.
	const PALETTE = ['#6366f1', '#22c55e', '#f59e0b', '#ec4899', '#14b8a6', '#94a3b8'];

	onMount(async () => {
		await reload();
	});

	async function setDays(d: number) {
		if (d === days) return;
		days = d;
		await reload();
	}

	async function reload() {
		loading = true;
		error = null;
		try {
			data = teamID
				? await getTeamTimeBreakdown(teamID, days)
				: await getMyTimeBreakdown(days);
		} catch (e) {
			error = e instanceof Error ? e.message : String(e);
		} finally {
			loading = false;
		}
	}

	// Если родитель сменил teamID — перезагружаем.
	$effect(() => {
		void teamID;
		if (!loading) reload();
	});

	const donutOption = $derived.by(() => {
		if (!data || data.items.length === 0) return null;
		return {
			tooltip: {
				trigger: 'item',
				formatter: (p: { name: string; value: number; percent: number }) => {
					const h = (p.value / 60).toFixed(1);
					return `<b>${p.name}</b><br/>${h} ч (${p.percent.toFixed(1)}%)`;
				}
			},
			series: [
				{
					name: 'Категории',
					type: 'pie',
					radius: ['55%', '78%'],
					avoidLabelOverlap: false,
					label: { show: false },
					labelLine: { show: false },
					data: data.items.map((it, i) => ({
						name: it.category,
						value: it.minutes,
						itemStyle: { color: PALETTE[i % PALETTE.length] }
					}))
				}
			]
		};
	});

	function fmtHours(h: number): string {
		if (h < 1) return `${Math.round(h * 60)} мин`;
		return `${h.toFixed(1)} ч`;
	}
</script>

<Card
	title={titleOverride ?? 'Куда уходит время'}
	subtitle={teamID
		? 'Раскладка встреч команды по категориям — определяется по словам в названии'
		: 'Раскладка встреч по категориям — определяется по словам в названии'}
>
	{#snippet actions()}
		<div class="tb__period">
			<button
				class="tb__period-btn"
				class:tb__period-btn--active={days === 7}
				onclick={() => setDays(7)}
				disabled={loading}
			>
				7 дн
			</button>
			<button
				class="tb__period-btn"
				class:tb__period-btn--active={days === 30}
				onclick={() => setDays(30)}
				disabled={loading}
			>
				30 дн
			</button>
			<button
				class="tb__period-btn"
				class:tb__period-btn--active={days === 90}
				onclick={() => setDays(90)}
				disabled={loading}
			>
				90 дн
			</button>
		</div>
	{/snippet}

	{#if loading && !data}
		<div class="text-text-3 text-sm" style="padding: 16px;">Загрузка…</div>
	{:else if error}
		<Badge variant="danger"><i class="ti ti-alert-circle"></i>{error}</Badge>
	{:else if data && data.items.length === 0}
		<div class="text-text-3 text-sm" style="padding: 16px; text-align: center;">
			За {days} дней встреч не было.
		</div>
	{:else if data}
		<div class="tb">
			<div class="tb__chart">
				{#if donutOption}
					<EChart option={donutOption} height="220px" />
				{/if}
				<div class="tb__total">
					<div class="tb__total-value">{fmtHours(data.total_hours)}</div>
					<div class="tb__total-label">всего на встречи</div>
				</div>
			</div>

			<div class="tb__list">
				{#each data.items as it, i (it.category)}
					<div class="tb__row">
						<span class="tb__dot" style="background:{PALETTE[i % PALETTE.length]}"></span>
						<div class="tb__name">{it.category}</div>
						<div class="tb__meta">
							<span class="tb__hours">{fmtHours(it.hours)}</span>
							<span class="tb__pct">{it.percent.toFixed(0)}%</span>
						</div>
					</div>
				{/each}
			</div>
		</div>
	{/if}
</Card>

<style>
	.tb {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 20px;
		align-items: center;
	}
	@media (max-width: 760px) {
		.tb {
			grid-template-columns: 1fr;
		}
	}
	.tb__chart {
		position: relative;
	}
	.tb__total {
		position: absolute;
		inset: 0;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		pointer-events: none;
	}
	.tb__total-value {
		font-size: 22px;
		font-weight: 700;
		color: var(--text);
	}
	.tb__total-label {
		font-size: 11px;
		color: var(--text-3);
		margin-top: 2px;
	}
	.tb__list {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.tb__row {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 6px 8px;
		border-radius: 8px;
	}
	.tb__row:hover {
		background: var(--surface);
	}
	.tb__dot {
		width: 10px;
		height: 10px;
		border-radius: 50%;
		flex-shrink: 0;
	}
	.tb__name {
		flex: 1;
		font-size: 13px;
		color: var(--text);
	}
	.tb__meta {
		display: flex;
		gap: 10px;
		align-items: baseline;
	}
	.tb__hours {
		font-size: 13px;
		font-weight: 600;
		color: var(--text);
	}
	.tb__pct {
		font-size: 11px;
		color: var(--text-3);
		min-width: 30px;
		text-align: right;
	}

	.tb__period {
		display: inline-flex;
		gap: 4px;
		padding: 3px;
		background: var(--surface);
		border-radius: 8px;
	}
	.tb__period-btn {
		padding: 4px 10px;
		font-size: 12px;
		background: transparent;
		border: none;
		color: var(--text-2);
		border-radius: 6px;
		cursor: pointer;
	}
	.tb__period-btn:hover {
		color: var(--text);
	}
	.tb__period-btn--active {
		background: var(--bg, white);
		color: var(--text);
		font-weight: 600;
		box-shadow: 0 1px 3px rgba(0, 0, 0, 0.08);
	}
	.tb__period-btn:disabled {
		opacity: 0.5;
		cursor: default;
	}
</style>
