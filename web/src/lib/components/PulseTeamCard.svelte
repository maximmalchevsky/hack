<script lang="ts">
	// PulseTeamCard — менеджер видит pulse-ответы своих сотрудников.
	// Если у пользователя нет доступа (403) — компонент не рендерит ничего.
	import { onMount } from 'svelte';
	import Card from './Card.svelte';
	import Avatar from './Avatar.svelte';
	import Badge from './Badge.svelte';
	import { getPulseTeam, type PulseTeamSummary } from '$lib/api/pulse';
	import { ApiError } from '$lib/api/client';

	let data = $state<PulseTeamSummary | null>(null);
	let loading = $state(true);
	let forbidden = $state(false);
	let error = $state<string | null>(null);

	const EMOJI: Record<number, string> = {
		1: '😞',
		2: '😐',
		3: '🙂',
		4: '😊',
		5: '🤩'
	};

	onMount(async () => {
		try {
			data = await getPulseTeam();
		} catch (e) {
			if (e instanceof ApiError && e.status === 403) {
				forbidden = true;
			} else {
				error = e instanceof Error ? e.message : String(e);
			}
		} finally {
			loading = false;
		}
	});

	function initials(name: string): string {
		const parts = name.trim().split(/\s+/);
		return ((parts[0]?.[0] ?? '') + (parts[1]?.[0] ?? '')).toUpperCase() || '??';
	}

	function scoreVariant(score?: number): 'success' | 'warning' | 'danger' | 'neutral' {
		if (score == null) return 'neutral';
		if (score <= 2) return 'danger';
		if (score === 3) return 'warning';
		return 'success';
	}

	function fmtDays(days?: number): string {
		if (days == null) return '—';
		if (days === 0) return 'сегодня';
		if (days === 1) return 'вчера';
		return `${days} дн. назад`;
	}
</script>

{#if forbidden}
	<!-- ничего — не менеджер -->
{:else if loading}
	<!-- skeleton — не отрисовываем чтобы не мигало -->
{:else if error}
	<Card title="Pulse команды" subtitle="Не удалось загрузить">
		<div class="text-text-3 text-sm" style="padding: 8px 0;">{error}</div>
	</Card>
{:else if data && data.members.length > 0}
	<Card
		title="Pulse команды"
		subtitle="Самочувствие сотрудников за последний цикл (раз в 2 недели)"
	>
		<div class="ptc-summary">
			<div class="ptc-stat">
				<div class="ptc-stat__value">{data.avg_last > 0 ? data.avg_last.toFixed(1) : '—'}</div>
				<div class="ptc-stat__label">средний балл</div>
			</div>
			<div class="ptc-stat">
				<div class="ptc-stat__value ptc-stat__value--danger">{data.red_zone}</div>
				<div class="ptc-stat__label">в красной зоне</div>
			</div>
			<div class="ptc-stat">
				<div class="ptc-stat__value ptc-stat__value--muted">{data.no_data}</div>
				<div class="ptc-stat__label">ещё не отвечали</div>
			</div>
		</div>

		<div class="ptc-list">
			{#each data.members as m (m.employee_id)}
				<div class="ptc-row">
					<Avatar initials={initials(m.full_name)} size="sm" variant="purple" />
					<div class="ptc-row__main">
						<div class="ptc-row__name">{m.full_name}</div>
						<div class="ptc-row__meta">
							{#if m.department}{m.department} · {/if}
							{fmtDays(m.days_since)}
						</div>
						{#if m.comment}
							<div class="ptc-row__comment">«{m.comment}»</div>
						{/if}
					</div>
					<div class="ptc-row__score">
						{#if m.last_score != null}
							<div class="ptc-row__emoji">{EMOJI[m.last_score]}</div>
							<Badge variant={scoreVariant(m.last_score)}>{m.last_score}/5</Badge>
						{:else}
							<Badge variant="neutral">нет ответов</Badge>
						{/if}
					</div>
				</div>
			{/each}
		</div>
	</Card>
{/if}

<style>
	.ptc-summary {
		display: grid;
		grid-template-columns: repeat(3, 1fr);
		gap: 12px;
		margin-bottom: 14px;
	}
	.ptc-stat {
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: 10px;
		padding: 10px 12px;
		text-align: center;
	}
	.ptc-stat__value {
		font-size: 22px;
		font-weight: 700;
		color: var(--text);
	}
	.ptc-stat__value--danger {
		color: var(--danger-strong, #b91c1c);
	}
	.ptc-stat__value--muted {
		color: var(--text-3);
	}
	.ptc-stat__label {
		font-size: 11px;
		color: var(--text-3);
		margin-top: 2px;
	}
	.ptc-list {
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	.ptc-row {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 8px 10px;
		border-radius: 10px;
	}
	.ptc-row:hover {
		background: var(--surface);
	}
	.ptc-row__main {
		flex: 1;
		min-width: 0;
	}
	.ptc-row__name {
		font-size: 13px;
		font-weight: 600;
		color: var(--text);
	}
	.ptc-row__meta {
		font-size: 11px;
		color: var(--text-3);
		margin-top: 1px;
	}
	.ptc-row__comment {
		font-size: 12px;
		color: var(--text-2);
		margin-top: 4px;
		font-style: italic;
	}
	.ptc-row__score {
		display: flex;
		align-items: center;
		gap: 8px;
	}
	.ptc-row__emoji {
		font-size: 20px;
		line-height: 1;
	}
</style>
