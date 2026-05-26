<script lang="ts">
	import { onMount } from 'svelte';
	import Card from '$lib/components/Card.svelte';
	import Badge from '$lib/components/Badge.svelte';
	import Button from '$lib/components/Button.svelte';
	import Tabs from '$lib/components/Tabs.svelte';
	import {
		listRecommendations,
		generateRecommendations,
		applyRecommendation,
		dismissRecommendation,
		snoozeRecommendation,
		type Recommendation,
		type RecommendationScope
	} from '$lib/api/recommendations';
	import { ApiError } from '$lib/api/client';
	import { user } from '$lib/stores/user';
	import { goto } from '$app/navigation';

	let recs = $state<Recommendation[]>([]);
	let loading = $state(true);
	let generating = $state(false);
	let error = $state<string | null>(null);
	let activeTab = $state<RecommendationScope>('mine');

	const role = $derived($user?.role ?? 'employee');

	// Какие табы видны: employee — только Мои.
	const tabs = $derived(buildTabs(role));

	function buildTabs(r: UserRole): { id: RecommendationScope; label: string }[] {
		const list: { id: RecommendationScope; label: string }[] = [{ id: 'mine', label: 'Мои' }];
		if (['manager', 'hr', 'pm', 'admin'].includes(r)) {
			list.push({ id: 'team', label: 'Подчинённые' });
		}
		if (['hr', 'admin', 'analyst'].includes(r)) {
			list.push({ id: 'all', label: 'Вся компания' });
		}
		return list;
	}

	onMount(async () => {
		// Если у роли есть скоуп шире — стартуем с него, чтобы сразу было что показать.
		if (['hr', 'admin', 'analyst'].includes(role)) {
			activeTab = 'all';
		} else if (['manager', 'pm'].includes(role)) {
			activeTab = 'team';
		}
		await load();
	});

	// Перезагружаем при смене таба.
	$effect(() => {
		// зависим только от activeTab
		activeTab;
		load();
	});

	async function load() {
		loading = true;
		error = null;
		try {
			const r = await listRecommendations(activeTab);
			recs = r.recommendations ?? [];
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
			recs = [];
		} finally {
			loading = false;
		}
	}

	async function generate() {
		generating = true;
		error = null;
		try {
			await generateRecommendations();
			await load();
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		} finally {
			generating = false;
		}
	}

	async function apply(id: string) {
		try {
			await applyRecommendation(id);
			recs = recs.filter((r) => r.id !== id);
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		}
	}

	async function dismiss(id: string) {
		try {
			await dismissRecommendation(id);
			recs = recs.filter((r) => r.id !== id);
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		}
	}

	async function snooze(id: string) {
		try {
			await snoozeRecommendation(id, 7);
			recs = recs.filter((r) => r.id !== id);
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		}
	}

	// «Сделать» — открывает страницу, на которой пользователь может реально
	// разобраться с рекомендацией. Параллельно помечаем её «applied» на бэке,
	// чтобы в следующий раз не показывать.
	async function doIt(r: Recommendation) {
		// Для task_overload: если в payload есть jira_link — открываем
		// его в новой вкладке. Это конкретный actionable шаг.
		const jiraLink = (r.payload as Record<string, unknown> | undefined)?.jira_link as
			| string
			| undefined;
		if (r.kind === 'task_overload' && jiraLink) {
			try {
				await applyRecommendation(r.id);
			} catch {
				// ok
			}
			window.open(jiraLink, '_blank', 'noopener');
			return;
		}

		const target = targetPathFor(r.kind);
		try {
			await applyRecommendation(r.id);
		} catch {
			// если не сразу — не страшно, пользователь уже идёт делать
		}
		await goto(target);
	}

	function targetPathFor(kind: string): string {
		switch (kind) {
			case 'update_profile':
			case 'confirm_schedule':
			case 'check_tz':
			case 'check_hr_data':
				return '/profile';
			case 'move_meeting':
			case 'change_meeting_window':
				return '/scheduler';
			case 'reduce_load':
			case 'no_new_meetings':
			case 'task_overload':
				return '/workload';
			default:
				return '/diagnostics';
		}
	}

	function priorityVariant(
		p: Recommendation['priority']
	): 'info' | 'warning' | 'danger' | 'neutral' {
		switch (p) {
			case 'critical':
				return 'danger';
			case 'high':
				return 'danger';
			case 'medium':
				return 'warning';
			case 'low':
				return 'info';
			default:
				return 'neutral';
		}
	}

	function priorityLabel(p: Recommendation['priority']): string {
		return { critical: 'критично', high: 'высокий', medium: 'средний', low: 'низкий' }[p] ?? p;
	}

	const emptyText = $derived(
		activeTab === 'mine'
			? 'Рекомендаций нет'
			: activeTab === 'team'
				? 'У подчинённых рекомендаций нет'
				: 'По компании рекомендаций нет'
	);
