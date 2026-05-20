<script lang="ts">
	// Toast — фиксированный контейнер в правом нижнем углу.
	// Рендерит все activeshen toasts из store $lib/stores/toasts.
	// Один экземпляр на всё приложение — монтируется в (app)/+layout.svelte.
	import { toasts } from '$lib/stores/toasts';
</script>

<div class="toasts" aria-live="polite" role="status">
	{#each $toasts as t (t.id)}
		<div class="toast toast--{t.variant}" role="alert">
			{#if t.icon}<i class="ti {t.icon} toast__icon"></i>{/if}
			<div class="toast__body">
				<div class="toast__title">{t.title}</div>
				{#if t.body}<div class="toast__text">{t.body}</div>{/if}
			</div>
			<button
				type="button"
				class="toast__close"
				aria-label="Закрыть"
				onclick={() => toasts.dismiss(t.id)}
			>
				<i class="ti ti-x"></i>
			</button>
		</div>
	{/each}
</div>

<style>
	.toasts {
		position: fixed;
		bottom: 24px;
		right: 24px;
		display: flex;
		flex-direction: column;
		gap: 10px;
		z-index: 9999;
		pointer-events: none;
		max-width: min(400px, calc(100vw - 48px));
	}
	.toast {
		display: flex;
		align-items: flex-start;
		gap: 10px;
		padding: 12px 14px;
		border-radius: 12px;
		background: var(--surface);
		border: 1px solid var(--border);
		box-shadow: 0 8px 24px rgba(0, 0, 0, 0.12);
		pointer-events: auto;
		animation: toast-in 180ms ease-out;
	}
	.toast--info {
		border-left: 3px solid var(--info-strong);
	}
	.toast--success {
		border-left: 3px solid var(--success-strong);
	}
	.toast--warning {
		border-left: 3px solid var(--warning-strong);
	}
	.toast--danger {
		border-left: 3px solid var(--danger-strong);
	}
	.toast__icon {
		font-size: 18px;
		margin-top: 1px;
	}
	.toast--info .toast__icon { color: var(--info-strong); }
	.toast--success .toast__icon { color: var(--success-strong); }
	.toast--warning .toast__icon { color: var(--warning-strong); }
	.toast--danger .toast__icon { color: var(--danger-strong); }
	.toast__body {
		flex: 1;
		min-width: 0;
	}
	.toast__title {
		font-size: 14px;
		font-weight: 600;
		color: var(--text);
		line-height: 1.3;
	}
	.toast__text {
		font-size: 12px;
		color: var(--text-2);
		line-height: 1.4;
		margin-top: 2px;
	}
	.toast__close {
		background: transparent;
		border: 0;
		color: var(--text-3);
		cursor: pointer;
		padding: 2px 4px;
		font-size: 14px;
		border-radius: 6px;
		transition: background 0.12s;
	}
	.toast__close:hover {
		background: var(--surface-2);
	}
	@keyframes toast-in {
		from {
			opacity: 0;
			transform: translateY(8px);
		}
		to {
			opacity: 1;
			transform: translateY(0);
		}
	}
</style>
