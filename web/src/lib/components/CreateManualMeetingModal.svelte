<script lang="ts">
	// CreateManualMeetingModal — ручное создание встречи на любую дату/время.
	//
	// В отличие от блока «Найти окна», тут ничего не подсказывает AI: ты сам
	// выбираешь дату, время, длительность, участников. Слот в прошлом не
	// разрешён. Конфликты проверяются live через POST /meetings/check-conflicts
	// (debounce ~400ms) — мягкое предупреждение, не блокирует создание.
	//
	// Поддерживает два режима:
	//   - Командная: выбираешь одну из своих команд, пушим всем её members.
	//   - Межкомандная: галочками выбираешь N команд → список их сотрудников →
	//     можно снять лишних → отправляем явный invitee_emp_ids.

	import Modal from './Modal.svelte';
	import Button from './Button.svelte';
	import Badge from './Badge.svelte';
	import {
		listTeams,
		listAllTeamsForMeetings,
		listMembers as listTeamMembers,
		proposeMeeting,
		proposeCrossMeeting,
		MEETING_CATEGORIES,
		type Team
	} from '$lib/api/teams';
	import { checkConflicts, type ConflictEntry, type OverloadEntry } from '$lib/api/meetings';
	import { ApiError } from '$lib/api/client';

	interface Props {
		open: boolean;
		onClose: () => void;
		// onCreated — вызывается после успешного создания. Родитель должен
		// перезагрузить список «Мои встречи».
		onCreated: () => void;
	}

	let { open, onClose, onCreated }: Props = $props();

	// --- Форма ---
	let title = $state('');
	let category = $state(''); // '' = «определить автоматически»
	let mode = $state<'team' | 'cross'>('team');

	// Командный: одна команда (только свои).
	let teams = $state<Team[]>([]);
	let teamID = $state<string | null>(null);

	// Межкомандный: список всех команд + выбранные галочками.
	let allTeams = $state<Team[]>([]);
	let allTeamsLoaded = $state(false);
	let crossTeamIDs = $state<string[]>([]);
	type TeamMember = { employee_id: string; full_name: string };
	let teamMembersCache = $state<Record<string, TeamMember[]>>({});
	let crossExcluded = $state(new Set<string>());

	// Дата / время / длительность.
	let date = $state(''); // 'YYYY-MM-DD'
	let timeHM = $state(''); // 'HH:MM'
	let durationMin = $state(60);

	// Конфликты + статус submit.
	let conflicts = $state<ConflictEntry[]>([]);
	let overload = $state<OverloadEntry[]>([]);
	let conflictsChecking = $state(false);
	let submitting = $state(false);
	let formError = $state<string | null>(null);
	let teamsLoading = $state(false);

	// initialized — флаг «дефолты уже выставлены в текущей сессии открытия».
	// Без него $effect для defaults читал бы date/timeHM, и при каждом изменении
	// этих полей (включая «стереть и перенабрать») эффект снова ставил их в
	// «сейчас + 30 мин». Из-за этого окно дёргалось и даты сбрасывались.
	// Reset происходит при закрытии — в onClose-обвязке через $effect ниже.
	let initialized = $state(false);

	// --- Sane defaults: при первом открытии заполняем дату/время «через 30 минут от сейчас». ---
	$effect(() => {
		if (!open) return;
		if (initialized) return;
		initialized = true;

		const d = new Date();
		// Округляем до следующей половины часа (или часа).
		d.setSeconds(0, 0);
		d.setMinutes(d.getMinutes() + 30);
		const m = d.getMinutes();
		if (m >= 30) {
			d.setMinutes(30);
		} else {
			d.setMinutes(0);
			d.setHours(d.getHours() + 1);
		}
		date = ymd(d);
		timeHM = hm(d);
	});

	// При закрытии окна сбрасываем initialized — следующее открытие пересчитает дефолты.
	$effect(() => {
		if (!open && initialized) {
			initialized = false;
		}
	});

	// --- Загрузка команд при первом открытии. ---
	$effect(() => {
		if (!open || teams.length > 0) return;
		void loadTeams();
	});

	async function loadTeams() {
		teamsLoading = true;
		try {
			const r = await listTeams();
			teams = r.teams ?? [];
			if (teams.length > 0 && !teamID) teamID = teams[0].id;
		} catch {
			teams = [];
		} finally {
			teamsLoading = false;
		}
	}

	// Ленивая загрузка allTeams при переключении в crossMode.
	async function ensureAllTeams() {
		if (allTeamsLoaded) return;
		try {
			const r = await listAllTeamsForMeetings();
			allTeams = r.teams ?? [];
			allTeamsLoaded = true;
		} catch {
			allTeams = teams;
			allTeamsLoaded = true;
		}
	}

	function switchMode(next: 'team' | 'cross') {
		mode = next;
		if (next === 'cross') void ensureAllTeams();
	}

	async function toggleCrossTeam(id: string) {
		const idx = crossTeamIDs.indexOf(id);
		if (idx >= 0) {
			crossTeamIDs = crossTeamIDs.filter((x) => x !== id);
			return;
		}
		crossTeamIDs = [...crossTeamIDs, id];
		// Подтягиваем участников выбранной команды.
		if (!teamMembersCache[id]) {
			try {
				const r = await listTeamMembers(id);
				teamMembersCache = {
					...teamMembersCache,
					[id]: (r.members ?? []).map((m) => ({
						employee_id: m.employee_id,
						full_name: m.full_name
					}))
				};
			} catch {
				teamMembersCache = { ...teamMembersCache, [id]: [] };
			}
		}
	}

	function toggleCrossEmp(empID: string) {
		const next = new Set(crossExcluded);
		if (next.has(empID)) next.delete(empID);
		else next.add(empID);
		crossExcluded = next;
	}

	// Сводный список участников в межкомандном режиме (uniq + имя команды).
	const crossAllMembers = $derived.by(() => {
		const seen = new Map<string, { team_name: string; member: TeamMember }>();
		for (const tid of crossTeamIDs) {
			const team =
				allTeams.find((t) => t.id === tid) ?? teams.find((t) => t.id === tid);
			const teamName = team?.name ?? '';
			const ms = teamMembersCache[tid] ?? [];
			for (const m of ms) {
				if (!seen.has(m.employee_id)) {
					seen.set(m.employee_id, { team_name: teamName, member: m });
				}
			}
		}
		return Array.from(seen.values());
	});

	const crossFinalEmpIDs = $derived(
		crossAllMembers
			.filter((x) => !crossExcluded.has(x.member.employee_id))
			.map((x) => x.member.employee_id)
	);

	// Участники для проверки конфликтов: в командном режиме нужно подгрузить
	// members выбранной команды. Кешируем в teamMembersCache, как и в cross.
	$effect(() => {
		if (mode !== 'team' || !teamID) return;
		if (teamMembersCache[teamID]) return;
		void (async () => {
			try {
				const r = await listTeamMembers(teamID!);
				teamMembersCache = {
					...teamMembersCache,
					[teamID!]: (r.members ?? []).map((m) => ({
						employee_id: m.employee_id,
						full_name: m.full_name
					}))
				};
			} catch {
				teamMembersCache = { ...teamMembersCache, [teamID!]: [] };
			}
		})();
	});

	// Финальный список emp_id (в обоих режимах) — для check-conflicts и submit.
	const targetEmpIDs = $derived.by(() => {
		if (mode === 'cross') return crossFinalEmpIDs;
		if (teamID && teamMembersCache[teamID]) {
			return teamMembersCache[teamID].map((m) => m.employee_id);
		}
		return [] as string[];
	});

	// Считаем end_at и валидность.
	const startISO = $derived.by(() => {
		if (!date || !timeHM) return '';
		const dt = new Date(`${date}T${timeHM}`);
		if (Number.isNaN(dt.getTime())) return '';
		return dt.toISOString();
	});

	const endISO = $derived.by(() => {
		if (!startISO) return '';
		const dt = new Date(startISO);
		dt.setMinutes(dt.getMinutes() + durationMin);
		return dt.toISOString();
	});

	const endLabel = $derived.by(() => {
		if (!endISO) return '—';
		const d = new Date(endISO);
		return d.toLocaleTimeString('ru', { hour: '2-digit', minute: '2-digit' });
	});

	// Слот в будущем? «Прошлое» = старт раньше now() (минус буфер 1 мин на ввод).
	const isFutureSlot = $derived.by(() => {
		if (!startISO) return false;
		return new Date(startISO).getTime() > Date.now() - 60_000;
	});

	const canSubmit = $derived.by(() => {
		if (submitting) return false;
		if (!startISO || !endISO) return false;
		if (!isFutureSlot) return false;
		if (mode === 'team' && !teamID) return false;
		if (mode === 'cross' && crossFinalEmpIDs.length === 0) return false;
		return true;
	});

	// --- Live-проверка конфликтов (debounce 400ms). ---
	let conflictsTimer: ReturnType<typeof setTimeout> | null = null;
	$effect(() => {
		// Зависимости: startISO, endISO, targetEmpIDs.
		if (!open) return;
		if (!startISO || !endISO || targetEmpIDs.length === 0) {
			conflicts = [];
			overload = [];
			return;
		}
		// Снимок текущих значений для замыкания.
		const s = startISO;
		const e = endISO;
		const ids = [...targetEmpIDs];

		if (conflictsTimer) clearTimeout(conflictsTimer);
		conflictsTimer = setTimeout(async () => {
			conflictsChecking = true;
			try {
				const r = await checkConflicts({ start_at: s, end_at: e, employee_ids: ids });
				conflicts = r.conflicts ?? [];
				overload = r.overload ?? [];
			} catch {
				// Тихо: проверка не критична, основной flow всё равно работает.
				conflicts = [];
				overload = [];
			} finally {
				conflictsChecking = false;
			}
		}, 400);
	});

	async function onSubmit() {
		if (!canSubmit) return;

		// Анти-burnout soft-block: если у кого-то будет перегруз — спросим
		// явное подтверждение перед отправкой запроса. Бэк всё равно сделает
		// свою проверку и при отсутствии force вернёт 409 (страховка от
		// stale-данных, если конфликты изменились между check-conflicts и submit).
		let force = false;
		if (overload.length > 0) {
			const lines = overload.map(
				(o) =>
					`• ${o.full_name}: ${o.current_hours} ч → ${o.projected_hours} ч (порог ${o.limit})`
			);
			const msg =
				`После этой встречи у ${overload.length} участников будет больше ${WeeklyMeetingHoursLimit} ч встреч на неделе:\n\n` +
				lines.join('\n') +
				'\n\nВсё равно создать?';
			if (!confirm(msg)) {
				return;
			}
			force = true;
		}

		submitting = true;
		formError = null;
		try {
			const titleTrim = title.trim();
			if (mode === 'cross') {
				const defaultTitle = `Межкомандная встреча (${crossFinalEmpIDs.length} участников)`;
				await proposeCrossMeeting({
					start_at: startISO,
					end_at: endISO,
					title: titleTrim || defaultTitle,
					category: category || undefined,
					employee_ids: crossFinalEmpIDs,
					primary_team_id: crossTeamIDs[0] || undefined,
					force
				});
			} else {
				const team = teams.find((t) => t.id === teamID);
				await proposeMeeting(teamID!, {
					start_at: startISO,
					end_at: endISO,
					title: titleTrim || `Встреча команды «${team?.name ?? ''}»`,
					category: category || undefined,
					force
				});
			}
			// Закрываем + дергаем родителя.
			resetForm();
			onCreated();
			onClose();
		} catch (e) {
			// Если бэк прислал 409 overload (хоть мы и проверяли через check-conflicts —
			// данные могли устареть за секунды), берём конкретику из payload и
			// показываем пользователю повторный confirm.
			if (
				e instanceof ApiError &&
				e.status === 409 &&
				e.payload &&
				typeof e.payload === 'object' &&
				'overload' in e.payload
			) {
				const list = (e.payload as { overload?: OverloadEntry[] }).overload ?? [];
				overload = list;
				formError = `У ${list.length} участников будет перегруз — подтверди ещё раз.`;
			} else {
				formError = e instanceof ApiError ? e.message : String(e);
			}
		} finally {
			submitting = false;
		}
	}

	// WeeklyMeetingHoursLimit — продублирован на фронте только для текста
	// в confirm-диалоге. Реальный порог проверяется на бэке (см. service).
	const WeeklyMeetingHoursLimit = 35;

	function resetForm() {
		title = '';
		category = '';
		date = '';
		timeHM = '';
		durationMin = 60;
		conflicts = [];
		overload = [];
		crossExcluded = new Set();
		formError = null;
		// teamID/crossTeamIDs/cache оставляем — пользователь часто открывает
		// модалку повторно для той же команды.
	}

	// --- helpers ---

	function ymd(d: Date): string {
		const y = d.getFullYear();
		const m = String(d.getMonth() + 1).padStart(2, '0');
		const dd = String(d.getDate()).padStart(2, '0');
		return `${y}-${m}-${dd}`;
	}
	function hm(d: Date): string {
		const h = String(d.getHours()).padStart(2, '0');
		const m = String(d.getMinutes()).padStart(2, '0');
		return `${h}:${m}`;
	}

	const todayYMD = $derived(ymd(new Date()));

	function fmtConflictTime(iso: string): string {
		const d = new Date(iso);
		return d.toLocaleTimeString('ru', { hour: '2-digit', minute: '2-digit' });
	}
	function fmtConflictDate(iso: string): string {
		const d = new Date(iso);
		return d.toLocaleDateString('ru', { day: 'numeric', month: 'short' });
	}

	// Сгруппированный список конфликтов: emp_id → массив. UI рендерит по
	// сотруднику, чтобы «Анна Иванова: 2 пересечения» читалось проще, чем
	// плоский список.
	const conflictsByEmp = $derived.by(() => {
		const m = new Map<string, { full_name: string; items: ConflictEntry[] }>();
		for (const c of conflicts) {
			const prev = m.get(c.employee_id);
			if (prev) {
				prev.items.push(c);
			} else {
				m.set(c.employee_id, { full_name: c.full_name, items: [c] });
			}
		}
		return Array.from(m.values()).sort((a, b) => a.full_name.localeCompare(b.full_name, 'ru'));
	});

	const busyCount = $derived(conflictsByEmp.length);
