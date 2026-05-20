<script lang="ts">
	interface Props {
		initials: string;
		size?: 'xs' | 'sm' | 'md' | 'lg' | 'xl';
		variant?: 'default' | 'purple' | 'teal';
	}

	let { initials, size = 'sm', variant = 'default' }: Props = $props();

	const cls = $derived(
		[
			'avatar',
			size === 'md' && 'avatar--md',
			size === 'lg' && 'avatar--lg',
			size === 'xl' && 'avatar--xl',
			variant === 'purple' && 'avatar--purple',
			variant === 'teal' && 'avatar--teal'
		]
			.filter(Boolean)
			.join(' ')
	);
</script>

<span class={cls}>{initials}</span>
