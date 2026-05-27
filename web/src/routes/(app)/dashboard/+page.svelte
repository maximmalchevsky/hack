<script lang="ts">
	import { onMount } from 'svelte';
	import { user } from '$lib/stores/user';
	import Button from '$lib/components/Button.svelte';
	import Stat from '$lib/components/Stat.svelte';
	import Card from '$lib/components/Card.svelte';
	import Badge from '$lib/components/Badge.svelte';
	import IncomingInvitesCard from '$lib/components/IncomingInvitesCard.svelte';
	import PulseCheckCard from '$lib/components/PulseCheckCard.svelte';
	import PulseTeamCard from '$lib/components/PulseTeamCard.svelte';
	import type { TimelineEventKind } from '$lib/components/Timeline.svelte';
	import {
		listMyEvents,
		getWeeklySummary,
		setEventCategory,
		type CalendarEvent,
		type WeeklySummary
	} from '$lib/api/profile';
	import { MEETING_CATEGORIES } from '$lib/api/teams';
	import { listIntegrations, type Integration } from '$lib/api/integrations';
	import { listMyTasks, type TrackerTask } from '$lib/api/tasks';
	import { ApiError } from '$lib/api/client';
	import { marked } from 'marked';

	marked.setOptions({ gfm: true, breaks: true });
	function renderMd(s: string): string {
		const esc = s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
		return marked.parse(esc) as string;
	}

	const role = $derived($user?.role ?? 'employee');

	// Имя из ФИО — берём только первое слово, чтобы не было «Иванов Иван Иванович».
	const firstName = $derived(firstWord($user?.fullName));

	// Динамическое приветствие: «Доброе утро / День / Вечер / Ночь, Иван».
	const greeting = $derived(timeGreeting(firstName));
	// Подзаголовок зависит от роли — то, что было в шапке раньше.
	const roleSubtitle = $derived(roleSubtitleFor(role));

	function firstWord(name?: string): string {
		if (!name) return '';
		const parts = name.trim().split(/\s+/);
		return parts[0] ?? '';
	}

	function timeGreeting(name: string): string {
		const h = new Date().getHours();
		let part = 'Доброе утро';
		if (h >= 5 && h < 12) part = 'Доброе утро';
		else if (h >= 12 && h < 17) part = 'Добрый день';
		else if (h >= 17 && h < 23) part = 'Добрый вечер';
		else part = 'Доброй ночи';
		return name ? `${part}, ${name}!` : part;
	}

	function roleSubtitleFor(r: UserRole): string {
		switch (r) {
			case 'admin':
				return 'Админ-сводка';
			case 'manager':
				return 'Команда сегодня';
			case 'pm':
				return 'Планирование';
			case 'hr':
				return 'HR-сводка';
			case 'analyst':
				return 'Аналитика';
			default:
				return 'Твой рабочий день';
		}
	}

	// --- Навигация недель ---

	// offset = 0 — текущая неделя, -1 — прошлая, +1 — следующая.
	let weekOffset = $state(0);
	let selectedDayIdx = $state(0); // 0..6, выставится в onMount/effect
	let expandedID = $state<string | null>(null);
	let events = $state<CalendarEvent[]>([]);
	let summary = $state<WeeklySummary | null>(null);
	let summaryLoading = $state(false);
	let loading = $state(false);
	let error = $state<string | null>(null);

	// --- Источники данных (Outlook-style) ---
	// integration_id → { label, color } для UI чекбоксов.
	// null-источник = «Workie» (нативные события, созданные на /scheduler или seed).
	let integrations = $state<Integration[]>([]);
	// hiddenSources — Set<string>, где 'native' для NULL и id интеграции иначе.
	// Хранится в localStorage чтобы сохранять между перезагрузками.
	let hiddenSources = $state(new Set<string>());

	function sourceKey(integID: string | null | undefined): string {
		return integID || 'native';
	}

	function sourceLabel(integ: Integration | null): string {
		if (!integ) return 'Календарь Workie';
		if (integ.account_label) return integ.account_label;
		if (integ.account_email) return integ.account_email;
		switch (integ.provider) {
			case 'yandex_calendar':
				return 'Яндекс Календарь';
			case 'ical':
				return 'iCal / ICS';
			case 'caldav':
				return 'CalDAV';
			case 'google_calendar':
				return 'Google Calendar';
			case 'ms365':
				return 'MS 365';
			default:
				return integ.provider;
		}
	}

	// Палитра по индексу — стабильна для одной интеграции, разные источники
	// получают разные цвета. 'native' всегда нейтрально-серый.
	const SOURCE_COLORS = ['#6366f1', '#22c55e', '#f59e0b', '#ec4899', '#14b8a6', '#0ea5e9'];
	function sourceColor(key: string, idx: number): string {
		if (key === 'native') return '#94a3b8';
		return SOURCE_COLORS[idx % SOURCE_COLORS.length];
	}

	// Список «источников» для UI: native + только календарные integrations.
	// Tracker-провайдеры (Jira, Yandex Tracker) исключаем — их задачи живут
	// в /tasks, а не в недельной агенде.
	const CALENDAR_PROVIDERS = new Set([
		'yandex_calendar',
		'google_calendar',
		'ms365',
		'caldav',
		'ical'
	]);
	const sources = $derived.by(() => {
		const out: { key: string; label: string; color: string; integ: Integration | null }[] = [
			{ key: 'native', label: 'Календарь Workie', color: sourceColor('native', 0), integ: null }
		];
		integrations
			.filter((i) => CALENDAR_PROVIDERS.has(i.provider))
			.forEach((i, idx) => {
				out.push({
					key: i.id,
					label: sourceLabel(i),
					color: sourceColor(i.id, idx),
					integ: i
				});
			});
		return out;
	});

	// События с учётом фильтра по источникам.
	const visibleEvents = $derived(
		events.filter((e) => !hiddenSources.has(sourceKey(e.integration_id)))
	);

	function toggleSource(key: string) {
		const next = new Set(hiddenSources);
		if (next.has(key)) next.delete(key);
		else next.add(key);
		hiddenSources = next;
		try {
			localStorage.setItem('workie:dashboard:hiddenSources', JSON.stringify([...next]));
		} catch {
			// localStorage может быть отрублен — переживём.
		}
	}

	function loadHiddenSources() {
		try {
			const raw = localStorage.getItem('workie:dashboard:hiddenSources');
			if (raw) {
				const arr = JSON.parse(raw) as string[];
				hiddenSources = new Set(arr);
			}
		} catch {
			// ignore
		}
	}

	async function loadIntegrations() {
		try {
			const r = await listIntegrations();
			integrations = r.integrations ?? [];
		} catch {
			integrations = [];
		}
	}

	// --- Виджет «Запланировано сегодня» ---
	let plannedTasks = $state<TrackerTask[]>([]);

	async function loadTasks() {
		try {
			const r = await listMyTasks();
			plannedTasks = r.tasks ?? [];
		} catch {
			plannedTasks = [];
		}
	}


	// Хелпер: задачи с слотами на конкретный день, отсортированные по приоритету.
	function tasksForDate(date: Date): { task: TrackerTask; hours: number }[] {
		const key = dateToKey(date);
		const list = plannedTasks
			.map((t) => {
				const slot = t.slots?.find((s) => s.date === key);
				return slot ? { task: t, hours: slot.hours } : null;
			})
			.filter((x): x is { task: TrackerTask; hours: number } => x !== null);
		const rank: Record<string, number> = {
			highest: 5,
			high: 4,
			medium: 3,
			low: 2,
			lowest: 1
		};
		list.sort(
			(a, b) =>
				(rank[b.task.priority ?? 'medium'] ?? 3) -
				(rank[a.task.priority ?? 'medium'] ?? 3)
		);
		return list;
	}

	function dateToKey(d: Date): string {
		const y = d.getFullYear();
		const m = String(d.getMonth() + 1).padStart(2, '0');
		const dd = String(d.getDate()).padStart(2, '0');
		return `${y}-${m}-${dd}`;
	}

	// Топ-3 задачи на СЕГОДНЯ — для виджета над недельной агендой.
	const tasksToday = $derived.by(() => {
		const today = new Date();
		return tasksForDate(today).slice(0, 3);
	});

	const hoursToday = $derived(
		tasksToday.reduce((s, { hours }) => s + hours, 0)
	);

	function priorityColorTask(p?: string): string {
		switch (p) {
			case 'highest':
				return 'var(--danger-strong)';
			case 'high':
				return 'var(--warning-strong)';
			case 'low':
				return 'var(--success-strong)';
			case 'lowest':
				return 'var(--text-3)';
			default:
				return 'var(--info-strong)';
		}
	}

	const weekStart = $derived(mondayOf(new Date(), weekOffset));
	const weekEnd = $derived(addDays(weekStart, 7)); // ПН..ВС включительно
	const weekLabel = $derived(formatWeekRange(weekStart, addDays(weekStart, 6)));
	const isCurrentWeek = $derived(weekOffset === 0);

	$effect(() => {
		// Подгружаем события при смене недели.
		weekOffset;
		void loadEvents();
	});

	// При смене недели — переключаемся на сегодня если он в этой неделе,
	// иначе на первый рабочий день (ПН).
	$effect(() => {
		weekOffset;
		const today = new Date();
		today.setHours(0, 0, 0, 0);
		const diff = Math.floor((today.getTime() - weekStart.getTime()) / (1000 * 60 * 60 * 24));
		selectedDayIdx = diff >= 0 && diff <= 6 ? diff : 0;
	});

	onMount(() => {
		loadHiddenSources();
		void loadEvents();
		void loadSummary();
		void loadIntegrations();
		void loadTasks();
	});

	async function loadSummary() {
		summaryLoading = true;
		try {
			summary = await getWeeklySummary();
		} catch {
			summary = null;
		} finally {
			summaryLoading = false;
		}
	}

	async function loadEvents() {
		loading = true;
		error = null;
		try {
			const r = await listMyEvents(weekStart.toISOString(), weekEnd.toISOString());
			events = r.events ?? [];
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
			events = [];
		} finally {
			loading = false;
		}
	}

	function mondayOf(base: Date, offsetWeeks: number): Date {
		const d = new Date(base);
		d.setHours(0, 0, 0, 0);
		const wd = d.getDay() === 0 ? 7 : d.getDay(); // воскресенье → 7
		d.setDate(d.getDate() - (wd - 1) + offsetWeeks * 7);
		return d;
	}

	function addDays(d: Date, n: number): Date {
		const r = new Date(d);
		r.setDate(r.getDate() + n);
		return r;
	}

	function formatWeekRange(start: Date, end: Date): string {
		const fmt = new Intl.DateTimeFormat('ru', { day: 'numeric', month: 'short' });
		return `${fmt.format(start)} — ${fmt.format(end)}`;
	}

	// --- Дневной вид: 5 карточек ПН..ПТ с агендой ---

	const DAY_NAMES = ['ПН', 'ВТ', 'СР', 'ЧТ', 'ПТ', 'СБ', 'ВС'];

	interface AgendaEvent {
		id: string;
		title: string;
		startAt: Date;
		endAt: Date;
		kind: TimelineEventKind;
		organizer?: string;
		attendees?: number;
		durationMin: number;
		description?: string;
		status: string;
		timezone?: string;
		category?: string;
	}

	interface DayAgenda {
		name: string; // ПН
		date: Date; // 19 мая
		dateLabel: string; // «19 мая»
		isToday: boolean;
		events: AgendaEvent[];
	}

	const days = $derived(buildAgenda(visibleEvents, weekStart));

	function buildAgenda(evs: CalendarEvent[], monday: Date): DayAgenda[] {
		const today = new Date();
		today.setHours(0, 0, 0, 0);

		const result: DayAgenda[] = DAY_NAMES.map((name, i) => {
			const date = addDays(monday, i);
			const norm = new Date(date);
			norm.setHours(0, 0, 0, 0);
			return {
				name,
				date,
				dateLabel: date.toLocaleDateString('ru', { day: 'numeric', month: 'short' }),
				isToday: norm.getTime() === today.getTime(),
				events: []
			};
		});

		for (const ev of evs) {
			const start = new Date(ev.start_at);
			const end = new Date(ev.end_at);
			const dayDiff = Math.floor((start.getTime() - monday.getTime()) / (1000 * 60 * 60 * 24));
			if (dayDiff < 0 || dayDiff > 6) continue;

			const durMs = end.getTime() - start.getTime();
			result[dayDiff].events.push({
				id: ev.id,
				title: ev.title || 'Без названия',
				startAt: start,
				endAt: end,
				kind: kindFor(ev),
				organizer: ev.organizer,
				attendees: ev.attendees_count,
				durationMin: Math.round(durMs / 60000),
				description: ev.description,
				status: ev.status,
				timezone: ev.timezone,
				category: ev.category
			});
		}

		// Сортировка событий внутри дня по времени.
		for (const d of result) {
			d.events.sort((a, b) => a.startAt.getTime() - b.startAt.getTime());
		}
		return result;
	}

	// kindFor — определяет тип события для бейджей/иконок в таймлайне.
	// Конфликт = пересечение по времени с другим событием того же сотрудника
	// (double-booking). Считается через множество conflictedIDs ниже —
	// чтобы один и тот же event помечался как 'conflict' на всех его
	// отображениях.
	function kindFor(ev: CalendarEvent): TimelineEventKind {
		if (conflictedIDs.has(ev.id)) return 'conflict';
		return ev.attendees_count && ev.attendees_count > 1 ? 'meeting' : 'task';
	}

	// conflictedIDs — множество id событий, которые пересекаются хотя бы
	// с одним другим. Считаем по visibleEvents — те же события, что попали
	// в виджет «Событий за неделю». Если включён фильтр источников —
	// конфликты считаются по тому же набору.
	//
	// overlapTitlesByID — для каждого id-конфликта храним заголовки событий,
	// с которыми он пересекается. Используется для tooltip'а на бейдже,
	// чтобы было видно конкретно с чем накладка.
	const conflictedIDs = $derived.by(() => {
		const out = new Set<string>();
		for (const id of overlapTitlesByID.keys()) out.add(id);
		return out;
	});

	const overlapTitlesByID = $derived.by(() => {
		const out = new Map<string, string[]>();
		const sorted = [...visibleEvents].sort(
			(a, b) => new Date(a.start_at).getTime() - new Date(b.start_at).getTime()
		);
		const add = (id: string, title: string) => {
			const cur = out.get(id);
			if (cur) {
				if (!cur.includes(title)) cur.push(title);
			} else {
				out.set(id, [title]);
			}
		};
		// Sweep: для каждого ev сравниваем с теми что начались раньше и ещё не закончились.
		for (let i = 0; i < sorted.length; i++) {
			const a = sorted[i];
			const aS = new Date(a.start_at).getTime();
			const aE = new Date(a.end_at).getTime();
			for (let j = i + 1; j < sorted.length; j++) {
				const b = sorted[j];
				const bS = new Date(b.start_at).getTime();
				if (bS >= aE) break; // b начинается после конца a → дальше не пересечений с a
				const bE = new Date(b.end_at).getTime();
				if (aS < bE && bS < aE) {
					add(a.id, b.title || 'без названия');
					add(b.id, a.title || 'без названия');
				}
			}
		}
		return out;
	});

	function overlapTooltip(id: string): string {
		const titles = overlapTitlesByID.get(id);
		if (!titles || titles.length === 0) return '';
		if (titles.length === 1) return `Накладывается на «${titles[0]}»`;
		return `Накладывается на: «${titles.join('», «')}»`;
	}

	// overlapBadgeLabel — текст для бейджа конфликта.
	// Если 1 пересечение → «пересекается с «Архитектурный»».
	// Если 2 → «пересекается с «А», «Б»».
	// Если 3+ → «пересекается с «А» и ещё N» (полный список — в tooltip).
	function overlapBadgeLabel(id: string): string {
		const titles = overlapTitlesByID.get(id);
		if (!titles || titles.length === 0) return 'пересекается по времени';
		if (titles.length === 1) return `пересекается с «${titles[0]}»`;
		if (titles.length === 2) return `пересекается с «${titles[0]}», «${titles[1]}»`;
		return `пересекается с «${titles[0]}» и ещё ${titles.length - 1}`;
	}

	function fmtHM(d: Date): string {
		return d.toLocaleTimeString('ru', { hour: '2-digit', minute: '2-digit' });
	}

	function fmtDur(min: number): string {
		if (min < 60) return `${min} мин`;
		const h = Math.floor(min / 60);
		const m = min % 60;
		return m === 0 ? `${h} ч` : `${h} ч ${m} мин`;
	}

	function kindIcon(k: TimelineEventKind): string {
		switch (k) {
			case 'meeting':
				return 'ti-users';
			case 'task':
				return 'ti-checkbox';
			case 'focus':
				return 'ti-bulb';
			case 'conflict':
				return 'ti-alert-triangle';
		}
	}

	function kindBadge(
		k: TimelineEventKind
	): 'info' | 'success' | 'warning' | 'danger' | 'purple' | 'neutral' {
		switch (k) {
			case 'meeting':
				return 'info';
			case 'task':
				return 'neutral';
			case 'focus':
				return 'purple';
			case 'conflict':
				return 'danger';
		}
	}

	function kindLabel(k: TimelineEventKind): string {
		switch (k) {
			case 'meeting':
				return 'Встреча';
			case 'task':
				return 'Задача';
			case 'focus':
				return 'Фокус-время';
			case 'conflict':
				return 'Конфликт';
		}
	}

	function statusLabel(s?: string): string {
		switch (s) {
			case 'confirmed':
				return 'подтверждено';
			case 'tentative':
				return 'не подтверждено';
			case 'cancelled':
				return 'отменено';
			default:
				return s ?? '';
		}
	}

	function fmtDayLong(d: Date): string {
		return d.toLocaleDateString('ru', {
			weekday: 'long',
			day: 'numeric',
			month: 'long'
		});
	}

	function toggleExpanded(id: string) {
		expandedID = expandedID === id ? null : id;
	}

	// --- Inline-редактирование категории встречи ---
	let categorySaving = $state<string | null>(null);

	async function onCategoryChange(eventID: string, category: string) {
		categorySaving = eventID;
		try {
			await setEventCategory(eventID, category);
			// Локально обновим список — без полного перезапроса.
			events = events.map((ev) =>
				ev.id === eventID ? { ...ev, category: category || undefined } : ev
			);
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		} finally {
			categorySaving = null;
		}
	}

	// --- Stat-карточки ---
	// Считаем по видимым событиям — чтобы цифры отражали выбранные источники.

	const eventsCount = $derived(visibleEvents.length);
	const totalHours = $derived(
		visibleEvents.reduce((acc, e) => {
			const ms = new Date(e.end_at).getTime() - new Date(e.start_at).getTime();
			return acc + ms / (1000 * 60 * 60);
		}, 0)
	);
	// Конфликтов = сколько событий пересекаются хотя бы с одним другим.
	// Каждое такое событие считаем один раз, чтобы число читалось как
	// «у меня X встреч в double-booking», а не «X пар пересечений».
	const conflictsCount = $derived(conflictedIDs.size);
