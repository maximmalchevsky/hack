<script lang="ts">
	import type { Snippet, HTMLButtonAttributes } from 'svelte/elements';

	interface Props extends HTMLButtonAttributes {
		variant?: 'default' | 'primary' | 'danger' | 'ghost';
		size?: 'default' | 'sm' | 'xs';
		icon?: string;
		children?: Snippet;
	}

	let {
		variant = 'default',
		size = 'default',
		icon,
		children,
		...rest
	}: Props = $props();

	const cls = $derived(
		[
			'btn',
			variant === 'primary' && 'btn--primary',
			variant === 'danger' && 'btn--danger',
			variant === 'ghost' && 'btn--ghost',
			size === 'sm' && 'btn--sm',
			size === 'xs' && 'btn--xs'
		]
			.filter(Boolean)
			.join(' ')
	);
</script>

<button class={cls} {...rest}>
	{#if icon}<i class="ti {icon}"></i>{/if}
	{@render children?.()}
</button>
