<script lang="ts">
	// PulseCheckCard — короткий 2-недельный пульс самочувствия.
	// Показывается на /dashboard. Если ответ был < 14 дней назад — карточка
	// просто показывает «спасибо, в следующий раз через N дней».
	import { onMount } from 'svelte';
	import Card from './Card.svelte';
	import Button from './Button.svelte';
	import { getPulseMe, submitPulse, type PulseMe } from '$lib/api/pulse';

	let state = $state<PulseMe | null>(null);
	let loading = $state(true);
	let saving = $state(false);
	let error = $state<string | null>(null);

	let selectedScore = $state<number | null>(null);
	let comment = $state('');

	// 5 эмодзи + подписи. Слева — плохо, справа — отлично.
	const SCORES: { value: number; emoji: string; label: string }[] = [
		{ value: 1, emoji: '😞', label: 'Тяжело' },
		{ value: 2, emoji: '😐', label: 'Так себе' },
		{ value: 3, emoji: '🙂', label: 'Нормально' },
		{ value: 4, emoji: '😊', label: 'Хорошо' },
		{ value: 5, emoji: '🤩', label: 'Огонь' }
	];

	onMount(async () => {
		await reload();
	});

	async function reload() {
		loading = true;
		error = null;
		try {
			state = await getPulseMe();
		} catch (e) {
			error = e instanceof Error ? e.message : String(e);
		} finally {
			loading = false;
		}
	}

	async function send() {
		if (selectedScore == null) return;
		saving = true;
		error = null;
		try {
			await submitPulse(selectedScore, comment.trim());
			selectedScore = null;
			comment = '';
			await reload();
		} catch (e) {
			error = e instanceof Error ? e.message : String(e);
		} finally {
			saving = false;
		}
	}

</script>

{#if loading}
	<!-- ничего, чтобы не мигать на дашборде -->
{:else if error}
	<Card title="Pulse-check" subtitle="Не удалось загрузить">
		<div class="text-text-3 text-sm" style="padding: 8px 0;">{error}</div>
	</Card>
{:else if state?.should_ask}
		<Card
			title="Как ты сейчас?"
			subtitle="Короткий пульс раз в 2 недели — помогает менеджеру понять, когда что-то идёт не так"
		>
			<div class="pulse">
				<div class="pulse__row">
					{#each SCORES as s (s.value)}
						<button
							type="button"
							class="pulse__cell"
							class:pulse__cell--selected={selectedScore === s.value}
							onclick={() => (selectedScore = s.value)}
							disabled={saving}
							aria-label={s.label}
							title={s.label}
						>
							<span class="pulse__emoji">{s.emoji}</span>
							<span class="pulse__label">{s.label}</span>
						</button>
					{/each}
				</div>

				{#if selectedScore != null}
					<textarea
						class="pulse__comment"
						bind:value={comment}
						maxlength={300}
						placeholder="Хочешь добавить пару слов? (необязательно)"
						rows="2"
						disabled={saving}
					></textarea>

					<div class="pulse__actions">
						<Button onclick={send} disabled={saving}>
							{saving ? 'Отправляю…' : 'Отправить'}
						</Button>
						<button
							type="button"
							class="pulse__skip"
							onclick={() => {
								selectedScore = null;
								comment = '';
							}}
							disabled={saving}
						>
							Сбросить
						</button>
					</div>
				{/if}
		</div>
	</Card>
{/if}

<style>
	.pulse {
		display: flex;
		flex-direction: column;
		gap: 12px;
	}
	.pulse__row {
		display: grid;
		grid-template-columns: repeat(5, 1fr);
		gap: 8px;
	}
	.pulse__cell {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 4px;
		padding: 12px 8px;
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: 10px;
		cursor: pointer;
		transition: transform 0.1s, border-color 0.12s, background 0.12s;
	}
	.pulse__cell:hover {
		border-color: var(--info-strong);
		transform: translateY(-1px);
	}
	.pulse__cell:active {
		transform: translateY(0);
	}
	.pulse__cell:disabled {
		opacity: 0.6;
		cursor: not-allowed;
	}
	.pulse__cell--selected {
		border-color: var(--info-strong);
		background: var(--info-bg);
		box-shadow: 0 0 0 2px var(--info-strong);
	}
	.pulse__emoji {
		font-size: 26px;
		line-height: 1;
	}
	.pulse__label {
		font-size: 11px;
		color: var(--text-2);
		font-weight: 500;
	}
	.pulse__comment {
		width: 100%;
		font-family: inherit;
		font-size: 13px;
		padding: 8px 10px;
		border: 1px solid var(--border);
		border-radius: 8px;
		background: var(--surface);
		color: var(--text);
		resize: vertical;
		min-height: 40px;
	}
	.pulse__comment:focus {
		outline: none;
		border-color: var(--info-strong);
	}
	.pulse__actions {
		display: flex;
		align-items: center;
		gap: 12px;
	}
	.pulse__skip {
		background: transparent;
		border: none;
		color: var(--text-3);
		font-size: 12px;
		cursor: pointer;
	}
	.pulse__skip:hover {
		color: var(--text);
	}
</style>
