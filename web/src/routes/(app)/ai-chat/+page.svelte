<script lang="ts">
	import { onMount, tick } from 'svelte';
	import Card from '$lib/components/Card.svelte';
	import Button from '$lib/components/Button.svelte';
	import Badge from '$lib/components/Badge.svelte';
	import {
		streamChat,
		aiStatus,
		getLatestConversation,
		getConversationMessages,
		deleteConversation
	} from '$lib/api/ai';
	import { ApiError, getAccessToken } from '$lib/api/client';
	import { browser } from '$app/environment';
	import { env } from '$env/dynamic/public';

	function backendURL(): string {
		return env.PUBLIC_API_URL || (browser ? '' : 'http://localhost:8080');
	}
	import {
		listTeams,
		findWindow,
		proposeMeeting,
		type Team,
		type MeetingWindow
	} from '$lib/api/teams';
	import {
		listMyMeetings,
		updateMeeting,
		type MyMeeting
	} from '$lib/api/meetings';
	import { notifyStaleEmployees } from '$lib/api/hr';
	import { broadcastNotifications, type BroadcastKind } from '$lib/api/notifications';
	import { getBurnout } from '$lib/api/diagnostics';
	import { getAnomalies } from '$lib/api/analytics';
	import { user } from '$lib/stores/user';
	import { marked } from 'marked';

	const viewerTZ = $derived($user?.timezone || 'Europe/Moscow');

	marked.setOptions({ gfm: true, breaks: true });

	function renderMd(src: string): string {
		const escaped = src
			.replace(/&/g, '&amp;')
			.replace(/</g, '&lt;')
			.replace(/>/g, '&gt;');
		return marked.parse(escaped) as string;
	}

	// --- Сообщения и action-кнопки под ними ---

	type ActionKind =
		| 'start_flow' // «Запланировать встречу»
		| 'cancel'
		| 'pick_team'
		| 'pick_window'
		| 'confirm_propose'
		| 'start_notify_stale' // «Разослать запросы устаревшим»
		| 'confirm_notify_stale'
		| 'pick_export' // открыть меню «Что выгрузить?»
		| 'pick_format' // спросить формат (xlsx/pdf) для выбранного пресета
		| 'do_export' // запустить выгрузку (payload.kind + format)
		| 'start_reschedule' // «Перенести мою встречу»
		| 'pick_meeting_to_move' // выбрать какую встречу переносить
		| 'do_reschedule' // финальный перенос (payload.meeting_id + start/end)
		| 'broadcast_notify' // массовая рассылка burnout/overload/anomaly/stale_profile
		| 'navigate'; // переход на страницу системы (payload.href)

	interface MsgAction {
		label: string;
		variant?: 'primary' | 'ghost' | 'danger';
		icon?: string;
		kind: ActionKind;
		// Контекст: id команды, окно, длительность и т.п.
		payload?: Record<string, unknown>;
	}

	interface Msg {
		id: number;
		role: 'user' | 'assistant';
		content: string;
		ts: number;
		actions?: MsgAction[];
		consumed?: boolean; // после клика прячем кнопки
	}

	let nextMsgId = 0;
	function newMsg(m: Omit<Msg, 'id' | 'ts'>): Msg {
		nextMsgId++;
		return { ...m, id: nextMsgId, ts: Date.now() + nextMsgId };
	}

	let messages = $state<Msg[]>([]);
	let input = $state('');
	let conversationID = $state<string | undefined>(undefined);
	let sending = $state(false);
	let available = $state(false);
	let error = $state<string | null>(null);
	let scrollEl: HTMLDivElement | null = $state(null);
	let bottomAnchor: HTMLDivElement | null = $state(null);

	// Состояние flow «запланировать встречу».
	let flowDurationMin = $state(60);

	const suggestions = [
		'У кого данные о рабочем времени устарели?',
		'Кто из команды перегружен на этой неделе?',
		'Кто в зоне выгорания?',
		'У кого высокий риск конфликтов?',
		'У кого аномальная активность за последние дни?',
		'Где есть конфликты в календаре?',
		'Когда команда реально доступна?',
		'Какие встречи лучше перенести?',
		'Какие действия выполнить в первую очередь?',
		'Какая команда сейчас самая актуальная?',
		'Разошли запросы на обновление графика всем устаревшим',
		'Когда можно собрать всю команду на 60 минут?'
	];

	let restoring = $state(true);
	let clearing = $state(false);

	onMount(async () => {
		try {
			const s = await aiStatus();
			available = s.available;
		} catch {
			available = false;
		}

		// Восстанавливаем последнюю беседу пользователя, если она есть.
		try {
			const latest = await getLatestConversation();
			if (latest.conversation_id) {
				conversationID = latest.conversation_id;
				const r = await getConversationMessages(latest.conversation_id);
				const restored = (r.messages ?? [])
					.filter((m) => m.role === 'user' || m.role === 'assistant')
					.map((m, i) =>
						newMsg({
							role: m.role as 'user' | 'assistant',
							content: m.content
						})
					);
				if (restored.length > 0) {
					messages = restored;
				}
			}
		} catch {
			// Тихо игнорим — это лишь nice-to-have.
		}
		restoring = false;

		if (!available && messages.length === 0) {
			messages.push(
				newMsg({
					role: 'assistant',
					content: 'ИИ-ассистент сейчас недоступен — отвечаю по встроенным правилам.'
				})
			);
		}
	});

	async function clearChat() {
		if (clearing) return;
		if (!confirm('Удалить всю историю чата? Действие необратимо.')) return;
		clearing = true;
		try {
			if (conversationID) {
				await deleteConversation(conversationID).catch(() => null);
			}
		} finally {
			messages = [];
			conversationID = undefined;
			error = null;
			clearing = false;
			await tick();
			scrollDown(true);
		}
	}

	async function send(text?: string) {
		const msg = (text ?? input).trim();
		if (!msg || sending) return;
		messages.push(newMsg({ role: 'user', content: msg }));
		input = '';
		error = null;

		// Сначала пытаемся распознать намерение в реплике пользователя.
		// Если оно чёткое (export / meeting / notify_stale) — НЕ дёргаем LLM,
		// сразу отдаём короткий action-prompt с кнопками. Это исключает
		// противоречие, когда модель «не знает» о фиче и говорит «недоступно».
		const intent = detectIntent(msg);
		if (intent.kind !== 'none') {
			await tick();
			scrollDown(true);
			handleIntentDirectly(intent);
			return;
		}

		// Свободный диалог — спрашиваем LLM.
		sending = true;
		const assistantMsg = newMsg({ role: 'assistant', content: '' });
		messages.push(assistantMsg);
		const idx = messages.length - 1;

		await tick();
		scrollDown(true);

		try {
			await streamChat({ conversation_id: conversationID, message: msg }, (ev) => {
				if (ev.type === 'meta') {
					conversationID = ev.conversation_id;
				} else if (ev.type === 'delta') {
					messages[idx] = { ...messages[idx], content: messages[idx].content + ev.text };
					scrollDown();
				} else if (ev.type === 'error') {
					error = ev.message;
				}
			});
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
			messages[idx] = { ...messages[idx], content: `Ошибка: ${error}` };
		} finally {
			sending = false;
			await tick();
			scrollDown();
		}

		// Под свободный ответ LLM подвешиваем релевантные навигационные/action-кнопки.
		// Анализируем и сам вопрос, и ответ — так покрываем оба случая
		// (юзер задал чёткий вопрос; LLM сам наскрипел тему).
		if (!messages[idx].actions) {
			const actions = suggestActionsForReply(msg, messages[idx].content);
			if (actions.length > 0) {
				messages[idx] = { ...messages[idx], actions };
				await tick();
				scrollDown();
			}
		}
	}

	// suggestActionsForReply — анализирует вопрос юзера + ответ LLM и собирает
	// набор кнопок-действий. Покрывает сценарии из кейса №3 §15:
	//   • устаревшие графики → /diagnostics, разослать запросы, Excel
	//   • перегрузка         → /analytics, /workload
	//   • конфликты          → /conflicts, Excel
	//   • доступность команды → /team-map, /scheduler
	//   • что делать первым  → /hr-roadmap
	//   • подтвердить свой   → /profile
	function suggestActionsForReply(userMsg: string, llmReply: string): MsgAction[] {
		const haystack = (userMsg + '\n' + llmReply).toLowerCase();
		const out: MsgAction[] = [];

		// Темы — порядок важен: первая совпавшая определяет первые кнопки.
		const stale = /устаревш|неактуальн|давно не обнов|не обновл/.test(haystack);
		const overload = /перегруж|перегруз|высокая нагрузк|высок.* загруз|загружен/.test(haystack);
		const conflict = /конфликт|вне рабоч|вне график/.test(haystack);
		const availability = /доступн|общее окно|когда .* команд|собрать команд/.test(haystack);
		const roadmap = /первую очередь|дорожн|пошагов|сначала .* потом|какие действ/.test(haystack);
		const myProfile = /мой профил|подтверд.* мой|обновить мой график|мой график/.test(haystack);
		// Новые фичи из кейса §13:
		const burnout = /выгоран|истощен|burnout|на грани|зона риска вы/.test(haystack);
		const forecast = /прогноз|вероятн.* конфликт|тренд|в ближайш|через .* недел/.test(haystack);
		const anomaly = /аномал|необычн.* активн|резк.* рост|внезапн/.test(haystack);
		const leaderboard = /рейтинг|лидерборд|лучш.* команд|худш.* команд|сравнить команд|самая .* команд/.test(haystack);
		const history = /истори.* график|истори.* профил|когда .* менял|изменени.* график|какие изменени/.test(haystack);

		if (stale) {
			out.push({
				label: 'Открыть диагностику',
				variant: 'primary',
				icon: 'ti-stethoscope',
				kind: 'navigate',
				payload: { href: '/diagnostics' }
			});
			out.push({
				label: 'Разослать запросы',
				variant: 'ghost',
				icon: 'ti-mail-forward',
				kind: 'start_notify_stale'
			});
		}
		if (overload) {
			out.push({
				label: 'Разослать уведомление',
				variant: 'primary',
				icon: 'ti-send',
				kind: 'broadcast_notify',
				payload: { broadcast_kind: 'overload' }
			});
			out.push({
				label: 'Аналитика',
				variant: 'ghost',
				icon: 'ti-chart-line',
				kind: 'navigate',
				payload: { href: '/analytics' }
			});
			out.push({
				label: 'Моя загрузка',
				variant: 'ghost',
				icon: 'ti-gauge',
				kind: 'navigate',
				payload: { href: '/workload' }
			});
		}
		if (conflict) {
			out.push({
				label: 'Конфликты',
				variant: 'ghost',
				icon: 'ti-alert-triangle',
				kind: 'navigate',
				payload: { href: '/conflicts' }
			});
		}
		if (availability) {
			out.push({
				label: 'Карта команды',
				variant: 'ghost',
				icon: 'ti-calendar-event',
				kind: 'navigate',
				payload: { href: '/team-map' }
			});
			out.push({
				label: 'Найти окно',
				variant: 'primary',
				icon: 'ti-calendar-plus',
				kind: 'start_flow',
				payload: { duration: 60 }
			});
			flowDurationMin = 60;
		}
		if (roadmap) {
			out.push({
				label: 'Дорожная карта HR',
				variant: 'primary',
				icon: 'ti-map-2',
				kind: 'navigate',
				payload: { href: '/hr-roadmap' }
			});
		}
		if (myProfile) {
			out.push({
				label: 'Открыть мой профиль',
				variant: 'primary',
				icon: 'ti-user',
				kind: 'navigate',
				payload: { href: '/profile' }
			});
		}
		if (burnout) {
			out.push({
				label: 'Разослать уведомление',
				variant: 'primary',
				icon: 'ti-send',
				kind: 'broadcast_notify',
				payload: { broadcast_kind: 'burnout' }
			});
			out.push({
				label: 'Зона выгорания',
				variant: 'ghost',
				icon: 'ti-flame',
				kind: 'navigate',
				payload: { href: '/diagnostics' }
			});
		}
		if (forecast) {
			out.push({
				label: 'Прогноз риска',
				variant: 'primary',
				icon: 'ti-chart-line',
				kind: 'navigate',
				payload: { href: '/analytics' }
			});
		}
		if (anomaly) {
			out.push({
				label: 'Разослать уведомление',
				variant: 'primary',
				icon: 'ti-send',
				kind: 'broadcast_notify',
				payload: { broadcast_kind: 'anomaly' }
			});
			out.push({
				label: 'Аномальная активность',
				variant: 'ghost',
				icon: 'ti-alert-octagon',
				kind: 'navigate',
				payload: { href: '/analytics' }
			});
		}
		if (leaderboard) {
			out.push({
				label: 'Рейтинг команд',
				variant: 'primary',
				icon: 'ti-trophy',
				kind: 'navigate',
				payload: { href: '/analytics' }
			});
		}
		if (history) {
			out.push({
				label: 'История моего графика',
				variant: 'ghost',
				icon: 'ti-history',
				kind: 'navigate',
				payload: { href: '/profile' }
			});
		}

		// Excel — общий хвост, если ответ напоминает список людей/событий
		// и при этом нет уже specifics-кнопки экспорта.
		const looksLikeList = /список|перечен|вот \w+ сотрудник|^\s*[—\-*]/m.test(llmReply);
		if (looksLikeList && (stale || overload || conflict)) {
			out.push({
				label: 'Выгрузить в Excel',
				variant: 'ghost',
				icon: 'ti-file-spreadsheet',
				kind: 'pick_export'
			});
		}

		return out;
	}

	// handleIntentDirectly — короткий ответ под чёткое намерение, БЕЗ вызова LLM.
	// Текст согласован с кнопками: «Готов сделать X». Сохранение в историю чата
	// (БД) не делаем — это локальный action-prompt, при перезагрузке сценарий
	// просто будет проигран заново при следующей реплике пользователя.
	function handleIntentDirectly(intent: Intent) {
		let content = '';
		let actions: MsgAction[] = [];

		if (intent.kind === 'meeting') {
			flowDurationMin = intent.durationMin;
			content = `Готов подобрать общее окно на **${intent.durationMin} мин**.`;
			actions = [
				{
					label: `Запланировать на ${intent.durationMin} мин`,
					variant: 'primary',
					icon: 'ti-calendar-plus',
					kind: 'start_flow',
					payload: { duration: intent.durationMin }
				},
				{ label: 'Отмена', variant: 'ghost', kind: 'cancel' }
			];
		} else if (intent.kind === 'notify_stale') {
			content = 'Готов разослать запросы тем, у кого профиль давно не обновлялся.';
			actions = [
				{
					label: 'Разослать запросы',
					variant: 'primary',
					icon: 'ti-mail-forward',
					kind: 'start_notify_stale'
				},
				{ label: 'Не сейчас', variant: 'ghost', kind: 'cancel' }
			];
		} else if (intent.kind === 'export') {
			const label = exportHintLabel(intent.hint);
			content = label
				? `Готов выгрузить в Excel: **${label}**.`
				: 'Готов выгрузить в Excel. Выбери, что именно.';
			actions = exportActions(intent.hint, intent.kinds);
		} else if (intent.kind === 'reschedule') {
			// Без промежуточного «Поехали» — раз юзер уже сказал «перенеси»,
			// сразу подбираем варианты. Дальше будет ИЛИ выбор встречи (если их
			// несколько), ИЛИ сразу выбор слота (если встреча одна).
			tick().then(() => {
				scrollDown();
				void flowRescheduleStart();
			});
			return;
		}

		messages.push(newMsg({ role: 'assistant', content, actions }));
		tick().then(() => scrollDown());
	}

	function exportHintLabel(hint: ExportHint): string {
		switch (hint) {
			case 'vacations':
				return 'ближайшие отпуска';
			case 'stale':
				return 'устаревшие профили';
			case 'conflicts':
				return 'конфликты в календаре';
			case 'employees':
				return 'список всех сотрудников';
			default:
				return '';
		}
	}

	type ExportHint = 'vacations' | 'stale' | 'conflicts' | 'employees' | 'any';

	type Intent =
		| { kind: 'none' }
		| { kind: 'meeting'; durationMin: number }
		| { kind: 'notify_stale' }
		| { kind: 'export'; hint: ExportHint; kinds?: string[] }
		| { kind: 'reschedule' };

	function detectIntent(text: string): Intent {
		const t = text.toLowerCase();

		// «Перенести встречу» — пробуем РАНЬШЕ «meeting», т.к. слово «встречу»
		// есть в обоих, а перенос всегда более конкретный.
		const rescheduleTriggers = [
			'перенеси',
			'перенести',
			'перенесите',
			'сдвин', // сдвинь, сдвиньте, сдвинуть
			'изменить время встреч',
			'измени время встреч',
			'reschedule'
		];
		if (rescheduleTriggers.some((tr) => t.includes(tr)) && /встреч/.test(t)) {
			return { kind: 'reschedule' };
		}

		const meetingTriggers = [
			'собрать команду',
			'окно для встреч',
			'когда можно собрать',
			'встреча на',
			'найти время',
			'когда команда',
			'когда собрать'
		];
		if (meetingTriggers.some((tr) => t.includes(tr))) {
			const minMatch = t.match(/(\d+)\s*мин/);
			if (minMatch) return { kind: 'meeting', durationMin: parseInt(minMatch[1], 10) };
			const hourMatch = t.match(/(\d+)\s*час/);
			if (hourMatch) return { kind: 'meeting', durationMin: parseInt(hourMatch[1], 10) * 60 };
			return { kind: 'meeting', durationMin: 60 };
		}

		const notifyTriggers = [
			'разошли запрос',
			'разошли запросы',
			'попроси обновить',
			'попросить обновить',
			'отправь напоминан',
			'попроси подтвердить',
			'попросить подтвердить',
			'пинг устаревш',
			'актуализир'
		];
		if (notifyTriggers.some((tr) => t.includes(tr))) {
			return { kind: 'notify_stale' };
		}

		const exportTriggers = ['выгруз', 'эксел', 'excel', 'xlsx', 'таблиц', 'экспорт'];
		if (exportTriggers.some((tr) => t.includes(tr))) {
			let hint: ExportHint = 'any';
			let kinds: string[] | undefined;
			// Внутри «отпуск/командировка/больничный/отсутствие» выделяем конкретный тип,
			// чтобы выгрузить ТОЛЬКО его, а не все исключения подряд.
			if (/отпуск|командиров|больнич|отсутств|личные часы/i.test(t)) {
				hint = 'vacations';
				const picked: string[] = [];
				if (/командиров/i.test(t)) picked.push('business_trip');
				if (/больнич/i.test(t)) picked.push('sick_leave');
				if (/отпуск/i.test(t)) picked.push('vacation');
				if (/личные час/i.test(t)) picked.push('personal_hours');
				if (picked.length > 0) kinds = picked;
			} else if (/устаревш|неактуальн|давно не обнов/i.test(t)) hint = 'stale';
			else if (/конфликт|вне рабочих/i.test(t)) hint = 'conflicts';
			else if (/сотрудник|всех людей|справочник/i.test(t)) hint = 'employees';
			return { kind: 'export', hint, kinds };
		}

		return { kind: 'none' };
	}

	function exportActions(hint: ExportHint, kinds?: string[]): MsgAction[] {
		// Если запрошен конкретный тип отсутствия — primary-кнопка с явным фильтром.
		const all: MsgAction[] = [];
		if (kinds && kinds.length > 0) {
			const labelMap: Record<string, string> = {
				vacation: 'Только отпуска',
				business_trip: 'Только командировки',
				sick_leave: 'Только больничные',
				personal_hours: 'Только личные часы'
			};
			const iconMap: Record<string, string> = {
				vacation: 'ti-beach',
				business_trip: 'ti-briefcase',
				sick_leave: 'ti-thermometer',
				personal_hours: 'ti-clock'
			};
			const label = kinds.map((k) => labelMap[k] ?? k).join(' + ');
			all.push({
				label,
				icon: iconMap[kinds[0]] ?? 'ti-beach',
				variant: 'primary',
				kind: 'pick_format',
				payload: { kind: 'upcoming_vacations', kinds }
			});
		}

		all.push(
			{
				label: 'Все отпуска/больничные/командировки',
				icon: 'ti-beach',
				kind: 'pick_format',
				payload: { kind: 'upcoming_vacations' }
			},
			{
				label: 'Устаревшие профили',
				icon: 'ti-clock-exclamation',
				kind: 'pick_format',
				payload: { kind: 'stale_profiles' }
			},
			{
				label: 'Конфликты',
				icon: 'ti-alert-triangle',
				kind: 'pick_format',
				payload: { kind: 'conflicts' }
			},
			{
				label: 'Все сотрудники',
				icon: 'ti-users',
				kind: 'pick_format',
				payload: { kind: 'all_employees' }
			},
			{ label: 'Отмена', variant: 'ghost', kind: 'cancel' }
		);

		// Если конкретный тип не выбран, но есть hint — primary-им релевантный пресет.
		if (!kinds || kinds.length === 0) {
			const targetKind =
				hint === 'vacations'
					? 'upcoming_vacations'
					: hint === 'stale'
						? 'stale_profiles'
						: hint === 'conflicts'
							? 'conflicts'
							: hint === 'employees'
								? 'all_employees'
								: '';
			if (targetKind) {
				for (const a of all) {
					if (a.payload && a.payload.kind === targetKind) {
						a.variant = 'primary';
					}
				}
			}
		}
		return all;
	}

	// --- Обработка кликов по action-кнопкам ---

	async function handleAction(msgIdx: number, action: MsgAction) {
		// Сразу скрываем кнопки этого сообщения — чтобы нельзя было дважды.
		messages[msgIdx] = { ...messages[msgIdx], consumed: true };

		// Логируем выбор пользователя в чате как «системное» сообщение от него.
		messages.push(newMsg({ role: 'user', content: '→ ' + action.label }));

		await tick();
		scrollDown(true);

		switch (action.kind) {
			case 'cancel':
				messages.push(newMsg({ role: 'assistant', content: 'Отменено.' }));
				break;
			case 'start_flow':
				await flowChooseTeam();
				break;
			case 'pick_team':
				await flowChooseWindow(action.payload?.team_id as string, action.payload?.team_name as string);
				break;
			case 'pick_window':
				await flowConfirm(action.payload);
				break;
			case 'confirm_propose':
				await flowPropose(action.payload);
				break;
			case 'start_notify_stale':
				await flowNotifyStaleChoose();
				break;
			case 'confirm_notify_stale':
				await flowNotifyStaleRun(action.payload?.min_days as number);
				break;
			case 'pick_export':
				await flowExportPick();
				break;
			case 'pick_format':
				await flowFormatPick(
					action.payload?.kind as string,
					action.label,
					action.payload?.kinds as string[] | undefined
				);
				break;
			case 'do_export':
				await flowExportRun(
					action.payload?.kind as string,
					action.label,
					(action.payload?.format as 'xlsx' | 'pdf') ?? 'xlsx',
					action.payload?.kinds as string[] | undefined
				);
				break;
			case 'start_reschedule':
				await flowRescheduleStart();
				break;
			case 'pick_meeting_to_move':
				await flowRescheduleChooseWindow(action.payload?.meeting_id as string);
				break;
			case 'do_reschedule':
				await flowRescheduleApply(action.payload);
				break;
			case 'broadcast_notify':
				await flowBroadcastNotify(action.payload?.broadcast_kind as BroadcastKind);
				break;
			case 'navigate':
				if (action.payload?.href && typeof window !== 'undefined') {
					window.location.href = action.payload.href as string;
				}
				break;
		}

		await tick();
		scrollDown();
	}

	// flowBroadcastNotify — массовая рассылка in-app уведомлений по сценарию.
	// 1) Тянем актуальный список emp_ids из соответствующего endpoint'а.
	// 2) Шлём POST /notifications/broadcast.
	// 3) Печатаем результат в чат — «Отправил N уведомлений / N пропущено».
	async function flowBroadcastNotify(kind: BroadcastKind) {
		let empIDs: string[] = [];
		try {
			if (kind === 'burnout' || kind === 'overload') {
				// Burnout-детектор уже учитывает и overload (высокая L), и стресс
				// (высокая C). Для overload и burnout берём один и тот же список.
				const r = await getBurnout();
				empIDs = (r.burnout ?? []).map((b) => b.employee_id);
			} else if (kind === 'anomaly') {
				const r = await getAnomalies();
				// Берём уникальные emp_id — за 30 дней один человек может быть в нескольких аномалиях.
				const seen = new Set<string>();
				empIDs = (r.anomalies ?? [])
					.map((a) => a.employee_id)
					.filter((id) => (seen.has(id) ? false : (seen.add(id), true)));
			} else if (kind === 'stale_profile') {
				// stale-сценарий уже покрыт flowNotifyStaleRun — на всякий случай
				// не дублируем рассылку, просто говорим.
				messages.push(
					newMsg({
						role: 'assistant',
						content: 'Для рассылки по устаревшим профилям используй кнопку «Разослать запросы устаревшим» — она шлёт более подробное письмо.'
					})
				);
				return;
			}
		} catch (e) {
			messages.push(
				newMsg({ role: 'assistant', content: `Не удалось получить список: ${errStr(e)}` })
			);
			return;
		}

		if (empIDs.length === 0) {
			messages.push(
				newMsg({ role: 'assistant', content: 'Сейчас никого нет в этой категории — рассылать некому.' })
			);
			return;
		}

		try {
			const r = await broadcastNotifications(kind, empIDs);
			const parts: string[] = [];
			parts.push(`Отправил ${r.sent} ${pluralRu(r.sent, ['уведомление', 'уведомления', 'уведомлений'])}`);
			if (r.skipped > 0) parts.push(`пропустил ${r.skipped} (получали такое же за сутки)`);
			messages.push(newMsg({ role: 'assistant', content: parts.join(' · ') + '.' }));
		} catch (e) {
			messages.push(
				newMsg({ role: 'assistant', content: `Не удалось разослать: ${errStr(e)}` })
			);
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

	async function flowChooseTeam() {
		// Загружаем команды и предлагаем выбрать.
		let teams: Team[] = [];
		try {
			const r = await listTeams();
			teams = r.teams ?? [];
		} catch (e) {
			messages.push(
				newMsg({ role: 'assistant', content: `Не удалось загрузить команды: ${errStr(e)}` })
			);
			return;
		}
		if (teams.length === 0) {
			messages.push(
				newMsg({
					role: 'assistant',
					content: 'Нет ни одной команды — создайте её в разделе «Команды».'
				})
			);
			return;
		}
		messages.push(
			newMsg({
				role: 'assistant',
				content: 'Для какой команды найти и запланировать встречу?',
				actions: teams
					.map<MsgAction>((t) => ({
						label: t.name,
						kind: 'pick_team',
						payload: { team_id: t.id, team_name: t.name }
					}))
					.concat([{ label: 'Отмена', variant: 'ghost', kind: 'cancel' }])
			})
		);
	}

	async function flowChooseWindow(teamID: string, teamName: string) {
		// Кладём placeholder и запоминаем его индекс — Svelte 5 runes требует
		// мутаций через индекс массива, прямые мутации объекта не отслеживаются.
		messages.push(
			newMsg({
				role: 'assistant',
				content: `Ищу свободные окна команды «${teamName}» на ${flowDurationMin} мин…`
			})
		);
		const idx = messages.length - 1;
		await tick();
		scrollDown();

		let windows: MeetingWindow[] = [];
		try {
			const r = await findWindow(teamID, {
				duration_min: flowDurationMin,
				days: 14,
				top_n: 3
			});
			windows = r.windows ?? [];
		} catch (e) {
			messages[idx] = { ...messages[idx], content: `Не удалось найти окна: ${errStr(e)}` };
			return;
		}
		if (windows.length === 0) {
			messages[idx] = {
				...messages[idx],
				content: `Свободных ${flowDurationMin}-минутных окон в команде «${teamName}» на ближайшие 7 дней не нашлось.`
			};
			return;
		}

		const actions: MsgAction[] = windows.map<MsgAction>((w, i) => ({
			label: `${i + 1}. ${fmtWin(w)} — доступно ${w.available_count}/${w.total_count}`,
			kind: 'pick_window',
			payload: {
				team_id: teamID,
				team_name: teamName,
				start_at: w.start_at,
				end_at: w.end_at,
				available_count: w.available_count,
				total_count: w.total_count,
				unavailable_names: w.unavailable.map((p) => p.full_name).join(', ')
			}
		}));
		actions.push({ label: 'Отмена', variant: 'ghost', kind: 'cancel' });

		messages[idx] = {
			...messages[idx],
			content: `Команда «${teamName}». Лучшие окна:`,
			actions
		};
	}

	async function flowConfirm(p: Record<string, unknown> | undefined) {
		if (!p) return;
		const win = `${fmtDate(p.start_at as string)} ${fmtTime(p.start_at as string)}–${fmtTime(p.end_at as string)} (${tzLabel()})`;
		const ratio = `${p.available_count}/${p.total_count}`;
		const unavail = (p.unavailable_names as string) || '';
		let txt = `Готов отправить уведомление о встрече команды «${p.team_name}» — ${win} (доступно ${ratio}).`;
		if (unavail) txt += `\n\nКто пока не сможет: ${unavail}.`;
		txt += `\n\nРазослать всем участникам команды + вам?`;

		messages.push(
			newMsg({
				role: 'assistant',
				content: txt,
				actions: [
					{
						label: 'Отправить уведомления',
						variant: 'primary',
						icon: 'ti-send',
						kind: 'confirm_propose',
						payload: p
					},
					{ label: 'Отмена', variant: 'ghost', kind: 'cancel' }
				]
			})
		);
	}

	async function flowPropose(p: Record<string, unknown> | undefined) {
		if (!p) return;
		try {
			const r = await proposeMeeting(p.team_id as string, {
				start_at: p.start_at as string,
				end_at: p.end_at as string
			});
			messages.push(
				newMsg({
					role: 'assistant',
					content: `Готово. Уведомление о встрече команды «${r.team_name}» (${fmtDate(r.start_at)} ${fmtTime(r.start_at)}–${fmtTime(r.end_at)} ${tzLabel()}) отправлено **${r.sent}** пользователям.`
				})
			);
		} catch (e) {
			messages.push(
				newMsg({ role: 'assistant', content: `Не удалось разослать: ${errStr(e)}` })
			);
		}
	}

	// --- Flow: пинг устаревших профилей ---

	async function flowNotifyStaleChoose() {
		messages.push(
			newMsg({
				role: 'assistant',
				content:
					'Кому отправить запрос на обновление графика?\n' +
					'• Всем, у кого профиль не обновлялся более 60 дней.\n' +
					'• Только тем, у кого больше 90 дней — критически устаревшие.',
				actions: [
					{
						label: 'Всех (>60 дней)',
						variant: 'primary',
						icon: 'ti-mail-forward',
						kind: 'confirm_notify_stale',
						payload: { min_days: 60 }
					},
					{
						label: 'Только критических (>90)',
						icon: 'ti-alert-triangle',
						kind: 'confirm_notify_stale',
						payload: { min_days: 90 }
					},
					{ label: 'Отмена', variant: 'ghost', kind: 'cancel' }
				]
			})
		);
	}

	async function flowNotifyStaleRun(minDays: number) {
		const placeholderIdx = messages.length;
		messages.push(
			newMsg({
				role: 'assistant',
				content: `Рассылаю запросы тем, чей профиль не обновлялся ${minDays}+ дней…`
			})
		);
		await tick();
		scrollDown();
		try {
			const r = await notifyStaleEmployees(minDays);
			const lines: string[] = [];
			lines.push(`Готово. Отправлено: **${r.sent}**.`);
			if (r.skipped > 0) {
				lines.push(`Пропущено (уже получили запрос в последние 24 ч): ${r.skipped}.`);
			}
			if (r.targeted === 0) {
				lines.push('Сотрудников с просроченным профилем не нашлось — все актуальны.');
			}
			if (r.emails && r.emails.length > 0) {
				lines.push('\nКому ушло:');
				lines.push(r.emails.map((e) => `• ${e}`).join('\n'));
			}
			messages[placeholderIdx] = {
				...messages[placeholderIdx],
				content: lines.join('\n')
			};
		} catch (e) {
			messages[placeholderIdx] = {
				...messages[placeholderIdx],
				content: `Не удалось разослать: ${errStr(e)}`
			};
		}
	}

	// --- Flow: Excel-выгрузка ---

	async function flowExportPick() {
		messages.push(
			newMsg({
				role: 'assistant',
				content: 'Что выгрузить в Excel?',
				actions: exportActions('any')
			})
		);
	}

	// --- Flow: перенести встречу ---

	// Локальное хранилище встреч, чтобы при клике по pick_meeting_to_move
	// быстро найти duration и team_id.
	let rescheduleCache = $state<MyMeeting[]>([]);

	async function flowRescheduleStart() {
		messages.push(newMsg({ role: 'assistant', content: 'Загружаю твои встречи…' }));
		const idx = messages.length - 1;
		await tick();
		scrollDown();

		let meetings: MyMeeting[] = [];
		try {
			const r = await listMyMeetings();
			meetings = (r.meetings ?? []).filter((m) => m.can_cancel && m.team_id);
		} catch (e) {
			messages[idx] = {
				...messages[idx],
				content: `Не получилось загрузить встречи: ${errStr(e)}`
			};
			return;
		}

		if (meetings.length === 0) {
			messages[idx] = {
				...messages[idx],
				content:
					'У тебя нет активных встреч, которые можно переносить. Сначала создай встречу или попроси переноса того, кто инициатор.'
			};
			return;
		}

		rescheduleCache = meetings;

		if (meetings.length === 1) {
			messages[idx] = {
				...messages[idx],
				content: `Нашлась одна встреча: «${meetings[0].title}» (${fmtWinFromIso(meetings[0].start_at, meetings[0].end_at)}). Ищу свободные окна…`
			};
			await flowRescheduleChooseWindow(meetings[0].id);
			return;
		}

		messages[idx] = {
			...messages[idx],
			content: `Какую встречу перенести? Найдено ${meetings.length}.`,
			actions: meetings
				.map<MsgAction>((m) => ({
					label: `${m.title} · ${fmtWinFromIso(m.start_at, m.end_at)}`,
					kind: 'pick_meeting_to_move',
					payload: { meeting_id: m.id }
				}))
				.concat([{ label: 'Отмена', variant: 'ghost', kind: 'cancel' }])
		};
	}

	async function flowRescheduleChooseWindow(meetingID: string) {
		const m = rescheduleCache.find((x) => x.id === meetingID);
		if (!m) {
			messages.push(
				newMsg({ role: 'assistant', content: 'Не нашёл встречу в кэше. Начни заново.' })
			);
			return;
		}
		if (!m.team_id) {
			messages.push(
				newMsg({ role: 'assistant', content: 'У встречи нет команды — окно подобрать не получится.' })
			);
			return;
		}
		const durationMs = new Date(m.end_at).getTime() - new Date(m.start_at).getTime();
		const durationMin = Math.max(15, Math.round(durationMs / 60000));

		messages.push(
			newMsg({
				role: 'assistant',
				content: `Ищу окна команды «${m.team_name ?? ''}» на ${durationMin} мин (исключая текущее время встречи)…`
			})
		);
		const idx = messages.length - 1;
		await tick();
		scrollDown();

		let windows: MeetingWindow[] = [];
		try {
			const r = await findWindow(m.team_id, {
				duration_min: durationMin,
				days: 14,
				top_n: 5
			});
			windows = r.windows ?? [];
		} catch (e) {
			messages[idx] = {
				...messages[idx],
				content: `Не получилось найти окна: ${errStr(e)}`
			};
			return;
		}

		// Исключаем то же самое время, что уже занято этой встречей.
		const sameStart = new Date(m.start_at).toISOString();
		windows = windows.filter((w) => new Date(w.start_at).toISOString() !== sameStart);

		if (windows.length === 0) {
			messages[idx] = {
				...messages[idx],
				content: 'Других свободных окон на ближайшие 7 дней не нашлось.'
			};
			return;
		}

		const actions: MsgAction[] = windows.slice(0, 4).map<MsgAction>((w, i) => ({
			label: `${i + 1}. ${fmtWin(w)} — доступно ${w.available_count}/${w.total_count}`,
			kind: 'do_reschedule',
			payload: {
				meeting_id: m.id,
				meeting_title: m.title,
				start_at: w.start_at,
				end_at: w.end_at,
				available_count: w.available_count,
				total_count: w.total_count
			}
		}));
		actions.push({ label: 'Отмена', variant: 'ghost', kind: 'cancel' });

		messages[idx] = {
			...messages[idx],
			content: `Куда перенести «${m.title}»? Лучшие окна:`,
			actions
		};
	}

	async function flowRescheduleApply(p: Record<string, unknown> | undefined) {
		if (!p?.meeting_id || !p?.start_at || !p?.end_at) {
			messages.push(newMsg({ role: 'assistant', content: 'Не хватает данных для переноса.' }));
			return;
		}
		const meetingID = p.meeting_id as string;
		const startAt = p.start_at as string;
		const endAt = p.end_at as string;
		const title = (p.meeting_title as string) ?? 'встречу';

		messages.push(
			newMsg({ role: 'assistant', content: `Переношу «${title}» на ${fmtWinFromIso(startAt, endAt)}…` })
		);
		const idx = messages.length - 1;
		await tick();
		scrollDown();

		try {
			await updateMeeting(meetingID, { start_at: startAt, end_at: endAt });
		} catch (e) {
			messages[idx] = {
				...messages[idx],
				content: `Не получилось перенести: ${errStr(e)}`
			};
			return;
		}

		messages[idx] = {
			...messages[idx],
			content: `Готово. «${title}» перенесена на **${fmtWinFromIso(startAt, endAt)}**.`
		};
	}

	// Форматтер «ср, 20 мая, 11:00–12:00» из двух ISO.
	function fmtWinFromIso(startIso: string, endIso: string): string {
		try {
			const s = new Date(startIso);
			const e = new Date(endIso);
			const day = s.toLocaleDateString('ru', { weekday: 'short', day: 'numeric', month: 'short' });
			const fmtT = (d: Date) =>
				d.toLocaleTimeString('ru', { hour: '2-digit', minute: '2-digit' });
			return `${day}, ${fmtT(s)}–${fmtT(e)}`;
		} catch {
			return `${startIso} — ${endIso}`;
		}
	}

	// Шаг 2 экспорт-флоу: после выбора пресета спрашиваем формат (Excel или PDF).
	async function flowFormatPick(kind: string, label: string, kinds?: string[]) {
		messages.push(
			newMsg({
				role: 'assistant',
				content: `В каком формате выгрузить «${label}»?`,
				actions: [
					{
						label: 'Excel (.xlsx)',
						variant: 'primary',
						icon: 'ti-table',
						kind: 'do_export',
						payload: { kind, label, kinds, format: 'xlsx' }
					},
					{
						label: 'PDF',
						icon: 'ti-file-type-pdf',
						kind: 'do_export',
						payload: { kind, label, kinds, format: 'pdf' }
					},
					{ label: 'Отмена', variant: 'ghost', kind: 'cancel' }
				]
			})
		);
		await tick();
		scrollDown();
	}

	async function flowExportRun(
		kind: string,
		label: string,
		format: 'xlsx' | 'pdf',
		kinds?: string[]
	) {
		const placeholderIdx = messages.length;
		messages.push(
			newMsg({
				role: 'assistant',
				content: `Готовлю ${format === 'pdf' ? 'PDF' : 'Excel'} «${label}»…`
			})
		);
		await tick();
		scrollDown();

		try {
			if (format === 'pdf') {
				// PDF — берём JSON-датасет и рендерим тем же путём, что и на /reports.
				const tok = getAccessToken() ?? '';
				const params = new URLSearchParams({ format: 'json' });
				if (kinds && kinds.length > 0) params.set('kinds', kinds.join(','));
				const resp = await fetch(
					`${backendURL()}/api/v1/exports/${kind}?${params.toString()}`,
					{ headers: tok ? { Authorization: `Bearer ${tok}` } : {} }
				);
				if (!resp.ok) throw new Error(`status ${resp.status}`);
				const ds = (await resp.json()) as {
					kind: string;
					title: string;
					headers: string[];
					rows: unknown[][];
				};
				const filename = await renderPDFInline(ds, kind, label);
				messages[placeholderIdx] = {
					...messages[placeholderIdx],
					content: `Готово. PDF **${filename}** сохранён (${ds.rows.length} записей).`
				};
				return;
			}

			// xlsx — старый путь.
			const tok = getAccessToken() ?? '';
			const params = new URLSearchParams();
			if (kinds && kinds.length > 0) params.set('kinds', kinds.join(','));
			const query = params.toString() ? `?${params.toString()}` : '';
			const resp = await fetch(`${backendURL()}/api/v1/exports/${kind}${query}`, {
				headers: tok ? { Authorization: `Bearer ${tok}` } : {}
			});
			if (!resp.ok) throw new Error(`status ${resp.status}`);
			const blob = await resp.blob();
			const cd = resp.headers.get('Content-Disposition') ?? '';
			const m = cd.match(/filename="?([^"]+)"?/);
			const filename = m ? m[1] : `worktime-${kind}.xlsx`;

			const url = URL.createObjectURL(blob);
			const a = document.createElement('a');
			a.href = url;
			a.download = filename;
			document.body.appendChild(a);
			a.click();
			a.remove();
			URL.revokeObjectURL(url);

			messages[placeholderIdx] = {
				...messages[placeholderIdx],
				content: `Готово. Файл **${filename}** (${formatBytes(blob.size)}) сохранён.`
			};
		} catch (e) {
			messages[placeholderIdx] = {
				...messages[placeholderIdx],
				content: `Не удалось выгрузить: ${errStr(e)}`
			};
		}
	}

	// Лёгкий рендер PDF — тот же подход что на /reports, но завёрнут в локальную функцию.
	async function renderPDFInline(
		ds: { title: string; headers: string[]; rows: unknown[][] },
		kind: string,
		label: string
	): Promise<string> {
		if (typeof window === 'undefined') return 'pdf';
		const mod = await import('html2pdf.js');
		const html2pdf = mod.default;

		const dateStr = new Date().toISOString().slice(0, 10);
		const filename = `workie-${kind}-${dateStr}.pdf`;

		const container = document.createElement('div');
		container.style.cssText =
			'font-family: Arial, Helvetica, sans-serif; color:#0f172a; padding:24px; width:1024px; word-spacing:0.1em;';
		container.innerHTML = buildPDFHtml(ds, label);
		document.body.appendChild(container);

		try {
			await html2pdf()
				.from(container)
				.set({
					margin: [10, 12, 12, 12],
					filename,
					image: { type: 'jpeg', quality: 0.96 },
					html2canvas: { scale: 2, useCORS: true },
					jsPDF: { unit: 'mm', format: 'a4', orientation: 'landscape' }
				})
				.save();
		} finally {
			container.remove();
		}
		return filename;
	}

	function buildPDFHtml(
		ds: { title: string; headers: string[]; rows: unknown[][] },
		label: string
	): string {
		const now = new Date();
		const dateStr = now.toLocaleDateString('ru', { day: 'numeric', month: 'long', year: 'numeric' });
		const timeStr = now.toLocaleTimeString('ru', { hour: '2-digit', minute: '2-digit' });
		const esc = (s: string) =>
			s
				.replaceAll('&', '&amp;')
				.replaceAll('<', '&lt;')
				.replaceAll('>', '&gt;')
				.replaceAll('"', '&quot;');
		const head = ds.headers
			.map(
				(h) =>
					`<th style="text-align:left;padding:8px 10px;background:#f1f5f9;border-bottom:1px solid #cbd5e1;font-size:11px;text-transform:uppercase;letter-spacing:.5px;color:#475569;">${esc(h)}</th>`
			)
			.join('');
		const rowsHtml = ds.rows
			.map((row, idx) => {
				const cells = row
					.map(
						(v) =>
							`<td style="padding:6px 10px;border-bottom:1px solid #e2e8f0;font-size:11px;color:#0f172a;vertical-align:top;">${esc(String(v ?? ''))}</td>`
					)
					.join('');
				const bg = idx % 2 === 0 ? '#ffffff' : '#fafafa';
				return `<tr style="background:${bg};">${cells}</tr>`;
			})
			.join('');
		const empty =
			ds.rows.length === 0
				? `<div style="padding:24px;text-align:center;color:#64748b;font-size:13px;">Нет данных</div>`
				: '';
		return `
			<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:18px;border-bottom:2px solid #3b82f6;padding-bottom:10px;">
				<div>
					<div style="font-size:11px;color:#64748b;letter-spacing:.5px;text-transform:uppercase;">Workie · отчёт</div>
					<div style="font-size:22px;font-weight:700;color:#0f172a;margin-top:2px;word-spacing:0.25em;">${esc(label).replace(/ /g, '&nbsp;')}</div>
					<div style="font-size:12px;color:#475569;margin-top:4px;">${esc(ds.title).replace(/ /g, '&nbsp;')}</div>
				</div>
				<div style="text-align:right;font-size:11px;color:#64748b;">
					Сформировано<br/>
					<span style="color:#0f172a;font-weight:600;">${dateStr}, ${timeStr}</span><br/>
					Записей: <b>${ds.rows.length}</b>
				</div>
			</div>
			${empty}
			<table style="width:100%;border-collapse:collapse;">
				<thead><tr>${head}</tr></thead>
				<tbody>${rowsHtml}</tbody>
			</table>
		`;
	}

	function formatBytes(b: number): string {
		if (b < 1024) return `${b} Б`;
		if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} КБ`;
		return `${(b / (1024 * 1024)).toFixed(2)} МБ`;
	}

	// --- Утилиты форматирования. Время — в TZ зрителя, чтобы не путать UTC. ---

	function fmtDate(iso: string): string {
		try {
			return new Date(iso).toLocaleDateString('ru', {
				day: 'numeric',
				month: 'short',
				timeZone: viewerTZ
			});
		} catch {
			return iso;
		}
	}
	function fmtTime(iso: string): string {
		try {
			return new Date(iso).toLocaleTimeString('ru', {
				hour: '2-digit',
				minute: '2-digit',
				timeZone: viewerTZ
			});
		} catch {
			return iso;
		}
	}
	function tzLabel(): string {
		// "Europe/Moscow" → "MSK" не выйдет без таблицы. Берём городскую часть.
		const parts = viewerTZ.split('/');
		return parts[parts.length - 1].replace(/_/g, ' ');
	}
	function fmtWin(w: MeetingWindow): string {
		return `${fmtDate(w.start_at)} ${fmtTime(w.start_at)}–${fmtTime(w.end_at)}`;
	}
	function errStr(e: unknown): string {
		return e instanceof ApiError ? e.message : String(e);
	}

	function scrollDown(smooth = false) {
		// 1. Прокручиваем сам список сообщений вниз (внутренний overflow).
		// 2. scrollIntoView на якорь — на случай если страница ещё и сама не у чата
		//    (например, после клика по «Быстрому вопросу» в правой колонке).
		requestAnimationFrame(() => {
			if (scrollEl) scrollEl.scrollTop = scrollEl.scrollHeight;
			bottomAnchor?.scrollIntoView({
				behavior: smooth ? 'smooth' : 'auto',
				block: 'end'
			});
		});
	}

	function onKeydown(e: KeyboardEvent) {
		if (e.key === 'Enter' && !e.shiftKey) {
			e.preventDefault();
			send();
		}
	}
</script>

<div class="page-header">
	<div>
		<h1>ИИ-ассистент</h1>
		<div class="page-header__subtitle">
			Чат с системой о рабочем времени, доступности, рекомендациях
		</div>
	</div>
	<div class="page-header__actions">
		{#if messages.length > 0 || conversationID}
			<Button
				variant="ghost"
				icon="ti-trash"
				onclick={clearChat}
				disabled={clearing || sending}
			>
				{clearing ? 'Очищаю…' : 'Очистить чат'}
			</Button>
		{/if}
	</div>
</div>

<div class="grid-2-1" style="height: calc(100vh - 200px);">
	<Card padded={false} title="Чат">
		<div bind:this={scrollEl} style="height: calc(100% - 80px); overflow-y: auto; padding: 12px;">
			{#if messages.length === 0}
				<div class="text-text-3 text-sm" style="text-align: center; padding: 32px;">
					Введите вопрос или выберите шаблон
				</div>
			{:else}
				<div class="space-y-3">
					{#each messages as m, mi (m.id)}
						<div class="flex gap-2" class:flex-row-reverse={m.role === 'user'}>
							<div
								class="header__logo-icon"
								style:background={m.role === 'user' ? 'var(--purple-bg)' : 'var(--info-bg)'}
								style:color={m.role === 'user' ? 'var(--purple-text)' : 'var(--info-text)'}
							>
								<i class="ti {m.role === 'user' ? 'ti-user' : 'ti-sparkles'}"></i>
							</div>
							<div style="max-width: 80%;">
								{#if m.role === 'assistant'}
									<div
										class="msg-bubble msg-bubble--ai"
										style="padding: 8px 12px; border-radius: var(--radius-md); background: var(--surface-2); font-size: 13px;"
									>
										{@html renderMd(m.content || '…')}
									</div>
								{:else}
									<div
										style="padding: 8px 12px; border-radius: var(--radius-md); background: var(--surface-2); font-size: 13px; white-space: pre-wrap;"
									>
										{m.content}
									</div>
								{/if}

								{#if m.actions && !m.consumed}
									<div class="msg-actions">
										{#each m.actions as a, ai (a.label + ai)}
											<button
												class="msg-action msg-action--{a.variant ?? 'default'}"
												onclick={() => handleAction(mi, a)}
											>
												{#if a.icon}<i class="ti {a.icon}"></i>{/if}
												{a.label}
											</button>
										{/each}
									</div>
								{/if}
							</div>
						</div>
					{/each}
					{#if sending}
						<div class="text-text-3 text-xs" style="padding: 8px 0;">AI думает…</div>
					{/if}
					<div bind:this={bottomAnchor} style="height: 1px;"></div>
				</div>
			{/if}
		</div>

		<div
			style="padding: 12px; border-top: 0.5px solid var(--border); display: flex; gap: 8px;"
		>
			<input
				type="text"
				bind:value={input}
				placeholder="Сообщение"
				onkeydown={onKeydown}
				disabled={sending}
				style="flex: 1; height: 36px;"
			/>
			<Button
				variant="primary"
				icon="ti-send"
				onclick={() => send()}
				disabled={sending || !input.trim()}>Отправить</Button
			>
		</div>
	</Card>

	<Card title="Быстрые вопросы">
		<div class="space-y-2">
			{#each suggestions as s (s)}
				<button
					class="btn btn--ghost"
					style="width: 100%; justify-content: flex-start; height: auto; padding: 8px 12px; text-align: left; white-space: normal;"
					onclick={() => send(s)}
					disabled={sending}
				>
					<i class="ti ti-message-circle"></i>
					{s}
				</button>
			{/each}
		</div>

		<div
			class="text-text-3 text-xs"
			style="margin-top: 16px; padding-top: 12px; border-top: 0.5px solid var(--border);"
		>
			Спросите про окно для встречи — предложу запланировать и разошлю уведомления участникам.
		</div>
	</Card>
</div>

<style>
	:global(.msg-bubble--ai) {
		line-height: 1.45;
	}
	:global(.msg-bubble--ai > *:first-child) {
		margin-top: 0;
	}
	:global(.msg-bubble--ai > *:last-child) {
		margin-bottom: 0;
	}
	:global(.msg-bubble--ai p) {
		margin: 0 0 8px;
	}
	:global(.msg-bubble--ai p:last-child) {
		margin-bottom: 0;
	}
	:global(.msg-bubble--ai strong) {
		font-weight: 600;
		color: var(--text);
	}
	:global(.msg-bubble--ai em) {
		font-style: italic;
	}
	:global(.msg-bubble--ai ul),
	:global(.msg-bubble--ai ol) {
		margin: 0 0 8px;
		padding-left: 20px;
	}
	:global(.msg-bubble--ai li) {
		margin: 2px 0;
	}
	:global(.msg-bubble--ai li > p) {
		margin: 0;
	}
	:global(.msg-bubble--ai h1),
	:global(.msg-bubble--ai h2),
	:global(.msg-bubble--ai h3),
	:global(.msg-bubble--ai h4) {
		font-size: 14px;
		font-weight: 600;
		margin: 10px 0 4px;
		color: var(--text);
	}
	:global(.msg-bubble--ai h1) {
		font-size: 15px;
	}
	:global(.msg-bubble--ai code) {
		font-family: 'JetBrains Mono', ui-monospace, monospace;
		font-size: 12px;
		background: var(--surface-3);
		border: 0.5px solid var(--border);
		border-radius: 4px;
		padding: 1px 5px;
	}
	:global(.msg-bubble--ai pre) {
		background: var(--surface-3);
		border: 0.5px solid var(--border);
		border-radius: var(--radius-md);
		padding: 10px 12px;
		margin: 8px 0;
		overflow-x: auto;
	}
	:global(.msg-bubble--ai pre code) {
		background: transparent;
		border: none;
		padding: 0;
		font-size: 12px;
	}
	:global(.msg-bubble--ai a) {
		color: var(--info-strong);
		text-decoration: underline;
		text-underline-offset: 2px;
	}
	:global(.msg-bubble--ai blockquote) {
		margin: 8px 0;
		padding: 4px 10px;
		border-left: 3px solid var(--border-2);
		color: var(--text-2);
	}

	.msg-actions {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
		margin-top: 8px;
	}
	.msg-action {
		font-size: 12px;
		padding: 5px 10px;
		border-radius: 6px;
		cursor: pointer;
		border: 0.5px solid var(--border-2);
		background: var(--surface);
		color: var(--text);
		display: inline-flex;
		align-items: center;
		gap: 4px;
		transition: background 0.12s;
	}
	.msg-action:hover {
		background: var(--surface-2);
	}
	.msg-action--primary {
		background: var(--info-strong);
		color: var(--surface);
		border-color: var(--info-strong);
	}
	.msg-action--primary:hover {
		filter: brightness(1.05);
		background: var(--info-strong);
	}
	.msg-action--ghost {
		background: transparent;
	}
	.msg-action--danger {
		background: var(--danger-bg);
		color: var(--danger-strong);
		border-color: var(--danger-strong);
	}
</style>
