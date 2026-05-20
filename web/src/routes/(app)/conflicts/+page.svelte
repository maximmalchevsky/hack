<script lang="ts">
	import { onMount } from 'svelte';
	import Card from '$lib/components/Card.svelte';
	import Badge from '$lib/components/Badge.svelte';
	import Tabs from '$lib/components/Tabs.svelte';
	import { listConflicts, type ConflictRow } from '$lib/api/conflicts';
	import { ApiError } from '$lib/api/client';

	let conflicts = $state<ConflictRow[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let activeTab = $state('all');

	onMount(async () => {
		try {
			const r = await listConflicts();
			conflicts = r.conflicts ?? [];
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		} finally {
			loading = false;
		}
	});

	const counts = $derived({
		high: conflicts.filter((c) => c.severity === 'high').length,
		medium: conflicts.filter((c) => c.severity === 'medium').length,
		low: conflicts.filter((c) => c.severity === 'low').length
	});

	const tabs = $derived([
		{ id: 'all', label: 'Все', count: conflicts.length },
		{ id: 'high', label: 'Критичные', count: counts.high },
		{ id: 'medium', label: 'Средние', count: counts.medium },
		{ id: 'low', label: 'Низкие', count: counts.low }
	]);

	const visible = $derived(activeTab === 'all' ? conflicts : conflicts.filter((c) => c.severity === activeTab));

	function reasonLabel(r: ConflictRow['reason']): string {
		switch (r) {
			case 'outside_hours':
				return 'Вне рабочих часов';
			case 'weekend':
				return 'Выходной';
			case 'within_exception':
				return 'В отпуске/больничном';
			case 'no_profile':
				return 'Нет профиля';
			default:
				return r;
		}
	}

	function severityVariant(s: ConflictRow['severity']): 'danger' | 'warning' | 'info' {
		return s === 'high' ? 'danger' : s === 'medium' ? 'warning' : 'info';
	}

	function fmt(iso: string): string {
		return new Date(iso).toLocaleString('ru', {
			weekday: 'short',
			day: 'numeric',
			month: 'short',
			hour: '2-digit',
			minute: '2-digit'
		});
	}
</script>

<div class="page-header">
	<div>
		<h1>Конфликты</h1>
		<div class="page-header__subtitle">
			События вне рабочих часов, в выходные и в исключениях — по всем сотрудникам
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
{:else if conflicts.length === 0}
	<Card>
		<div class="text-text-3 text-sm" style="padding: 16px; text-align: center;">
			Конфликтов не найдено за последнюю неделю и следующие 2 недели.
		</div>
	</Card>
{:else}
	<Tabs {tabs} bind:value={activeTab} />

	<div class="space-y-2">
		{#each visible as c (c.event_id)}
			<Card>
				<div class="flex items-start gap-3">
					<Badge variant={severityVariant(c.severity)}>{c.severity}</Badge>
					<div class="flex-1">
						<div class="flex items-center gap-2 mb-1">
							<div class="card__title">{c.title || '(без названия)'}</div>
							<Badge variant="neutral">{reasonLabel(c.reason)}</Badge>
						</div>
						<div class="text-text-2 text-sm">
							<a href="/employees/{c.employee_id}" class="emp-link">{c.full_name}</a>{#if c.department} · {c.department}{/if}
						</div>
						<div class="text-text-3 text-xs">
							{fmt(c.start_at)} — {fmt(c.end_at)}
						</div>
					</div>
				</div>
			</Card>
		{/each}
	</div>
{/if}
