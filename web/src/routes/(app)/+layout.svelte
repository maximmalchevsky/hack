<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { goto } from '$app/navigation';
	import { browser } from '$app/environment';
	import Sidebar from '$lib/components/Sidebar.svelte';
	import Header from '$lib/components/Header.svelte';
	import Toast from '$lib/components/Toast.svelte';
	import { user } from '$lib/stores/user';
	import { api, getAccessToken, clearTokens } from '$lib/api/client';
	import { notifications } from '$lib/stores/notifications';

	let { children } = $props();

	let ready = $state(false);

	// /me возвращает полную форму (user + employee + work_profile + exceptions).
	interface MeApi {
		user: {
			id: string;
			email: string;
			role: UserRole;
			full_name: string;
			timezone: string;
			locale: string;
			avatar_url?: string;
		};
		employee?: { id: string };
	}

	onMount(async () => {
		if (!browser) return;

		const token = getAccessToken();
		if (!token) {
			await goto('/login');
			return;
		}

		try {
			const me = await api.get<MeApi>('/api/v1/me');
			user.set({
				id: me.user.id,
				email: me.user.email,
				role: me.user.role,
				fullName: me.user.full_name,
				timezone: me.user.timezone,
				avatarUrl: me.user.avatar_url
			});
		} catch {
			clearTokens();
			await goto('/login');
			return;
		}

		// Загружаем уведомления и подключаем SSE-стрим.
		await notifications.reload();
		notifications.start();

		// Глобальный хоткей Cmd+N / Ctrl+N — открыть планировщик.
		// Игнорируем когда фокус в input/textarea/contenteditable,
		// чтобы не перехватывать ввод.
		window.addEventListener('keydown', onGlobalKey);

		ready = true;
	});

	function onGlobalKey(e: KeyboardEvent) {
		if (!((e.metaKey || e.ctrlKey) && (e.key === 'n' || e.key === 'N'))) return;
		const target = e.target as HTMLElement | null;
		const tag = target?.tagName ?? '';
		const editable =
			tag === 'INPUT' ||
			tag === 'TEXTAREA' ||
			tag === 'SELECT' ||
			target?.isContentEditable;
		if (editable) return;
		e.preventDefault();
		goto('/scheduler?focus=duration');
	}

	onDestroy(() => {
		if (browser) {
			notifications.stop();
			window.removeEventListener('keydown', onGlobalKey);
		}
	});
</script>

{#if ready}
	<div class="app">
		<Header />
		<Sidebar />
		<main class="main">
			<div class="main-inner">
				{@render children()}
			</div>
		</main>
		<Toast />
	</div>
{:else}
	<div class="flex items-center justify-center min-h-screen text-text-3 text-sm">
		Загрузка…
	</div>
{/if}
