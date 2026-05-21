<script lang="ts">
	// ViewPresetsBar — компактный dropdown пресетов + кнопка «Сохранить как...».
	// Используется на /analytics и /diagnostics.
	import { onMount } from 'svelte';
	import {
		listViewPresets,
		createViewPreset,
		deleteViewPreset,
		type ViewPage,
		type ViewPreset
	} from '$lib/api/view-presets';

	interface Props {
		page: ViewPage;
		// Что записать в filters при сохранении пресета.
		currentFilters: () => Record<string, unknown>;
		// Что сделать при выборе пресета — родитель применяет filters к своему state.
		onApply: (filters: Record<string, unknown>) => void;
	}
	let { page, currentFilters, onApply }: Props = $props();

	let presets = $state<ViewPreset[]>([]);
	let open = $state(false);
	let savingMode = $state(false);
	let newName = $state('');
	let activeID = $state<string | null>(null);
	let error = $state<string | null>(null);

	onMount(async () => {
		await reload();
	});

	async function reload() {
		try {
			const r = await listViewPresets(page);
			presets = r.presets ?? [];
		} catch (e) {
			error = e instanceof Error ? e.message : String(e);
		}
	}

	function apply(p: ViewPreset) {
		activeID = p.id;
		onApply(p.filters);
		open = false;
	}

	async function save() {
		const name = newName.trim();
		if (!name) return;
		try {
			const p = await createViewPreset(page, name, currentFilters());
			presets = [p, ...presets];
			activeID = p.id;
			newName = '';
			savingMode = false;
			open = false;
		} catch (e) {
			error = e instanceof Error ? e.message : String(e);
		}
	}

	async function remove(p: ViewPreset, ev: MouseEvent) {
		ev.stopPropagation();
		if (!confirm(`Удалить пресет «${p.name}»?`)) return;
		try {
			await deleteViewPreset(p.id);
			presets = presets.filter((x) => x.id !== p.id);
			if (activeID === p.id) activeID = null;
		} catch (e) {
			error = e instanceof Error ? e.message : String(e);
		}
	}

	const activeLabel = $derived(
		(activeID && presets.find((p) => p.id === activeID)?.name) || 'Пресеты'
	);
</script>

<div class="vpb">
	<button
		type="button"
		class="vpb__trigger"
		onclick={() => (open = !open)}
		aria-expanded={open}
	>
		<i class="ti ti-bookmark"></i>
		<span>{activeLabel}</span>
		<i class="ti ti-chevron-down vpb__chev" class:vpb__chev--open={open}></i>
	</button>

	{#if open}
		<div class="vpb__menu">
			{#if presets.length === 0 && !savingMode}
				<div class="vpb__empty">Нет сохранённых пресетов</div>
			{/if}
			{#each presets as p (p.id)}
				<div
					role="button"
					tabindex="0"
					class="vpb__item"
					class:vpb__item--active={activeID === p.id}
					onclick={() => apply(p)}
					onkeydown={(e) => e.key === 'Enter' && apply(p)}
				>
					<span class="vpb__item-name">{p.name}</span>
					<button
						type="button"
						class="vpb__del"
						aria-label="Удалить"
						onclick={(e) => remove(p, e)}
					>
						<i class="ti ti-x"></i>
					</button>
				</div>
			{/each}

			<div class="vpb__sep"></div>

			{#if !savingMode}
				<button type="button" class="vpb__item vpb__item--add" onclick={() => (savingMode = true)}>
					<i class="ti ti-plus"></i> Сохранить текущий вид
				</button>
			{:else}
				<div class="vpb__save">
					<input
						type="text"
						placeholder="Название пресета"
						bind:value={newName}
						onkeydown={(e) => e.key === 'Enter' && save()}
						autofocus
					/>
					<button type="button" class="vpb__save-btn" onclick={save} disabled={!newName.trim()}>
						<i class="ti ti-check"></i>
					</button>
					<button type="button" class="vpb__save-cancel" onclick={() => (savingMode = false)}>
						<i class="ti ti-x"></i>
					</button>
				</div>
			{/if}

			{#if error}
				<div class="vpb__error">{error}</div>
			{/if}
		</div>
	{/if}
</div>

<style>
	.vpb {
		position: relative;
		display: inline-block;
	}
	.vpb__trigger {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		padding: 6px 10px;
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: 8px;
		font-size: 13px;
		color: var(--text);
		cursor: pointer;
		transition: border-color 0.12s;
	}
	.vpb__trigger:hover {
		border-color: var(--info-strong);
	}
	.vpb__chev {
		font-size: 14px;
		transition: transform 0.15s;
	}
	.vpb__chev--open {
		transform: rotate(180deg);
	}
	.vpb__menu {
		position: absolute;
		top: calc(100% + 4px);
		right: 0;
		min-width: 240px;
		background: var(--bg, white);
		border: 1px solid var(--border);
		border-radius: 8px;
		box-shadow: 0 8px 24px rgba(0, 0, 0, 0.12);
		padding: 6px;
		z-index: 50;
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.vpb__empty {
		font-size: 12px;
		color: var(--text-3);
		padding: 8px 10px;
	}
	.vpb__item {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 6px;
		padding: 6px 10px;
		background: transparent;
		border: none;
		border-radius: 6px;
		font-size: 13px;
		text-align: left;
		color: var(--text);
		cursor: pointer;
		width: 100%;
	}
	.vpb__item:hover {
		background: var(--surface);
	}
	.vpb__item--active {
		background: var(--info-bg);
		color: var(--info-strong);
		font-weight: 600;
	}
	.vpb__item--add {
		color: var(--info-strong);
	}
	.vpb__del {
		opacity: 0;
		background: transparent;
		border: none;
		color: var(--text-3);
		padding: 2px;
		border-radius: 4px;
		cursor: pointer;
		font-size: 14px;
	}
	.vpb__item:hover .vpb__del {
		opacity: 1;
	}
	.vpb__del:hover {
		color: var(--danger-strong);
		background: var(--danger-bg);
	}
	.vpb__sep {
		height: 1px;
		background: var(--border);
		margin: 4px 0;
	}
	.vpb__save {
		display: flex;
		gap: 4px;
		padding: 4px;
	}
	.vpb__save input {
		flex: 1;
		font-size: 13px;
		padding: 4px 8px;
		border: 1px solid var(--border);
		border-radius: 6px;
		background: var(--surface);
		color: var(--text);
	}
	.vpb__save-btn,
	.vpb__save-cancel {
		background: transparent;
		border: none;
		padding: 4px 8px;
		border-radius: 6px;
		cursor: pointer;
		font-size: 14px;
	}
	.vpb__save-btn {
		color: var(--success-strong);
	}
	.vpb__save-btn:hover:not(:disabled) {
		background: var(--success-bg);
	}
	.vpb__save-btn:disabled {
		opacity: 0.4;
		cursor: not-allowed;
	}
	.vpb__save-cancel {
		color: var(--text-3);
	}
	.vpb__save-cancel:hover {
		background: var(--surface);
	}
	.vpb__error {
		font-size: 11px;
		color: var(--danger-strong);
		padding: 4px 10px;
	}
</style>
