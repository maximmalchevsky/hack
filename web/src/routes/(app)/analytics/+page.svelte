<script lang="ts">
	import { onMount } from 'svelte';
	import Card from '$lib/components/Card.svelte';
	import Stat from '$lib/components/Stat.svelte';
	import Badge from '$lib/components/Badge.svelte';
	import EChart from '$lib/components/EChart.svelte';
	import Tabs from '$lib/components/Tabs.svelte';
	import { user } from '$lib/stores/user';
	import {
		// Company
		getOverview,
		getRiskByTeam,
		getConflictsByWeekday,
		getFreshnessTrend,
		getGroupsDistribution,
		getLeaderboard,
		getAnomalies,
		getForecast,
		type TeamScore,
		type Anomaly,
		type ConflictForecast,
		// Me
		getMeOverview,
		getMeTrend,
		getMeConflictsByWeekday,
		getMeHoursByWeek,
		// Teams
		getTeamsMy,
		getTeamsOverview,
		getTeamsRiskByTeam,
		getTeamsConflictsByWeekday,
		getTeamsFreshnessTrend,
		getTeamsGroupsDistribution,
		type OverviewKPI,
		type TeamRisk,
		type WeekdayConflicts,
		type WeekFreshness,
		type GroupSlice,
		type MeOverview,
		type MeTrendPoint,
		type MeHoursWeek,
		type TeamRef,
		type TeamScopeOverview
	} from '$lib/api/analytics';
	import { ApiError } from '$lib/api/client';
	import ViewPresetsBar from '$lib/components/ViewPresetsBar.svelte';

	type Tab = 'me' | 'teams' | 'company';
	const dayNames = ['ПН', 'ВТ', 'СР', 'ЧТ', 'ПТ', 'СБ', 'ВС'];

	// --- Активный таб ---
	let activeTab = $state<Tab>('me');
	let initialized = $state(false);

	// --- Видимость табов по роли ---
	const role = $derived($user?.role ?? 'employee');
	const canSeeCompany = $derived(
		role === 'admin' || role === 'hr' || role === 'pm' || role === 'analyst'
	);
	const canSeeTeams = $derived(
		role === 'manager' || role === 'pm' || role === 'hr' || role === 'admin'
	);

	// --- Состояние «Моя» ---
	let meOverview = $state<MeOverview | null>(null);
	let meTrend = $state<MeTrendPoint[]>([]);
	let meWeekdays = $state<WeekdayConflicts[]>([]);
	let meHours = $state<MeHoursWeek[]>([]);
	let meLoading = $state(false);
	let meLoaded = $state(false);
	let meError = $state<string | null>(null);

	// --- Состояние «Команды» ---
	let myTeams = $state<TeamRef[]>([]);
	let selectedTeamId = $state<string>(''); // '' = все мои команды
	let teamsOv = $state<TeamScopeOverview | null>(null);
	let teamsRisk = $state<TeamRisk[]>([]);
	let teamsWd = $state<WeekdayConflicts[]>([]);
	let teamsTrend = $state<WeekFreshness[]>([]);
	let teamsGroups = $state<GroupSlice[]>([]);
	let teamsLoading = $state(false);
	let teamsLoaded = $state(false);
	let teamsError = $state<string | null>(null);

	// --- Состояние «Компания» ---
	let companyOv = $state<OverviewKPI | null>(null);
	let companyTeams = $state<TeamRisk[]>([]);
	let companyWeekdays = $state<WeekdayConflicts[]>([]);
	let companyTrend = $state<WeekFreshness[]>([]);
	let companyGroups = $state<GroupSlice[]>([]);
	let companyLeaderboard = $state<TeamScore[]>([]);
	let companyAnomalies = $state<Anomaly[]>([]);
	let companyForecast = $state<ConflictForecast[]>([]);
	let companyLoading = $state(false);
	let companyLoaded = $state(false);
	let companyError = $state<string | null>(null);

	// --- Init: грузим список моих команд (для таба «Команды»), потом выбираем дефолтный таб ---
	onMount(async () => {
		// Сначала пробуем загрузить «мои команды» — нужно для решения, показывать ли таб.
		if (canSeeTeams) {
			try {
				const r = await getTeamsMy();
				myTeams = r.teams ?? [];
			} catch (e) {
				// 403 для роли, у которой нет owner-команд — норм, просто скрываем таб.
				myTeams = [];
			}
		}
		// Выбираем дефолтный таб.
		if (role === 'manager' && myTeams.length > 0) {
			activeTab = 'teams';
		} else if (canSeeCompany) {
			activeTab = 'company';
		} else {
			activeTab = 'me';
		}
		initialized = true;
		void ensureLoaded(activeTab);
	});

	// --- Доступные табы ---
	const visibleTabs = $derived(buildTabs());

	function buildTabs() {
		const items: { id: string; label: string; icon?: string }[] = [
			{ id: 'me', label: 'Моя', icon: 'ti-user' }
		];
		if (canSeeTeams && myTeams.length > 0) {
			items.push({ id: 'teams', label: 'Команды', icon: 'ti-users' });
		}
		if (canSeeCompany) {
			items.push({ id: 'company', label: 'Компания', icon: 'ti-building' });
		}
		return items;
	}

	async function setTab(id: string) {
		const t = id as Tab;
		activeTab = t;
		await ensureLoaded(t);
	}

	async function ensureLoaded(t: Tab) {
		if (t === 'me' && !meLoaded) await loadMe();
		if (t === 'teams' && !teamsLoaded) await loadTeams();
		if (t === 'company' && !companyLoaded) await loadCompany();
	}

	// --- Loaders ---

	async function loadMe() {
		meLoading = true;
		meError = null;
		try {
			const [ov, tr, wd, hw] = await Promise.all([
				getMeOverview(),
				getMeTrend(),
				getMeConflictsByWeekday(),
				getMeHoursByWeek()
			]);
			meOverview = ov;
			meTrend = tr.weeks ?? [];
			meWeekdays = wd.days ?? [];
			meHours = hw.weeks ?? [];
			meLoaded = true;
		} catch (e) {
			meError = e instanceof ApiError ? e.message : String(e);
		} finally {
			meLoading = false;
		}
	}

	async function loadTeams() {
		teamsLoading = true;
		teamsError = null;
		const teamId = selectedTeamId || undefined;
		try {
			const [ov, risk, wd, tr, gd] = await Promise.all([
				getTeamsOverview(teamId),
				getTeamsRiskByTeam(),
				getTeamsConflictsByWeekday(teamId),
				getTeamsFreshnessTrend(teamId),
				getTeamsGroupsDistribution(teamId)
			]);
			teamsOv = ov;
			teamsRisk = risk.teams ?? [];
			teamsWd = wd.days ?? [];
			teamsTrend = tr.weeks ?? [];
			teamsGroups = gd.groups ?? [];
			teamsLoaded = true;
		} catch (e) {
			teamsError = e instanceof ApiError ? e.message : String(e);
		} finally {
			teamsLoading = false;
		}
	}

	async function reloadTeamsForScope() {
		teamsLoaded = false;
		await loadTeams();
	}

	async function loadCompany() {
		companyLoading = true;
		companyError = null;
		try {
			const [ov, tb, wd, ft, gd, lb, an, fc] = await Promise.all([
				getOverview(),
				getRiskByTeam(),
				getConflictsByWeekday(),
				getFreshnessTrend(),
				getGroupsDistribution(),
				getLeaderboard(),
				getAnomalies(),
				getForecast()
			]);
			companyOv = ov;
			companyTeams = tb.teams ?? [];
			companyWeekdays = wd.days ?? [];
			companyTrend = ft.weeks ?? [];
			companyGroups = gd.groups ?? [];
			companyLeaderboard = lb.teams ?? [];
			companyAnomalies = an.anomalies ?? [];
			companyForecast = fc.forecast ?? [];
			companyLoaded = true;
		} catch (e) {
			companyError = e instanceof ApiError ? e.message : String(e);
		} finally {
			companyLoading = false;
		}
	}

	// --- ECharts options ---

	// Личная: тренд A/L
	const meTrendOption = $derived({
		grid: { top: 30, right: 20, bottom: 30, left: 40, containLabel: true },
		tooltip: {
			trigger: 'axis',
			valueFormatter: (v: number) => v.toFixed(2)
		},
		legend: { top: 0, right: 0, textStyle: { fontSize: 11 } },
		xAxis: { type: 'category', data: meTrend.map((w) => fmtWeek(w.week_start)) },
		yAxis: { type: 'value', min: 0, max: 1.5 },
		series: [
			{
				name: 'Актуальность A',
				type: 'line',
				smooth: true,
				symbolSize: 7,
				data: meTrend.map((w) => w.avg_a),
				lineStyle: { width: 3, color: '#3b82f6' },
				itemStyle: { color: '#3b82f6' }
			},
			{
				name: 'Загрузка L',
				type: 'line',
				smooth: true,
				symbolSize: 7,
				data: meTrend.map((w) => w.avg_l),
				lineStyle: { width: 3, color: '#f59e0b' },
				itemStyle: { color: '#f59e0b' }
			}
		]
	});

	// Личная: конфликты по дням
	const meWeekdayOption = $derived(buildWeekdayOption(meWeekdays));

	// Личная: часы по неделям
	const meHoursOption = $derived({
		grid: { top: 30, right: 20, bottom: 30, left: 40, containLabel: true },
		tooltip: {
			trigger: 'axis',
			valueFormatter: (v: number) => `${v.toFixed(1)} ч`
		},
		xAxis: { type: 'category', data: meHours.map((w) => fmtWeek(w.week_start)) },
		yAxis: { type: 'value', axisLabel: { formatter: '{value} ч' } },
		series: [
			{
				name: 'Часы встреч',
				type: 'bar',
				data: meHours.map((w) => +w.hours.toFixed(1)),
				itemStyle: { color: '#22c55e', borderRadius: [6, 6, 0, 0] },
				barWidth: 28
			}
		]
	});

	// Команды: те же 4 чарта (тренд / weekday / risk-by-team / groups)
	const teamsTrendOption = $derived({
		grid: { top: 30, right: 20, bottom: 30, left: 40, containLabel: true },
		tooltip: { trigger: 'axis', valueFormatter: (v: number) => v.toFixed(2) },
		xAxis: { type: 'category', data: teamsTrend.map((w) => fmtWeek(w.week_start)) },
		yAxis: { type: 'value', min: 0, max: 1 },
		series: [
			{
				name: 'Средняя A',
				type: 'line',
				smooth: true,
				symbolSize: 8,
				data: teamsTrend.map((w) => w.avg_a),
				lineStyle: { width: 3, color: '#3b82f6' },
				itemStyle: { color: '#3b82f6' },
				areaStyle: { color: 'rgba(59,130,246,0.12)' }
			}
		]
	});
	const teamsWeekdayOption = $derived(buildWeekdayOption(teamsWd));
	const teamsRiskOption = $derived(buildRiskOption(teamsRisk));
	const teamsGroupsOption = $derived(buildGroupsOption(teamsGroups));

	// Компания: чарты как раньше
	const companyTrendOption = $derived({
		grid: { top: 30, right: 20, bottom: 30, left: 40, containLabel: true },
		tooltip: { trigger: 'axis', valueFormatter: (v: number) => v.toFixed(2) },
		xAxis: { type: 'category', data: companyTrend.map((w) => fmtWeek(w.week_start)) },
		yAxis: { type: 'value', min: 0, max: 1 },
		series: [
			{
				name: 'Средняя актуальность A',
				type: 'line',
				smooth: true,
				symbolSize: 8,
				data: companyTrend.map((w) => w.avg_a),
				lineStyle: { width: 3, color: '#3b82f6' },
				itemStyle: { color: '#3b82f6' },
				areaStyle: { color: 'rgba(59,130,246,0.12)' }
			}
		]
	});
	const companyWeekdayOption = $derived(buildWeekdayOption(companyWeekdays));
	const companyTeamsOption = $derived(buildRiskOption(companyTeams));
	const companyGroupsOption = $derived(buildGroupsOption(companyGroups));

	// --- Helpers ---

	function buildWeekdayOption(days: WeekdayConflicts[]) {
		return {
			grid: { top: 30, right: 20, bottom: 30, left: 40, containLabel: true },
			tooltip: { trigger: 'axis' },
			xAxis: { type: 'category', data: dayNames },
			yAxis: { type: 'value' },
			series: [
				{
					name: 'Конфликтов',
					type: 'bar',
					data: days.map((w) => w.count),
					itemStyle: {
						color: (params: { dataIndex: number }) => {
							const colors = [
								'#3b82f6',
								'#3b82f6',
								'#3b82f6',
								'#3b82f6',
								'#3b82f6',
								'#ef4444',
								'#ef4444'
							];
							return colors[params.dataIndex];
						},
						borderRadius: [6, 6, 0, 0]
					},
					barWidth: 28
				}
			]
		};
	}

	function buildRiskOption(teams: TeamRisk[]) {
		return {
			grid: { top: 30, right: 60, bottom: 30, left: 100, containLabel: true },
			tooltip: {
				trigger: 'axis',
				axisPointer: { type: 'shadow' },
				valueFormatter: (v: number) => v.toFixed(2)
			},
			xAxis: { type: 'value', min: 0, max: 1 },
			yAxis: { type: 'category', data: teams.map((t) => t.team_name) },
			series: [
				{
					name: 'Риск R',
					type: 'bar',
					data: teams.map((t) => ({
						value: t.avg_r,
						itemStyle: {
							color: t.avg_r > 0.5 ? '#ef4444' : t.avg_r > 0.25 ? '#f59e0b' : '#22c55e',
							borderRadius: [0, 6, 6, 0]
						}
					})),
					barWidth: 22,
					// Чтобы при R=0 столбик всё-таки был видимым и не превращался в пустое место.
					barMinHeight: 3,
					label: {
						show: true,
						position: 'right',
						formatter: (p: { value: number }) => p.value.toFixed(2),
						color: '#475569',
						fontSize: 12,
						fontWeight: 600
					}
				}
			]
		};
	}

	function buildGroupsOption(groups: GroupSlice[]) {
		return {
			tooltip: { trigger: 'item' },
			legend: { bottom: 0, textStyle: { fontSize: 11 } },
			series: [
				{
					name: 'Сотрудники',
					type: 'pie',
					radius: ['45%', '70%'],
					avoidLabelOverlap: false,
					label: { show: false },
					labelLine: { show: false },
					data: groups.map((g) => ({
						value: g.count,
						name: ruGroup(g.group),
						itemStyle: { color: groupColor(g.group) }
					}))
				}
			]
		};
	}

	function ruGroup(g: string): string {
		return (
			{ fresh: 'Актуальные', needs_confirm: 'Подтвердить', stale: 'Устаревшие', unknown: 'Без данных' }[
				g
			] ?? g
		);
	}
	function groupColor(g: string): string {
		return (
			{ fresh: '#22c55e', needs_confirm: '#f59e0b', stale: '#ef4444', unknown: '#94a3b8' }[g] ??
			'#94a3b8'
		);
	}
	function fmtWeek(iso: string): string {
		try {
			return new Date(iso).toLocaleDateString('ru', { day: 'numeric', month: 'short' });
		} catch {
			return iso;
		}
	}

	function fmtDays(d: number): string {
		if (d < 0) return '—';
		if (d === 0) return 'сегодня';
		if (d === 1) return '1 день';
		const mod10 = d % 10;
		const mod100 = d % 100;
		if (mod10 === 1 && mod100 !== 11) return `${d} день`;
		if ([2, 3, 4].includes(mod10) && ![12, 13, 14].includes(mod100)) return `${d} дня`;
		return `${d} дней`;
	}

	// --- Company-инсайты (как раньше) ---
	const companyInsights = $derived(
		buildCompanyInsights(companyOv, companyTeams, companyTrend, companyWeekdays)
	);

	function buildCompanyInsights(
		ov: OverviewKPI | null,
		t: TeamRisk[],
		tr: WeekFreshness[],
		wd: WeekdayConflicts[]
	): string[] {
		const out: string[] = [];
		if (!ov) return out;
		if (ov.stale_profiles > 0) {
			out.push(`Устаревших профилей: **${ov.stale_profiles}**. Рекомендуется разослать запросы.`);
		}
		if (ov.conflicts_7d > 5) {
			out.push(
				`За неделю **${ov.conflicts_7d}** событий вне графика — возможно, нужно пересмотреть графики.`
			);
		}
		const worstTeam = t[0];
		if (worstTeam && worstTeam.avg_r > 0.4) {
			out.push(
				`Команда **${worstTeam.team_name}** в зоне риска: средний R=${worstTeam.avg_r.toFixed(2)}.`
			);
		}
		if (tr.length >= 2) {
			const first = tr[0].avg_a;
			const last = tr[tr.length - 1].avg_a;
			const diff = last - first;
			if (Math.abs(diff) >= 0.05) {
				const dir = diff > 0 ? 'выросла' : 'упала';
				out.push(
					`Средняя актуальность профилей ${dir} с ${first.toFixed(2)} до ${last.toFixed(2)} за 8 недель.`
				);
			}
		}
		let maxIdx = -1;
		let maxCnt = 0;
		for (let i = 0; i < wd.length; i++) {
			if (wd[i].count > maxCnt) {
				maxCnt = wd[i].count;
				maxIdx = i;
			}
		}
		if (maxIdx >= 0 && maxCnt >= 3) {
			out.push(`Больше всего конфликтов происходит по **${dayNames[maxIdx]}** — ${maxCnt} событий.`);
		}
		if (ov.on_vacation_now > 0) {
			out.push(`Сейчас в отпуске/командировке: **${ov.on_vacation_now}** сотрудник(а/ов).`);
		}
		return out;
	}

	// --- Личные инсайты ---
	const meInsights = $derived(buildMeInsights(meOverview, meTrend, meWeekdays));

	function buildMeInsights(
		ov: MeOverview | null,
		tr: MeTrendPoint[],
		wd: WeekdayConflicts[]
	): string[] {
		const out: string[] = [];
		if (!ov) return out;
		if (ov.days_since_update >= 60) {
			out.push(
				`Профиль не обновлялся **${fmtDays(ov.days_since_update)}**. Стоит проверить актуальность графика.`
			);
		} else if (ov.days_since_update === -1) {
			out.push('Профиль ни разу не обновлялся — заполни рабочие часы на странице «Мой профиль».');
		}
		if (ov.avg_l > 0.8) {
			out.push(`Загрузка **${(ov.avg_l * 100).toFixed(0)}%** — высокая. Стоит снизить число встреч.`);
		} else if (ov.avg_l > 0 && ov.avg_l < 0.2) {
			out.push(
				`Загрузка **${(ov.avg_l * 100).toFixed(0)}%** — хорошее время для фокус-работы.`
			);
		}
		if (ov.conflicts_30d > 5) {
			out.push(
				`За 30 дней **${ov.conflicts_30d}** событий вне графика — возможно, график сдвинулся.`
			);
		}
		let maxIdx = -1;
		let maxCnt = 0;
		for (let i = 0; i < wd.length; i++) {
			if (wd[i].count > maxCnt) {
				maxCnt = wd[i].count;
				maxIdx = i;
			}
		}
		if (maxIdx >= 0 && maxCnt >= 2) {
			out.push(
				`Чаще всего встречи вне графика по **${dayNames[maxIdx]}** — ${maxCnt} ${
					maxCnt === 1 ? 'событие' : 'события'
				}.`
			);
		}
		if (tr.length >= 2) {
			const first = tr[0].avg_a;
			const last = tr[tr.length - 1].avg_a;
			if (last < first - 0.1) {
				out.push(
					`Актуальность профиля упала с ${first.toFixed(2)} до ${last.toFixed(2)} за 8 недель.`
				);
			}
		}
		return out;
	}
