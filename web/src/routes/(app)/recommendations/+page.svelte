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
		type Recommendation,
		type RecommendationScope
	} from '$lib/api/recommendations';
	import { ApiError } from '$lib/api/client';
	import { user } from '$lib/stores/user';

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
				<div class="flex items-start gap-3">
					<div class="flex-1">
						<div class="flex items-center gap-2 mb-1">
							<Badge variant={priorityVariant(r.priority)}>{priorityLabel(r.priority)}</Badge>
							<div class="card__title">{r.title}</div>
							<Badge variant="neutral">
								{r.generated_by === 'ai' ? 'AI' : 'rule'}
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
						<div class="text-text-2 text-sm" style="margin-bottom: 8px;">
							{r.explanation}
						</div>
						{#if r.evidence}
							<details>
								<summary class="text-text-3 text-xs cursor-pointer">evidence</summary>
								<pre class="text-text-2 text-xs" style="margin-top: 6px;">{JSON.stringify(
										r.evidence,
										null,
										2
									)}</pre>
							</details>
						{/if}
					</div>
					<div class="flex flex-col gap-1">
						<Button size="sm" variant="primary" icon="ti-check" onclick={() => apply(r.id)}
							>Принять</Button
						>
						<Button size="sm" variant="ghost" icon="ti-x" onclick={() => dismiss(r.id)}
							>Отклонить</Button
						>
					</div>
				</div>
			</Card>
		{/each}
	</div>
{/if}
