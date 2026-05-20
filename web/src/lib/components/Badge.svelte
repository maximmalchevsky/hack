<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		variant?: 'info' | 'success' | 'warning' | 'danger' | 'purple' | 'teal' | 'neutral';
		dot?: boolean;
		title?: string;
		children?: Snippet;
	}

	let { variant = 'neutral', dot = false, title, children }: Props = $props();
</script>

<span class="badge badge--{variant}" title={title} class:badge--hint={!!title}>
	{#if dot}<span class="badge__dot" style:background={`var(--${variant}-strong, var(--${variant}-text))`}></span>{/if}
	{@render children?.()}
</span>

<style>
	.badge--hint {
		cursor: help;
	}
</style>
