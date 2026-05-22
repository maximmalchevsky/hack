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
		void loadEvents();
		void loadSummary();
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

	const days = $derived(buildAgenda(events, weekStart));

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

	function kindFor(ev: CalendarEvent): TimelineEventKind {
		const startH = new Date(ev.start_at).getHours();
		const endH = new Date(ev.end_at).getHours();
		if (startH < 8 || endH > 20) return 'conflict';
		return ev.attendees_count && ev.attendees_count > 1 ? 'meeting' : 'task';
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

	const eventsCount = $derived(events.length);
	const totalHours = $derived(
		events.reduce((acc, e) => {
			const ms = new Date(e.end_at).getTime() - new Date(e.start_at).getTime();
			return acc + ms / (1000 * 60 * 60);
		}, 0)
	);
	const conflictsCount = $derived(events.filter((e) => kindFor(e) === 'conflict').length);
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
					<div class="weekly__footer">
						{#if summary.generated_by === 'ai'}
							<i class="ti ti-sparkles"></i>
							<span>Сгенерировано GigaChat</span>
						{:else}
							<i class="ti ti-template"></i>
							<span>Текст по шаблону</span>
						{/if}
					</div>
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
						</div>
					</div>
				</div>

				{#if sel.events.length === 0}
					<div class="day-empty">Нет событий</div>
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
												<Badge variant={kindBadge(e.kind)}>вне рабочих часов</Badge>
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