</script>

<Modal {open} title="Создать встречу вручную" size="md" {onClose}>
	<div class="form">
		{#if formError}
			<Badge variant="danger"><i class="ti ti-alert-circle"></i>{formError}</Badge>
		{/if}

		<!-- Режим -->
		<div class="row">
			<div class="row__label">Режим</div>
			<div class="mode-switch">
				<button
					type="button"
					class="mode-btn"
					class:mode-btn--active={mode === 'team'}
					onclick={() => switchMode('team')}
				>
					<i class="ti ti-users"></i>
					Командная
				</button>
				<button
					type="button"
					class="mode-btn"
					class:mode-btn--active={mode === 'cross'}
					onclick={() => switchMode('cross')}
				>
					<i class="ti ti-users-group"></i>
					Межкомандная
				</button>
			</div>
		</div>

		{#if mode === 'team'}
			<div class="row">
				<label class="row__label" for="m-team">Команда</label>
				{#if teamsLoading}
					<span class="text-text-3 text-xs">Загрузка…</span>
				{:else if teams.length === 0}
					<span class="text-text-3 text-xs">Команды не созданы</span>
				{:else}
					<select id="m-team" bind:value={teamID}>
						{#each teams as t (t.id)}
							<option value={t.id}>{t.name}</option>
						{/each}
					</select>
				{/if}
			</div>
		{:else}
			<div class="row">
				<div class="row__label">Команды</div>
				<div class="cross-teams">
					{#if !allTeamsLoaded}
						<span class="text-text-3 text-xs">Загрузка…</span>
					{:else}
						{#each allTeams as t (t.id)}
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
			</div>

			{#if crossAllMembers.length > 0}
				<div class="row">
					<div class="row__label">Участники</div>
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
					<div class="text-text-3 text-xs">
						Позовём <strong>{crossFinalEmpIDs.length}</strong> чел.
					</div>
				</div>
			{/if}
		{/if}

		<!-- Тема -->
		<div class="row">
			<label class="row__label" for="m-title">Тема</label>
			<input
				id="m-title"
				type="text"
				bind:value={title}
				placeholder="Например: Демо для команды"
			/>
		</div>

		<!-- Тип встречи -->
		<div class="row">
			<label class="row__label" for="m-cat">Тип встречи</label>
			<select id="m-cat" bind:value={category}>
				<option value="">Автоматически</option>
				{#each MEETING_CATEGORIES as c (c)}
					<option value={c}>{c}</option>
				{/each}
			</select>
		</div>

		<!-- Дата / время / длительность — в одну строку -->
		<div class="row row--cols">
			<div class="col">
				<label class="row__label" for="m-date">Дата</label>
				<input
					id="m-date"
					type="date"
					bind:value={date}
					min={todayYMD}
				/>
			</div>
			<div class="col col--time">
				<label class="row__label" for="m-time">Начало</label>
				<input id="m-time" type="time" bind:value={timeHM} />
			</div>
			<div class="col col--dur">
				<label class="row__label" for="m-dur">Длительность</label>
				<select id="m-dur" bind:value={durationMin}>
					<option value={15}>15 мин</option>
					<option value={30}>30 мин</option>
					<option value={45}>45 мин</option>
					<option value={60}>60 мин</option>
					<option value={90}>90 мин</option>
					<option value={120}>2 часа</option>
				</select>
			</div>
			<div class="col col--end">
				<div class="row__label">Окончание</div>
				<div class="end-label">{endLabel}</div>
			</div>
		</div>

		{#if !isFutureSlot && startISO}
			<Badge variant="warning">
				<i class="ti ti-alert-triangle"></i>
				Выбранное время в прошлом — выбери будущий слот
			</Badge>
		{/if}

		<!-- Конфликты -->
		{#if startISO && isFutureSlot && targetEmpIDs.length > 0}
			<div class="conflicts">
				{#if conflictsChecking}
					<div class="text-text-3 text-xs">
						<i class="ti ti-loader"></i> Проверяю занятость…
					</div>
				{:else if conflicts.length === 0}
					<div class="conflicts__ok">
						<i class="ti ti-check" style="color: var(--success-strong);"></i>
						Все {targetEmpIDs.length} участников свободны в это время
					</div>
				{:else}
					<div class="conflicts__head">
						<i class="ti ti-alert-circle" style="color: var(--warning-strong);"></i>
						Заняты {busyCount} из {targetEmpIDs.length}:
					</div>
					<ul class="conflicts__list">
						{#each conflictsByEmp as g (g.full_name)}
							<li class="conflicts__row">
								<span class="conflicts__name">{g.full_name}</span>
								<span class="conflicts__items">
									{#each g.items as c, i (i)}
										{#if i > 0}, {/if}
										{#if c.kind === 'exception' || c.kind === 'outside_hours'}
											<span class="conflicts__title">{c.title}</span>
										{:else}
											<span class="conflicts__title">«{c.title}»</span>
											({fmtConflictDate(c.start_at)} {fmtConflictTime(c.start_at)}–{fmtConflictTime(c.end_at)})
										{/if}
									{/each}
								</span>
							</li>
						{/each}
					</ul>
				{/if}
			</div>
		{/if}

		<!-- Анти-burnout: после этой встречи у кого-то >35ч/неделю. Не блокирует
		     submit, но onSubmit спросит явное подтверждение перед force=true. -->
		{#if overload.length > 0}
			<div class="overload">
				<div class="overload__head">
					<i class="ti ti-flame" style="color: var(--danger-strong);"></i>
					Перегруз по встречам у {overload.length}
					{overload.length === 1 ? 'участника' : 'участников'}:
				</div>
				<ul class="overload__list">
					{#each overload as o (o.employee_id)}
						<li class="overload__row">
							<span class="overload__name">{o.full_name}</span>
							<span class="overload__hours">
								{o.current_hours} ч → <strong>{o.projected_hours} ч</strong>
								<span class="text-text-3 text-xs">(порог {o.limit} ч)</span>
							</span>
						</li>
					{/each}
				</ul>
			</div>
		{/if}
	</div>

	{#snippet footer()}
		<Button variant="ghost" onclick={onClose} disabled={submitting}>Отмена</Button>
		<Button
			variant="primary"
			icon={submitting ? 'ti-loader' : 'ti-calendar-plus'}
			onclick={onSubmit}
			disabled={!canSubmit}
		>
			{submitting ? 'Создаём…' : 'Создать встречу'}
		</Button>
	{/snippet}
</Modal>

<style>
	.form {
		display: flex;
		flex-direction: column;
		gap: 14px;
	}
	.row {
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	.row__label {
		font-size: 11px;
		font-weight: 600;
		color: var(--text-3);
		text-transform: uppercase;
		letter-spacing: 0.4px;
	}
	.row--cols {
		display: grid;
		grid-template-columns: 1.4fr 1fr 1.1fr 0.8fr;
		gap: 12px;
		align-items: end;
	}
	.col {
		display: flex;
		flex-direction: column;
		gap: 6px;
		min-width: 0;
	}
	.end-label {
		font-size: 14px;
		font-weight: 600;
		color: var(--text);
		padding: 7px 0;
	}
	.row input[type='text'],
	.row input[type='date'],
	.row input[type='time'],
	.row select,
	.col input,
	.col select {
		padding: 7px 10px;
		font-size: 13px;
		border: 0.5px solid var(--border);
		border-radius: 6px;
		background: var(--bg);
		color: var(--text);
		width: 100%;
	}
	.mode-switch {
		display: inline-flex;
		gap: 4px;
		background: var(--surface-2);
		padding: 3px;
		border-radius: 8px;
		width: fit-content;
	}
	.mode-btn {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		padding: 6px 12px;
		font-size: 13px;
		border: 0;
		background: transparent;
		color: var(--text-2);
		cursor: pointer;
		border-radius: 6px;
		transition: all 0.12s;
	}
	.mode-btn--active {
		background: var(--surface);
		color: var(--text);
		box-shadow: 0 0 0 1px var(--border);
	}
	.cross-teams {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
	}
	.cross-teams__item {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		padding: 5px 10px;
		font-size: 12px;
		border: 0.5px solid var(--border);
		border-radius: 6px;
		background: var(--bg);
		cursor: pointer;
	}
	.cross-members {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
		max-height: 180px;
		overflow-y: auto;
		padding: 6px;
		border: 0.5px solid var(--border);
		border-radius: 6px;
		background: var(--surface-2);
	}
	.cross-members__item {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		padding: 4px 8px;
		font-size: 12px;
		border-radius: 6px;
		background: var(--bg);
		cursor: pointer;
	}
	.cross-members__item--off {
		opacity: 0.45;
	}
	.cross-members__name {
		font-weight: 500;
		color: var(--text);
	}
	.cross-members__team {
		color: var(--text-3);
		font-size: 11px;
	}
	.conflicts {
		padding: 10px 12px;
		border-radius: 8px;
		background: var(--surface-2);
		border: 0.5px solid var(--border);
		font-size: 13px;
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	.conflicts__ok {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		color: var(--success-strong);
		font-weight: 500;
	}
	.conflicts__head {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		color: var(--warning-strong);
		font-weight: 600;
	}
	.conflicts__list {
		list-style: none;
		padding: 0;
		margin: 0;
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	.conflicts__row {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
		color: var(--text-2);
	}
	.conflicts__name {
		font-weight: 600;
		color: var(--text);
		min-width: 140px;
	}
	.conflicts__items {
		color: var(--text-2);
	}
	.conflicts__title {
		color: var(--text);
		font-weight: 500;
	}
	.overload {
		padding: 10px 12px;
		border-radius: 8px;
		background: var(--danger-bg);
		border: 0.5px solid var(--danger-text);
		font-size: 13px;
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	.overload__head {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		color: var(--danger-strong);
		font-weight: 600;
	}
	.overload__list {
		list-style: none;
		padding: 0;
		margin: 0;
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	.overload__row {
		display: flex;
		gap: 8px;
		color: var(--text-2);
	}
	.overload__name {
		font-weight: 600;
		color: var(--text);
		min-width: 140px;
	}
	.overload__hours {
		color: var(--text-2);
	}
</style>
