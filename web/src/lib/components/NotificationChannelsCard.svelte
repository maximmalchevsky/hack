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
</style>