</script>

<div class="page-header">
	<div>
		<h1>{greeting}</h1>
		<div class="page-header__subtitle">
			{roleSubtitle} ·
			{new Date().toLocaleDateString('ru', { weekday: 'long', day: 'numeric', month: 'long' })}
		</div>
	</div>
	<div class="page-header__actions">
		<Button icon="ti-refresh" onclick={loadEvents} disabled={loading}>
			{loading ? 'Обновляем…' : 'Обновить'}
		</Button>
	</div>
</div>

<div class="section">
	<IncomingInvitesCard title="Ждут ответа" />
</div>

<div class="section">
	<PulseCheckCard />
</div>

{#if summary && summary.ai_text}
	<div class="section">
		<Card>
			<div class="weekly">
				<div class="weekly__icon">
					<i class="ti ti-sparkles"></i>
				</div>
				<div class="weekly__body">
					<div class="weekly__head">
						<span>Резюме недели</span>
					</div>
					<div class="weekly__text">{@html renderMd(summary.ai_text)}</div>
					{#if summary.generated_by === 'ai'}
						<div class="weekly__footer">
							<i class="ti ti-sparkles"></i>
							<span>Сгенерировано ИИ-ассистентом</span>
						</div>
					{/if}
				</div>
			</div>
		</Card>
	</div>
{:else if summaryLoading}
	<div class="section">
		<Card>
			<div class="text-text-3 text-sm" style="padding: 8px;">Готовлю резюме недели…</div>
		</Card>
	</div>
{/if}

<div class="section">
	<div class="stat-grid">
		<Stat label="Событий за неделю" value={eventsCount.toString()} />
		<Stat label="Часов событий" value={totalHours.toFixed(1)} />
		<Stat label="Конфликтов" value={conflictsCount.toString()} />
		<Stat
			label="Загрузка"
			value={summary && summary.busy_percent ? summary.busy_percent + '%' : '—'}
		/>
	</div>
</div>

{#if error}
	<div class="section">
		<div class="badge badge--danger">
			<i class="ti ti-alert-circle"></i>
			{error}
		</div>
	</div>
{/if}

{#if tasksToday.length > 0}
	<div class="section">
		<Card
			title="Запланировано сегодня"
			subtitle="Задачи из Jira, разложенные планировщиком на сегодняшний день"
		>
			<div class="today-tasks">
				{#each tasksToday as { task, hours } (task.id)}
					<a class="today-task" href="/tasks">
						<span
							class="today-task__dot"
							style="background:{priorityColorTask(task.priority)}"
						></span>
						<span class="today-task__key">{task.source_task_id}</span>
						<span class="today-task__title">{task.title}</span>
						<span class="today-task__hours">{hours} ч</span>
					</a>
				{/each}
			</div>
			<div class="today-tasks__footer">
				<span>Всего на сегодня: <strong>{hoursToday.toFixed(1)} ч</strong></span>
				<a href="/tasks" class="today-tasks__all">Весь план →</a>
			</div>
		</Card>
	</div>
{/if}

<div class="section">
	<div class="flex items-center justify-between" style="margin-bottom: 12px;">
		<div>
			<div class="card__title" style="font-size: 16px;">Эта неделя</div>
			<div class="card__caption">
				{weekLabel}
				{#if !isCurrentWeek}
					<span style="color: var(--info-strong); margin-left: 4px;">
						{weekOffset < 0 ? `(−${-weekOffset} нед.)` : `(+${weekOffset} нед.)`}
					</span>
				{/if}
			</div>
		</div>
		<div class="flex gap-1">
			<Button size="sm" icon="ti-chevron-left" onclick={() => (weekOffset -= 1)}>
				Неделя назад
			</Button>
			<Button size="sm" onclick={() => (weekOffset = 0)} disabled={isCurrentWeek}>
				Сегодня
			</Button>
			<Button size="sm" icon="ti-chevron-right" onclick={() => (weekOffset += 1)}>
				Неделя вперёд
			</Button>
		</div>
	</div>

	{#if loading}
		<Card>
			<div class="text-text-3 text-sm" style="padding: 16px; text-align: center;">Загрузка…</div>
		</Card>
	{:else}
		<!-- Источники данных (Outlook-style): чекбоксы по интеграциям + Workie.
		     Показываем всегда — даже если у пользователя одна Workie, чтобы было
		     понятно куда подцепить внешний календарь. -->
		<div class="sources">
			<span class="sources__label">Источники:</span>
			{#each sources as s (s.key)}
				<label class="sources__item" class:sources__item--off={hiddenSources.has(s.key)}>
					<input
						type="checkbox"
						checked={!hiddenSources.has(s.key)}
						onchange={() => toggleSource(s.key)}
					/>
					<span class="sources__dot" style="background:{s.color}"></span>
					<span class="sources__name">{s.label}</span>
				</label>
			{/each}
			{#if sources.length === 1}
				<a href="/integrations" class="sources__add">
					<i class="ti ti-plus"></i>
					Подключить календарь
				</a>
			{/if}
		</div>

		<!-- Полоска дней недели — кликабельные кнопки. -->
		<div class="day-tabs">
			{#each days as d, i (d.name)}
				<button
					class="day-tab"
					class:day-tab--active={i === selectedDayIdx}
					class:day-tab--today={d.isToday}
					onclick={() => (selectedDayIdx = i)}
					type="button"
				>
					<span class="day-tab__name">{d.name}</span>
					<span class="day-tab__num">{d.date.getDate()}</span>
					{#if d.events.length > 0}
						<span class="day-tab__dots" title="{d.events.length} событий">
							{#each Array(Math.min(d.events.length, 5)) as _, di (di)}
								<span class="day-tab__dot"></span>
							{/each}
							{#if d.events.length > 5}
								<span class="day-tab__more">+{d.events.length - 5}</span>
							{/if}
						</span>
					{/if}
				</button>
			{/each}
		</div>

		<!-- Полный день. -->
		{#if days[selectedDayIdx]}
			{@const sel = days[selectedDayIdx]}
			{@const selTasks = tasksForDate(sel.date)}
			{@const selTaskHours = selTasks.reduce((s, x) => s + x.hours, 0)}
			<Card>
				<div class="day-head">
					<div>
						<div class="card__title" style="font-size: 16px;">
							{sel.name} · {sel.dateLabel}
							{#if sel.isToday}<span class="day-head__today">сегодня</span>{/if}
						</div>
						<div class="card__caption">
							{sel.events.length}
							{sel.events.length === 1
								? 'событие'
								: sel.events.length >= 2 && sel.events.length <= 4
									? 'события'
									: 'событий'}
							{#if selTasks.length > 0}
								· план задач: {selTaskHours.toFixed(1)} ч
							{/if}
						</div>
					</div>
				</div>

				{#if selTasks.length > 0}
					<div class="day-tasks">
						<div class="day-tasks__head">
							<i class="ti ti-checkbox"></i>
							Запланировано из Jira ({selTaskHours.toFixed(1)} ч)
						</div>
						<div class="day-tasks__list">
							{#each selTasks as { task, hours } (task.id)}
								<a class="day-task" href="/tasks">
									<span
										class="day-task__dot"
										style="background:{priorityColorTask(task.priority)}"
									></span>
									<span class="day-task__key">{task.source_task_id}</span>
									<span class="day-task__title">{task.title}</span>
									<span class="day-task__hours">{hours} ч</span>
								</a>
							{/each}
						</div>
					</div>
				{/if}

				{#if sel.events.length === 0}
					{#if selTasks.length === 0}
						<div class="day-empty">Нет событий</div>
					{/if}
				{:else}
					<div class="day-events">
						{#each sel.events as e (e.id)}
							{@const isOpen = expandedID === e.id}
							<div class="ev-full ev-full--{e.kind}" class:ev-full--open={isOpen}>
								<button
									class="ev-full__row"
									onclick={() => toggleExpanded(e.id)}
									type="button"
									aria-expanded={isOpen}
								>
									<div class="ev-full__time">
										<div class="ev-full__start">{fmtHM(e.startAt)}</div>
										<div class="ev-full__dash">→</div>
										<div class="ev-full__end">{fmtHM(e.endAt)}</div>
										<div class="ev-full__dur">{fmtDur(e.durationMin)}</div>
									</div>
									<div class="ev-full__body">
										<div class="ev-full__title">
											<i class="ti {kindIcon(e.kind)}"></i>
											{e.title}
										</div>
										<div class="ev-full__meta">
											{#if e.kind === 'conflict'}
												<span title={overlapTooltip(e.id)}>
													<Badge variant={kindBadge(e.kind)}>{overlapBadgeLabel(e.id)}</Badge>
												</span>
											{/if}
											{#if e.attendees && e.attendees > 1}
												<span class="text-text-3 text-xs">
													<i class="ti ti-users"></i>
													{e.attendees}
												</span>
											{/if}
											{#if e.organizer}
												<span class="text-text-3 text-xs">от {e.organizer}</span>
											{/if}
										</div>
									</div>
									<div class="ev-full__chev">
										<i class="ti {isOpen ? 'ti-chevron-up' : 'ti-chevron-down'}"></i>
									</div>
								</button>

								{#if isOpen}
									<div class="ev-full__details">
										<dl class="ev-full__props">
											<dt>Тип</dt>
											<dd>
												<Badge variant={kindBadge(e.kind)}>{kindLabel(e.kind)}</Badge>
											</dd>

											<dt>Когда</dt>
											<dd>
												{fmtDayLong(e.startAt)} · {fmtHM(e.startAt)}–{fmtHM(e.endAt)}
												<span class="text-text-3"> · {fmtDur(e.durationMin)}</span>
											</dd>

											{#if e.timezone}
												<dt>Часовой пояс</dt>
												<dd>{e.timezone}</dd>
											{/if}

											{#if e.attendees && e.attendees > 1}
												<dt>Участников</dt>
												<dd>{e.attendees}</dd>
											{/if}

											{#if e.organizer}
												<dt>Организатор</dt>
												<dd>{e.organizer}</dd>
											{/if}

											{#if e.status}
												<dt>Статус</dt>
												<dd>{statusLabel(e.status)}</dd>
											{/if}

											{#if e.description}
												<dt>Описание</dt>
												<dd class="ev-full__desc">{e.description}</dd>
											{/if}

											<dt>Тип встречи</dt>
											<dd class="ev-full__cat">
												<select
													value={e.category ?? ''}
													onchange={(ev) => onCategoryChange(e.id, (ev.currentTarget as HTMLSelectElement).value)}
													disabled={categorySaving === e.id}
												>
													<option value="">— определить автоматически —</option>
													{#each MEETING_CATEGORIES as c (c)}
														<option value={c}>{c}</option>
													{/each}
												</select>
												{#if categorySaving === e.id}
													<span class="text-text-3 text-xs">сохраняем…</span>
												{:else if !e.category}
													<span class="text-text-3 text-xs">пока решает AI</span>
												{/if}
											</dd>
										</dl>
									</div>
								{/if}
							</div>
						{/each}
					</div>
				{/if}
			</Card>
		{/if}
	{/if}
</div>

<div class="section">
	<PulseTeamCard />
</div>

<style>
	/* --- Блок задач внутри карточки дня --- */
	.day-tasks {
		margin: 12px 0;
		padding: 10px 12px;
		background: var(--surface);
		border: 0.5px solid var(--border);
		border-radius: var(--radius-md);
	}
	.day-tasks__head {
		display: flex;
		align-items: center;
		gap: 6px;
		font-size: 11px;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.4px;
		color: var(--text-3);
		margin-bottom: 6px;
	}
	.day-tasks__list {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.day-task {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 6px 8px;
		border-radius: 6px;
		text-decoration: none;
		color: inherit;
		transition: background 0.12s;
	}
	.day-task:hover {
		background: var(--surface-2);
	}
	.day-task__dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		flex-shrink: 0;
	}
	.day-task__key {
		font-family: 'JetBrains Mono', monospace;
		font-size: 11px;
		color: var(--text-3);
		padding: 1px 6px;
		background: var(--bg);
		border-radius: 4px;
		flex-shrink: 0;
	}
	.day-task__title {
		flex: 1;
		font-size: 13px;
		color: var(--text);
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.day-task__hours {
		font-size: 12px;
		font-weight: 600;
		color: var(--text-2);
		flex-shrink: 0;
	}

	/* --- Запланировано сегодня --- */
	.today-tasks {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	.today-task {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 8px 10px;
		border-radius: 6px;
		text-decoration: none;
		color: inherit;
		transition: background 0.12s;
	}
	.today-task:hover {
		background: var(--surface);
	}
	.today-task__dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		flex-shrink: 0;
	}
	.today-task__key {
		font-family: 'JetBrains Mono', monospace;
		font-size: 11px;
		color: var(--text-3);
		padding: 1px 6px;
		background: var(--surface);
		border-radius: 4px;
		flex-shrink: 0;
	}
	.today-task__title {
		flex: 1;
		font-size: 13px;
		color: var(--text);
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.today-task__hours {
		font-size: 12px;
		font-weight: 600;
		color: var(--text-2);
		flex-shrink: 0;
	}
	.today-tasks__footer {
		display: flex;
		justify-content: space-between;
		align-items: center;
		padding: 8px 10px;
		margin-top: 6px;
		border-top: 0.5px solid var(--border);
		font-size: 12px;
		color: var(--text-2);
	}
	.today-tasks__all {
		color: var(--info-strong);
		text-decoration: none;
	}
	.today-tasks__all:hover {
		text-decoration: underline;
	}

	/* --- Источники данных (Outlook-style) --- */
	.sources {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px 14px;
		padding: 8px 12px;
		margin-bottom: 12px;
		background: var(--surface);
		border: 0.5px solid var(--border);
		border-radius: var(--radius-md);
	}
	.sources__label {
		font-size: 12px;
		color: var(--text-3);
		font-weight: 500;
	}
	.sources__item {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		cursor: pointer;
		user-select: none;
		font-size: 13px;
		color: var(--text);
		padding: 2px 6px;
		border-radius: 6px;
		transition: background 0.12s, opacity 0.12s;
	}
	.sources__item:hover {
		background: var(--surface-2);
	}
	.sources__item--off {
		opacity: 0.45;
	}
	.sources__item input {
		margin: 0;
		cursor: pointer;
	}
	.sources__dot {
		width: 10px;
		height: 10px;
		border-radius: 50%;
		flex-shrink: 0;
	}
	.sources__name {
		font-weight: 500;
	}
	.sources__add {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		margin-left: auto;
		font-size: 12px;
		color: var(--info-strong);
		text-decoration: none;
		padding: 2px 8px;
		border-radius: 6px;
		transition: background 0.12s;
	}
	.sources__add:hover {
		background: var(--info-bg);
	}

	/* --- Полоска дней недели --- */
	.day-tabs {
		display: grid;
		grid-template-columns: repeat(7, minmax(0, 1fr));
		gap: 6px;
		margin-bottom: 12px;
	}
	@media (max-width: 700px) {
		.day-tabs {
			gap: 4px;
		}
	}
	.day-tab {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 2px;
		padding: 8px 4px 16px; /* нижний отступ — под точки */
		border-radius: var(--radius-md);
		border: 0.5px solid var(--border);
		background: var(--surface);
		cursor: pointer;
		font-family: inherit;
		color: var(--text-2);
		transition: background 0.12s, border-color 0.12s, transform 0.05s;
		position: relative;
	}
	.day-tab:hover {
		background: var(--surface-2);
	}
	.day-tab:active {
		transform: scale(0.98);
	}
	.day-tab__name {
		font-size: 11px;
		font-weight: 500;
		text-transform: uppercase;
		letter-spacing: 0.4px;
		color: var(--text-3);
	}
	.day-tab__num {
		font-size: 20px;
		font-weight: 600;
		font-variant-numeric: tabular-nums;
		line-height: 1.1;
	}
	.day-tab__dots {
		position: absolute;
		bottom: 6px;
		display: flex;
		align-items: center;
		gap: 3px;
	}
	.day-tab__dot {
		width: 4px;
		height: 4px;
		border-radius: 50%;
		background: var(--info-strong);
	}
	.day-tab__more {
		font-size: 9px;
		font-weight: 600;
		color: var(--info-strong);
		line-height: 1;
		margin-left: 2px;
	}
	.day-tab--today {
		border-color: var(--info-strong);
	}
	.day-tab--today .day-tab__num {
		color: var(--info-strong);
	}
	.day-tab--active {
		background: var(--info-strong);
		color: #fff;
		border-color: var(--info-strong);
	}
	.day-tab--active .day-tab__name,
	.day-tab--active .day-tab__num {
		color: #fff;
	}
	.day-tab--active .day-tab__dot {
		background: #fff;
	}
	.day-tab--active .day-tab__more {
		color: #fff;
	}

	/* --- Выбранный день — большая карточка --- */
	.day-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		margin-bottom: 16px;
		padding-bottom: 12px;
		border-bottom: 0.5px solid var(--border);
	}
	.day-head__today {
		font-size: 11px;
		font-weight: 500;
		text-transform: uppercase;
		letter-spacing: 0.4px;
		color: var(--info-strong);
		margin-left: 8px;
	}
	.day-empty {
		padding: 32px 0;
		text-align: center;
		color: var(--text-3);
		font-size: 13px;
	}
	.day-events {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}

	.ev-full {
		border-radius: var(--radius-md);
		border-left: 3px solid var(--border-2);
		background: var(--surface-2);
		overflow: hidden;
		transition: background 0.12s;
	}
	.ev-full--open {
		background: var(--surface);
		box-shadow: 0 0 0 0.5px var(--border-2) inset;
	}
	.ev-full--meeting {
		border-left-color: var(--info-strong);
	}
	.ev-full--task {
		border-left-color: var(--text-3);
	}
	.ev-full--focus {
		border-left-color: var(--purple-text);
	}
	.ev-full--conflict {
		border-left-color: var(--danger-strong);
		background: var(--danger-bg);
	}

	.ev-full__row {
		display: grid;
		grid-template-columns: 180px 1fr auto;
		gap: 16px;
		padding: 12px 14px;
		width: 100%;
		text-align: left;
		background: transparent;
		border: none;
		cursor: pointer;
		font-family: inherit;
		color: inherit;
		align-items: start;
	}
	.ev-full__row:hover {
		background: rgba(0, 0, 0, 0.025);
	}
	@media (max-width: 600px) {
		.ev-full__row {
			grid-template-columns: 1fr;
			gap: 6px;
		}
	}

	.ev-full__time {
		display: flex;
		align-items: baseline;
		gap: 6px;
		flex-wrap: wrap;
		font-variant-numeric: tabular-nums;
	}
	.ev-full__start,
	.ev-full__end {
		font-size: 15px;
		font-weight: 600;
	}
	.ev-full__dash {
		color: var(--text-3);
	}
	.ev-full__dur {
		font-size: 12px;
		color: var(--text-3);
		margin-left: 4px;
	}

	.ev-full__title {
		font-size: 14px;
		font-weight: 500;
		display: flex;
		align-items: flex-start;
		gap: 6px;
		line-height: 1.35;
	}
	.ev-full__title i {
		color: var(--text-3);
		font-size: 15px;
		flex-shrink: 0;
		margin-top: 2px;
	}
	.ev-full__meta {
		margin-top: 6px;
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 10px;
	}
	.ev-full__meta i {
		margin-right: 3px;
	}
	.ev-full__chev {
		color: var(--text-3);
		font-size: 14px;
		align-self: center;
	}

	/* Details — раскрытый блок */
	.ev-full__details {
		padding: 4px 14px 14px 14px;
		border-top: 0.5px solid var(--border);
		margin-top: 0;
	}
	.ev-full__props {
		display: grid;
		grid-template-columns: 140px 1fr;
		gap: 6px 14px;
		margin: 10px 0 0;
		font-size: 13px;
		line-height: 1.5;
	}
	.ev-full__props dt {
		color: var(--text-3);
		font-size: 12px;
		text-transform: uppercase;
		letter-spacing: 0.4px;
		padding-top: 2px;
	}
	.ev-full__props dd {
		margin: 0;
		color: var(--text);
	}
	.ev-full__desc {
		white-space: pre-wrap;
		color: var(--text-2);
		font-size: 13px;
	}
	@media (max-width: 600px) {
		.ev-full__props {
			grid-template-columns: 1fr;
			gap: 4px;
		}
		.ev-full__props dt {
			padding-top: 6px;
		}
	}

	/* Карточка резюме недели */
	.weekly {
		display: flex;
		gap: 14px;
	}
	.weekly__icon {
		width: 40px;
		height: 40px;
		border-radius: var(--radius-md);
		background: var(--info-bg);
		color: var(--info-strong);
		display: flex;
		align-items: center;
		justify-content: center;
		font-size: 20px;
		flex-shrink: 0;
	}
	.weekly__body {
		flex: 1;
		min-width: 0;
	}
	.weekly__head {
		display: flex;
		align-items: center;
		gap: 8px;
		font-size: 12px;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.4px;
		color: var(--text-2);
		margin-bottom: 4px;
	}
	.weekly__text {
		font-size: 14px;
		line-height: 1.55;
		color: var(--text);
	}
	.weekly__text :global(p) {
		margin: 0 0 6px;
	}
	.weekly__text :global(p:last-child) {
		margin-bottom: 0;
	}
	.weekly__footer {
		display: flex;
		align-items: center;
		gap: 6px;
		margin-top: 10px;
		padding-top: 8px;
		border-top: 1px dashed var(--border);
		font-size: 11px;
		color: var(--text-3);
	}
	.weekly__footer i {
		font-size: 13px;
		color: var(--info-strong);
	}
	.weekly__text :global(strong) {
		font-weight: 600;
	}
</style>
