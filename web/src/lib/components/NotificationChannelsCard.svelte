<script lang="ts">
	// NotificationChannelsCard — управление каналами уведомлений (email/telegram).
	// Используется на /profile. Сам грузит prefs + статус telegram.
	import { onMount } from 'svelte';
	import Card from './Card.svelte';
	import Badge from './Badge.svelte';
	import Button from './Button.svelte';
	import {
		getNotificationPrefs,
		updateNotificationPrefs,
		getTelegramStatus,
		unlinkTelegram,
		type NotificationPrefs,
		type TelegramStatus
	} from '$lib/api/notification-channels';

	let prefs = $state<NotificationPrefs | null>(null);
	let tg = $state<TelegramStatus | null>(null);
	let loading = $state(true);
	let saving = $state(false);
	let error = $state<string | null>(null);

	onMount(async () => {
		await reload();
	});

	async function reload() {
		loading = true;
		error = null;
		try {
			const [p, t] = await Promise.all([getNotificationPrefs(), getTelegramStatus()]);
			prefs = p;
			tg = t;
		} catch (e) {
			error = e instanceof Error ? e.message : String(e);
		} finally {
			loading = false;
		}
	}

	async function toggleEmail() {
		if (!prefs) return;
		saving = true;
		try {
			prefs = await updateNotificationPrefs({
				email_notifications: !prefs.email_notifications
			});
		} catch (e) {
			error = e instanceof Error ? e.message : String(e);
		} finally {
			saving = false;
		}
	}

	async function toggleTelegram() {
		if (!prefs) return;
		saving = true;
		try {
			prefs = await updateNotificationPrefs({
				telegram_notifications: !prefs.telegram_notifications
			});
		} catch (e) {
			error = e instanceof Error ? e.message : String(e);
		} finally {
			saving = false;
		}
	}

	async function onUnlink() {
		if (!confirm('Отвязать Telegram от аккаунта?')) return;
		saving = true;
		try {
			await unlinkTelegram();
			await reload();
		} catch (e) {
			error = e instanceof Error ? e.message : String(e);
		} finally {
			saving = false;
		}
	}

	// --- Фильтры ---

	// Группировка kinds: одна «галка» в UI = несколько kind'ов в БД.
	const KIND_GROUPS: { value: string; label: string; icon: string; kinds: string[] }[] = [
		{
			value: 'meetings',
			label: 'Встречи и приглашения',
			icon: 'ti-calendar-event',
			kinds: [
				'meeting_proposal',
				'meeting_response',
				'meeting_updated',
				'meeting_cancelled'
			]
		},
		{ value: 'reminders', label: 'Напоминания', icon: 'ti-bell', kinds: ['event_reminder', 'meeting_reminder'] },
		{ value: 'recos', label: 'Рекомендации ИИ', icon: 'ti-sparkles', kinds: ['recommendation'] },
		{ value: 'digest', label: 'Дайджест недели', icon: 'ti-mail', kinds: ['team_digest', 'weekly_summary'] },
		{ value: 'system', label: 'Системные', icon: 'ti-settings', kinds: ['system', 'stale_profile', 'pulse_check_due'] }
	];

	const PRIORITIES: { value: 'low' | 'medium' | 'high'; label: string }[] = [
		{ value: 'low', label: 'Все' },
		{ value: 'medium', label: 'Средний и выше' },
		{ value: 'high', label: 'Только высокий' }
	];

	// Когда юзер тыкает на группу — добавляем/удаляем ВСЕ её kinds сразу.
	async function toggleKind(group: string) {
		if (!prefs) return;
		const g = KIND_GROUPS.find((x) => x.value === group);
		if (!g) return;

		const set = new Set(prefs.notify_kinds);
		const isOn = g.kinds.every((k) => set.has(k));
		if (isOn) {
			g.kinds.forEach((k) => set.delete(k));
		} else {
			g.kinds.forEach((k) => set.add(k));
		}
		const next = Array.from(set);

		saving = true;
		try {
			prefs = await updateNotificationPrefs({ notify_kinds: next });
		} catch (e) {
			error = e instanceof Error ? e.message : String(e);
		} finally {
			saving = false;
		}
	}

	// «Группа включена» = есть хотя бы один её kind в notify_kinds.
	// Если notify_kinds пуст — считаем что все группы выключены (но шлются ВСЕ типы как fallback).
	function isKindGroupOn(
		p: NotificationPrefs,
		g: { kinds: string[] }
	): boolean {
		return g.kinds.some((k) => p.notify_kinds.includes(k));
	}

	async function setPriority(p: 'low' | 'medium' | 'high') {
		if (!prefs) return;
		saving = true;
		try {
			prefs = await updateNotificationPrefs({ notify_min_priority: p });
		} catch (e) {
			error = e instanceof Error ? e.message : String(e);
		} finally {
			saving = false;
		}
	}
