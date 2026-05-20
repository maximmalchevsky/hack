<script lang="ts">
	import '../app.css';
	import { onMount } from 'svelte';
	import { theme } from '$lib/stores/theme';

	let { children } = $props();

	onMount(() => {
		// Применяем тему сразу при загрузке (избегаем flash of light theme).
		const t = localStorage.getItem('wts-theme') ?? 'light';
		document.documentElement.setAttribute('data-theme', t);
		theme.set(t === 'dark' ? 'dark' : 'light');
	});
</script>

{@render children()}
