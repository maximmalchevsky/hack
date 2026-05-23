<script lang="ts">
	import { onMount, tick } from 'svelte';
	import { page } from '$app/state';
	import Card from '$lib/components/Card.svelte';
	import Badge from '$lib/components/Badge.svelte';
	import Button from '$lib/components/Button.svelte';
	import {
		listTeams,
		listAllTeamsForMeetings,
		listMembers as listTeamMembers,
		findWindow,
		proposeMeeting,
		findCrossWindow,
		proposeCrossMeeting,
		MEETING_CATEGORIES,
		type Team,
		type MeetingWindow,
		type MeetingParticipant,
		type UnavailableReason
	} from '$lib/api/teams';
	import {
		listMyMeetings,
		cancelMeeting,
		updateMeeting,
		type MyMeeting
	} from '$lib/api/meetings';
	import IncomingInvitesCard from '$lib/components/IncomingInvitesCard.svelte';
	import { ApiError } from '$lib/api/client';
	import { user } from '$lib/stores/user';

	let teams = $state<Team[]>([]);
	// allTeams — все команды организации (для multi-select в межкомандном режиме).
	// Грузим лениво при первом включении crossMode.
	let allTeams = $state<Team[]>([]);
	let allTeamsLoaded = $state(false);
	let selectedTeamID = $state<string | null>(null);
	let duration = $state(60);
	let days = $state(7);
	let windows = $state<MeetingWindow[]>([]);
	let searching = $state(false);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let success = $state<string | null>(null);

	// Состояние «создаём» для конкретного окна (ключ = start_at).
	let creatingKey = $state<string | null>(null);
	let createdKeys = $state(new Set<string>());

	// Заголовок встречи (общий для всех окон — пользователь может поменять).
	let meetingTitle = $state('');
	// Тип встречи — пустая строка значит «определить автоматически» (GigaChat).
	let meetingCategory = $state('');

	// --- Межкомандная встреча ---
	let crossMode = $state(false);
	// Идентификаторы выбранных команд (для multi-select).
	let crossTeamIDs = $state<string[]>([]);
	// Кэш участников по команде: teamID → массив members.
	type TeamMember = { employee_id: string; full_name: string };
	let teamMembersCache = $state<Record<string, TeamMember[]>>({});
	// Список «выключенных» empID — пользователь снял галочку в сводном списке.
	let crossExcluded = $state(new Set<string>());

	// Сводный (uniq) список участников из выбранных команд.
	const crossAllMembers = $derived.by(() => {
		const seen = new Map<string, { team_id: string; team_name: string; member: TeamMember }>();
		for (const tid of crossTeamIDs) {
			// Имя команды берём из объединённого списка: чужие команды лежат в
			// allTeams, свои — в teams.
			const team = allTeams.find((t) => t.id === tid) ?? teams.find((t) => t.id === tid);
			const teamName = team?.name ?? '';
			const ms = teamMembersCache[tid] ?? [];
			for (const m of ms) {
				if (!seen.has(m.employee_id)) {
					seen.set(m.employee_id, { team_id: tid, team_name: teamName, member: m });
				}
			}
		}
		return Array.from(seen.values());
	});

	// Финальный список emp_id, который пойдёт в API. = все из crossAllMembers
	// минус crossExcluded.
	const crossFinalEmpIDs = $derived(
		crossAllMembers
			.filter((x) => !crossExcluded.has(x.member.employee_id))
			.map((x) => x.member.employee_id)
	);

	// loadAllTeams — подтягиваем список всех команд для multi-select. Один раз
	// на сессию: после первого включения crossMode дальше переключаем тоггл
	// без повторного запроса.
	async function loadAllTeams() {
		if (allTeamsLoaded) return;
		try {
			const r = await listAllTeamsForMeetings();
			allTeams = r.teams ?? [];
			allTeamsLoaded = true;
		} catch {
			// fallback: используем «свои» команды — лучше что-то чем ничего.
			allTeams = teams;
			allTeamsLoaded = true;
		}
	}

	// Реактивный список команд для UI multi-select.
	const teamsForCross = $derived(allTeamsLoaded ? allTeams : teams);

	async function toggleCrossTeam(teamID: string) {
		const idx = crossTeamIDs.indexOf(teamID);
		if (idx >= 0) {
			crossTeamIDs = crossTeamIDs.filter((id) => id !== teamID);
		} else {
			crossTeamIDs = [...crossTeamIDs, teamID];
			// Lazy load members команды.
			if (!teamMembersCache[teamID]) {
				try {
					const r = await listTeamMembers(teamID);
					teamMembersCache = {
						...teamMembersCache,
						[teamID]: (r.members ?? []).map((m) => ({
							employee_id: m.employee_id,
							full_name: m.full_name
						}))
					};
				} catch {
					teamMembersCache = { ...teamMembersCache, [teamID]: [] };
				}
			}
		}
	}

	function toggleCrossEmp(empID: string) {
		const next = new Set(crossExcluded);
		if (next.has(empID)) next.delete(empID);
		else next.add(empID);
		crossExcluded = next;
	}

	// Мои созданные встречи.
	let myMeetings = $state<MyMeeting[]>([]);
	let cancellingId = $state<string | null>(null);

	// Inline-редактор: id раскрытой строки + черновик полей.
	let editingId = $state<string | null>(null);
	let editTitle = $state('');
	let editStart = $state(''); // datetime-local string
	let editEnd = $state('');
	let savingEdit = $state(false);

	// Для Cmd+N — фокусируем селект длительности.
	let durationSelect = $state<HTMLSelectElement | null>(null);

	const viewerTZ = $derived($user?.timezone || 'Europe/Moscow');

	// Кто видит «Найти окна» и «Мои встречи»: только тот, кто создаёт встречи.
	const canManageMeetings = $derived(
		$user?.role === 'manager' ||
			$user?.role === 'pm' ||
			$user?.role === 'admin' ||
			$user?.role === 'hr'
	);

	onMount(async () => {
		try {
			if (canManageMeetings) {
				await Promise.all([
					listTeams()
						.then((r) => {
							teams = r.teams ?? [];
							if (teams.length > 0) selectedTeamID = teams[0].id;
						})
						.catch(() => {
							teams = [];
						}),
					loadMyMeetings()
				]);
			}
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		} finally {
			loading = false;
		}

		// ?focus=duration — пришли через Cmd+N. Ставим фокус на селект.
		if (page.url.searchParams.get('focus') === 'duration') {
			await tick();
			durationSelect?.focus();
		}
	});

	async function loadMyMeetings() {
		try {
			const r = await listMyMeetings();
			myMeetings = r.meetings ?? [];
		} catch (e) {
			// 401/403 — просто прячем блок, не валим страницу.
			myMeetings = [];
		}
	}

	async function onCancel(m: MyMeeting) {
		if (!confirm(`Отменить встречу «${m.title}»?\n${fmt(m.start_at)} — ${fmt(m.end_at)}`)) return;
		cancellingId = m.id;
		error = null;
		success = null;
		try {
			await cancelMeeting(m.id);
			success = `Встреча «${m.title}» отменена.`;
			await loadMyMeetings();
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		} finally {
			cancellingId = null;
		}
	}

	function openEdit(m: MyMeeting) {
		editingId = m.id;
		editTitle = m.title;
		editStart = isoToLocalInput(m.start_at);
		editEnd = isoToLocalInput(m.end_at);
		error = null;
		success = null;
	}
	function closeEdit() {
		editingId = null;
	}

	// "2026-05-20T10:30:00.000Z" → "2026-05-20T13:30" (в TZ браузера, формат datetime-local)
	function isoToLocalInput(iso: string): string {
		const d = new Date(iso);
		const pad = (n: number) => String(n).padStart(2, '0');
		return (
			d.getFullYear() +
			'-' +
			pad(d.getMonth() + 1) +
			'-' +
			pad(d.getDate()) +
			'T' +
			pad(d.getHours()) +
			':' +
			pad(d.getMinutes())
		);
	}

	async function onSaveEdit(m: MyMeeting) {
		const titleTrim = editTitle.trim();
		if (!titleTrim) {
			error = 'Название не может быть пустым.';
			return;
		}
		const newStart = new Date(editStart);
		const newEnd = new Date(editEnd);
		if (Number.isNaN(newStart.getTime()) || Number.isNaN(newEnd.getTime())) {
			error = 'Неверная дата.';
			return;
		}
		if (newEnd.getTime() <= newStart.getTime()) {
			error = 'Время окончания должно быть позже начала.';
			return;
		}

		// Шлём только реально изменённые поля.
		const body: { title?: string; start_at?: string; end_at?: string } = {};
		if (titleTrim !== m.title) body.title = titleTrim;
		if (newStart.toISOString() !== new Date(m.start_at).toISOString()) {
			body.start_at = newStart.toISOString();
		}
		if (newEnd.toISOString() !== new Date(m.end_at).toISOString()) {
			body.end_at = newEnd.toISOString();
		}
		if (Object.keys(body).length === 0) {
			closeEdit();
			return;
		}

		savingEdit = true;
		error = null;
		success = null;
		try {
			await updateMeeting(m.id, body);
			success = `Встреча «${titleTrim}» обновлена.`;
			closeEdit();
			await loadMyMeetings();
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		} finally {
			savingEdit = false;
		}
	}

	async function search() {
		searching = true;
		error = null;
		try {
			if (crossMode) {
				if (crossFinalEmpIDs.length === 0) {
					error = 'Выбери хотя бы одного участника';
					return;
				}
				const r = await findCrossWindow({
					employee_ids: crossFinalEmpIDs,
					duration_min: duration,
					days,
					tz: viewerTZ,
					top_n: 3
				});
				windows = r.windows ?? [];
			} else {
				if (!selectedTeamID) return;
				const r = await findWindow(selectedTeamID, {
					duration_min: duration,
					days,
					tz: viewerTZ,
					top_n: 3
				});
				windows = r.windows ?? [];
			}
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		} finally {
			searching = false;
		}
	}

	function fmt(iso: string): string {
		return new Date(iso).toLocaleString('ru', {
			weekday: 'short',
			day: 'numeric',
			month: 'short',
			hour: '2-digit',
			minute: '2-digit'
		});
	}

	function percent(av: number, total: number): number {
		if (total === 0) return 0;
		return Math.round((av / total) * 100);
	}

	function reasonLabel(p: MeetingParticipant): string {
		// Конкретный тип отсутствия в приоритете — он точнее «общего» reason.
		if (p.reason === 'in_exception' && p.exception_kind) {
			switch (p.exception_kind) {
				case 'vacation':
					return 'в отпуске';
				case 'sick_leave':
					return 'на больничном';
				case 'business_trip':
					return 'в командировке';
				case 'personal_hours':
					return 'личное время';
				case 'custom':
					return 'отсутствует';
			}
		}
		switch (p.reason) {
			case 'busy':
				return 'занят встречей';
			case 'in_exception':
				return 'отсутствует';
			case 'outside_hours':
				return 'вне рабочего времени';
			case 'no_profile':
				return 'нет графика';
			default:
				return '—';
		}
	}

	// pluralRu(2, ['яблоко','яблока','яблок']) → 'яблока'
	function pluralRu(n: number, forms: [string, string, string]): string {
		const m10 = n % 10;
		const m100 = n % 100;
		if (m10 === 1 && m100 !== 11) return forms[0];
		if (m10 >= 2 && m10 <= 4 && (m100 < 10 || m100 >= 20)) return forms[1];
		return forms[2];
	}

	function reasonVariant(r?: UnavailableReason): 'danger' | 'warning' | 'info' | 'neutral' {
		switch (r) {
			case 'busy':
				return 'danger';
			case 'in_exception':
				return 'warning';
			case 'outside_hours':
				return 'info';
			default:
				return 'neutral';
		}
	}

	async function create(w: MeetingWindow) {
		// Предупреждение если кому-то это будет вне его графика.
		const offHours = w.unavailable.filter((p) => p.reason === 'outside_hours');
		if (offHours.length > 0) {
			const lines = offHours.map((p) => `• ${p.full_name}`);
			const msg =
				`Эта встреча будет вне рабочих часов у ${offHours.length}:\n\n${lines.join('\n')}\n\nВсё равно создать?`;
			if (!confirm(msg)) return;
		}

		const key = w.start_at;
		creatingKey = key;
		error = null;
		success = null;
		try {
			let r;
			if (crossMode) {
				const defaultTitle = `Межкомандная встреча (${crossFinalEmpIDs.length} участников)`;
				r = await proposeCrossMeeting({
					start_at: w.start_at,
					end_at: w.end_at,
					title: meetingTitle.trim() || defaultTitle,
					category: meetingCategory || undefined,
					employee_ids: crossFinalEmpIDs,
					primary_team_id: crossTeamIDs[0] || undefined
				});
			} else {
				if (!selectedTeamID) return;
				const team = teams.find((t) => t.id === selectedTeamID);
				const teamName = team?.name ?? '';
				r = await proposeMeeting(selectedTeamID, {
					start_at: w.start_at,
					end_at: w.end_at,
					title: meetingTitle.trim() || `Встреча команды «${teamName}»`,
					category: meetingCategory || undefined
				});
			}
			createdKeys = new Set(createdKeys).add(key);
			const parts = [`Уведомление отправлено ${r.sent} участникам`];
			if (r.yandex_pushed && r.yandex_pushed > 0) {
				const calWord = pluralRu(r.yandex_pushed, ['календарь', 'календаря', 'календарей']);
				parts.push(`событие добавлено в ${r.yandex_pushed} ${calWord}`);
			}
			success = parts.join(' · ');
			await loadMyMeetings();
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		} finally {
			creatingKey = null;
		}
	}
