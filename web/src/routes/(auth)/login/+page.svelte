<script lang="ts">
	import { goto } from '$app/navigation';
	import { login } from '$lib/api/auth';
	import { ApiError } from '$lib/api/client';
	import Button from '$lib/components/Button.svelte';

	let email = $state('');
	let password = $state('');
	let loading = $state(false);
	let error = $state<string | null>(null);

	async function onSubmit(e: Event) {
		e.preventDefault();
		error = null;
		loading = true;
		try {
			await login(email, password);
			await goto('/dashboard');
		} catch (err) {
			if (err instanceof ApiError) {
				error = err.message;
			} else {
				error = String(err);
			}
		} finally {
			loading = false;
		}
	}
</script>

<div class="card card--padded">
	<div class="flex flex-col items-center gap-3 mb-6">
		<div class="header__logo-icon" style="width: 40px; height: 40px; font-size: 22px;">
			<i class="ti ti-clock-hour-4"></i>
		</div>
		<div class="text-center">
			<div class="text-base font-medium">Войти в WorkTime Sync</div>
			<div class="text-text-2 text-xs mt-1">Используй корпоративную почту</div>
		</div>
	</div>

	<form onsubmit={onSubmit} class="space-y-3">
		<div class="field">
			<label class="field__label" for="email">Email</label>
			<input
				id="email"
				type="email"
				required
				bind:value={email}
				placeholder="ivan.ivanov@company.com"
			/>
		</div>

		<div class="field">
			<label class="field__label" for="password">Пароль</label>
			<input
				id="password"
				type="password"
				required
				minlength={8}
				bind:value={password}
				placeholder="не менее 8 символов"
			/>
		</div>

		{#if error}
			<div class="badge badge--danger" style="width: 100%; justify-content: flex-start;">
				<i class="ti ti-alert-circle"></i>
				{error}
			</div>
		{/if}

		<div class="flex gap-2 pt-2">
			<Button variant="primary" type="submit" disabled={loading}>
				{loading ? 'Входим…' : 'Войти'}
			</Button>
			<a href="/register" class="btn btn--ghost">Регистрация</a>
		</div>
	</form>
</div>
