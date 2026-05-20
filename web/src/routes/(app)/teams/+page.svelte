<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import Card from '$lib/components/Card.svelte';
	import Badge from '$lib/components/Badge.svelte';
	import Button from '$lib/components/Button.svelte';
	import Avatar from '$lib/components/Avatar.svelte';
	import {
		listTeams,
		listMembers,
		createTeam,
		updateTeam,
		deleteTeam,
		addMember,
		removeMember,
		setTeamManager,
		type Team,
		type TeamMember
	} from '$lib/api/teams';
	import { listEmployees, type EmployeeListRow } from '$lib/api/employees';
	import { ApiError } from '$lib/api/client';
	import { user } from '$lib/stores/user';

	let teams = $state<Team[]>([]);
	let employees = $state<EmployeeListRow[]>([]);
	let selectedTeamID = $state<string | null>(null);
	let members = $state<TeamMember[]>([]);
	let loading = $state(true);
	let busy = $state(false);
	let error = $state<string | null>(null);
	let success = $state<string | null>(null);

	let newTeamName = $state('');
	let addEmpID = $state<string>('');
	let addEmpQuery = $state<string>('');
	let pickerOpen = $state(false);
	let pickerHover = $state(-1);

	const role = $derived($user?.role ?? 'employee');
	const canEdit = $derived(['admin', 'hr', 'pm', 'manager'].includes(role));
	const selectedTeam = $derived(teams.find((t) => t.id === selectedTeamID) ?? null);
	const ownerEmpID = $derived(selectedTeam?.owner_id);
	const availableToAdd = $derived(
		employees.filter((e) => !members.some((m) => m.employee_id === e.employee_id))
	);
	const filteredCandidates = $derived(filterCandidates(availableToAdd, addEmpQuery));

	function filterCandidates(list: EmployeeListRow[], q: string): EmployeeListRow[] {
		const needle = q.trim().toLowerCase();
		if (!needle) return list.slice(0, 8);
		return list
			.filter((e) => {
				return (
					e.full_name.toLowerCase().includes(needle) ||
					(e.email ?? '').toLowerCase().includes(needle) ||
					ruRole(e.role).toLowerCase().includes(needle)
				);
			})
			.slice(0, 8);
	}

	function pickCandidate(e: EmployeeListRow) {
		addEmpID = e.employee_id;
		addEmpQuery = `${e.full_name} · ${e.email}`;
		pickerOpen = false;
		pickerHover = -1;
	}

	function onPickerKey(ev: KeyboardEvent) {
		if (ev.key === 'ArrowDown') {
			ev.preventDefault();
			pickerHover = Math.min(pickerHover + 1, filteredCandidates.length - 1);
			pickerOpen = true;
		} else if (ev.key === 'ArrowUp') {
			ev.preventDefault();
			pickerHover = Math.max(pickerHover - 1, 0);
		} else if (ev.key === 'Enter') {
			ev.preventDefault();
			const cand = filteredCandidates[pickerHover] ?? filteredCandidates[0];
			if (cand) pickCandidate(cand);
		} else if (ev.key === 'Escape') {
			pickerOpen = false;
			pickerHover = -1;
		} else {
			// При вводе сбрасываем выбранный id — пока пользователь не выберет заново.
			addEmpID = '';
			pickerOpen = true;
			pickerHover = -1;
		}
	}

	// Закрытие пикера по клику вне поля.
	function onDocClick(ev: MouseEvent) {
		if (!pickerOpen) return;
		const target = ev.target as HTMLElement;
		if (!target.closest('.field') && !target.closest('.picker')) {
			pickerOpen = false;
			pickerHover = -1;
		}
	}

	onMount(async () => {
		await Promise.all([loadTeams(), loadEmployees()]);
		loading = false;
		if (typeof document !== 'undefined') {
			document.addEventListener('click', onDocClick);
		}
	});

	onDestroy(() => {
		if (typeof document !== 'undefined') {
			document.removeEventListener('click', onDocClick);
		}
	});

	async function loadTeams() {
		try {
			const r = await listTeams();
			teams = r.teams ?? [];
			if (!selectedTeamID && teams.length > 0) {
				await select(teams[0].id);
			}
		} catch (e) {
			error = errStr(e);
		}
	}

	async function loadEmployees() {
		try {
			const r = await listEmployees();
			employees = r.employees ?? [];
		} catch (e) {
			error = errStr(e);
		}
	}

	async function select(id: string) {
		selectedTeamID = id;
		members = [];
		try {
			const r = await listMembers(id);
			members = r.members ?? [];
		} catch (e) {
			error = errStr(e);
		}
	}

	async function onCreate() {
		if (!newTeamName.trim()) return;
		busy = true;
		error = success = null;
		try {
			const t = await createTeam({ name: newTeamName.trim() });
			teams = [...teams, t].sort((a, b) => a.name.localeCompare(b.name));
			newTeamName = '';
			await select(t.id);
			success = 'Команда создана';
		} catch (e) {
			error = errStr(e);
		} finally {
			busy = false;
		}
	}

	async function onRename() {
		if (!selectedTeam) return;
		const name = prompt('Новое название команды', selectedTeam.name);
		if (!name || name === selectedTeam.name) return;
		busy = true;
		error = success = null;
		try {
			const t = await updateTeam(selectedTeam.id, { name });
			teams = teams.map((x) => (x.id === t.id ? t : x));
			success = 'Переименовано';
		} catch (e) {
			error = errStr(e);
		} finally {
			busy = false;
		}
	}

	async function onDelete() {
		if (!selectedTeam) return;
		if (!confirm(`Удалить команду «${selectedTeam.name}»? Состав будет очищен.`)) return;
		busy = true;
		error = success = null;
		try {
			await deleteTeam(selectedTeam.id);
			teams = teams.filter((t) => t.id !== selectedTeam!.id);
			selectedTeamID = teams[0]?.id ?? null;
			members = [];
			if (selectedTeamID) await select(selectedTeamID);
			success = 'Команда удалена';
		} catch (e) {
			error = errStr(e);
		} finally {
			busy = false;
		}
	}

	async function onAddMember() {
		if (!selectedTeamID || !addEmpID) return;
		busy = true;
		error = success = null;
		try {
			await addMember(selectedTeamID, addEmpID);
			await select(selectedTeamID);
			addEmpID = '';
			addEmpQuery = '';
			pickerOpen = false;
			success = 'Участник добавлен';
		} catch (e) {
			error = errStr(e);
		} finally {
			busy = false;
		}
	}

	async function onRemoveMember(empID: string) {
		if (!selectedTeamID) return;
		busy = true;
		error = success = null;
		try {
			await removeMember(selectedTeamID, empID);
			await select(selectedTeamID);
			success = 'Участник убран';
		} catch (e) {
			error = errStr(e);
		} finally {
			busy = false;
		}
	}

	async function onSetManager(empID: string) {
		if (!selectedTeamID) return;
		busy = true;
		error = success = null;
		try {
			await setTeamManager(selectedTeamID, empID);
			// Перечитать команду чтобы owner_id обновился.
			await loadTeams();
			success = 'Руководитель назначен';
		} catch (e) {
			error = errStr(e);
		} finally {
			busy = false;
		}
	}

	function errStr(e: unknown): string {
		return e instanceof ApiError ? e.message : String(e);
	}

	function initials(name: string): string {
		const parts = name.trim().split(/\s+/);
		return ((parts[0]?.[0] ?? '') + (parts[1]?.[0] ?? '')).toUpperCase() || '??';
	}

	function ruRole(r: string): string {
		return (
			{
				admin: 'администратор',
				employee: 'сотрудник',
				manager: 'руководитель',
				hr: 'HR',
				pm: 'проектный менеджер',
				analyst: 'аналитик'
			}[r] ?? r
		);
	}