</script>

<div class="page-header">
	<div>
		<h1>Рекомендации</h1>
		<div class="page-header__subtitle">
			Объяснимые подсказки: обновить профиль, перенести встречу, проверить часовой пояс и т.д.
		</div>
	</div>
	<div class="page-header__actions">
		<Button icon="ti-refresh" onclick={generate} disabled={generating}>
			{generating ? 'Обновляем…' : 'Обновить'}
		</Button>
	</div>
</div>

{#if tabs.length > 1}
	<Tabs {tabs} bind:value={activeTab as string} />
{/if}

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
{:else if recs.length === 0}
	<Card>
		<div class="text-text-3 text-sm" style="padding: 16px; text-align: center;">
			{emptyText}
		</div>
	</Card>
{:else}
	<div class="space-y-2">
		{#each recs as r (r.id)}
			<Card>
				<div class="rec">
					<div class="rec__head">
						<Badge variant={priorityVariant(r.priority)}>{priorityLabel(r.priority)}</Badge>
						<div class="card__title">{r.title}</div>
						<Badge variant={r.generated_by === 'ai' ? 'info' : 'neutral'}>
							{r.generated_by === 'ai' ? '✨ ИИ-ассистент' : 'По шаблону'}
						</Badge>
					</div>
					{#if r.employee && activeTab !== 'mine'}
						<div class="text-text-3 text-xs" style="margin-bottom: 6px;">
							<i class="ti ti-user"></i>
							<a href="/employees/{r.employee.id}" style="color: inherit;">
								{r.employee.full_name}
							</a>
							{#if r.employee.department} · {r.employee.department}{/if}
						</div>
					{/if}
					<div class="text-text-2 text-sm" style="margin-bottom: 12px;">
						{r.explanation}
					</div>

					{#if r.kind === 'task_overload' && (r.payload as Record<string, unknown> | undefined)?.task_key}
						{@const p = r.payload as Record<string, unknown>}
						<div class="rec-jira">
							<i class="ti ti-checkbox" style="color: #0052cc;"></i>
							<span class="rec-jira__key">{p.task_key}</span>
							{#if p.task_title}<span class="rec-jira__title">{p.task_title}</span>{/if}
							{#if p.jira_link}
								<a
									href={p.jira_link as string}
									target="_blank"
									rel="noopener"
									class="rec-jira__link"
								>
									Открыть в Jira →
								</a>
							{/if}
						</div>
					{/if}

					<div class="rec__actions">
						<Button size="sm" variant="primary" icon="ti-arrow-right" onclick={() => doIt(r)}>
							Сделать
						</Button>
						<Button size="sm" variant="ghost" icon="ti-clock-pause" onclick={() => snooze(r.id)}>
							Отложить на неделю
						</Button>
						<Button size="sm" variant="ghost" icon="ti-x" onclick={() => dismiss(r.id)}>
							Отклонить
						</Button>
					</div>

					{#if r.generated_by === 'ai'}
						<div class="rec__footer">
							<i class="ti ti-sparkles"></i>
							<span>Сгенерировано ИИ-ассистентом</span>
						</div>
					{/if}
				</div>
			</Card>
		{/each}
	</div>
{/if}

<style>
	/* --- Блок Jira-задачи в рекомендации task_overload --- */
	.rec-jira {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 8px 12px;
		margin-bottom: 12px;
		background: var(--surface);
		border: 0.5px solid var(--border);
		border-radius: 8px;
		font-size: 13px;
		flex-wrap: wrap;
	}
	.rec-jira__key {
		font-family: 'JetBrains Mono', monospace;
		font-size: 11px;
		font-weight: 600;
		color: #0052cc;
		padding: 2px 6px;
		background: rgba(0, 82, 204, 0.08);
		border-radius: 4px;
	}
	.rec-jira__title {
		color: var(--text);
		flex: 1;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.rec-jira__link {
		color: #0052cc;
		text-decoration: none;
		font-weight: 500;
		font-size: 12px;
		white-space: nowrap;
	}
	.rec-jira__link:hover {
		text-decoration: underline;
	}

	.rec {
		display: flex;
		flex-direction: column;
	}
	.rec__head {
		display: flex;
		align-items: center;
		gap: 8px;
		margin-bottom: 6px;
		flex-wrap: wrap;
	}
	.rec__actions {
		display: flex;
		gap: 8px;
		flex-wrap: wrap;
	}
	.rec__footer {
		display: flex;
		align-items: center;
		gap: 6px;
		margin-top: 10px;
		padding-top: 8px;
		border-top: 1px dashed var(--border);
		font-size: 11px;
		color: var(--text-3);
	}
	.rec__footer i {
		font-size: 13px;
		color: var(--info-strong);
	}
</style>
