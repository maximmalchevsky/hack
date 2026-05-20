<script lang="ts">
	import { onMount } from 'svelte';
	import Card from '$lib/components/Card.svelte';
	import Badge from '$lib/components/Badge.svelte';
	import Button from '$lib/components/Button.svelte';
	import { getRules, updateRules, type AnalyticsWeights } from '$lib/api/admin';
	import { ApiError } from '$lib/api/client';

	let w = $state<AnalyticsWeights>({
		w1: 0.3,
		w2: 0.25,
		w3: 0.2,
		w4: 0.15,
		w5: 0.1,
		freshness_d_days: 90
	});
	let loading = $state(true);
	let saving = $state(false);
	let error = $state<string | null>(null);
	let success = $state<string | null>(null);

	onMount(async () => {
		try {
			w = await getRules();
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		} finally {
			loading = false;
		}
	});

	const total = $derived(w.w1 + w.w2 + w.w3 + w.w4 + w.w5);
	const totalOK = $derived(Math.abs(total - 1.0) < 0.01);

	async function save() {
		if (!totalOK) {
			error = `Сумма весов должна быть = 1.0 (сейчас ${total.toFixed(3)})`;
			return;
		}
		saving = true;
		error = null;
		success = null;
		try {
			await updateRules(w);
			success = 'Веса обновлены';
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		} finally {
			saving = false;
		}
	}

	function reset() {
		w = { w1: 0.3, w2: 0.25, w3: 0.2, w4: 0.15, w5: 0.1, freshness_d_days: 90 };
	}

	function normalize() {
		const t = w.w1 + w.w2 + w.w3 + w.w4 + w.w5;
		if (t <= 0) return;
		w = {
			...w,
			w1: Math.round((w.w1 / t) * 100) / 100,
			w2: Math.round((w.w2 / t) * 100) / 100,
			w3: Math.round((w.w3 / t) * 100) / 100,
			w4: Math.round((w.w4 / t) * 100) / 100,
			w5: Math.round((w.w5 / t) * 100) / 100
		};
	}

	type WeightColor = 'info' | 'warning' | 'danger' | 'purple' | 'teal';
	type WeightField = {
		key: 'w1' | 'w2' | 'w3' | 'w4' | 'w5';
		short: string;
		title: string;
		color: WeightColor;
		icon: string;
	};

	const weightFields: WeightField[] = [
		{ key: 'w1', short: 'w1', title: 'Устаревший профиль', color: 'info', icon: 'ti-clock-exclamation' },
		{ key: 'w2', short: 'w2', title: 'Конфликты', color: 'danger', icon: 'ti-alert-triangle' },
		{ key: 'w3', short: 'w3', title: 'Загрузка', color: 'warning', icon: 'ti-gauge' },
		{ key: 'w4', short: 'w4', title: 'Часовой пояс', color: 'teal', icon: 'ti-clock-shield' },
		{ key: 'w5', short: 'w5', title: 'HR-mismatch', color: 'purple', icon: 'ti-building' }
	];

	function pct(v: number): number {
		return Math.round(v * 100);
	}

	function strongColor(c: WeightColor): string {
		switch (c) {
			case 'info': return 'var(--info-strong)';
			case 'danger': return 'var(--danger-strong)';
			case 'warning': return 'var(--warning-strong)';
			case 'teal': return 'var(--teal-text)';
			case 'purple': return 'var(--purple-text)';
		}
	}
</script>

<div class="page-header">
	<div>
		<h1>Правила метрик</h1>
		<div class="page-header__subtitle">
			<span class="formula">
				<span class="formula__var">R</span>
				=
				<span class="formula__w formula__w--info">w<sub>1</sub></span>(1−<span class="formula__var">A</span>)
				+
				<span class="formula__w formula__w--danger">w<sub>2</sub></span>·<span class="formula__var">C</span>
				+
				<span class="formula__w formula__w--warning">w<sub>3</sub></span>·<span class="formula__var">L</span>
				+
				<span class="formula__w formula__w--teal">w<sub>4</sub></span>·<span class="formula__var">Z</span>
				+
				<span class="formula__w formula__w--purple">w<sub>5</sub></span>·<span class="formula__var">H</span>
			</span>
			<dl class="formula__legend">
				<div class="formula__legend-item">
					<dt>R</dt>
					<dd>интегральный риск неактуальности</dd>
				</div>
				<div class="formula__legend-item">
					<dt>A</dt>
					<dd>актуальность, 1 = свежий профиль, 0 = устарел</dd>
				</div>
				<div class="formula__legend-item">
					<dt>C</dt>
					<dd>доля событий вне рабочего графика</dd>
				</div>
				<div class="formula__legend-item">
					<dt>L</dt>
					<dd>загрузка: занятые часы / рабочие часы</dd>
				</div>
				<div class="formula__legend-item">
					<dt>Z</dt>
					<dd>смещение фактической активности от заявленного TZ</dd>
				</div>
				<div class="formula__legend-item">
					<dt>H</dt>
					<dd>расхождение HR-формата с фактическим профилем</dd>
				</div>
			</dl>
		</div>
	</div>
	<div class="page-header__actions">
		<Button icon="ti-arrow-back-up" onclick={reset}>Сбросить</Button>
		<Button icon="ti-scale" onclick={normalize} disabled={totalOK}>Нормализовать</Button>
		<Button variant="primary" icon="ti-device-floppy" onclick={save} disabled={saving || !totalOK}>
			{saving ? 'Сохраняем…' : 'Сохранить'}
		</Button>
	</div>
