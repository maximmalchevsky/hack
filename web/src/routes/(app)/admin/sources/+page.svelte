<script lang="ts">
	import { onMount } from 'svelte';
	import Card from '$lib/components/Card.svelte';
	import Badge from '$lib/components/Badge.svelte';
	import Tabs from '$lib/components/Tabs.svelte';
	import { listAdminSources, type AdminSource } from '$lib/api/admin';
	import { ApiError } from '$lib/api/client';

	let sources = $state<AdminSource[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let activeTab = $state('all');

	onMount(async () => {
		try {
			const r = await listAdminSources();
			sources = r.sources ?? [];
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		} finally {
			loading = false;
		}
	});

	const counts = $derived({
		connected: sources.filter((s) => s.status === 'connected').length,
		error: sources.filter((s) => s.status === 'error').length,
		disabled: sources.filter((s) => s.status === 'disabled').length
	});

	const tabs = $derived([
		{ id: 'all', label: 'Все', count: sources.length },
		{ id: 'connected', label: 'Активные', count: counts.connected },
		{ id: 'error', label: 'Ошибки', count: counts.error },
		{ id: 'disabled', label: 'Отключённые', count: counts.disabled }
	]);

	const visible = $derived(
		activeTab === 'all' ? sources : sources.filter((s) => s.status === activeTab)
	);

	function statusVariant(s: string): 'success' | 'danger' | 'neutral' | 'warning' {
		if (s === 'connected') return 'success';
		if (s === 'error') return 'danger';
		if (s === 'pending') return 'warning';
		return 'neutral';
	}

	function providerLabel(p: string): string {
		return ({
			ical: 'iCal / ICS',
			caldav: 'CalDAV',
			google_calendar: 'Google Calendar',
			ms365: 'Microsoft 365',
			jira: 'Jira',
			yandex_tracker: 'Yandex Tracker'
		} as Record<string, string>)[p] ?? p;
	}

	function fmtDate(iso?: string): string {
		if (!iso) return 'никогда';
		return new Date(iso).toLocaleString('ru', { dateStyle: 'short', timeStyle: 'short' });
	}
</script>

<div class="page-header">
	<div>
		<h1>Источники</h1>
		<div class="page-header__subtitle">Все подключённые интеграции в системе</div>
	</div>
</div>

{#if error}
	<div class="section">
		<Badge variant="danger"><i class="ti ti-alert-circle"></i>{error}</Badge>
	</div>
{/if}

{#if loading}
	<div class="text-text-3 text-sm">Загрузка…</div>
{:else if sources.length === 0}
	<Card>
		<div class="text-text-3 text-sm" style="padding: 16px;">Источников нет.</div>
	</Card>
{:else}
	<Tabs {tabs} bind:value={activeTab} />

	<div class="space-y-2">
		{#each visible as s (s.id)}
			<Card>
				<div class="flex items-center gap-3">
					<div
						class="header__logo-icon"
						style="background: var(--surface-2); color: var(--text-2);"
					>
						<i class="ti ti-plug"></i>
					</div>
					<div class="flex-1">
						<div class="flex items-center gap-2">
							<div class="card__title">{providerLabel(s.provider)}</div>
							<Badge variant={statusVariant(s.status)}>{s.status}</Badge>
						</div>
						<div class="text-text-3 text-xs">
							{s.employee_name}{#if s.account_label} · {s.account_label}{:else if s.account_email} · {s.account_email}{/if}
							· последний sync: {fmtDate(s.last_sync_at)}
						</div>
						{#if s.last_error}
							<div class="text-text-2 text-xs" style="color: var(--danger-strong);">
								{s.last_error}
							</div>
						{/if}
					</div>
				</div>
			</Card>
		{/each}
	</div>
{/if}
