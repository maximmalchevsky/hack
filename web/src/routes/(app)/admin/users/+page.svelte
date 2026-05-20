<script lang="ts">
	import { onMount } from 'svelte';
	import Card from '$lib/components/Card.svelte';
	import Badge from '$lib/components/Badge.svelte';
	import Avatar from '$lib/components/Avatar.svelte';
	import { listAdminUsers, updateUserRole, type AdminUser } from '$lib/api/admin';
	import { ApiError } from '$lib/api/client';

	let users = $state<AdminUser[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let success = $state<string | null>(null);

	const ROLES: { value: UserRole; label: string }[] = [
		{ value: 'admin', label: 'Администратор' },
		{ value: 'employee', label: 'Сотрудник' },
		{ value: 'manager', label: 'Руководитель' },
		{ value: 'hr', label: 'HR' },
		{ value: 'pm', label: 'Проектный менеджер' },
		{ value: 'analyst', label: 'Аналитик' }
	];

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
			success = `Роль "${u.full_name}" → ${role}`;
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
</script>

<div class="page-header">
	<div>
		<h1>Пользователи</h1>
		<div class="page-header__subtitle">Управление учётными записями и ролями</div>
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
