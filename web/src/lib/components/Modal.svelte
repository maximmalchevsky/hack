<script lang="ts">
	// Modal — общий wrapper для модальных окон. Использование:
	//
	// <Modal open={isOpen} title="Заголовок" onClose={() => isOpen = false}>
	//   <p>Содержимое</p>
	//   {#snippet footer()}
	//     <Button onclick={...}>OK</Button>
	//   {/snippet}
	// </Modal>
	//
	// Закрытие: Escape, клик по overlay (если closeOnOverlay), крестик в шапке.

	import { onMount, onDestroy } from 'svelte';
	import { browser } from '$app/environment';
	import type { Snippet } from 'svelte';

	interface Props {
		open: boolean;
		title?: string;
		size?: 'sm' | 'md' | 'lg';
		closeOnOverlay?: boolean;
		showClose?: boolean;
		onClose: () => void;
		children?: Snippet;
		footer?: Snippet;
	}

	let {
		open,
		title,
		size = 'sm',
		closeOnOverlay = true,
		showClose = true,
		onClose,
		children,
		footer
	}: Props = $props();

	function onKey(e: KeyboardEvent) {
		if (e.key === 'Escape' && open) onClose();
	}

	onMount(() => {
		if (browser) window.addEventListener('keydown', onKey);
	});

	onDestroy(() => {
		if (browser) window.removeEventListener('keydown', onKey);
	});

	function onOverlayClick() {
		if (closeOnOverlay) onClose();
	}
</script>

{#if open}
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<!-- svelte-ignore a11y_click_events_have_key_events -->
	<div class="overlay" role="presentation" onclick={onOverlayClick}>
		<!-- svelte-ignore a11y_no_static_element_interactions -->
		<!-- svelte-ignore a11y_click_events_have_key_events -->
		<div
			role="dialog"
			aria-modal="true"
			aria-labelledby={title ? 'modal-title' : undefined}
			class="dialog dialog--{size}"
			onclick={(e) => e.stopPropagation()}
		>
			{#if title || showClose}
				<div class="head">
					{#if title}
						<h3 id="modal-title" class="title">{title}</h3>
					{/if}
					{#if showClose}
						<button type="button" class="close" aria-label="Закрыть" onclick={onClose}>
							<i class="ti ti-x"></i>
						</button>
					{/if}
				</div>
			{/if}

			<div class="body">
				{@render children?.()}
			</div>

			{#if footer}
				<div class="footer">
					{@render footer()}
				</div>
			{/if}
		</div>
	</div>
{/if}

<style>
	.overlay {
		position: fixed;
		inset: 0;
		background: rgba(15, 23, 42, 0.4);
		backdrop-filter: blur(2px);
		display: flex;
		align-items: center;
		justify-content: center;
		z-index: 10000;
		padding: 16px;
		animation: fade-in 0.15s ease-out;
	}
	.dialog {
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: 14px;
		box-shadow: 0 24px 64px rgba(0, 0, 0, 0.25);
		width: 100%;
		display: flex;
		flex-direction: column;
		max-height: calc(100vh - 32px);
		overflow: hidden;
		animation: pop 0.18s ease-out;
	}
	.dialog--sm {
		max-width: 460px;
	}
	.dialog--md {
		max-width: 640px;
	}
	.dialog--lg {
		max-width: 880px;
	}
	.head {
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 14px 18px 12px;
		border-bottom: 1px solid var(--border);
	}
	.title {
		flex: 1;
		margin: 0;
		font-size: 15px;
		font-weight: 700;
		color: var(--text);
	}
	.close {
		background: transparent;
		border: 0;
		color: var(--text-3);
		font-size: 18px;
		cursor: pointer;
		padding: 4px 6px;
		border-radius: 6px;
		transition: background 0.12s;
	}
	.close:hover {
		background: var(--surface-2);
		color: var(--text);
	}
	.body {
		padding: 16px 18px;
		font-size: 14px;
		color: var(--text-2);
		line-height: 1.55;
		overflow-y: auto;
		flex: 1;
	}
	.footer {
		display: flex;
		gap: 8px;
		justify-content: flex-end;
		padding: 12px 18px;
		border-top: 1px solid var(--border);
		background: var(--surface-2);
	}

	@keyframes fade-in {
		from { opacity: 0; }
		to   { opacity: 1; }
	}
	@keyframes pop {
		from { opacity: 0; transform: translateY(8px) scale(0.98); }
		to   { opacity: 1; transform: translateY(0) scale(1); }
	}
</style>
