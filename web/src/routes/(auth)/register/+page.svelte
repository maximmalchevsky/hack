<script lang="ts">
	import { goto } from '$app/navigation';
	import { register } from '$lib/api/auth';
	import { ApiError } from '$lib/api/client';
	import Button from '$lib/components/Button.svelte';
	import { timezoneOptions } from '$lib/timezones';

	let email = $state('');
	let password = $state('');
	let fullName = $state('');
	let timezone = $state('Europe/Moscow');
	let loading = $state(false);
	let error = $state<string | null>(null);

	const timezones = timezoneOptions();

	async function onSubmit(e: Event) {
		e.preventDefault();
		error = null;
		loading = true;
		try {
			await register({ email, password, full_name: fullName, timezone });
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
		<div class="header__logo-icon header__logo-icon--img" style="width: 128px; height: 128px;">
			<img src="/logo.png" alt="Workie" />
		</div>
		<div class="text-center">
			<div class="text-base font-medium">Регистрация</div>
			<div class="text-text-2 text-xs mt-1">Создай учётную запись сотрудника</div>
		</div>
	</div>

	<form onsubmit={onSubmit} class="space-y-3">
		<div class="field">
			<label class="field__label" for="full_name">ФИО</label>
			<input id="full_name" type="text" required bind:value={fullName} placeholder="Иван Иванов" />
		</div>

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

		<div class="field">
			<label class="field__label" for="tz">Часовой пояс</label>
			<select id="tz" bind:value={timezone}>
				{#each timezones as tz (tz.value)}
					<option value={tz.value}>{tz.label}</option>
				{/each}
			</select>
		</div>

		{#if error}
			<div class="badge badge--danger" style="width: 100%; justify-content: flex-start;">
				<i class="ti ti-alert-circle"></i>
				{error}
			</div>
		{/if}

		<div class="flex gap-2 pt-2">
			<Button variant="primary" type="submit" disabled={loading}>
				{loading ? 'Создаём…' : 'Зарегистрироваться'}
			</Button>
			<a href="/login" class="btn btn--ghost">Уже есть аккаунт</a>
		</div>
	</form>
</div>
