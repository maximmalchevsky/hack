<script lang="ts">
	import { onMount } from 'svelte';
	import Card from '$lib/components/Card.svelte';
	import Badge from '$lib/components/Badge.svelte';
	import Tabs from '$lib/components/Tabs.svelte';
	import { notifications } from '$lib/stores/notifications';

	let activeTab = $state('all');

	const tabs = $derived([
		{ id: 'all', label: 'Все', count: $notifications.items.length },
		{ id: 'unread', label: 'Непрочитанные', count: $notifications.unread }
	]);

	const visible = $derived(
		activeTab === 'unread'
			? $notifications.items.filter((n) => !n.read)
			: $notifications.items
	);

	onMount(async () => {
		await notifications.reload();
	});

	function fmtTime(iso: string): string {
		try {
			const d = new Date(iso);
			const diffMs = Date.now() - d.getTime();
			const hours = Math.floor(diffMs / (3600 * 1000));
			if (hours < 1) return 'менее часа назад';
			if (hours < 24) return `${hours} ч назад`;
			const days = Math.floor(hours / 24);
			return `${days} дн назад`;
		} catch {
			return iso;
		}
	}

	function kindVariant(k: string): 'info' | 'success' | 'warning' | 'danger' | 'neutral' {
		if (k.includes('error') || k.includes('stale')) return 'danger';
		if (k.includes('sync') || k.includes('success')) return 'success';
		if (k.includes('recommend')) return 'info';
		if (k.includes('confirm') || k.includes('warning')) return 'warning';
		return 'neutral';
	}
</script>

<div class="page-header">
	<div>
		<h1>Уведомления</h1>
		<div class="page-header__subtitle">
			Непрочитанных: {$notifications.unread}{#if !$notifications.connected} · подключение прервано{/if}
		</div>
	</div>
	<div class="page-header__actions">
		<button class="btn" onclick={() => notifications.markAllRead()}>
			<i class="ti ti-checks"></i>Отметить всё прочитанным
		</button>
	</div>
</div>

<Tabs {tabs} bind:value={activeTab} />

{#if visible.length === 0}
	<Card>
		<div class="text-text-3 text-sm" style="padding: 16px; text-align: center;">
			Уведомлений нет.
		</div>
	</Card>
{:else}
	<div class="space-y-2">
		{#each visible as n (n.id)}
			<div class="notif-row" class:notif-row--read={n.read}>
				<Card>
					<div class="flex items-start gap-3">
						<Badge variant={kindVariant(n.kind)} dot />
						<div class="flex-1">
							<div class="flex items-center gap-2 mb-1">
								<div class="card__title">{n.title}</div>
								{#if !n.read}
									<span
										style="width: 6px; height: 6px; background: var(--info-strong); border-radius: 50%; display: inline-block;"
									></span>
								{/if}
							</div>
							{#if n.body}
								<div class="text-text-2 text-sm">{n.body}</div>
							{/if}
							<div class="text-text-3 text-xs mt-1">
								{fmtTime(n.created_at)} · {n.kind}
							</div>
						</div>
						{#if !n.read}
							<button class="btn btn--xs btn--ghost" onclick={() => notifications.markRead(n.id)}>
								<i class="ti ti-check"></i>Прочитано
							</button>
						{:else}
							<span class="text-text-3 text-xs" style="white-space: nowrap;">прочитано</span>
						{/if}
					</div>
				</Card>
			</div>
		{/each}
	</div>
{/if}

<style>
	.notif-row--read :global(.card) {
		opacity: 0.55;
		background: var(--surface-2);
	}
	.notif-row--read :global(.card__title) {
		font-weight: 500;
	}
</style>
