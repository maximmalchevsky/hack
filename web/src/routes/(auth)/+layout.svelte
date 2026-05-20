<script lang="ts">
	let { children } = $props();
</script>

<div class="min-h-screen flex items-center justify-center p-6" style="background: var(--bg);">
	<div class="w-full max-w-sm">
		{@render children()}
	</div>
</div>
