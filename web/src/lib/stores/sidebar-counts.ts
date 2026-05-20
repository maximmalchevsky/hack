// Живые счётчики для бейджей в Sidebar. Подтягиваются параллельно из 3 API
// + локального notifications-store. Тихо игнорируют ошибки (403/401/500) —
// если что-то отвалилось, бейдж просто не показывается.

import { writable, get } from 'svelte/store';
import { browser } from '$app/environment';
import { getDiagnostics } from '$lib/api/diagnostics';
import { listConflicts } from '$lib/api/conflicts';
import { getHRRoadmap } from '$lib/api/hr';
import { user } from './user';
import { notifications } from './notifications';

export interface SidebarCounts {
	diagnostics: number | null; // stale + needs_confirm
	conflicts: number | null;
	hrRoadmap: number | null;
	notifications: number | null; // unread
}

const empty: SidebarCounts = {
	diagnostics: null,
	conflicts: null,
	hrRoadmap: null,
	notifications: null
};

function createStore() {
	const { subscribe, update, set } = writable<SidebarCounts>(empty);
	let intervalID: ReturnType<typeof setInterval> | null = null;
	let unsubNotifs: (() => void) | null = null;

	async function reload() {
		const u = get(user);
		if (!u) return;

		const role = u.role;
		const canDiag = ['manager', 'pm', 'hr', 'admin'].includes(role);
		const canConflicts = ['manager', 'pm', 'hr', 'admin'].includes(role);
		const canRoadmap = ['hr', 'admin'].includes(role);

		const [diag, conf, road] = await Promise.allSettled([
			canDiag ? getDiagnostics() : Promise.resolve(null),
			canConflicts ? listConflicts() : Promise.resolve(null),
			canRoadmap ? getHRRoadmap() : Promise.resolve(null)
		]);

		update((s) => {
			if (diag.status === 'fulfilled' && diag.value) {
				const g = diag.value;
				s.diagnostics = (g.stale?.length ?? 0) + (g.needs_confirm?.length ?? 0);
			} else if (!canDiag) {
				s.diagnostics = null;
			}

			if (conf.status === 'fulfilled' && conf.value) {
				s.conflicts = conf.value.conflicts?.length ?? 0;
			} else if (!canConflicts) {
				s.conflicts = null;
			}

			if (road.status === 'fulfilled' && road.value) {
				s.hrRoadmap = road.value.items?.length ?? 0;
			} else if (!canRoadmap) {
				s.hrRoadmap = null;
			}

			return s;
		});
	}

	function start() {
		if (!browser) return;
		stop();
		reload();
		// Перезагружаем раз в 60 сек.
		intervalID = setInterval(reload, 60_000);
		// Подписываемся на notifications для счётчика непрочитанных.
		unsubNotifs = notifications.subscribe((n) => {
			update((s) => ({ ...s, notifications: n.unread }));
		});
	}

	function stop() {
		if (intervalID) {
			clearInterval(intervalID);
			intervalID = null;
		}
		if (unsubNotifs) {
			unsubNotifs();
			unsubNotifs = null;
		}
	}

	return {
		subscribe,
		reload,
		start,
		stop,
		reset: () => set(empty)
	};
}

export const sidebarCounts = createStore();
