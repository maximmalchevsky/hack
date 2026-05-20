<script lang="ts">
	import type { Snippet } from 'svelte';
	import MetricInfo from './MetricInfo.svelte';

	type Letter = 'A' | 'C' | 'L' | 'Z' | 'H' | 'R';

	interface Props {
		label: string;
		value: string | number;
		valueVariant?: 'default' | 'success' | 'warning' | 'danger';
		trend?: string;
		trendDirection?: 'up' | 'down' | 'flat';
		labelIcon?: string;
		// Если задана буква метрики — справа от label появляется (i)-иконка
		// с popover-расшифровкой (что такое A/C/L/Z/H/R).
		metricLetter?: Letter;
		extra?: Snippet;
	}

	let {
		label,
		value,
		valueVariant = 'default',
		trend,
		trendDirection,
		labelIcon,
		metricLetter,
		extra
	}: Props = $props();

	const trendIcon = $derived(
		trendDirection === 'up' ? 'ti-arrow-up' : trendDirection === 'down' ? 'ti-arrow-down' : 'ti-minus'
	);

	const valueClass = $derived(
		valueVariant === 'default' ? '' : `stat__value--${valueVariant}`
	);
	const trendClass = $derived(
		trendDirection === 'up'
			? 'stat__trend--up'
			: trendDirection === 'down'
				? 'stat__trend--down'
				: ''
	);
</script>

<div class="stat">
	<div class="stat__label">
		{#if labelIcon}<i class="ti {labelIcon}"></i>{/if}
		{label}
		{#if metricLetter}<MetricInfo letter={metricLetter} />{/if}
	</div>
	<div class="stat__value {valueClass}">{value}</div>
	{#if trend}
		<div class="stat__trend {trendClass}">
			<i class="ti {trendIcon}"></i>{trend}
		</div>
	{/if}
	{@render extra?.()}
</div>