</script>

<div class="page-header">
	<div>
		<h1>Команды</h1>
		<div class="page-header__subtitle">
			Состав, руководители, владельцы. Используется в /scheduler, /team-map и для рекомендаций.
		</div>
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
{:else}
	<div class="grid-2-1" style="gap: 16px;">
		<!-- Левая колонка: список команд -->
		<Card title="Все команды">
			<div class="space-y-1">
				{#each teams as t (t.id)}
					<button
						class="nav-item"
						class:active={t.id === selectedTeamID}
						style="width: 100%; text-align: left; cursor: pointer; border: none; background: {t.id ===
						selectedTeamID
							? 'var(--surface-2)'
							: 'transparent'};"
						onclick={() => select(t.id)}
					>
						<i class="ti ti-users"></i>
						{t.name}
					</button>
				{/each}
				{#if teams.length === 0}
					<div class="text-text-3 text-sm">Команд пока нет</div>
				{/if}
			</div>

			{#if canEdit}
				<div
					style="margin-top: 12px; padding-top: 12px; border-top: 0.5px solid var(--border); display: flex; gap: 6px;"
				>
					<input
						type="text"
						placeholder="Новая команда"
						bind:value={newTeamName}
						style="flex: 1;"
						disabled={busy}
					/>
					<Button
						size="sm"
						variant="primary"
						icon="ti-plus"
						onclick={onCreate}
						disabled={busy || !newTeamName.trim()}>Создать</Button
					>
				</div>
			{/if}
		</Card>

		<!-- Правая колонка: детали команды -->
		{#if selectedTeam}
			<div>
				<Card>
					<div class="flex items-center justify-between" style="margin-bottom: 12px;">
						<div>
							<div class="card__title" style="font-size: 18px;">{selectedTeam.name}</div>
							<div class="card__caption">{members.length} участник(а/ов)</div>
						</div>
						{#if canEdit}
							<div class="flex gap-2">
								<Button size="sm" icon="ti-edit" onclick={onRename} disabled={busy}>
									Переименовать
								</Button>
								<Button
									size="sm"
									variant="danger"
									icon="ti-trash"
									onclick={onDelete}
									disabled={busy}>Удалить</Button
								>
							</div>
						{/if}
					</div>

					{#if canEdit && availableToAdd.length > 0}
						<div class="flex gap-2" style="margin-bottom: 14px; align-items: end;">
							<div class="field" style="margin-bottom: 0; flex: 1; position: relative;">
								<label class="field__label" for="addEmpInput">Добавить участника</label>
								<input
									id="addEmpInput"
									type="text"
									placeholder="Поиск по имени или email"
									bind:value={addEmpQuery}
									onfocus={() => (pickerOpen = true)}
									onkeydown={onPickerKey}
									autocomplete="off"
								/>
								{#if pickerOpen && filteredCandidates.length > 0}
									<div
										class="picker"
										role="listbox"
										onmouseleave={() => (pickerHover = -1)}
									>
										{#each filteredCandidates as e, i (e.employee_id)}
											<div
												role="option"
												aria-selected={pickerHover === i || addEmpID === e.employee_id}
												class="picker__item"
												class:picker__item--active={pickerHover === i}
												class:picker__item--chosen={addEmpID === e.employee_id}
												onmouseenter={() => (pickerHover = i)}
												onclick={() => pickCandidate(e)}
												onkeydown={(ev) => ev.key === 'Enter' && pickCandidate(e)}
												tabindex="0"
											>
												<div class="picker__name">{e.full_name}</div>
												<div class="picker__sub">
													{e.email} · {ruRole(e.role)}
												</div>
											</div>
										{/each}
									</div>
								{:else if pickerOpen && addEmpQuery.trim()}
									<div class="picker picker--empty">Никого не нашлось</div>
								{/if}
							</div>
							<Button
								variant="primary"
								icon="ti-user-plus"
								onclick={onAddMember}
								disabled={busy || !addEmpID}>Добавить</Button
							>
						</div>
					{/if}

					<div class="space-y-2">
						{#each members as m (m.employee_id)}
							<div
								class="flex items-center gap-3 p-2"
								style="border: 0.5px solid var(--border); border-radius: var(--radius-md);"
							>
								<a href="/employees/{m.employee_id}" style="display: contents;">
									<Avatar initials={initials(m.full_name)} size="sm" variant="purple" />
								</a>
								<div class="flex-1">
									<div class="card__title">
										<a href="/employees/{m.employee_id}" class="emp-link">{m.full_name}</a>
										{#if ownerEmpID === m.employee_id}
											<Badge variant="info">руководитель</Badge>
										{/if}
									</div>
									<div class="text-text-3 text-xs">
										{ruRole(m.role)}
										{#if m.timezone} · {m.timezone}{/if}
									</div>
								</div>
								{#if canEdit}
									<div class="flex gap-1">
										{#if ownerEmpID !== m.employee_id}
											<Button
												size="xs"
												icon="ti-crown"
												onclick={() => onSetManager(m.employee_id)}
												disabled={busy}>Назначить рук.</Button
											>
										{/if}
										<Button
											size="xs"
											variant="ghost"
											icon="ti-user-minus"
											onclick={() => onRemoveMember(m.employee_id)}
											disabled={busy}>Убрать</Button
										>
									</div>
								{/if}
							</div>
						{/each}
						{#if members.length === 0}
							<div class="text-text-3 text-sm">В команде пока никого нет</div>
						{/if}
					</div>
				</Card>
			</div>
		{:else}
			<Card>
				<div class="text-text-3 text-sm" style="padding: 24px; text-align: center;">
					Выберите команду слева или создайте новую
				</div>
			</Card>
		{/if}
	</div>
{/if}

<style>
	.picker {
		position: absolute;
		top: calc(100% + 4px);
		left: 0;
		right: 0;
		max-height: 280px;
		overflow-y: auto;
		background: var(--surface);
		border: 0.5px solid var(--border-2);
		border-radius: var(--radius-md);
		box-shadow: 0 6px 18px rgba(0, 0, 0, 0.12);
		z-index: 50;
		padding: 4px;
	}
	.picker--empty {
		padding: 12px;
		font-size: 12px;
		color: var(--text-3);
		text-align: center;
	}
	.picker__item {
		padding: 8px 10px;
		border-radius: 6px;
		cursor: pointer;
		outline: none;
	}
	.picker__item:hover,
	.picker__item--active {
		background: var(--surface-2);
	}
	.picker__item--chosen {
		background: var(--info-bg);
	}
	.picker__name {
		font-size: 13px;
		font-weight: 500;
		color: var(--text);
	}
	.picker__sub {
		font-size: 11px;
		color: var(--text-3);
		margin-top: 1px;
	}
</style>
