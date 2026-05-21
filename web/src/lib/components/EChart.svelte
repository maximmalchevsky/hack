<script lang="ts">
	import { onMount, onDestroy } from 'svelte';

	interface Props {
		option: Record<string, unknown>;
		height?: string;
	}

	let { option, height = '280px' }: Props = $props();

	let container: HTMLDivElement | null = $state(null);
	// eslint-disable-next-line @typescript-eslint/no-explicit-any
	let chart: any = null;
	let ready = $state(false);

	function resize() {
		chart?.resize();
	}

	// Динамический импорт ECharts — модуль ~500KB подгружается только когда
	// компонент реально монтируется. Так страницы без графиков (login, profile,
	// scheduler) грузятся мгновенно.
	onMount(async () => {
		if (!container) return;

		const [coreMod, renderersMod, chartsMod, componentsMod] = await Promise.all([
			import('echarts/core'),
			import('echarts/renderers'),
			import('echarts/charts'),
			import('echarts/components')
		]);

		coreMod.use([
			renderersMod.CanvasRenderer,
			chartsMod.BarChart,
			chartsMod.LineChart,
			chartsMod.PieChart,
			componentsMod.TooltipComponent,
			componentsMod.GridComponent,
			componentsMod.LegendComponent,
			componentsMod.DatasetComponent,
			componentsMod.TitleComponent
		]);

		chart = coreMod.init(container);
		chart.setOption(option);
		ready = true;
		window.addEventListener('resize', resize);
	});

	onDestroy(() => {
		window.removeEventListener('resize', resize);
		chart?.dispose();
		chart = null;
	});

	$effect(() => {
		// Реактивное обновление при изменении option.
		// БЕЗ notMerge=true — обновляем только то что поменялось, в разы быстрее
		// при интерактиве (hover/click по легенде/тултипу).
		if (ready && chart) chart.setOption(option);
	});
</script>

<div bind:this={container} style:height style:width="100%"></div>
