<script lang="ts">
	export type TimelineEventKind = 'meeting' | 'task' | 'focus' | 'conflict';

	export interface TimelineEvent {
		// Позиция в треке (0..100%).
		leftPct: number;
		widthPct: number;
		kind: TimelineEventKind;
		title?: string;
		// Опционально: ISO-строки для tooltip-а.
		startAt?: string;
		endAt?: string;
		subtitle?: string;
	}

	export interface TimelineRow {
		day: string;
		events: TimelineEvent[];
	}

	interface Props {
		rows: TimelineRow[];
	}

	let { rows }: Props = $props();

	const KIND_LABEL: Record<TimelineEventKind, string> = {
		meeting: 'Встреча',
		task: 'Задача',
		focus: 'Фокус-время',
		conflict: 'Конфликт'
	};

	function fmtTime(iso?: string): string {
		if (!iso) return '';
		try {
			return new Date(iso).toLocaleTimeString('ru', { hour: '2-digit', minute: '2-digit' });
		} catch {
			return '';
		}
	}

	function tooltipText(ev: TimelineEvent): string {
		const lines: string[] = [];
		lines.push(ev.title || KIND_LABEL[ev.kind]);
		if (ev.startAt || ev.endAt) {
			lines.push(`${fmtTime(ev.startAt)}${ev.endAt ? '–' + fmtTime(ev.endAt) : ''}`);
		}
		if (ev.subtitle) lines.push(ev.subtitle);
		if (ev.kind === 'conflict') lines.push('Пересекается по времени с другим событием');
		return lines.join('\n');
	}

	let hover = $state<{
		text: string;
		anchorX: number;
		anchorTop: number;
		anchorBottom: number;
		left: number;
		top: number;
	} | null>(null);
	let tooltipEl: HTMLDivElement | null = $state(null);

	function onEnter(e: MouseEvent, ev: TimelineEvent) {
		const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
		hover = {
			text: tooltipText(ev),
			anchorX: rect.left + rect.width / 2,
			anchorTop: rect.top,
			anchorBottom: rect.bottom,
			left: -9999,
			top: -9999
		};
	}
	function onLeave() {
		hover = null;
	}

	$effect(() => {
		if (!hover || !tooltipEl) return;
		const r = tooltipEl.getBoundingClientRect();
		const PAD = 8;
		const GAP = 6;

		let left = hover.anchorX - r.width / 2;
		if (left < PAD) left = PAD;
		if (left + r.width > window.innerWidth - PAD) left = window.innerWidth - r.width - PAD;

		let top = hover.anchorTop - r.height - GAP;
		if (top < PAD) top = hover.anchorBottom + GAP;

		if (left !== hover.left || top !== hover.top) {
			hover = { ...hover, left, top };
		}
	});
</script>

<div>
	{#each rows as row (row.day)}
		<div class="timeline-row">
			<div class="timeline-row__day">{row.day}</div>
			<div class="timeline-row__track">
				{#each row.events as ev, i (row.day + '-' + i)}
					<div
						role="button"
						tabindex="0"
						class="timeline-event te--{ev.kind}"
						style:left="{ev.leftPct}%"
						style:width="{ev.widthPct}%"
						onmouseenter={(e) => onEnter(e, ev)}
						onmouseleave={onLeave}
						onfocus={(e) => onEnter(e as unknown as MouseEvent, ev)}
						onblur={onLeave}
					></div>
				{/each}
			</div>
		</div>
	{/each}
</div>

{#if hover}
	<div
		bind:this={tooltipEl}
		class="timeline-tooltip"
		style:left="{hover.left}px"
		style:top="{hover.top}px"
	>
		{#each hover.text.split('\n') as line, i (i)}
			<div>{line}</div>
		{/each}
	</div>
{/if}

<style>
	.timeline-tooltip {
		position: fixed;
		background: var(--surface);
		color: var(--text);
		border: 0.5px solid var(--border-2);
		border-radius: var(--radius-md);
		padding: 8px 10px;
		font-size: 12px;
		line-height: 1.45;
		width: max-content;
		max-width: 320px;
		box-shadow: 0 6px 18px rgba(0, 0, 0, 0.12);
		pointer-events: none;
		white-space: normal;
		word-break: keep-all;
		z-index: 1000;
	}
	.timeline-tooltip > div {
		min-height: 1em;
	}
	.timeline-tooltip > div:first-child {
		font-weight: 600;
		margin-bottom: 2px;
	}
</style>
