<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { goto } from '$app/navigation';
	import Avatar from './Avatar.svelte';
	import { theme } from '$lib/stores/theme';
	import { user } from '$lib/stores/user';
	import { notifications } from '$lib/stores/notifications';
	import { logout } from '$lib/api/auth';
	import { listIncomingInvites } from '$lib/api/meetings';

	function initials(name?: string): string {
		if (!name) return 'ЯЯ';
		const parts = name.trim().split(/\s+/);
		const a = parts[0]?.[0] ?? '';
		const b = parts[1]?.[0] ?? '';
		return (a + b).toUpperCase() || 'ЯЯ';
	}

	function ruRole(r?: string): string {
		switch (r) {
			case 'admin':
				return 'Администратор';
			case 'employee':
				return 'Сотрудник';
			case 'manager':
				return 'Руководитель';
			case 'hr':
				return 'HR';
			case 'pm':
				return 'Проектный менеджер';
			case 'analyst':
				return 'Аналитик';
			default:
				return r ?? '';
		}
	}

	let menuOpen = $state(false);

	// Счётчик pending-приглашений — sticky-бейдж справа от колокольчика.
	let pendingInvites = $state(0);
	let invitesTimer: ReturnType<typeof setInterval> | null = null;

	async function refreshInvites() {
		try {
			const r = await listIncomingInvites();
			pendingInvites = (r.invites ?? []).filter((i) => i.status === 'pending').length;
		} catch {
			// silent
		}
	}

	function toggleMenu(e: MouseEvent) {
		e.stopPropagation();
		menuOpen = !menuOpen;
	}

	function go(path: string) {
		menuOpen = false;
		goto(path);
	}

	async function doLogout() {
		menuOpen = false;
		logout();
		await goto('/login');
	}

	function onDocClick(ev: MouseEvent) {
		if (!menuOpen) return;
		const target = ev.target as HTMLElement;
		if (!target.closest('.user-menu') && !target.closest('.user-btn')) {
			menuOpen = false;
		}
	}

	onMount(() => {
		if (typeof document !== 'undefined') {
			document.addEventListener('click', onDocClick);
		}
		void refreshInvites();
		invitesTimer = setInterval(refreshInvites, 60_000);
	});
	onDestroy(() => {
		if (typeof document !== 'undefined') {
			document.removeEventListener('click', onDocClick);
		}
		if (invitesTimer) clearInterval(invitesTimer);
	});
</script>

<header class="header">
	<div class="header__logo">
		<div class="header__logo-icon">
			<i class="ti ti-clock-hour-4"></i>
		</div>
		<div class="header__logo-text">WorkTime Sync</div>
	</div>

	<div class="search">
		<i class="ti ti-search"></i>
		<input type="text" placeholder="Найти сотрудника, команду..." />
		<kbd>⌘K</kbd>
	</div>

	<div class="header__spacer"></div>

	<div class="header__actions">
		<button class="icon-btn" onclick={() => theme.toggle()} aria-label="Переключить тему">
			<i class="ti ti-sun-moon"></i>
		</button>

		{#if pendingInvites > 0}
			<a
				href="/scheduler"
				class="invites-pill"
				aria-label="Ждут вашего ответа"
				title="Приглашения на встречи, ждущие ответа"
			>
				<i class="ti ti-mail-question"></i>
				<span class="invites-pill__text">
					{pendingInvites}
					{#if pendingInvites === 1}
						приглашение
					{:else if pendingInvites < 5}
						приглашения
					{:else}
						приглашений
					{/if}
				</span>
			</a>
		{/if}

		<a
			href="/notifications"
			class="icon-btn"
			class:has-badge={$notifications.unread > 0}
			aria-label="Уведомления"
			title={$notifications.connected ? 'Подключение активно' : 'SSE отключён'}
		>
			<i class="ti ti-bell"></i>
		</a>

		<div class="user-wrap">
			<button class="icon-btn user-btn" onclick={toggleMenu} aria-label="Профиль">
				<Avatar initials={initials($user?.fullName)} size="sm" variant="purple" />
			</button>

			{#if menuOpen}
				<div class="user-menu" role="menu">
					<div class="user-menu__head">
						<div class="user-menu__name">{$user?.fullName ?? '—'}</div>
						<div class="user-menu__email">{$user?.email ?? ''}</div>
						{#if $user?.role}
							<div class="user-menu__role">{ruRole($user.role)}</div>
						{/if}
					</div>

					<div class="user-menu__divider"></div>

					<button class="user-menu__item" onclick={() => go('/profile')} role="menuitem">
						<i class="ti ti-user"></i> Мой профиль
					</button>
					<button class="user-menu__item" onclick={() => go('/integrations')} role="menuitem">
						<i class="ti ti-plug"></i> Интеграции
					</button>
					<button class="user-menu__item" onclick={() => go('/notifications')} role="menuitem">
						<i class="ti ti-bell"></i> Уведомления
					</button>

					<div class="user-menu__divider"></div>

					<button class="user-menu__item user-menu__item--danger" onclick={doLogout} role="menuitem">
						<i class="ti ti-logout"></i> Выйти
					</button>
				</div>
			{/if}
		</div>
	</div>
</header>

<style>
	.invites-pill {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		padding: 6px 12px;
		border-radius: 999px;
		background: var(--warning-bg);
		color: var(--warning-strong);
		font-size: 12px;
		font-weight: 600;
		text-decoration: none;
		border: 1px solid var(--warning-strong);
		transition: filter 0.12s;
		animation: pulse 2.2s ease-in-out infinite;
	}
	.invites-pill:hover {
		filter: brightness(0.95);
	}
	.invites-pill i {
		font-size: 14px;
	}
	.invites-pill__text {
		white-space: nowrap;
	}
	@keyframes pulse {
		0%, 100% { box-shadow: 0 0 0 0 rgba(245, 158, 11, 0.0); }
		50%      { box-shadow: 0 0 0 4px rgba(245, 158, 11, 0.15); }
	}

	.user-wrap {
		position: relative;
	}
	.user-menu {
		position: absolute;
		top: calc(100% + 6px);
		right: 0;
		min-width: 240px;
		background: var(--surface);
		border: 0.5px solid var(--border-2);
		border-radius: var(--radius-md);
		box-shadow: 0 8px 24px rgba(0, 0, 0, 0.12);
		padding: 6px;
		z-index: 100;
	}
	.user-menu__head {
		padding: 8px 10px 10px;
	}
	.user-menu__name {
		font-size: 13px;
		font-weight: 600;
		color: var(--text);
	}
	.user-menu__email {
		font-size: 11px;
		color: var(--text-3);
		margin-top: 2px;
		word-break: break-all;
	}
	.user-menu__role {
		font-size: 11px;
		color: var(--text-2);
		margin-top: 4px;
	}
	.user-menu__divider {
		height: 0.5px;
		background: var(--border);
		margin: 4px 0;
	}
	.user-menu__item {
		display: flex;
		align-items: center;
		gap: 8px;
		width: 100%;
		padding: 8px 10px;
		font-size: 13px;
		font-family: inherit;
		color: var(--text);
		background: transparent;
		border: none;
		border-radius: 6px;
		cursor: pointer;
		text-align: left;
	}
	.user-menu__item i {
		font-size: 15px;
		color: var(--text-3);
	}
	.user-menu__item:hover {
		background: var(--surface-2);
	}
	.user-menu__item--danger {
		color: var(--danger-strong);
	}
	.user-menu__item--danger i {
		color: var(--danger-strong);
	}
	.user-menu__item--danger:hover {
		background: var(--danger-bg);
	}
</style>
