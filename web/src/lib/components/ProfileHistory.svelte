<script lang="ts">
	// ProfileHistory — timeline версий work_profile сотрудника.
	// Используется на /employees/[id] и /profile.
	import { onMount } from 'svelte';
	import Card from './Card.svelte';
	import Badge from './Badge.svelte';
	import { getProfileHistory, type WorkProfile } from '$lib/api/profile';

	interface Props {
		employeeID: string;
		title?: string;
		subtitle?: string;
	}

	let { employeeID, title = 'История изменений графика', subtitle = '' }: Props = $props();

	let versions = $state<WorkProfile[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);

	onMount(async () => {
		await load();
	});

	async function load() {
		loading = true;
		error = null;
		try {
			const r = await getProfileHistory(employeeID);
			versions = r.versions ?? [];
		} catch (e) {
			error = e instanceof Error ? e.message : String(e);
		} finally {
			loading = false;
		}
	}

	function fmtDate(iso: string | undefined): string {
		if (!iso) return '—';
		try {
			return new Date(iso).toLocaleDateString('ru', {
				day: 'numeric',
				month: 'short',
				year: 'numeric'
			});
		} catch {
			return iso;
		}
	}

	function formatLabel(v: WorkProfile): string {
		return v.work_format === 'office'
			? 'офис'
			: v.work_format === 'remote'
				? 'удалённо'
				: v.work_format === 'hybrid'
					? 'гибрид'
					: v.work_format;
	}

	function summarizeHours(v: WorkProfile): string {
		const dayLabels: Array<[keyof WorkProfile['days_of_week'], string]> = [
			['mon', 'ПН'],
			['tue', 'ВТ'],
			['wed', 'СР'],
			['thu', 'ЧТ'],
			['fri', 'ПТ'],
			['sat', 'СБ'],
			['sun', 'ВС']
		];
		const parts: string[] = [];
		for (const [k, label] of dayLabels) {
			const dh = v.days_of_week?.[k];
			if (dh?.start && dh?.end) {
				parts.push(`${label} ${dh.start}–${dh.end}`);
			}
		}
		if (parts.length === 0) return 'без рабочих дней';
		// Группируем по диапазонам — слишком сложно, оставлю просто перечислением.
		return parts.join(' · ');
	}

	function diffWith(prev: WorkProfile | null, curr: WorkProfile): string[] {
		if (!prev) return ['Первая версия'];
		const out: string[] = [];
		if (prev.timezone !== curr.timezone) {
			out.push(`TZ: ${prev.timezone} → ${curr.timezone}`);
		}
		if (prev.work_format !== curr.work_format) {
			out.push(`формат: ${formatLabel(prev)} → ${formatLabel(curr)}`);
		}
		const prevHours = summarizeHours(prev);
		const currHours = summarizeHours(curr);
		if (prevHours !== currHours) {
			out.push('часы скорректированы');
		}
		if (out.length === 0) {
			out.push('косметическое обновление');
		}
		return out;
	}
</script>

<Card {title} {subtitle}>
	{#if loading}
		<div class="text-text-3 text-sm" style="padding: 12px;">Загрузка…</div>
	{:else if error}
		<Badge variant="danger"><i class="ti ti-alert-circle"></i>{error}</Badge>
	{:else if versions.length === 0}
		<div class="text-text-3 text-sm" style="padding: 12px; text-align: center;">
			Профиль ещё не редактировался.
		</div>
	{:else}
		<ol class="ph">
			{#each versions as v, i (v.id)}
				{@const isActive = !v.valid_to}
				{@const prev = versions[i + 1] ?? null}
				<li class="ph-row" class:ph-row--active={isActive}>
					<div class="ph-dot" class:ph-dot--active={isActive}></div>
					<div class="ph-body">
						<div class="ph-head">
							<span class="ph-date">
								{fmtDate(v.valid_from)}
								{#if v.valid_to}
									<span class="ph-arrow">→ {fmtDate(v.valid_to)}</span>
								{:else}
									<Badge variant="success">актуальная</Badge>
								{/if}
							</span>
						</div>
						<div class="ph-meta">
							<span class="ph-chip"><i class="ti ti-clock"></i>{summarizeHours(v)}</span>
							<span class="ph-chip"><i class="ti ti-world"></i>{v.timezone}</span>
							<span class="ph-chip"><i class="ti ti-building"></i>{formatLabel(v)}</span>
						</div>
						{#if !isActive || prev}
							<div class="ph-diff">
								{#each diffWith(prev, v) as d, j (j)}
									<span class="ph-diff-item">{d}</span>
								{/each}
							</div>
						{/if}
					</div>
				</li>
			{/each}
		</ol>
	{/if}
</Card>

<style>
	.ph {
		margin: 0;
		padding: 0;
		list-style: none;
		position: relative;
	}
	.ph::before {
		content: '';
		position: absolute;
		left: 8px;
		top: 8px;
		bottom: 8px;
		width: 1px;
		background: var(--border);
	}
	.ph-row {
		position: relative;
		display: flex;
		gap: 14px;
		padding: 8px 0 14px;
	}
	.ph-row:last-child {
		padding-bottom: 0;
	}
	.ph-dot {
		flex-shrink: 0;
		width: 9px;
		height: 9px;
		border-radius: 50%;
		background: var(--text-3);
		margin-top: 4px;
		margin-left: 4px;
		border: 2px solid var(--surface);
		position: relative;
		z-index: 1;
	}
	.ph-dot--active {
		background: var(--success-strong);
		box-shadow: 0 0 0 3px rgba(34, 197, 94, 0.15);
	}
	.ph-body {
		flex: 1;
		min-width: 0;
	}
	.ph-head {
		font-size: 13px;
		color: var(--text);
		font-weight: 600;
		margin-bottom: 4px;
		display: flex;
		align-items: center;
		gap: 8px;
		flex-wrap: wrap;
	}
	.ph-arrow {
		color: var(--text-3);
		font-weight: 400;
	}
	.ph-date {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		flex-wrap: wrap;
	}
	.ph-meta {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		font-size: 12px;
		color: var(--text-2);
		margin-bottom: 4px;
	}
	.ph-chip {
		display: inline-flex;
		align-items: center;
		gap: 4px;
	}
	.ph-chip i {
		font-size: 13px;
		opacity: 0.7;
	}
	.ph-diff {
		font-size: 11px;
		color: var(--text-3);
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
	}
	.ph-diff-item {
		padding: 2px 6px;
		background: var(--surface-2);
		border-radius: 6px;
	}
</style>