</div>

{#if error}
	<div class="section">
		<Badge variant="danger"><i class="ti ti-alert-circle"></i>{error}</Badge>
	</div>
{/if}
{#if success}
	<div class="section">
		<Badge variant="success"><i class="ti ti-check"></i>{success}</Badge>
	</div>
{/if}

{#if loading}
	<div class="text-text-3 text-sm">Загрузка…</div>
{:else}
	<!-- Сумма весов: визуальный progress bar -->
	<Card padded>
		<div class="flex items-center justify-between" style="margin-bottom: 10px;">
			<div>
				<div class="card__caption">Сумма весов</div>
				<div
					class="stat__value"
					style:color={totalOK ? 'var(--success-strong)' : 'var(--danger-strong)'}
				>
					{total.toFixed(3)}
					{#if !totalOK}
						<span class="text-text-3 text-xs" style="font-weight: 400; margin-left: 6px;">
							(должно быть = 1.000)
						</span>
					{/if}
				</div>
			</div>
		</div>
		<div class="weight-stack">
			{#each weightFields as f (f.key)}
				<div
					class="weight-stack__seg"
					style:width="{pct(w[f.key])}%"
					style:background={strongColor(f.color)}
					title="{f.title}: {pct(w[f.key])}%"
				></div>
			{/each}
		</div>
		<div class="weight-stack__legend">
			{#each weightFields as f (f.key)}
				<div class="weight-stack__legend-item">
					<span class="weight-stack__swatch" style:background={strongColor(f.color)}></span>
					<span>{f.title} · {pct(w[f.key])}%</span>
				</div>
			{/each}
		</div>
	</Card>

	<!-- Слайдеры весов в 2 колонки -->
	<div class="section grid-2" style="margin-top: 16px;">
		{#each weightFields as f (f.key)}
			<Card>
				<div class="weight-row">
					<div
						class="weight-row__icon"
						style:background="var(--{f.color}-bg)"
						style:color="var(--{f.color}-text)"
					>
						<i class="ti {f.icon}"></i>
					</div>
					<div class="weight-row__meta">
						<div class="card__title">{f.short} · {f.title}</div>
					</div>
					<div class="weight-row__value">
						<div class="stat__value" style="font-size: 18px;">{w[f.key].toFixed(2)}</div>
						<div class="text-text-3 text-xs">{pct(w[f.key])}%</div>
					</div>
				</div>

				<input
					type="range"
					min="0"
					max="1"
					step="0.01"
					bind:value={w[f.key]}
					class="weight-slider weight-slider--{f.color}"
				/>
			</Card>
		{/each}
	</div>

	<div class="section" style="margin-top: 16px;">
		<Card>
			<div class="weight-row">
				<div class="weight-row__icon" style="background: var(--surface-2); color: var(--text-2);">
					<i class="ti ti-calendar-stats"></i>
				</div>
				<div class="weight-row__meta">
					<div class="card__title">Freshness D — порог актуальности</div>
					<div class="card__subtitle">Через сколько дней без обновления A падает до 0</div>
				</div>
				<div class="weight-row__value">
					<div class="stat__value" style="font-size: 18px;">{w.freshness_d_days}</div>
					<div class="text-text-3 text-xs">дней</div>
				</div>
			</div>
			<input
				type="range"
				min="7"
				max="365"
				step="1"
				bind:value={w.freshness_d_days}
				class="weight-slider weight-slider--info"
			/>
			<div class="weight-slider__scale">
				<span>7</span>
				<span>90 (по умолчанию)</span>
				<span>365</span>
			</div>
		</Card>
	</div>
{/if}

<style>
	/* Формула: тот же шрифт сайта, цветовые акценты на весах */
	.formula {
		font-family: inherit;
		font-size: 18px;
		font-weight: 500;
		color: var(--text);
		letter-spacing: 0.2px;
		display: inline-block;
		margin-top: 6px;
	}
	.formula__w {
		font-weight: 600;
	}
	.formula__w sub {
		font-size: 0.7em;
		vertical-align: -0.25em;
		font-weight: 500;
		font-style: normal;
		margin-left: 0.5px;
	}
	.formula__w--info {
		color: var(--info-strong);
	}
	.formula__w--danger {
		color: var(--danger-strong);
	}
	.formula__w--warning {
		color: var(--warning-strong);
	}
	.formula__w--teal {
		color: var(--teal-text);
	}
	.formula__w--purple {
		color: var(--purple-text);
	}

	.formula__var {
		font-weight: 600;
	}

	/* Сноска с расшифровкой переменных под формулой */
	.formula__legend {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
		column-gap: 20px;
		row-gap: 4px;
		margin: 14px 0 0;
		padding: 0;
		max-width: 820px;
	}
	.formula__legend-item {
		display: flex;
		align-items: baseline;
		gap: 8px;
		font-size: 12.5px;
		color: var(--text-3);
		font-style: normal;
	}
	.formula__legend dt {
		font-family: inherit;
		font-weight: 700;
		font-size: 13px;
		color: var(--text-2);
		min-width: 14px;
		text-align: center;
		flex-shrink: 0;
	}
	.formula__legend dd {
		margin: 0;
		line-height: 1.5;
	}

	.weight-row {
		display: flex;
		align-items: center;
		gap: 12px;
		margin-bottom: 14px;
	}
	.weight-row__icon {
		width: 40px;
		height: 40px;
		border-radius: var(--radius-md);
		display: flex;
		align-items: center;
		justify-content: center;
		font-size: 20px;
		flex-shrink: 0;
	}
	.weight-row__meta {
		flex: 1;
		min-width: 0;
	}
	.weight-row__value {
		text-align: right;
		flex-shrink: 0;
	}

	.weight-stack {
		display: flex;
		height: 10px;
		background: var(--surface-2);
		border-radius: 5px;
		overflow: hidden;
	}
	.weight-stack__seg {
		transition: width 0.15s;
		border-right: 1px solid var(--surface);
	}
	.weight-stack__seg:last-child {
		border-right: none;
	}
	.weight-stack__legend {
		display: flex;
		flex-wrap: wrap;
		gap: 14px;
		margin-top: 10px;
		font-size: 11px;
		color: var(--text-2);
	}
	.weight-stack__legend-item {
		display: flex;
		align-items: center;
		gap: 5px;
	}
	.weight-stack__swatch {
		width: 10px;
		height: 10px;
		border-radius: 2px;
	}

	.weight-slider {
		-webkit-appearance: none;
		appearance: none;
		width: 100%;
		height: 6px;
		border-radius: 3px;
		background: var(--surface-2);
		outline: none;
		padding: 0;
		margin: 0;
		cursor: pointer;
		border: none;
	}
	.weight-slider::-webkit-slider-thumb {
		-webkit-appearance: none;
		appearance: none;
		width: 18px;
		height: 18px;
		border-radius: 50%;
		background: var(--info-strong);
		cursor: pointer;
		transition: transform 0.12s;
		border: 2px solid var(--surface);
		box-shadow: 0 1px 4px rgba(0, 0, 0, 0.15);
	}
	.weight-slider::-webkit-slider-thumb:hover {
		transform: scale(1.15);
	}
	.weight-slider::-moz-range-thumb {
		width: 18px;
		height: 18px;
		border-radius: 50%;
		background: var(--info-strong);
		cursor: pointer;
		border: 2px solid var(--surface);
	}

	.weight-slider--warning::-webkit-slider-thumb {
		background: var(--warning-strong);
	}
	.weight-slider--warning::-moz-range-thumb {
		background: var(--warning-strong);
	}
	.weight-slider--danger::-webkit-slider-thumb {
		background: var(--danger-strong);
	}
	.weight-slider--danger::-moz-range-thumb {
		background: var(--danger-strong);
	}
	.weight-slider--purple::-webkit-slider-thumb {
		background: var(--purple-text);
	}
	.weight-slider--purple::-moz-range-thumb {
		background: var(--purple-text);
	}
	.weight-slider--teal::-webkit-slider-thumb {
		background: var(--teal-text);
	}
	.weight-slider--teal::-moz-range-thumb {
		background: var(--teal-text);
	}

	.weight-slider__scale {
		display: flex;
		justify-content: space-between;
		margin-top: 6px;
		font-size: 11px;
		color: var(--text-3);
	}
</style>
