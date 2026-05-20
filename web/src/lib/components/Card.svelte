<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		title?: string;
		subtitle?: string;
		caption?: string;
		padded?: boolean;
		actions?: Snippet;
		children?: Snippet;
	}

	let { title, subtitle, caption, padded = false, actions, children }: Props = $props();
</script>

<div class="card {padded ? 'card--padded' : ''}">
	{#if title || subtitle || caption || actions}
		<div class="card__header">
			<div>
				{#if caption}<div class="card__caption">{caption}</div>{/if}
				{#if title}<div class="card__title">{title}</div>{/if}
				{#if subtitle}<div class="card__subtitle">{subtitle}</div>{/if}
			</div>
			{#if actions}<div>{@render actions()}</div>{/if}
		</div>
	{/if}
	{@render children?.()}
</div>
