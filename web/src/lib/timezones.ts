export type TimezoneOption = {
	value: string;
	label: string;
	offsetMin: number;
};

const FALLBACK = [
	'UTC',
	'Europe/Kaliningrad',
	'Europe/Moscow',
	'Asia/Yekaterinburg',
	'Asia/Novosibirsk',
	'Asia/Krasnoyarsk',
	'Asia/Irkutsk',
	'Asia/Vladivostok',
	'Europe/Lisbon',
	'Europe/Belgrade',
	'Europe/Berlin',
	'Europe/London',
	'America/New_York',
	'America/Los_Angeles',
	'Asia/Tokyo',
	'Asia/Shanghai',
	'Asia/Dubai',
	'Asia/Kolkata',
	'Australia/Sydney'
];

function rawZones(): string[] {
	try {
		const fn = (Intl as unknown as { supportedValuesOf?: (k: string) => string[] })
			.supportedValuesOf;
		if (typeof fn === 'function') {
			const list = fn('timeZone');
			if (list && list.length > 0) return list;
		}
	} catch {
		// noop
	}
	return FALLBACK;
}

function offsetMinutes(tz: string, now: Date): number {
	try {
		const dtf = new Intl.DateTimeFormat('en-US', {
			timeZone: tz,
			timeZoneName: 'shortOffset'
		});
		const parts = dtf.formatToParts(now);
		const tzPart = parts.find((p) => p.type === 'timeZoneName');
		if (!tzPart) return 0;
		// "GMT", "GMT+3", "GMT-08:30"
		if (tzPart.value === 'GMT' || tzPart.value === 'UTC') return 0;
		const m = tzPart.value.match(/(?:GMT|UTC)([+-])(\d{1,2})(?::(\d{2}))?/);
		if (!m) return 0;
		const sign = m[1] === '+' ? 1 : -1;
		const h = parseInt(m[2], 10);
		const mm = m[3] ? parseInt(m[3], 10) : 0;
		return sign * (h * 60 + mm);
	} catch {
		return 0;
	}
}

function formatOffset(min: number): string {
	if (min === 0) return 'UTC';
	const sign = min > 0 ? '+' : '−';
	const abs = Math.abs(min);
	const h = Math.floor(abs / 60);
	const m = abs % 60;
	return m === 0 ? `UTC${sign}${h}` : `UTC${sign}${h}:${String(m).padStart(2, '0')}`;
}

let cached: TimezoneOption[] | null = null;

export function timezoneOptions(): TimezoneOption[] {
	if (cached) return cached;
	const now = new Date();
	const items = rawZones().map((tz) => {
		const off = offsetMinutes(tz, now);
		return {
			value: tz,
			offsetMin: off,
			label: `${tz} · ${formatOffset(off)}`
		};
	});
	items.sort((a, b) => a.offsetMin - b.offsetMin || a.value.localeCompare(b.value));
	cached = items;
	return items;
}