</script>

<Card
	title="Каналы уведомлений"
	subtitle="Куда дублировать in-app сообщения о встречах, переносах и подтверждениях"
>
	{#if loading}
		<div class="text-text-3 text-sm" style="padding: 12px;">Загрузка…</div>
	{:else if error}
		<Badge variant="danger"><i class="ti ti-alert-circle"></i>{error}</Badge>
	{:else if prefs}
		<div class="channels">
			<!-- Email -->
			<div class="channel">
				<div class="channel__icon channel__icon--info">
					<i class="ti ti-mail"></i>
				</div>
				<div class="channel__main">
					<div class="channel__title">Email</div>
					<div class="channel__meta">Отправка копии на ваш рабочий e-mail</div>
				</div>
				<label class="switch">
					<input
						type="checkbox"
						checked={prefs.email_notifications}
						onchange={toggleEmail}
						disabled={saving}
					/>
					<span class="switch__slider"></span>
				</label>
			</div>

			<!-- Telegram -->
			<div class="channel">
				<div class="channel__icon channel__icon--purple">
					<i class="ti ti-brand-telegram"></i>
				</div>
				<div class="channel__main">
					<div class="channel__title">Telegram</div>
					<div class="channel__meta">
						{#if tg?.linked}
							Подключено
						{:else if tg?.bot_username}
							Не подключено
						{:else}
							Бот не настроен в системе
						{/if}
					</div>
				</div>

				{#if tg?.linked}
					<div class="channel__actions">
						<label class="switch">
							<input
								type="checkbox"
								checked={prefs.telegram_notifications}
								onchange={toggleTelegram}
								disabled={saving}
							/>
							<span class="switch__slider"></span>
						</label>
						<Button size="sm" variant="ghost" icon="ti-unlink" onclick={onUnlink} disabled={saving}>
							Отвязать
						</Button>
					</div>
				{:else if tg?.deep_link}
					<a
						href={tg.deep_link}
						target="_blank"
						rel="noreferrer"
						class="tg-link"
					>
						<i class="ti ti-brand-telegram"></i>
						Подключить
					</a>
				{:else}
					<Badge variant="neutral">недоступно</Badge>
				{/if}
			</div>
		</div>

		{#if tg && !tg.linked && tg.deep_link}
			<div class="hint">
				Нажмите «Подключить» — откроется бот в Telegram, нажмите там <code>/start</code>.
				После этого вернитесь и обновите страницу.
				<button type="button" class="hint-refresh" onclick={reload}>
					<i class="ti ti-refresh"></i> Проверить
				</button>
			</div>
		{/if}

		<!-- Фильтры: типы + приоритет -->
		<div class="filters">
			<div class="filters__title">Типы уведомлений</div>
			<div class="filters__hint">
				Если ничего не отмечено — приходят все типы. Иначе только выбранные.
			</div>
			<div class="filters__kinds">
				{#each KIND_GROUPS as g (g.value)}
					<label class="kind-pill" class:kind-pill--active={isKindGroupOn(prefs, g)}>
						<input
							type="checkbox"
							checked={isKindGroupOn(prefs, g)}
							onchange={() => toggleKind(g.value)}
							disabled={saving}
						/>
						<i class="ti {g.icon}"></i>
						<span>{g.label}</span>
					</label>
				{/each}
			</div>

			<div class="filters__title" style="margin-top: 16px;">Минимальный приоритет</div>
			<div class="filters__hint">
				Не отправлять уведомления ниже выбранного уровня важности.
			</div>
			<div class="filters__priorities">
				{#each PRIORITIES as p (p.value)}
					<label class="prio-pill" class:prio-pill--active={prefs.notify_min_priority === p.value}>
						<input
							type="radio"
							name="prio"
							value={p.value}
							checked={prefs.notify_min_priority === p.value}
							onchange={() => setPriority(p.value)}
							disabled={saving}
						/>
						<span>{p.label}</span>
					</label>
				{/each}
			</div>
		</div>
	{/if}
</Card>

<style>
	.channels {
		display: flex;
		flex-direction: column;
		gap: 10px;
	}
	.channel {
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 10px 12px;
		border: 1px solid var(--border);
		border-radius: 10px;
		background: var(--surface);
	}
	.channel__icon {
		width: 36px;
		height: 36px;
		border-radius: 10px;
		display: flex;
		align-items: center;
		justify-content: center;
		font-size: 18px;
		flex-shrink: 0;
	}
	.channel__icon--info {
		background: var(--info-bg);
		color: var(--info-strong);
	}
	.channel__icon--purple {
		background: var(--purple-bg, #ede9fe);
		color: var(--purple-strong, #7c3aed);
	}
	.channel__main {
		flex: 1;
		min-width: 0;
	}
	.channel__title {
		font-weight: 600;
		font-size: 14px;
		color: var(--text);
	}
	.channel__meta {
		font-size: 12px;
		color: var(--text-2);
		margin-top: 2px;
	}
	.channel__actions {
		display: flex;
		align-items: center;
		gap: 8px;
	}

	/* iOS-like switch */
	.switch {
		position: relative;
		display: inline-block;
		width: 38px;
		height: 22px;
		flex-shrink: 0;
	}
	.switch input {
		opacity: 0;
		width: 0;
		height: 0;
	}
	.switch__slider {
		position: absolute;
		inset: 0;
		background: var(--surface-2);
		border: 1px solid var(--border);
		border-radius: 22px;
		transition: background 0.15s, border-color 0.15s;
		cursor: pointer;
	}
	.switch__slider::before {
		content: '';
		position: absolute;
		left: 2px;
		top: 2px;
		width: 16px;
		height: 16px;
		background: white;
		border-radius: 50%;
		box-shadow: 0 1px 3px rgba(0, 0, 0, 0.15);
		transition: transform 0.15s;
	}
	.switch input:checked + .switch__slider {
		background: var(--info-strong);
		border-color: var(--info-strong);
	}
	.switch input:checked + .switch__slider::before {
		transform: translateX(16px);
	}
	.switch input:disabled + .switch__slider {
		opacity: 0.6;
		cursor: default;
	}

	.tg-link {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		padding: 7px 12px;
		background: #229ED9;
		color: white;
		border-radius: 8px;
		font-size: 13px;
		font-weight: 600;
		text-decoration: none;
	}
	.tg-link:hover {
		filter: brightness(1.05);
	}

	.hint {
		margin-top: 10px;
		padding: 10px 12px;
		background: var(--info-bg);
		border-radius: 8px;
		font-size: 12px;
		color: var(--text-2);
		display: flex;
		align-items: center;
		gap: 8px;
		flex-wrap: wrap;
	}
	.hint code {
		font-family: 'JetBrains Mono', ui-monospace, monospace;
		background: rgba(0, 0, 0, 0.08);
		padding: 1px 6px;
		border-radius: 4px;
	}
	.hint-refresh {
		margin-left: auto;
		background: transparent;
		border: 1px solid var(--border);
		border-radius: 8px;
		padding: 4px 10px;
		font-size: 12px;
		cursor: pointer;
		color: var(--info-strong);
		display: inline-flex;
		align-items: center;
		gap: 4px;
	}
	.hint-refresh:hover {
		background: var(--surface);
	}

	.filters {
		margin-top: 18px;
		padding-top: 14px;
		border-top: 1px dashed var(--border);
	}
	.filters__title {
		font-size: 12px;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.4px;
		color: var(--text-2);
	}
	.filters__hint {
		font-size: 11px;
		color: var(--text-3);
		margin-top: 4px;
		margin-bottom: 10px;
	}
	.filters__kinds,
	.filters__priorities {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
	}
	.kind-pill,
	.prio-pill {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		padding: 6px 12px;
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: 20px;
		font-size: 12px;
		color: var(--text-2);
		cursor: pointer;
		transition: all 0.12s;
		user-select: none;
	}
	.kind-pill:hover,
	.prio-pill:hover {
		border-color: var(--info-strong);
	}
	.kind-pill input,
	.prio-pill input {
		display: none;
	}
	.kind-pill--active,
	.prio-pill--active {
		background: var(--info-bg);
		border-color: var(--info-strong);
		color: var(--info-strong);
		font-weight: 600;
	}
	.kind-pill i {
		font-size: 14px;
	}
</style>
