<script lang="ts">
	import type { Snippet } from 'svelte';
	import MetricInfo from './MetricInfo.svelte';

	interface Props {
		title?: string;
		subtitle?: string;
		caption?: string;
		padded?: boolean;
		// fill — растянуть карточку на всю высоту родителя (flex-column).
		// Контент тогда сам распределяет высоту: используется в чате, где лента
		// сообщений должна скроллиться внутри, а инпут — прилипать к низу.
		fill?: boolean;
		// Если задан — рядом с title появится (i) с расшифровкой метрики.
		metricLetter?: 'A' | 'C' | 'L' | 'Z' | 'H' | 'R';
		actions?: Snippet;
		children?: Snippet;
	}

	let { title, subtitle, caption, padded = false, fill = false, metricLetter, actions, children }: Props = $props();
</script>

<div class="card {padded ? 'card--padded' : ''} {fill ? 'card--fill' : ''}">
	{#if title || subtitle || caption || actions}
		<div class="card__header">
			<div>
				{#if caption}<div class="card__caption">{caption}</div>{/if}
				{#if title}
					<div class="card__title">
						{title}
						{#if metricLetter}<MetricInfo letter={metricLetter} size="md" />{/if}
					</div>
				{/if}
				{#if subtitle}<div class="card__subtitle">{subtitle}</div>{/if}
			</div>
			{#if actions}<div>{@render actions()}</div>{/if}
		</div>
	{/if}
	{@render children?.()}
</div>
