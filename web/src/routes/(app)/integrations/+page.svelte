<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/stores';
	import { browser } from '$app/environment';
	import { env } from '$env/dynamic/public';
	import Card from '$lib/components/Card.svelte';
	import Badge from '$lib/components/Badge.svelte';
	import Button from '$lib/components/Button.svelte';
	import {
		listIntegrations,
		connectICal,
		connectCalDAV,
		triggerSync,
		removeIntegration,
		type Integration
	} from '$lib/api/integrations';
	import { ApiError, getAccessToken } from '$lib/api/client';

	let integrations = $state<Integration[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let success = $state<string | null>(null);

	// iCal form
	let icalUrl = $state('');
	let icalLabel = $state('');
	let icalBusy = $state(false);

	// CalDAV form
	let cdEndpoint = $state('https://caldav.yandex.ru');
	let cdUsername = $state('');
	let cdPassword = $state('');
	let cdLabel = $state('');
	let cdBusy = $state(false);

	onMount(async () => {
		await load();
		// Отображаем результат OAuth-callback'а (Яндекс).
		if (browser) {
			const url = new URL(window.location.href);
			if (url.searchParams.get('connected') === 'yandex') {
				success = 'Яндекс Календарь подключён, синхронизация запущена';
				url.searchParams.delete('connected');
				window.history.replaceState({}, '', url.pathname + (url.search ? url.search : ''));
			}
			const errCode = url.searchParams.get('error');
			if (errCode) {
				error = `OAuth прервался: ${errCode}`;
				url.searchParams.delete('error');
				window.history.replaceState({}, '', url.pathname + (url.search ? url.search : ''));
			}
		}
	});

	function backendURL(): string {
		return env.PUBLIC_API_URL || (browser ? '' : 'http://localhost:8080');
	}

	function startYandexOAuth() {
		// /connect защищён AuthRequired — передаём JWT в query (middleware принимает
		// и через Authorization-header, и через ?token=).
		const tok = getAccessToken() ?? '';
		const url = `${backendURL()}/api/v1/integrations/oauth/yandex/connect?token=${encodeURIComponent(tok)}`;
		window.location.href = url;
	}

	const yandexIntegrations = $derived(
		integrations.filter((i) => i.provider === 'yandex_calendar')
	);

	async function load() {
		loading = true;
		try {
			const r = await listIntegrations();
			integrations = r.integrations ?? [];
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		} finally {
			loading = false;
		}
	}

	async function submitICal() {
		icalBusy = true;
		error = null;
		success = null;
		try {
			await connectICal({ feed_url: icalUrl || undefined, label: icalLabel || undefined });
			success = 'iCal-источник подключён, синхронизация в очереди';
			icalUrl = '';
			icalLabel = '';
			await load();
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		} finally {
			icalBusy = false;
		}
	}

	async function submitCalDAV() {
		cdBusy = true;
		error = null;
		success = null;
		try {
			await connectCalDAV({
				endpoint: cdEndpoint,
				username: cdUsername,
				password: cdPassword,
				label: cdLabel || undefined
			});
			success = 'CalDAV-источник подключён, синхронизация в очереди';
			cdUsername = '';
			cdPassword = '';
			cdLabel = '';
			await load();
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		} finally {
			cdBusy = false;
		}
	}

	async function sync(id: string) {
		try {
			await triggerSync(id);
			success = 'Синхронизация поставлена в очередь';
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		}
	}

	async function remove(id: string) {
		try {
			await removeIntegration(id);
			integrations = integrations.filter((i) => i.id !== id);
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		}
	}

	function fmtDate(iso?: string): string {
		if (!iso) return 'никогда';
		return new Date(iso).toLocaleString('ru', { dateStyle: 'short', timeStyle: 'short' });
	}

	function statusVariant(s: Integration['status']): 'success' | 'danger' | 'warning' | 'neutral' {
		switch (s) {
			case 'connected':
				return 'success';
			case 'error':
				return 'danger';
			case 'pending':
				return 'warning';
			default:
				return 'neutral';
		}
	}

	function providerLabel(p: Integration['provider']): string {
		return {
			ical: 'iCal / ICS',
			caldav: 'CalDAV',
			google_calendar: 'Google Calendar',
			ms365: 'Microsoft 365',
			jira: 'Jira',
			yandex_tracker: 'Yandex Tracker'
		}[p];
	}
</script>

<div class="page-header">
	<div>
		<h1>Интеграции</h1>
		<div class="page-header__subtitle">
			Источники событий и задач для расчёта актуальности рабочего времени
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
{#if success}
	<div class="section">
		<Badge variant="success">
			<i class="ti ti-check"></i>
			{success}
		</Badge>
	</div>
{/if}

{#if yandexIntegrations.length === 0}
	<div class="section" style="margin-bottom: 16px;">
		<Card>
			<div class="yandex-block">
				<div class="yandex-block__icon">
					<i class="ti ti-calendar-event"></i>
				</div>
				<div class="yandex-block__body">
					<div class="card__title">Яндекс Календарь</div>
					<div class="card__subtitle">
						Подключение через Яндекс ID — один клик, без app-password. Прочитаем
						события на ближайшие 7 дней и будем обновлять каждые 5 минут.
					</div>
				</div>
				<button class="yandex-btn" onclick={startYandexOAuth} type="button">
					<span class="yandex-btn__logo">Я</span>
					Войти через Яндекс
				</button>
			</div>
		</Card>
	</div>
{:else}
	<div class="section" style="margin-bottom: 16px;">
		<Card>
			<div class="yandex-block">
				<div class="yandex-block__icon">
					<i class="ti ti-calendar-event"></i>
				</div>
				<div class="yandex-block__body">
					<div class="card__title">
						Яндекс Календарь <Badge variant="success">подключён</Badge>
					</div>
					<div class="card__subtitle">
						{yandexIntegrations[0].account_email || 'Аккаунт подключён'} · события
						синхронизируются автоматически
					</div>
				</div>
				<button class="yandex-btn yandex-btn--ghost" onclick={startYandexOAuth} type="button">
					<i class="ti ti-refresh"></i>
					Переподключить
				</button>
			</div>
		</Card>
	</div>
{/if}

<div class="grid-2" style="margin-bottom: 24px;">
	<Card title="Подключить iCal / ICS" subtitle="Публичная ссылка на .ics-feed (без OAuth)">
		<div class="field">
			<label class="field__label" for="ical-url">URL feed'a</label>
			<input
				id="ical-url"
				type="text"
				bind:value={icalUrl}
				placeholder="https://calendar.google.com/calendar/ical/.../basic.ics"
			/>
			<div class="field__hint">Пустой URL — режим ручной загрузки .ics</div>
		</div>
		<div class="field">
			<label class="field__label" for="ical-label">Метка</label>
			<input id="ical-label" type="text" bind:value={icalLabel} placeholder="Мой Google Calendar" />
		</div>
		<Button variant="primary" icon="ti-plug" onclick={submitICal} disabled={icalBusy}>
			{icalBusy ? 'Подключаем…' : 'Подключить'}
		</Button>
	</Card>

	<Card title="Подключить CalDAV" subtitle="Yandex Календарь, Apple iCloud, Nextcloud">
		<div class="field">
			<label class="field__label" for="cd-endpoint">Endpoint</label>
			<input id="cd-endpoint" type="text" bind:value={cdEndpoint} />
		</div>
		<div class="field">
			<label class="field__label" for="cd-username">Логин</label>
			<input id="cd-username" type="text" bind:value={cdUsername} placeholder="user@yandex.ru" />
		</div>
		<div class="field">
			<label class="field__label" for="cd-password">App-password</label>
			<input id="cd-password" type="password" bind:value={cdPassword} />
			<div class="field__hint">Для Yandex/Apple — это специальный app-password, не основной</div>
		</div>
		<div class="field">
			<label class="field__label" for="cd-label">Метка</label>
			<input id="cd-label" type="text" bind:value={cdLabel} placeholder="Yandex Календарь" />
		</div>
		<Button variant="primary" icon="ti-plug" onclick={submitCalDAV} disabled={cdBusy}>
			{cdBusy ? 'Подключаем…' : 'Подключить'}
		</Button>
	</Card>
</div>

<Card title="Подключённые источники">
	{#if loading}
		<div class="text-text-3 text-sm">Загрузка…</div>
	{:else if integrations.length === 0}
		<div class="text-text-3 text-sm" style="padding: 12px 0;">
			Источники не подключены. Добавь iCal или CalDAV выше — события начнут синхронизироваться.
		</div>
	{:else}
		<div class="space-y-2">
			{#each integrations as i (i.id)}
				<div
					class="flex items-center gap-3 p-3"
					style="border: 0.5px solid var(--border); border-radius: var(--radius-md);"
				>
					<div class="header__logo-icon" style="background: var(--surface-2); color: var(--text-2);">
						<i class="ti ti-plug"></i>
					</div>
					<div class="flex-1">
						<div class="flex items-center gap-2">
							<div class="card__title">{providerLabel(i.provider)}</div>
							<Badge variant={statusVariant(i.status)}>{i.status}</Badge>
						</div>
						<div class="text-text-3 text-xs">
							{#if i.account_label}{i.account_label}{:else if i.account_email}{i.account_email}{:else}—{/if}
							· последний sync: {fmtDate(i.last_sync_at)}
						</div>
						{#if i.last_error}
							<div class="text-text-2 text-xs" style="color: var(--danger-strong);">
								Ошибка: {i.last_error}
							</div>
						{/if}
					</div>
					<Button size="sm" icon="ti-refresh" onclick={() => sync(i.id)}>Синхронизировать</Button>
					<Button size="sm" variant="ghost" icon="ti-trash" onclick={() => remove(i.id)}>
						Удалить
					</Button>
				</div>
			{/each}
		</div>
	{/if}
</Card>

<style>
	.yandex-block {
		display: flex;
		align-items: center;
		gap: 16px;
	}
	.yandex-block__icon {
		width: 44px;
		height: 44px;
		border-radius: var(--radius-md);
		background: #fc3f1d; /* Яндекс red */
		color: #fff;
		display: flex;
		align-items: center;
		justify-content: center;
		font-size: 22px;
		flex-shrink: 0;
	}
	.yandex-block__body {
		flex: 1;
		min-width: 0;
	}
	.yandex-btn {
		display: inline-flex;
		align-items: center;
		gap: 8px;
		padding: 8px 16px;
		font-size: 13px;
		font-weight: 500;
		background: #000;
		color: #fff;
		border: none;
		border-radius: 6px;
		cursor: pointer;
		transition: background 0.12s, transform 0.12s;
	}
	.yandex-btn:hover {
		background: #1a1a1a;
	}
	.yandex-btn:active {
		transform: scale(0.98);
	}
	.yandex-btn__logo {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 20px;
		height: 20px;
		background: #fc3f1d;
		border-radius: 4px;
		font-weight: 700;
		font-size: 13px;
		font-family: 'Helvetica Neue', Arial, sans-serif;
	}
	.yandex-btn--ghost {
		background: transparent;
		color: var(--text-2);
		border: 0.5px solid var(--border-2);
	}
	.yandex-btn--ghost:hover {
		background: var(--surface-2);
		color: var(--text);
	}
</style>
