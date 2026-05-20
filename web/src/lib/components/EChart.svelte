<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import * as echarts from 'echarts';

	interface Props {
		option: echarts.EChartsCoreOption;
		height?: string;
	}

	let { option, height = '280px' }: Props = $props();

	let container: HTMLDivElement | null = $state(null);
	let chart: echarts.ECharts | null = null;

	function resize() {
		chart?.resize();
	}

	onMount(() => {
		if (!container) return;
		chart = echarts.init(container);
		chart.setOption(option);
		window.addEventListener('resize', resize);
	});

	onDestroy(() => {
		window.removeEventListener('resize', resize);
		chart?.dispose();
		chart = null;
	});

	$effect(() => {
		// Реактивное обновление при изменении option.
		if (chart) chart.setOption(option, true);
	});
</script>

<div bind:this={container} style:height style:width="100%"></div>