</script>

<div class="page-header">
	<div>
		<h1>Планировщик встреч</h1>
		<div class="page-header__subtitle">
			Подбор оптимальных окон для всей команды. TZ просмотра: {viewerTZ}
		</div>
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
	<div class="section" style="margin-bottom: 16px;">
		<IncomingInvitesCard title="Входящие приглашения" />
	</div>

	{#if canManageMeetings && myMeetings.length > 0}
		<div class="section" style="margin-bottom: 16px;">
			<Card title="Мои встречи" subtitle="Активные предложения. Отмена удаляет событие у всех участников.">
				<div class="space-y-2">
					{#each myMeetings as m (m.id)}
						<div class="my-meeting" class:my-meeting--editing={editingId === m.id}>
							<div class="my-meeting__row">
								<div class="my-meeting__main">
									<div class="my-meeting__title">{m.title}</div>
									<div class="my-meeting__meta">
										<span class="my-meeting__time">
											<i class="ti ti-clock"></i>
											{fmt(m.start_at)} — {fmt(m.end_at)}
										</span>
										{#if m.team_name}
											<span class="my-meeting__team">
												<i class="ti ti-users"></i>
												{m.team_name}
											</span>
										{/if}
										{#if m.total_invited > 0}
											<Badge variant={m.pending === 0 ? 'success' : 'warning'}>
												Подтвердили {m.accepted} из {m.total_invited}
												{#if m.declined > 0}· отклонили {m.declined}{/if}
											</Badge>
										{/if}
										{#if !m.is_owner}
											<Badge variant="neutral">не вы создали</Badge>
										{/if}
									</div>
								</div>
								{#if m.can_cancel}
									{#if editingId === m.id}
										<Button
											size="sm"
											variant="ghost"
											icon="ti-x"
											onclick={closeEdit}
											disabled={savingEdit}
										>
											Отмена
										</Button>
									{:else}
										<Button
											size="sm"
											variant="ghost"
											icon="ti-pencil"
											onclick={() => openEdit(m)}
											disabled={cancellingId !== null || editingId !== null}
										>
											Изменить
										</Button>
										<Button
											size="sm"
											variant="danger"
											icon="ti-trash"
											onclick={() => onCancel(m)}
											disabled={cancellingId !== null || editingId !== null}
										>
											{cancellingId === m.id ? 'Отменяем…' : 'Отменить'}
										</Button>
									{/if}
								{/if}
							</div>

							{#if editingId === m.id}
								<div class="my-meeting__editor">
									<div class="edit-field">
										<label class="field__label" for="ed-title-{m.id}">Название</label>
										<input
											id="ed-title-{m.id}"
											type="text"
											bind:value={editTitle}
											disabled={savingEdit}
										/>
									</div>
									<div class="edit-field">
										<label class="field__label" for="ed-start-{m.id}">Начало</label>
										<input
											id="ed-start-{m.id}"
											type="datetime-local"
											bind:value={editStart}
											disabled={savingEdit}
										/>
									</div>
									<div class="edit-field">
										<label class="field__label" for="ed-end-{m.id}">Окончание</label>
										<input
											id="ed-end-{m.id}"
											type="datetime-local"
											bind:value={editEnd}
											disabled={savingEdit}
										/>
									</div>
									<div class="edit-actions">
										<Button
											size="sm"
											variant="primary"
											icon="ti-check"
											onclick={() => onSaveEdit(m)}
											disabled={savingEdit}
										>
											{savingEdit ? 'Сохраняем…' : 'Сохранить'}
										</Button>
									</div>
								</div>
							{/if}
						</div>
					{/each}
				</div>
			</Card>
		</div>
	{/if}

	{#if canManageMeetings}
	{#if teams.length === 0}
		<Card>
			<div class="text-text-3 text-sm" style="padding: 16px; text-align: center;">
				Команды не созданы
			</div>
		</Card>
	{:else}
		<Card title="Параметры">
		<!-- Тоггл режима: одна команда vs межкомандная встреча -->
		<div class="mode-switch">
			<button
				class="mode-switch__btn"
				class:mode-switch__btn--active={!crossMode}
				onclick={() => (crossMode = false)}
				type="button"
			>
				<i class="ti ti-users"></i>
				Командная
			</button>
			<button
				class="mode-switch__btn"
				class:mode-switch__btn--active={crossMode}
				onclick={() => {
					crossMode = true;
					void loadAllTeams();
				}}
				type="button"
			>
				<i class="ti ti-users-group"></i>
				Межкомандная
			</button>
		</div>

		<!-- В межкомандном режиме multi-select команд — отдельной строкой
		     над общими параметрами, чтобы не ломать выравнивание остальных полей. -->
		{#if crossMode}
			<div class="field" style="margin-bottom: 12px;">
				<label class="field__label">Команды</label>
				<div class="cross-teams">
					{#if teamsForCross.length === 0}
						<span class="text-text-3 text-xs">Загрузка…</span>
					{:else}
						{#each teamsForCross as t (t.id)}
							<label class="cross-teams__item">
								<input
									type="checkbox"
									checked={crossTeamIDs.includes(t.id)}
									onchange={() => toggleCrossTeam(t.id)}
								/>
								<span>{t.name}</span>
							</label>
						{/each}
					{/if}
				</div>
				<div class="field__hint">Выбери 2+ команд — их участники сложатся в общий список</div>
			</div>
		{/if}

		<div class="flex flex-wrap gap-3" style="align-items: end;">
			{#if !crossMode}
				<div class="field" style="margin-bottom: 0;">
					<label class="field__label" for="team">Команда</label>
					<select id="team" bind:value={selectedTeamID} style="width: 200px;">
						{#each teams as t (t.id)}
							<option value={t.id}>{t.name}</option>
						{/each}
					</select>
				</div>
			{/if}
			<div class="field" style="margin-bottom: 0;">
				<label class="field__label" for="dur">Длительность (мин)</label>
				<select
					id="dur"
					bind:value={duration}
					bind:this={durationSelect}
					style="width: 120px;"
				>
					<option value={30}>30</option>
					<option value={60}>60</option>
					<option value={90}>90</option>
					<option value={120}>120</option>
				</select>
			</div>
			<div class="field" style="margin-bottom: 0;">
				<label class="field__label" for="days">Горизонт (дней)</label>
				<select id="days" bind:value={days} style="width: 120px;">
					<option value={3}>3</option>
					<option value={7}>7</option>
					<option value={14}>14</option>
				</select>
			</div>
			<div class="field" style="margin-bottom: 0; flex: 1; min-width: 200px;">
				<label class="field__label" for="title">Название встречи</label>
				<input id="title" type="text" bind:value={meetingTitle} placeholder="Например: Демо для команды" />
			</div>
			<div class="field" style="margin-bottom: 0; min-width: 200px;">
				<label class="field__label" for="cat">Тип встречи</label>
				<select id="cat" bind:value={meetingCategory} style="width: 200px;">
					<option value="">Автоматически</option>
					{#each MEETING_CATEGORIES as c (c)}
						<option value={c}>{c}</option>
					{/each}
				</select>
			</div>
			<Button variant="primary" icon="ti-search" onclick={search} disabled={searching}>
				{searching ? 'Ищем…' : 'Найти окна'}
			</Button>
		</div>
	</Card>

	{#if crossMode && crossAllMembers.length > 0}
		<div class="section" style="margin-top: 16px;">
			<Card
				title="Участники"
				subtitle="Сними галочки с тех, кого не нужно звать. Дубли из пересекающихся команд убраны."
			>
				<div class="cross-members">
					{#each crossAllMembers as { team_name, member } (member.employee_id)}
						<label
							class="cross-members__item"
							class:cross-members__item--off={crossExcluded.has(member.employee_id)}
						>
							<input
								type="checkbox"
								checked={!crossExcluded.has(member.employee_id)}
								onchange={() => toggleCrossEmp(member.employee_id)}
							/>
							<span class="cross-members__name">{member.full_name}</span>
							<span class="cross-members__team">{team_name}</span>
						</label>
					{/each}
				</div>
				<div class="cross-members__total">
					Итого участников: <strong>{crossFinalEmpIDs.length}</strong>
				</div>
			</Card>
		</div>
	{/if}

	<div class="section" style="margin-top: 24px;">
		{#if windows.length === 0 && !searching}
			<Card>
				<div class="text-text-3 text-sm" style="padding: 16px; text-align: center;">
					Окна ещё не искали — выбери параметры и нажми «Найти окна».
				</div>
			</Card>
		{:else}
			<div class="space-y-2">
				{#each windows as w, i (w.start_at)}
					<Card>
						<div class="flex items-start gap-3" style="margin-bottom: 10px;">
							<div
								class="header__logo-icon"
								style="background: var(--info-bg); color: var(--info-strong); width: 40px; height: 40px; font-size: 18px;"
							>
								#{i + 1}
							</div>
							<div class="flex-1">
								<div class="card__title">
									{fmt(w.start_at)} — {fmt(w.end_at)}
								</div>
								<div class="text-text-2 text-sm">
									Доступно <strong>{w.available_count}</strong> из <strong>{w.total_count}</strong>
									({percent(w.available_count, w.total_count)}%)
								</div>
							</div>
							{#if createdKeys.has(w.start_at)}
								<Button size="sm" icon="ti-check" disabled>
									Создано
								</Button>
							{:else}
								<Button
									size="sm"
									variant="primary"
									icon="ti-calendar-plus"
									onclick={() => create(w)}
									disabled={creatingKey !== null}
								>
									{creatingKey === w.start_at ? 'Создаём…' : 'Создать встречу'}
								</Button>
							{/if}
						</div>

						<div class="grid-2" style="gap: 12px;">
							<div>
								<div class="card__caption" style="margin-bottom: 6px;">
									<i class="ti ti-check" style="color: var(--success-strong);"></i>
									Свободны ({w.available.length})
								</div>
								{#if w.available.length === 0}
									<div class="text-text-3 text-xs">—</div>
								{:else}
									<div class="flex flex-wrap gap-1">
										{#each w.available as p (p.employee_id)}
											<Badge variant="success">{p.full_name}</Badge>
										{/each}
									</div>
								{/if}
							</div>

							<div>
								<div class="card__caption" style="margin-bottom: 6px;">
									<i class="ti ti-x" style="color: var(--danger-strong);"></i>
									Недоступны ({w.unavailable.length})
								</div>
								{#if w.unavailable.length === 0}
									<div class="text-text-3 text-xs">—</div>
								{:else}
									<div class="space-y-1">
										{#each w.unavailable as p (p.employee_id)}
											<div class="flex items-center gap-2 text-xs unavail-row">
												<Badge variant={reasonVariant(p.reason)}>{reasonLabel(p)}</Badge>
												<span class="unavail-row__name">{p.full_name}</span>
												<a
													href="/employees/{p.employee_id}"
													class="text-text-3"
													style="margin-left: auto;"
													title="Открыть карточку и связаться"
												>
													<i class="ti ti-external-link"></i>
												</a>
											</div>
										{/each}
									</div>
								{/if}
							</div>
						</div>
					</Card>
				{/each}
			</div>
		{/if}
	</div>
{/if}
	{/if}
{/if}

<style>
	/* --- Тоггл «Командная / Межкомандная» --- */
	.mode-switch {
		display: inline-flex;
		gap: 2px;
		padding: 3px;
		margin-bottom: 12px;
		background: var(--surface);
		border-radius: 8px;
	}
	.mode-switch__btn {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		padding: 6px 14px;
		font-size: 13px;
		background: transparent;
		border: none;
		color: var(--text-2);
		border-radius: 6px;
		cursor: pointer;
		font-family: inherit;
	}
	.mode-switch__btn:hover {
		color: var(--text);
	}
	.mode-switch__btn--active {
		background: var(--bg, white);
		color: var(--text);
		font-weight: 600;
		box-shadow: 0 1px 3px rgba(0, 0, 0, 0.08);
	}

	/* --- Multi-select команд для межкомандной встречи --- */
	.cross-teams {
		display: flex;
		flex-wrap: wrap;
		gap: 8px 16px;
		padding: 8px 12px;
		border: 0.5px solid var(--border);
		border-radius: 8px;
		background: var(--surface);
	}
	.cross-teams__item {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		cursor: pointer;
		font-size: 13px;
		color: var(--text);
	}

	/* --- Список участников межкомандной встречи --- */
	.cross-members {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
		gap: 4px;
	}
	.cross-members__item {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 6px 8px;
		border-radius: 6px;
		cursor: pointer;
		transition: background 0.12s, opacity 0.12s;
	}
	.cross-members__item:hover {
		background: var(--surface);
	}
	.cross-members__item--off {
		opacity: 0.4;
	}
	.cross-members__name {
		font-size: 13px;
		color: var(--text);
		flex: 1;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.cross-members__team {
		font-size: 11px;
		color: var(--text-3);
		padding: 1px 6px;
		background: var(--surface);
		border-radius: 4px;
	}
	.cross-members__total {
		margin-top: 10px;
		padding-top: 8px;
		border-top: 0.5px solid var(--border);
		font-size: 13px;
		color: var(--text-2);
	}

	.my-meeting {
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding: 10px 12px;
		border: 1px solid var(--border);
		border-radius: 10px;
		background: var(--surface);
		transition: border-color 0.15s, background 0.15s;
	}
	.my-meeting--editing {
		border-color: var(--info-strong);
		background: var(--info-bg);
	}
	.my-meeting__row {
		display: flex;
		align-items: center;
		gap: 8px;
	}
	.my-meeting__main {
		flex: 1;
		min-width: 0;
	}
	.my-meeting__editor {
		display: grid;
		grid-template-columns: 2fr 1fr 1fr auto;
		gap: 10px;
		align-items: end;
		padding-top: 10px;
		border-top: 1px dashed var(--border);
	}
	.edit-field {
		display: flex;
		flex-direction: column;
		gap: 4px;
		min-width: 0;
	}
	.edit-field input {
		padding: 6px 10px;
		border: 1px solid var(--border);
		border-radius: 8px;
		background: var(--surface);
		color: var(--text);
		font-size: 13px;
		width: 100%;
	}
	.edit-actions {
		display: flex;
		gap: 8px;
		justify-content: flex-end;
	}
	@media (max-width: 720px) {
		.my-meeting__editor {
			grid-template-columns: 1fr;
		}
	}
	.my-meeting__title {
		font-weight: 600;
		font-size: 14px;
		color: var(--text);
		margin-bottom: 4px;
	}
	.my-meeting__meta {
		display: flex;
		flex-wrap: wrap;
		gap: 10px;
		font-size: 12px;
		color: var(--text-2);
		align-items: center;
	}
	.my-meeting__time,
	.my-meeting__team {
		display: inline-flex;
		align-items: center;
		gap: 4px;
	}
	.my-meeting__time i,
	.my-meeting__team i {
		font-size: 13px;
		opacity: 0.7;
	}

	.unavail-row__name {
		min-width: 0;
	}
	.unavail-row__tz {
		color: var(--warning-strong);
		font-size: 11px;
		white-space: nowrap;
	}

	/* «Входящие приглашения» */
	.invite {
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 10px 12px;
		border: 1px solid var(--border);
		border-radius: 10px;
		background: var(--surface);
		transition: opacity 0.15s;
	}
	.invite--working {
		opacity: 0.6;
	}
	.invite--pending {
		border-left: 3px solid var(--warning-strong);
	}
	.invite--accepted {
		border-left: 3px solid var(--success-strong);
	}
	.invite--declined {
		border-left: 3px solid var(--danger-strong);
		opacity: 0.75;
	}
	.invite__main {
		flex: 1;
		min-width: 0;
	}
	.invite__title {
		font-weight: 600;
		font-size: 14px;
		color: var(--text);
		margin-bottom: 4px;
		display: flex;
		align-items: center;
		gap: 8px;
		flex-wrap: wrap;
	}
	.invite__meta {
		display: flex;
		flex-wrap: wrap;
		gap: 10px;
		font-size: 12px;
		color: var(--text-2);
	}
	.invite__meta i {
		font-size: 13px;
		opacity: 0.7;
		margin-right: 3px;
	}
	.invite__actions {
		display: flex;
		gap: 6px;
		flex-shrink: 0;
	}
</style>
