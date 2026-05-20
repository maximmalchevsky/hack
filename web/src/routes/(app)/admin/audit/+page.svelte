<script lang="ts">
	import { onMount } from 'svelte';
	import Card from '$lib/components/Card.svelte';
	import Badge from '$lib/components/Badge.svelte';
	import { listAudit, type AuditRecord } from '$lib/api/admin';
	import { ApiError } from '$lib/api/client';

	let records = $state<AuditRecord[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let entityFilter = $state('');

	onMount(async () => {
		await load();
	});

	async function load() {
		loading = true;
		error = null;
		try {
			const r = await listAudit({
				entity: entityFilter || undefined,
				limit: 200
			});
			records = r.records ?? [];
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		} finally {
			loading = false;
		}
	}

	function actionVariant(a: string): 'success' | 'info' | 'warning' | 'danger' | 'neutral' {
		switch (a) {
			case 'create':
				return 'success';
			case 'update':
				return 'info';
			case 'delete':
				return 'danger';
			case 'apply':
				return 'success';
			case 'dismiss':
				return 'warning';
			default:
				return 'neutral';
		}
	}

	function fmt(iso: string): string {
		return new Date(iso).toLocaleString('ru', {
			day: '2-digit',
			month: 'short',
			hour: '2-digit',
			minute: '2-digit',
			second: '2-digit'
		});
	}

	function previewJSON(v: unknown): string {
		if (v === undefined || v === null) return '—';
		try {
			const s = typeof v === 'string' ? v : JSON.stringify(v);
			return s.length > 120 ? s.slice(0, 117) + '…' : s;
		} catch {
			return String(v);
		}
	}
</script>

<div class="page-header">
	<div>
		<h1>Журнал изменений</h1>
		<div class="page-header__subtitle">
			Audit log всех мутаций: профили, исключения, интеграции, рекомендации.
		</div>
	</div>
	<div class="page-header__actions">
		<select bind:value={entityFilter} onchange={load} style="width: 180px;">
			<option value="">Все сущности</option>
			<option value="work_profile">work_profile</option>
			<option value="exception">exception</option>
			<option value="integration">integration</option>
			<option value="recommendation">recommendation</option>
			<option value="user">user</option>
		</select>
		<button class="btn" onclick={load}>
			<i class="ti ti-refresh"></i>Обновить
		</button>
	</div>
</div>

{#if error}
	<div class="section">
		<Badge variant="danger"><i class="ti ti-alert-circle"></i>{error}</Badge>
	</div>
{/if}

{#if loading}
	<div class="text-text-3 text-sm">Загрузка…</div>
{:else if records.length === 0}
	<Card>
		<div class="text-text-3 text-sm" style="padding: 16px; text-align: center;">
			Записей нет.
		</div>
	</Card>
{:else}
	<div class="space-y-2">
		{#each records as r (r.id)}
			<Card>
				<div class="flex items-start gap-3">
					<Badge variant={actionVariant(r.action)}>{r.action}</Badge>
					<div class="flex-1">
						<div class="flex items-center gap-2 mb-1">
							<div class="card__title">{r.entity}</div>
							{#if r.entity_id}
								<span class="text-text-3 text-xs font-mono">#{r.entity_id.slice(0, 8)}</span>
							{/if}
						</div>
						<div class="text-text-3 text-xs">
							{fmt(r.created_at)}
							{#if r.actor_user_id} · actor: <span class="font-mono">{r.actor_user_id.slice(0, 8)}</span>{/if}
						</div>
						<details style="margin-top: 6px;">
							<summary class="text-text-2 text-xs cursor-pointer">before/after</summary>
							<pre class="text-text-2 text-xs" style="margin-top: 4px; white-space: pre-wrap;">before: {previewJSON(r.before)}
after:  {previewJSON(r.after)}</pre>
						</details>
					</div>
				</div>
			</Card>
		{/each}
	</div>
{/if}