</script>

<div class="page-header">
	<div>
		<h1>Аналитика</h1>
		<div class="page-header__subtitle">
			{#if activeTab === 'me'}
				Личная сводка: твои метрики, конфликты и загрузка.
			{:else if activeTab === 'teams'}
				Команды под твоим управлением: риск, конфликты, актуальность.
			{:else}
				Общая картина по сотрудникам, командам и динамике метрик.
			{/if}
		</div>
	</div>
	<div>
		<ViewPresetsBar
			page="analytics"
			currentFilters={() => ({ tab: activeTab, team_id: selectedTeamId })}
			onApply={(f) => {
				const t = (f.tab as Tab) ?? activeTab;
				if (t === 'me' || t === 'teams' || t === 'company') setTab(t);
				const tid = (f.team_id as string) ?? '';
				selectedTeamId = tid;
			}}
		/>
	</div>
</div>

{#if initialized && visibleTabs.length > 1}
	<div class="section">
		<Tabs tabs={visibleTabs} value={activeTab} onChange={setTab} />
	</div>
{/if}

<!-- ===================== Tab: Моя ===================== -->
{#if activeTab === 'me'}
	{#if meError}
		<div class="section">
			<Badge variant="danger"><i class="ti ti-alert-circle"></i>{meError}</Badge>
		</div>
	{/if}
	{#if meLoading && !meOverview}
		<div class="text-text-3 text-sm">Загрузка…</div>
	{:else if meOverview}
		<div class="section">
			<div class="stat-grid">
				<Stat label="Моя актуальность" metricLetter="A" value={meOverview.avg_a.toFixed(2)} />
				<Stat label="Мой риск" metricLetter="R" value={meOverview.avg_r.toFixed(2)} />
				<Stat label="Моя загрузка" metricLetter="L" value={meOverview.avg_l.toFixed(2)} />
				<Stat label="Профиль обновлён" value={fmtDays(meOverview.days_since_update)} />
				<Stat label="События за 7 дн" value={String(meOverview.events_7d)} />
				<Stat label="Часы за 7 дн" value={meOverview.hours_7d.toFixed(1)} />
				<Stat label="Конфликтов за 30 дн" value={String(meOverview.conflicts_30d)} />
			</div>
		</div>

		{#if meInsights.length > 0}
			<div class="section">
				<Card>
					<div class="insights">
						<div class="insights__head">
							<i class="ti ti-sparkles"></i>
							<span>Главное в твоих цифрах</span>
						</div>
						<ul class="insights__list">
							{#each meInsights as txt, i (i)}
								<li>{@html txt.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>')}</li>
							{/each}
						</ul>
					</div>
				</Card>
			</div>
		{/if}

		<div class="section grid-2" style="gap: 16px;">
			<Card title="Динамика A и L за 8 недель" subtitle="Актуальность и загрузка по неделям">
				<EChart option={meTrendOption} height="280px" />
			</Card>
			<Card title="Мои конфликты по дням недели" subtitle="События вне графика за 30 дней">
				<EChart option={meWeekdayOption} height="280px" />
			</Card>
		</div>

		<div class="section" style="margin-top: 16px;">
			<Card title="Часы встреч по неделям" subtitle="8 недель назад → сейчас">
				<EChart option={meHoursOption} height="280px" />
			</Card>
		</div>
	{/if}
{/if}

<!-- ===================== Tab: Команды ===================== -->
{#if activeTab === 'teams'}
	{#if teamsError}
		<div class="section">
			<Badge variant="danger"><i class="ti ti-alert-circle"></i>{teamsError}</Badge>
		</div>
	{/if}
	{#if myTeams.length === 0}
		<div class="section">
			<Card>
				<div class="empty-state">
					<i class="ti ti-users-off"></i>
					<div>У тебя пока нет команд, которыми ты управляешь.</div>
				</div>
			</Card>
		</div>
	{:else}
		<div class="section scope-bar">
			<label for="team-select" class="scope-bar__label">Команда:</label>
			<select
				id="team-select"
				class="scope-bar__select"
				bind:value={selectedTeamId}
				onchange={reloadTeamsForScope}
			>
				<option value="">Все мои команды ({myTeams.length})</option>
				{#each myTeams as t (t.id)}
					<option value={t.id}>{t.name} ({t.members})</option>
				{/each}
			</select>
		</div>

		{#if teamsLoading && !teamsOv}
			<div class="text-text-3 text-sm">Загрузка…</div>
		{:else if teamsOv}
			<div class="section">
				<div class="stat-grid">
					<Stat label="Сотрудников" value={String(teamsOv.employees)} />
					<Stat label="Средняя актуальность" metricLetter="A" value={teamsOv.avg_a.toFixed(2)} />
					<Stat label="Средний риск" metricLetter="R" value={teamsOv.avg_r.toFixed(2)} />
					<Stat label="Средняя загрузка" metricLetter="L" value={teamsOv.avg_l.toFixed(2)} />
					<Stat label="Конфликтов за 7 дн" value={String(teamsOv.conflicts_7d)} />
					<Stat label="Устаревших" value={String(teamsOv.stale_profiles)} />
					<Stat label="Подтвердить" value={String(teamsOv.needs_confirm)} />
					<Stat label="В отпуске сейчас" value={String(teamsOv.on_vacation_now)} />
				</div>
			</div>

			<div class="section grid-2" style="gap: 16px;">
				<Card title="Динамика A за 8 недель" subtitle="Актуальность профилей по неделям — по выбранным командам" metricLetter="A">
					<EChart option={teamsTrendOption} height="280px" />
				</Card>
				<Card title="Конфликты по дням недели" subtitle="За 30 дней">
					<EChart option={teamsWeekdayOption} height="280px" />
				</Card>
			</div>

			<div class="section grid-2" style="gap: 16px; margin-top: 16px;">
				<Card title="Риск по моим командам" subtitle="Средний интегральный риск каждой команды (шкала 0–1)" metricLetter="R">
					{#if teamsRisk.length === 0}
						<div class="text-text-3 text-sm" style="padding: 16px; text-align: center;">
							Нет данных.
						</div>
					{:else}
						<EChart option={teamsRiskOption} height={teamsRisk.length * 36 + 60 + 'px'} />
					{/if}
				</Card>
				<Card title="Распределение по группам" subtitle="Сколько актуальных, сколько устаревших">
					<EChart option={teamsGroupsOption} height="300px" />
				</Card>
			</div>
		{/if}
	{/if}
{/if}

<!-- ===================== Tab: Компания ===================== -->
{#if activeTab === 'company'}
	{#if companyError}
		<div class="section">
			<Badge variant="danger"><i class="ti ti-alert-circle"></i>{companyError}</Badge>
		</div>
	{/if}
	{#if companyLoading && !companyOv}
		<div class="text-text-3 text-sm">Загрузка…</div>
	{:else if companyOv}
		<div class="section">
			<div class="stat-grid">
				<Stat label="Сотрудников" value={String(companyOv.employees)} />
				<Stat label="Средняя актуальность" metricLetter="A" value={companyOv.avg_a.toFixed(2)} />
				<Stat label="Средний риск" metricLetter="R" value={companyOv.avg_r.toFixed(2)} />
				<Stat label="Конфликтов за 7 дн" value={String(companyOv.conflicts_7d)} />
				<Stat label="Устаревших" value={String(companyOv.stale_profiles)} />
				<Stat label="Подтвердить" value={String(companyOv.needs_confirm)} />
				<Stat label="В отпуске сейчас" value={String(companyOv.on_vacation_now)} />
				<Stat label="Средняя загрузка" metricLetter="L" value={companyOv.avg_l.toFixed(2)} />
			</div>
		</div>

		{#if companyInsights.length > 0}
			<div class="section">
				<Card>
					<div class="insights">
						<div class="insights__head">
							<i class="ti ti-sparkles"></i>
							<span>Главное в цифрах</span>
						</div>
						<ul class="insights__list">
							{#each companyInsights as txt, i (i)}
								<li>{@html txt.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>')}</li>
							{/each}
						</ul>
					</div>
				</Card>
			</div>
		{/if}

		<div class="section grid-2" style="gap: 16px;">
			<Card title="Динамика средней актуальности (8 недель)" subtitle="Чем выше — тем актуальнее графики">
				<EChart option={companyTrendOption} height="280px" />
			</Card>
			<Card title="Конфликты по дням недели" subtitle="События вне рабочего графика за 30 дней">
				<EChart option={companyWeekdayOption} height="280px" />
			</Card>
		</div>

		<div class="section grid-2" style="gap: 16px; margin-top: 16px;">
			<Card title="Риск по командам" subtitle="Средний интегральный риск по последним метрикам участников (шкала 0–1)" metricLetter="R">
				{#if companyTeams.length === 0}
					<div class="text-text-3 text-sm" style="padding: 16px; text-align: center;">
						Команды не созданы.
					</div>
				{:else}
					<EChart option={companyTeamsOption} height={companyTeams.length * 36 + 60 + 'px'} />
				{/if}
			</Card>
			<Card title="Распределение по группам" subtitle="Актуальные / Подтвердить / Устаревшие / Без данных">
				<EChart option={companyGroupsOption} height="300px" />
			</Card>
		</div>

		{#if companyForecast.length > 0}
			<div class="section" style="margin-top: 16px;">
				<Card
					title="Прогноз риска конфликтов"
					subtitle="Линейный тренд по последним 4 неделям. Кому стоит уделить внимание прямо сейчас."
				>
					<div class="space-y-2">
						{#each companyForecast.slice(0, 6) as f (f.employee_id)}
							<div class="fc fc--{f.risk}">
								<div class="fc__main">
									<a href="/employees/{f.employee_id}" class="fc__name">{f.full_name}</a>
									<div class="fc__meta">
										{f.reason}
										{#if f.department} · {f.department}{/if}
									</div>
								</div>
								<div class="fc__weeks">
									{#each f.weeks as v, i (i)}
										<span class="fc__bar" title="−{f.weeks.length - 1 - i} нед: {v}" style:height="{Math.min(40, v * 8 + 4)}px">
											{v}
										</span>
									{/each}
								</div>
								<div class="fc__risk">
									<Badge variant={f.risk === 'high' ? 'danger' : 'warning'}>
										{f.risk === 'high' ? 'высокий риск' : 'риск'}
									</Badge>
								</div>
							</div>
						{/each}
					</div>
				</Card>
			</div>
		{/if}

		{#if companyAnomalies.length > 0}
			<div class="section" style="margin-top: 16px;">
				<Card
					title="Аномальная активность"
					subtitle="Дни, когда у сотрудника событий заметно больше нормы (z > 2 за 30 дней)"
				>
					<div class="space-y-2">
						{#each companyAnomalies.slice(0, 8) as a, i (i)}
							<div class="anomaly">
								<div class="anomaly__main">
									<a href="/employees/{a.employee_id}" class="anomaly__name">{a.full_name}</a>
									<div class="anomaly__meta">
										{a.day.slice(0, 10)}
										{#if a.department} · {a.department}{/if}
									</div>
								</div>
								<div class="anomaly__stats">
									<div class="anomaly__big">{a.events}</div>
									<div class="anomaly__sub">
										событий · в <strong>{a.times_mean.toFixed(1)}×</strong> больше нормы
										({a.mean.toFixed(1)})
									</div>
								</div>
							</div>
						{/each}
					</div>
					{#if companyAnomalies.length > 8}
						<div class="text-text-3 text-xs" style="margin-top: 8px; text-align: center;">
							ещё {companyAnomalies.length - 8} аномалий
						</div>
					{/if}
				</Card>
			</div>
		{/if}

		{#if companyLeaderboard.length > 0}
			<div class="section" style="margin-top: 16px;">
				<Card
					title="Лидерборд команд по актуальности"
					subtitle="Score = средняя A − 0.5·средний R. Чем выше — тем здоровее данные."
				>
					<table class="lb">
						<thead>
							<tr>
								<th class="lb-rank">#</th>
								<th>Команда</th>
								<th class="lb-num">Чел.</th>
								<th class="lb-num">Актуальность A</th>
								<th class="lb-num">Риск R</th>
								<th class="lb-num">Score</th>
							</tr>
						</thead>
						<tbody>
							{#each companyLeaderboard as t (t.team_id)}
								<tr class:lb-top={t.rank === 1} class:lb-bottom={t.rank === companyLeaderboard.length && companyLeaderboard.length > 1}>
									<td class="lb-rank">
										{#if t.rank === 1}
											<span class="lb-medal lb-medal--gold">🥇</span>
										{:else if t.rank === 2}
											<span class="lb-medal lb-medal--silver">🥈</span>
										{:else if t.rank === 3}
											<span class="lb-medal lb-medal--bronze">🥉</span>
										{:else}
											{t.rank}
										{/if}
									</td>
									<td class="lb-name">{t.team_name}</td>
									<td class="lb-num">{t.members}</td>
									<td class="lb-num">{t.avg_a.toFixed(2)}</td>
									<td class="lb-num">{t.avg_r.toFixed(2)}</td>
									<td class="lb-num lb-score">
										<strong>{t.score.toFixed(2)}</strong>
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</Card>
			</div>
		{/if}
	{/if}
{/if}

<style>
	.lb {
		width: 100%;
		border-collapse: collapse;
		font-size: 13px;
	}
	.lb th {
		text-align: left;
		padding: 8px 10px;
		font-size: 11px;
		text-transform: uppercase;
		letter-spacing: 0.4px;
		color: var(--text-3);
		border-bottom: 1px solid var(--border);
	}
	.lb td {
		padding: 10px;
		border-bottom: 1px solid var(--border);
		color: var(--text);
	}
	.lb tr:last-child td {
		border-bottom: 0;
	}
	.lb-num {
		text-align: right;
		font-variant-numeric: tabular-nums;
		font-family: 'JetBrains Mono', ui-monospace, monospace;
	}
	.lb-rank {
		width: 36px;
		text-align: center;
		font-weight: 600;
		color: var(--text-2);
	}
	.lb-name {
		font-weight: 600;
	}
	.lb-medal {
		font-size: 18px;
	}
	.lb-score strong {
		color: var(--success-strong);
	}
	.lb tr.lb-top {
		background: rgba(34, 197, 94, 0.06);
	}
	.lb tr.lb-bottom .lb-score strong {
		color: var(--danger-strong);
	}

	.anomaly {
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 10px 12px;
		border: 1px solid var(--border);
		border-radius: 10px;
		background: var(--surface);
	}
	.anomaly__main {
		flex: 1;
		min-width: 0;
	}
	.anomaly__name {
		font-weight: 600;
		font-size: 14px;
		color: var(--text);
		text-decoration: none;
	}
	.anomaly__name:hover {
		color: var(--info-strong);
	}
	.anomaly__meta {
		font-size: 12px;
		color: var(--text-2);
		margin-top: 2px;
	}
	.anomaly__stats {
		text-align: right;
	}
	.anomaly__big {
		font-size: 24px;
		font-weight: 700;
		color: var(--danger-strong);
		font-variant-numeric: tabular-nums;
		font-family: 'JetBrains Mono', ui-monospace, monospace;
	}
	.anomaly__sub {
		font-size: 11px;
		color: var(--text-2);
	}

	.fc {
		display: flex;
		align-items: center;
		gap: 14px;
		padding: 10px 12px;
		border: 1px solid var(--border);
		border-radius: 10px;
		background: var(--surface);
	}
	.fc--high {
		border-left: 3px solid var(--danger-strong);
	}
	.fc--medium {
		border-left: 3px solid var(--warning-strong);
	}
	.fc__main {
		flex: 1;
		min-width: 0;
	}
	.fc__name {
		font-weight: 600;
		font-size: 14px;
		color: var(--text);
		text-decoration: none;
	}
	.fc__name:hover {
		color: var(--info-strong);
	}
	.fc__meta {
		font-size: 12px;
		color: var(--text-2);
		margin-top: 2px;
	}
	.fc__weeks {
		display: flex;
		align-items: flex-end;
		gap: 4px;
		height: 48px;
	}
	.fc__bar {
		width: 22px;
		min-height: 4px;
		border-radius: 4px 4px 0 0;
		background: var(--info-bg);
		color: var(--info-strong);
		font-size: 10px;
		font-weight: 600;
		text-align: center;
		display: flex;
		align-items: flex-start;
		justify-content: center;
		padding-top: 2px;
		font-variant-numeric: tabular-nums;
	}
	.fc--high .fc__bar:last-child {
		background: var(--danger-bg);
		color: var(--danger-strong);
	}
	.fc--medium .fc__bar:last-child {
		background: var(--warning-bg);
		color: var(--warning-strong);
	}
	.fc__risk {
		flex-shrink: 0;
	}

	.insights {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.insights__head {
		display: flex;
		align-items: center;
		gap: 6px;
		font-size: 12px;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.4px;
		color: var(--info-strong);
	}
	.insights__head i {
		font-size: 14px;
	}
	.insights__list {
		margin: 0;
		padding-left: 18px;
		font-size: 13px;
		color: var(--text-2);
		line-height: 1.6;
	}
	.insights__list :global(strong) {
		color: var(--text);
		font-weight: 600;
	}

	.scope-bar {
		display: flex;
		align-items: center;
		gap: 10px;
	}
	.scope-bar__label {
		font-size: 13px;
		color: var(--text-2);
		font-weight: 500;
	}
	.scope-bar__select {
		padding: 6px 10px;
		border: 1px solid var(--border);
		border-radius: 8px;
		background: var(--surface);
		color: var(--text);
		font-size: 13px;
		min-width: 220px;
	}

	.empty-state {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 8px;
		padding: 24px;
		color: var(--text-3);
		text-align: center;
	}
	.empty-state i {
		font-size: 32px;
		opacity: 0.5;
	}
</style>
