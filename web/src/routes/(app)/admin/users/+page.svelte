<script lang="ts">
	import { onMount } from 'svelte';
	import Card from '$lib/components/Card.svelte';
	import Badge from '$lib/components/Badge.svelte';
	import Avatar from '$lib/components/Avatar.svelte';
	import Button from '$lib/components/Button.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import {
		listAdminUsers,
		updateUserRole,
		importUsersCSV,
		type AdminUser,
		type ImportResult
	} from '$lib/api/admin';
	import { ApiError } from '$lib/api/client';
	import { ROLES, roleLabel, type RoleSlug } from '$lib/roles';

	let users = $state<AdminUser[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let success = $state<string | null>(null);

	// Локальный alias на тип роли — чтобы старая сигнатура changeRole(UserRole) не сломалась.
	type UserRole = RoleSlug;

	onMount(async () => {
		try {
			const r = await listAdminUsers();
			users = r.users ?? [];
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		} finally {
			loading = false;
		}
	});

	async function changeRole(u: AdminUser, role: UserRole) {
		try {
			await updateUserRole(u.id, role);
			users = users.map((x) => (x.id === u.id ? { ...x, role } : x));
			success = `Роль "${u.full_name}" → ${roleLabel(role)}`;
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		}
	}

	function initials(name: string): string {
		const parts = name.trim().split(/\s+/);
		return ((parts[0]?.[0] ?? '') + (parts[1]?.[0] ?? '')).toUpperCase() || '??';
	}

	function fmtDate(iso: string): string {
		return new Date(iso).toLocaleDateString('ru', {
			day: 'numeric',
			month: 'short',
			year: 'numeric'
		});
	}

	// --- CSV-импорт ---

	let importOpen = $state(false);
	let importing = $state(false);
	let importResult = $state<ImportResult | null>(null);
	let importError = $state<string | null>(null);
	let csvText = $state('');

	const CSV_TEMPLATE = `email,full_name,department,position,timezone,hire_date,manager_email
ivan@example.com,Иван Иванов,Platform,Backend Engineer,Europe/Moscow,2024-01-15,
maria@example.com,Мария Петрова,Product,PM,Europe/Moscow,2023-06-01,ivan@example.com`;

	function openImport() {
		importOpen = true;
		importResult = null;
		importError = null;
		csvText = '';
	}

	function loadTemplate() {
		csvText = CSV_TEMPLATE;
	}

	async function onFileChange(e: Event) {
		const input = e.target as HTMLInputElement;
		const file = input.files?.[0];
		if (!file) return;
		csvText = await file.text();
	}

	async function runImport() {
		if (!csvText.trim()) {
			importError = 'Пусто';
			return;
		}
		importing = true;
		importError = null;
		try {
			importResult = await importUsersCSV(csvText);
			// перезагружаем список юзеров
			const r = await listAdminUsers();
			users = r.users ?? [];
		} catch (e) {
			importError = e instanceof Error ? e.message : String(e);
		} finally {
			importing = false;
		}
	}

	function downloadPasswords() {
		if (!importResult || importResult.created.length === 0) return;
		const rows = ['email,full_name,password'];
		for (const u of importResult.created) {
			rows.push(`${u.email},"${u.full_name}",${u.password}`);
		}
		const blob = new Blob([rows.join('\n')], { type: 'text/csv;charset=utf-8' });
		const url = URL.createObjectURL(blob);
		const a = document.createElement('a');
		a.href = url;
		a.download = `imported-passwords-${new Date().toISOString().slice(0, 10)}.csv`;
		a.click();
		URL.revokeObjectURL(url);
	}
</script>

<div class="page-header">
	<div>
		<h1>Пользователи</h1>
		<div class="page-header__subtitle">Управление учётными записями и ролями</div>
	</div>
	<div>
		<Button icon="ti-file-import" onclick={openImport}>Импорт CSV</Button>
	</div>
</div>

{#if error}
	<div class="section">
		<Badge variant="danger"><i class="ti ti-alert-circle"></i>{error}</Badge>
	</div>
{/if}
{#if success}
	<div class="section">
		<Badge variant="success"><i class="ti ti-check"></i>{success}</Badge>
	</div>
{/if}

{#if loading}
	<div class="text-text-3 text-sm">Загрузка…</div>
{:else if users.length === 0}
	<Card>
		<div class="text-text-3 text-sm" style="padding: 16px;">Пусто</div>
	</Card>
{:else}
	<Card>
		<div class="space-y-2">
			{#each users as u (u.id)}
				<div
					class="flex items-center gap-3 p-2"
					style="border: 0.5px solid var(--border); border-radius: var(--radius-md);"
				>
					{#if u.employee_id}
						<a href="/employees/{u.employee_id}" style="display: contents;">
							<Avatar initials={initials(u.full_name)} size="md" variant="purple" />
						</a>
					{:else}
						<Avatar initials={initials(u.full_name)} size="md" variant="purple" />
					{/if}
					<div class="flex-1">
						{#if u.employee_id}
							<a href="/employees/{u.employee_id}" class="emp-link">
								<div class="card__title">{u.full_name}</div>
							</a>
						{:else}
							<div class="card__title">{u.full_name}</div>
						{/if}
						<div class="text-text-3 text-xs">{u.email} · {u.timezone} · с {fmtDate(u.created_at)}</div>
					</div>
					<select
						value={u.role}
						onchange={(e) => changeRole(u, (e.target as HTMLSelectElement).value as UserRole)}
						style="width: 180px;"
					>
						{#each ROLES as r (r.value)}
							<option value={r.value}>{r.label}</option>
						{/each}
					</select>
				</div>
			{/each}
		</div>
	</Card>
{/if}

<Modal open={importOpen} title="Импорт сотрудников из CSV" size="lg" onClose={() => (importOpen = false)}>
	<div class="imp">
		{#if !importResult}
			<div class="imp__hint">
				CSV-файл с шапкой. Обязательные колонки: <code>email</code>, <code>full_name</code>.
				Опциональные: <code>department</code>, <code>position</code>, <code>timezone</code>,
				<code>hire_date</code> (YYYY-MM-DD или DD.MM.YYYY), <code>manager_email</code>.
				Дубликаты по email — пропускаются.
			</div>
			<div class="imp__actions-top">
				<input type="file" accept=".csv,text/csv" onchange={onFileChange} />
				<button type="button" class="imp__link" onclick={loadTemplate}>
					<i class="ti ti-file-text"></i> Загрузить шаблон
				</button>
			</div>
			<textarea
				bind:value={csvText}
				rows="10"
				placeholder="email,full_name,department,...&#10;ivan@example.com,Иван Иванов,Platform,..."
				class="imp__textarea"
			></textarea>
			{#if importError}
				<Badge variant="danger"><i class="ti ti-alert-circle"></i>{importError}</Badge>
			{/if}
		{:else}
			<div class="imp__result">
				<div class="imp__stat imp__stat--ok">
					<i class="ti ti-circle-check"></i>
					Создано: <strong>{importResult.created.length}</strong>
				</div>
				<div class="imp__stat imp__stat--warn">
					<i class="ti ti-info-circle"></i>
					Пропущено: <strong>{importResult.skipped.length}</strong>
				</div>
				<div class="imp__stat imp__stat--err">
					<i class="ti ti-alert-circle"></i>
					Ошибок: <strong>{importResult.errors.length}</strong>
				</div>
			</div>

			{#if importResult.created.length > 0}
				<div class="imp__section">
					<div class="imp__section-title">Созданные аккаунты</div>
					<div class="imp__rows">
						{#each importResult.created as r (r.email)}
							<div class="imp__row">
								<span class="imp__row-name">{r.full_name}</span>
								<span class="imp__row-email">{r.email}</span>
								<code class="imp__row-pass">{r.password}</code>
							</div>
						{/each}
					</div>
					<button type="button" class="imp__link" onclick={downloadPasswords}>
						<i class="ti ti-download"></i> Скачать пароли в CSV
					</button>
				</div>
			{/if}

			{#if importResult.skipped.length > 0}
				<div class="imp__section">
					<div class="imp__section-title">Пропущенные (дубли)</div>
					{#each importResult.skipped as r (r.row)}
						<div class="imp__skip">строка {r.row} · {r.email} · {r.reason}</div>
					{/each}
				</div>
			{/if}

			{#if importResult.errors.length > 0}
				<div class="imp__section">
					<div class="imp__section-title">Ошибки</div>
					{#each importResult.errors as r (r.row + r.msg)}
						<div class="imp__err">строка {r.row}: {r.msg}</div>
					{/each}
				</div>
			{/if}
		{/if}
	</div>

	{#snippet footer()}
		{#if !importResult}
			<Button variant="ghost" onclick={() => (importOpen = false)}>Отмена</Button>
			<Button onclick={runImport} disabled={importing || !csvText.trim()}>
				{importing ? 'Импортирую…' : 'Импортировать'}
			</Button>
		{:else}
			<Button onclick={() => (importOpen = false)}>Готово</Button>
		{/if}
	{/snippet}
</Modal>

<style>
	.imp {
		display: flex;
		flex-direction: column;
		gap: 12px;
	}
	.imp__hint {
		font-size: 12px;
		color: var(--text-2);
		line-height: 1.5;
		background: var(--info-bg);
		padding: 10px 12px;
		border-radius: 8px;
	}
	.imp__hint code {
		font-family: 'JetBrains Mono', ui-monospace, monospace;
		font-size: 11px;
		background: rgba(0, 0, 0, 0.08);
		padding: 1px 5px;
		border-radius: 3px;
	}
	.imp__actions-top {
		display: flex;
		align-items: center;
		gap: 12px;
	}
	.imp__textarea {
		width: 100%;
		font-family: 'JetBrains Mono', ui-monospace, monospace;
		font-size: 12px;
		padding: 10px 12px;
		border: 1px solid var(--border);
		border-radius: 8px;
		background: var(--surface);
		color: var(--text);
		resize: vertical;
		min-height: 200px;
	}
	.imp__link {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		background: transparent;
		border: none;
		color: var(--info-strong);
		font-size: 12px;
		cursor: pointer;
		padding: 0;
	}
	.imp__link:hover {
		text-decoration: underline;
	}
	.imp__result {
		display: grid;
		grid-template-columns: repeat(3, 1fr);
		gap: 10px;
	}
	.imp__stat {
		display: flex;
		align-items: center;
		gap: 6px;
		padding: 10px 12px;
		border-radius: 8px;
		font-size: 13px;
	}
	.imp__stat--ok {
		background: var(--success-bg);
		color: var(--success-strong);
	}
	.imp__stat--warn {
		background: var(--warning-bg);
		color: var(--warning-strong);
	}
	.imp__stat--err {
		background: var(--danger-bg);
		color: var(--danger-strong);
	}
	.imp__section {
		display: flex;
		flex-direction: column;
		gap: 6px;
		margin-top: 4px;
	}
	.imp__section-title {
		font-size: 12px;
		font-weight: 600;
		color: var(--text-2);
		text-transform: uppercase;
		letter-spacing: 0.4px;
	}
	.imp__rows {
		display: flex;
		flex-direction: column;
		gap: 4px;
		max-height: 220px;
		overflow-y: auto;
	}
	.imp__row {
		display: grid;
		grid-template-columns: 1fr 1fr auto;
		gap: 10px;
		font-size: 12px;
		padding: 4px 8px;
		border-radius: 6px;
		background: var(--surface);
	}
	.imp__row-name {
		font-weight: 500;
	}
	.imp__row-email {
		color: var(--text-3);
	}
	.imp__row-pass {
		font-family: 'JetBrains Mono', ui-monospace, monospace;
		font-size: 11px;
		background: rgba(0, 0, 0, 0.05);
		padding: 1px 6px;
		border-radius: 3px;
	}
	.imp__skip,
	.imp__err {
		font-size: 12px;
		padding: 4px 8px;
		border-radius: 4px;
		font-family: 'JetBrains Mono', ui-monospace, monospace;
	}
	.imp__skip {
		color: var(--warning-strong);
		background: var(--warning-bg);
	}
	.imp__err {
		color: var(--danger-strong);
		background: var(--danger-bg);
	}
</style>
