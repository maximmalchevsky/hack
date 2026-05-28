<script lang="ts">
	// MetricInfo — крошечная (i)-иконка с popover-расшифровкой одной из метрик.
	// Используется в Stat-карточках, в карточках диагностики, на /employees/[id].
	// Никаких JS на hover — чистый CSS, не лезет в скролл/слои.

	type Letter = 'A' | 'C' | 'L' | 'Z' | 'H' | 'R';

	interface Props {
		letter: Letter;
		// Размер иконки. 'sm' — для inline (рядом с буквой), 'md' — для карточек.
		size?: 'sm' | 'md';
	}

	let { letter, size = 'sm' }: Props = $props();

	const META: Record<Letter, { title: string; description: string; scale: string }> = {
		A: {
			title: 'Актуальность профиля',
			description:
				'Насколько давно сотрудник обновлял свой рабочий график. Чем дольше нет правок — тем ниже значение.',
			scale: '1.0 — обновлён сегодня · 0.5 — около полутора месяцев назад · 0 — три месяца и больше.'
		},
		C: {
			title: 'Доля конфликтов',
			description:
				'Сколько встреч проблемные: либо стоят вне заявленных рабочих часов, либо наслаиваются на другую встречу (двойное бронирование). Высокое значение — события сдвинуты, график устарел или встречи пересекаются.',
			scale: '0 — всё чисто · 0.2 — единичные пересечения или выходы за график · 0.5+ — половина встреч проблемные.'
		},
		L: {
			title: 'Загрузка',
			description:
				'Доля рабочего времени, занятая встречами. Высокая загрузка — мало окон для задач, риск выгорания.',
			scale: 'до 70% — нормально · 70–95% — плотно · выше 95% — окон нет, перегруз.'
		},
		Z: {
			title: 'TZ-drift',
			description:
				'Признак, что заявленный часовой пояс не совпадает с реальным временем активности. Часто — переезд или работа из другой страны.',
			scale: '0 — пояс совпадает · 0.3+ — заметное расхождение · 1 — все события не в заявленном TZ.'
		},
		H: {
			title: 'Расхождение с HR',
			description:
				'Расхождение между заявленным в HR форматом (офис / удалёнка / гибрид) и фактическим паттерном работы.',
			scale: '0 — соответствует · 0.5 — частичное расхождение · 1 — полностью не совпадает.'
		},
		R: {
			title: 'Интегральный риск',
			description:
				'Сводная оценка: насколько вероятно, что график сотрудника неактуален. Складывается из всех метрик выше.',
			scale: 'до 0.25 — норма · 0.25–0.5 — стоит обратить внимание · выше 0.5 — зона риска.'
		}
	};

	const m = $derived(META[letter]);
</script>

<span class="mi" class:mi--md={size === 'md'}>
	<i class="ti ti-info-circle mi__icon" aria-hidden="true"></i>
	<span class="mi__popover" role="tooltip">
		<span class="mi__title">
			<span class="mi__letter">{letter}</span>
			{m.title}
		</span>
		<span class="mi__row">{m.description}</span>
		<span class="mi__row mi__row--muted">{m.scale}</span>
	</span>
</span>

<style>
	.mi {
		position: relative;
		display: inline-flex;
		align-items: center;
		cursor: help;
		color: var(--text-3);
		transition: color 0.12s;
	}
	.mi:hover,
	.mi:focus-within {
		color: var(--info-strong);
	}
	.mi__icon {
		font-size: 13px;
		line-height: 1;
	}
	.mi--md .mi__icon {
		font-size: 15px;
	}
	.mi__popover {
		position: absolute;
		top: calc(100% + 6px);
		left: 50%;
		transform: translateX(-50%);
		min-width: 240px;
		max-width: 320px;
		padding: 10px 12px;
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: 10px;
		box-shadow: 0 8px 24px rgba(0, 0, 0, 0.14);
		display: none;
		flex-direction: column;
		gap: 4px;
		z-index: 200;
		font-size: 12px;
		line-height: 1.45;
		color: var(--text-2);
		text-align: left;
		pointer-events: none;
	}
	.mi:hover .mi__popover,
	.mi:focus-within .mi__popover {
		display: flex;
	}
	.mi__title {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		font-weight: 600;
		font-size: 12px;
		color: var(--text);
	}
	.mi__letter {
		display: inline-flex;
		justify-content: center;
		align-items: center;
		width: 18px;
		height: 18px;
		border-radius: 6px;
		background: var(--info-bg);
		color: var(--info-strong);
		font-weight: 700;
		font-size: 11px;
		font-family: 'JetBrains Mono', ui-monospace, monospace;
	}
	.mi__row {
		display: block;
	}
	.mi__row--muted {
		color: var(--text-3);
	}
</style>
