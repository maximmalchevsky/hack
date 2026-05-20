<script lang="ts">
	export type HeatmapCellState = 'free' | 'busy' | 'conflict' | 'off' | 'focus';

	export interface HeatmapRow {
		label: string;
		sub?: string;
		cells: HeatmapCellState[];
		avatar?: { initials: string; variant?: 'default' | 'purple' | 'teal' };
		href?: string; // если задан — label оборачивается в ссылку
	}

	interface Props {
		rows: HeatmapRow[];
		hours: (string | number)[];
		onCellClick?: (rowIndex: number, cellIndex: number, state: HeatmapCellState) => void;
		// cellTooltip — если задан и возвращает не пустую строку, при hover
		// показывается всплывающий блок с этим текстом (multi-line, \n → перенос).
		cellTooltip?: (rowIndex: number, cellIndex: number, state: HeatmapCellState) => string | null;
	}

	let { rows, hours, onCellClick, cellTooltip }: Props = $props();

	const cellsTemplate = $derived(`repeat(${hours.length}, 1fr)`);

	// anchor — точка-якорь (центр верхней границы ячейки).
	// left/top — финальные left/top тултипа, считаются после измерения его размера.
	let hover = $state<{
		text: string;
		anchorX: number;
		anchorTop: number;
		anchorBottom: number;
		left: number;
		top: number;
	} | null>(null);
	let tooltipEl: HTMLDivElement | null = $state(null);

	function onEnter(e: MouseEvent, ri: number, ci: number, state: HeatmapCellState) {
		if (!cellTooltip) return;
		const text = cellTooltip(ri, ci, state);
		if (!text) return;
		const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
		hover = {
			text,
			anchorX: rect.left + rect.width / 2,
			anchorTop: rect.top,
			anchorBottom: rect.bottom,
			// до измерения отрисовываем за экраном, чтобы не было прыжка.
			left: -9999,
			top: -9999
		};
	}
	function onLeave() {
		hover = null;
	}

	// После рендера тултипа измеряем его и кладём в видимую область.
	$effect(() => {
		if (!hover || !tooltipEl) return;
		const r = tooltipEl.getBoundingClientRect();
		const PAD = 8;
		const GAP = 6;

		// X: центрируем под anchor, но клиппим к экрану.
		let left = hover.anchorX - r.width / 2;
		if (left < PAD) left = PAD;
		if (left + r.width > window.innerWidth - PAD) left = window.innerWidth - r.width - PAD;

		// Y: пытаемся над ячейкой; если не лезет — под ячейкой.
		let top = hover.anchorTop - r.height - GAP;
		if (top < PAD) top = hover.anchorBottom + GAP;

		if (left !== hover.left || top !== hover.top) {
			hover = { ...hover, left, top };
		}
	});
</script>

<div class="heatmap-hours">
	<div></div>
	<div class="heatmap-hours__cells" style:grid-template-columns={cellsTemplate}>
		{#each hours as h (h)}
			<div>{h}</div>
		{/each}
	</div>
</div>

<div class="heatmap">
	{#each rows as row, ri (row.label + ri)}
		<div class="heatmap-row">
			{#if row.href}
				<a class="heatmap-row__label heatmap-row__label--link" href={row.href}>
					{#if row.avatar}
						<span class="avatar avatar--{row.avatar.variant ?? 'default'}">
							{row.avatar.initials}
						</span>
					{/if}
					<span>
						{row.label}
						{#if row.sub}
							<span style="color: var(--text-3); font-size: 11px;"> · {row.sub}</span>
						{/if}
					</span>
				</a>
			{:else}
				<div class="heatmap-row__label">
					{#if row.avatar}
						<span class="avatar avatar--{row.avatar.variant ?? 'default'}">
							{row.avatar.initials}
						</span>
					{/if}
					<span>
						{row.label}
						{#if row.sub}
							<span style="color: var(--text-3); font-size: 11px;"> · {row.sub}</span>
						{/if}
					</span>
				</div>
			{/if}
			<div class="heatmap-row__cells" style:grid-template-columns={cellsTemplate}>
				{#each row.cells as state, ci (ri + '-' + ci)}
					<button
						type="button"
						class="heatmap-cell hc--{state}"
						aria-label="{row.label}, {hours[ci]}"
						onclick={() => onCellClick?.(ri, ci, state)}
						onmouseenter={(e) => onEnter(e, ri, ci, state)}
						onmouseleave={onLeave}
						onfocus={(e) => onEnter(e as unknown as MouseEvent, ri, ci, state)}
						onblur={onLeave}
					></button>
				{/each}
			</div>
		</div>
	{/each}
</div>

<div class="legend">
	<div class="legend-item">
		<span class="legend-item__swatch hc--free"></span>Свободен
	</div>
	<div class="legend-item">
		<span class="legend-item__swatch hc--busy"></span>Занят
	</div>
	<div class="legend-item">
		<span class="legend-item__swatch hc--conflict"></span>Конфликт
	</div>
	<div class="legend-item">
		<span class="legend-item__swatch hc--off"></span>Вне графика
	</div>
	<div class="legend-item">
		<span class="legend-item__swatch hc--focus"></span>Фокус-время
	</div>
</div>

{#if hover}
	<div
		bind:this={tooltipEl}
		class="heatmap-tooltip"
		style:left="{hover.left}px"
		style:top="{hover.top}px"
	>
		{#each hover.text.split('\n') as line, i (i)}
			<div>{line}</div>
		{/each}
	</div>
{/if}

<style>
	.heatmap-tooltip {
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
	.heatmap-tooltip > div {
		min-height: 1em;
	}
	.heatmap-tooltip > div:first-child {
		font-weight: 600;
		margin-bottom: 2px;
	}
</style>
