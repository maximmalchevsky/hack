<script lang="ts">
	interface TabItem {
		id: string;
		label: string;
		count?: number;
		icon?: string;
	}

	interface Props {
		tabs: TabItem[];
		value: string;
		onChange?: (id: string) => void;
	}

	let { tabs, value = $bindable(), onChange }: Props = $props();

	function select(id: string) {
		value = id;
		onChange?.(id);
	}
</script>

<div class="tabs">
	{#each tabs as t (t.id)}
		<button
			type="button"
			class="tab"
			class:active={value === t.id}
			onclick={() => select(t.id)}
		>
			{#if t.icon}<i class="ti {t.icon}"></i>{/if}
			{t.label}
			{#if t.count !== undefined}<span class="tab__count">{t.count}</span>{/if}
		</button>
	{/each}
</div>
