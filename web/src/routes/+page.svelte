<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { browser } from '$app/environment';
	import { getAccessToken } from '$lib/api/client';

	onMount(() => {
		if (!browser) return;
		if (getAccessToken()) {
			goto('/dashboard');
		} else {
			goto('/login');
		}
	});
</script>

<div class="flex items-center justify-center min-h-screen text-text-3 text-sm">
	Загрузка…
</div>
