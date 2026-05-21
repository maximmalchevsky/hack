<script lang="ts">
	import { onMount } from 'svelte';
	import Button from '$lib/components/Button.svelte';
	import Card from '$lib/components/Card.svelte';
	import Badge from '$lib/components/Badge.svelte';
	import Avatar from '$lib/components/Avatar.svelte';
	import ProfileHistory from '$lib/components/ProfileHistory.svelte';
	import NotificationChannelsCard from '$lib/components/NotificationChannelsCard.svelte';
	import TimeBreakdownCard from '$lib/components/TimeBreakdownCard.svelte';
	import {
		me,
		updateMyProfile,
		confirmMyProfile,
		listExceptions,
		createException,
		deleteException,
		type MeResponse,
		type DaysOfWeek,
		type WorkFormat,
		type TimeException
	} from '$lib/api/profile';
	import { ApiError } from '$lib/api/client';
	import { timezoneOptions } from '$lib/timezones';

	const DAYS: { key: keyof DaysOfWeek; label: string }[] = [
		{ key: 'mon', label: 'Пн' },
		{ key: 'tue', label: 'Вт' },
		{ key: 'wed', label: 'Ср' },
		{ key: 'thu', label: 'Чт' },
		{ key: 'fri', label: 'Пт' },
		{ key: 'sat', label: 'Сб' },
		{ key: 'sun', label: 'Вс' }
	];

	const TIMEZONES = timezoneOptions();

	const FORMATS: { key: WorkFormat; label: string }[] = [
		{ key: 'office', label: 'Офис' },
		{ key: 'remote', label: 'Удалённо' },
		{ key: 'hybrid', label: 'Гибрид' }
	];

	const EXC_KINDS: { key: TimeException['kind']; label: string; variant: string }[] = [
		{ key: 'vacation', label: 'Отпуск', variant: 'success' },
		{ key: 'sick_leave', label: 'Больничный', variant: 'warning' },
		{ key: 'business_trip', label: 'Командировка', variant: 'info' },
		{ key: 'personal_hours', label: 'Личные часы', variant: 'purple' },
		{ key: 'custom', label: 'Другое', variant: 'neutral' }
	];

	let meData = $state<MeResponse | null>(null);
	let exceptions = $state<TimeException[]>([]);
	let loading = $state(true);
	let saving = $state(false);
	let error = $state<string | null>(null);
	let success = $state<string | null>(null);

	// Редактируемые поля
	let days = $state<Record<keyof DaysOfWeek, { enabled: boolean; start: string; end: string }>>({
		mon: { enabled: true, start: '09:00', end: '18:00' },
		tue: { enabled: true, start: '09:00', end: '18:00' },
		wed: { enabled: true, start: '09:00', end: '18:00' },
		thu: { enabled: true, start: '09:00', end: '18:00' },
		fri: { enabled: true, start: '09:00', end: '18:00' },
		sat: { enabled: false, start: '09:00', end: '18:00' },
		sun: { enabled: false, start: '09:00', end: '18:00' }
	});
	let timezone = $state('Europe/Moscow');
	let workFormat = $state<WorkFormat>('office');

	// Форма исключения
	let excKind = $state<TimeException['kind']>('vacation');
	let excStart = $state('');
	let excEnd = $state('');
	let excComment = $state('');

	onMount(async () => {
		await load();
	});

	async function load() {
		loading = true;
		error = null;
		try {
			meData = await me();
			if (meData.work_profile) {
				const wp = meData.work_profile;
				for (const d of DAYS) {
					const cur = wp.days_of_week[d.key];
					days[d.key] = cur
						? { enabled: true, start: cur.start, end: cur.end }
						: { enabled: false, start: '09:00', end: '18:00' };
				}
				timezone = wp.timezone;
				workFormat = wp.work_format;
			}
			const r = await listExceptions();
			exceptions = r.exceptions ?? [];
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		} finally {
			loading = false;
		}
	}

	function buildDays(): DaysOfWeek {
		const out: DaysOfWeek = {};
		for (const d of DAYS) {
			const v = days[d.key];
			if (v.enabled) {
				out[d.key] = { start: v.start, end: v.end };
			}
		}
		return out;
	}

	async function save() {
		saving = true;
		error = null;
		success = null;
		try {
			await updateMyProfile({
				days_of_week: buildDays(),
				timezone,
				work_format: workFormat
			});
			success = 'Профиль обновлён';
			await load();
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		} finally {
			saving = false;
		}
	}

	async function confirm() {
		try {
			await confirmMyProfile();
			success = 'Профиль подтверждён';
			await load();
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		}
	}

	async function addException() {
		if (!excStart || !excEnd) return;
		error = null;
		try {
			await createException({
				kind: excKind,
				start_at: new Date(excStart).toISOString(),
				end_at: new Date(excEnd).toISOString(),
				comment: excComment || undefined
			});
			excStart = '';
			excEnd = '';
			excComment = '';
			const r = await listExceptions();
			exceptions = r.exceptions ?? [];
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		}
	}

	async function removeException(id: string) {
		try {
			await deleteException(id);
			exceptions = exceptions.filter((e) => e.id !== id);
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		}
	}

	function excLabel(k: TimeException['kind']) {
		return EXC_KINDS.find((x) => x.key === k)?.label ?? k;
	}
	function excVariant(k: TimeException['kind']) {
		return (EXC_KINDS.find((x) => x.key === k)?.variant ?? 'neutral') as
			| 'success'
			| 'warning'
			| 'info'
			| 'purple'
			| 'neutral';
	}

	function fmtDate(iso: string) {
		try {
			return new Date(iso).toLocaleString('ru', { dateStyle: 'medium', timeStyle: 'short' });
		} catch {
			return iso;
		}
	}

	function fmtDateOnly(iso: string) {
		try {
			return new Date(iso).toLocaleDateString('ru', {
				day: 'numeric',
				month: 'long',
				year: 'numeric'
			});
		} catch {
			return iso;
		}
	}

	function initials(name?: string): string {
		if (!name) return '??';
		const parts = name.trim().split(/\s+/);
		return ((parts[0]?.[0] ?? '') + (parts[1]?.[0] ?? '')).toUpperCase() || '??';
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

	function ruWorkFormat(f?: string): string {
		switch (f) {
			case 'office':
				return 'офис';
			case 'remote':
				return 'удалённо';
			case 'hybrid':
				return 'гибрид';
			default:
				return f ?? '';
		}
	}
</script>

<div class="page-header">
	<div>
		<h1>Мой профиль</h1>
		<div class="page-header__subtitle">
			Рабочие часы, формат, часовой пояс и исключения. История изменений сохраняется.
		</div>
	</div>
	<div class="page-header__actions">
		<Button icon="ti-check" onclick={confirm}>Подтвердить актуальность</Button>
		<Button variant="primary" icon="ti-device-floppy" onclick={save} disabled={saving}>
			{saving ? 'Сохраняем…' : 'Сохранить изменения'}
		</Button>
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

{#if loading}
	<div class="text-text-3 text-sm">Загрузка…</div>
{:else}
	{#if meData?.user}
		<div class="section">
			<Card>
				<div class="me-card">
					<Avatar initials={initials(meData.user.full_name)} size="xl" variant="purple" />
					<div class="me-card__main">
						<div class="me-card__name">
							{meData.user.full_name}
							<Badge variant="info">{ruRole(meData.user.role)}</Badge>
						</div>
						<div class="me-card__email">
							<i class="ti ti-mail"></i>
							{meData.user.email}
						</div>
						<div class="me-card__chips">
							{#if meData.employee?.position}
								<span class="me-chip">
									<i class="ti ti-briefcase"></i>
									{meData.employee.position}
								</span>
							{/if}
							{#if meData.employee?.department}
								<span class="me-chip">
									<i class="ti ti-building"></i>
									{meData.employee.department}
								</span>
							{/if}
							{#if meData.user.timezone}
								<span class="me-chip">
									<i class="ti ti-clock"></i>
									{meData.user.timezone}
								</span>
							{/if}
							{#if meData.work_profile?.work_format}
								<span class="me-chip">
									<i class="ti ti-device-laptop"></i>
									{ruWorkFormat(meData.work_profile.work_format)}
								</span>
							{/if}
						</div>
					</div>
					<div class="me-card__aside">
						<div class="text-text-3 text-xs">В команде с</div>
						<div class="me-card__since">{fmtDateOnly(meData.user.created_at)}</div>
					</div>
				</div>
			</Card>
		</div>
	{/if}

	<div class="grid-2-1">
		<Card title="Рабочие часы" subtitle="График работы">
			<div class="space-y-2">
				{#each DAYS as d (d.key)}
					<div class="flex items-center gap-3">
						<label class="flex items-center gap-2 w-16">
							<input type="checkbox" bind:checked={days[d.key].enabled} />
							<span class="text-sm">{d.label}</span>
						</label>
						<input
							type="text"
							style="width: 80px;"
							bind:value={days[d.key].start}
							disabled={!days[d.key].enabled}
							placeholder="09:00"
						/>
						<span class="text-text-3 text-xs">—</span>
						<input
							type="text"
							style="width: 80px;"
							bind:value={days[d.key].end}
							disabled={!days[d.key].enabled}
							placeholder="18:00"
						/>
					</div>
				{/each}
			</div>
		</Card>

		<Card title="Параметры">
			<div class="field">
				<label class="field__label" for="tz">Часовой пояс</label>
				<select id="tz" bind:value={timezone}>
					{#each TIMEZONES as tz (tz.value)}
						<option value={tz.value}>{tz.label}</option>
					{/each}
				</select>
			</div>

			<div class="field">
				<label class="field__label" for="wf">Формат работы</label>
				<select id="wf" bind:value={workFormat}>
					{#each FORMATS as f (f.key)}
						<option value={f.key}>{f.label}</option>
					{/each}
				</select>
			</div>

			{#if meData?.employee?.last_profile_update_at}
				<div class="field__hint">
					Последнее обновление: {fmtDate(meData.employee.last_profile_update_at)}
				</div>
			{/if}
			{#if meData?.employee?.last_confirmed_at}
				<div class="field__hint">
					Подтверждено: {fmtDate(meData.employee.last_confirmed_at)}
				</div>
			{/if}
		</Card>
	</div>

	{#if meData?.employee?.id}
		<div class="section" style="margin-top: 24px;">
			<TimeBreakdownCard />
		</div>

		<div class="section" style="margin-top: 24px;">
			<ProfileHistory employeeID={meData.employee.id} />
		</div>
	{/if}

	<div class="section" style="margin-top: 24px;">
		<NotificationChannelsCard />
	</div>

	<div class="section" style="margin-top: 24px;">
		<Card title="Исключения" subtitle="Отпуска, больничные, командировки, личные часы">
			<div class="flex flex-wrap" style="align-items: end; gap: 16px; margin-bottom: 32px;">
				<div class="field" style="margin-bottom: 0;">
					<label class="field__label" for="ek">Тип</label>
					<select id="ek" bind:value={excKind} style="width: 160px;">
						{#each EXC_KINDS as k (k.key)}
							<option value={k.key}>{k.label}</option>
						{/each}
					</select>
				</div>
				<div class="field" style="margin-bottom: 0;">
					<label class="field__label" for="es">Начало</label>
					<input
						id="es"
						type="datetime-local"
						bind:value={excStart}
						style="width: 200px;"
					/>
				</div>
				<div class="field" style="margin-bottom: 0;">
					<label class="field__label" for="ee">Окончание</label>
					<input id="ee" type="datetime-local" bind:value={excEnd} style="width: 200px;" />
				</div>
				<div class="field" style="margin-bottom: 0; flex: 1; min-width: 200px;">
					<label class="field__label" for="ec">Комментарий</label>
					<input id="ec" type="text" bind:value={excComment} placeholder="опционально" />
				</div>
				<Button variant="primary" icon="ti-plus" onclick={addException}>Добавить</Button>
			</div>

			{#if exceptions.length === 0}
				<div
					class="text-text-3 text-sm"
					style="padding: 20px; text-align: center; background: var(--surface); border-radius: 8px;"
				>
					Исключений пока нет.
				</div>
			{:else}
				<div class="space-y-2">
					{#each exceptions as e (e.id)}
						<div class="flex items-center gap-3 p-2" style="border: 0.5px solid var(--border); border-radius: var(--radius-md);">
							<Badge variant={excVariant(e.kind)}>{excLabel(e.kind)}</Badge>
							<div class="flex-1 text-sm">
								{fmtDate(e.start_at)} — {fmtDate(e.end_at)}
								{#if e.comment}
									<span class="text-text-3 ml-2">· {e.comment}</span>
								{/if}
							</div>
							<Button size="xs" variant="ghost" icon="ti-trash" onclick={() => removeException(e.id)}>
								Удалить
							</Button>
						</div>
					{/each}
				</div>
			{/if}
		</Card>
	</div>
{/if}

<style>
	.me-card {
		display: flex;
		align-items: center;
		gap: 18px;
	}
	@media (max-width: 700px) {
		.me-card {
			flex-wrap: wrap;
		}
	}
	.me-card__main {
		flex: 1;
		min-width: 0;
	}
	.me-card__name {
		display: flex;
		align-items: center;
		gap: 10px;
		font-size: 18px;
		font-weight: 600;
		color: var(--text);
		flex-wrap: wrap;
	}
	.me-card__email {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		margin-top: 4px;
		font-size: 13px;
		color: var(--text-2);
	}
	.me-card__email i {
		color: var(--text-3);
	}
	.me-card__chips {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
		margin-top: 10px;
	}
	.me-chip {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		padding: 3px 8px;
		font-size: 12px;
		color: var(--text-2);
		background: var(--surface-2);
		border-radius: 12px;
	}
	.me-chip i {
		font-size: 13px;
		color: var(--text-3);
	}
	.me-card__aside {
		text-align: right;
		flex-shrink: 0;
	}
	.me-card__since {
		font-size: 13px;
		font-weight: 500;
		color: var(--text);
		margin-top: 2px;
	}
</style>
