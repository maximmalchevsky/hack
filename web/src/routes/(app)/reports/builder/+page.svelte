<script lang="ts">
	import { onMount } from 'svelte';
	import { browser } from '$app/environment';
	import { env } from '$env/dynamic/public';
	import Card from '$lib/components/Card.svelte';
	import Button from '$lib/components/Button.svelte';
	import Badge from '$lib/components/Badge.svelte';
	import { getAccessToken } from '$lib/api/client';
	import { ApiError } from '$lib/api/client';
	import {
		getExportDataset,
		getExportDatasetFiltered,
		buildExportURL,
		type ExportDataset
	} from '$lib/api/exports';
	import {
		listPresets,
		createPreset,
		updatePreset,
		deletePreset,
		type ReportPreset,
		type ReportKind
	} from '$lib/api/report-presets';

	type SourceMeta = {
		kind: ReportKind;
		label: string;
		usePeriod: boolean;       // показывать поля from/to
		defaultFrom?: () => Date; // дефолтные значения если есть
		defaultTo?: () => Date;
	};

	const SOURCES: SourceMeta[] = [
		{
			kind: 'upcoming_vacations',
			label: 'Отпуска и командировки',
			usePeriod: true,
			defaultFrom: () => new Date(),
			defaultTo: () => addDays(new Date(), 30)
		},
		{
			kind: 'conflicts',
			label: 'Конфликты в календаре',
			usePeriod: true,
			defaultFrom: () => addDays(new Date(), -7),
			defaultTo: () => addDays(new Date(), 30)
		},
		{
			kind: 'stale_profiles',
			label: 'Устаревшие профили',
			usePeriod: false
		},
		{
			kind: 'all_employees',
			label: 'Все сотрудники',
			usePeriod: false
		}
	];

	function addDays(d: Date, n: number): Date {
		const x = new Date(d);
		x.setDate(x.getDate() + n);
		return x;
	}
	function toYMD(d: Date): string {
		const p = (n: number) => String(n).padStart(2, '0');
		return d.getFullYear() + '-' + p(d.getMonth() + 1) + '-' + p(d.getDate());
	}
	function backendURL(): string {
		return env.PUBLIC_API_URL || (browser ? '' : 'http://localhost:8080');
	}
	function formatBytes(n: number): string {
		if (n < 1024) return `${n} Б`;
		if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} КБ`;
		return `${(n / 1024 / 1024).toFixed(2)} МБ`;
	}

	// --- состояние конфигурации ---
	let selectedKind = $state<ReportKind>('upcoming_vacations');
	let fromDate = $state<string>('');    // YYYY-MM-DD
	let toDate = $state<string>('');
	let allColumns = $state<string[]>([]); // headers исходного dataset
	let pickedColumns = $state<Set<string>>(new Set());
	let allDepartments = $state<string[]>([]);
	let pickedDepartments = $state<Set<string>>(new Set());

	// --- состояние пресетов ---
	let presets = $state<ReportPreset[]>([]);
	let activePresetId = $state<string | null>(null);
	let presetName = $state('');

	// --- общие флаги ---
	let loading = $state(true);
	let busy = $state<'pdf' | 'xlsx' | 'preview' | 'save' | 'delete' | null>(null);
	let error = $state<string | null>(null);
	let success = $state<string | null>(null);
	let preview = $state<ExportDataset | null>(null);

	const currentSource = $derived(SOURCES.find((s) => s.kind === selectedKind)!);

	onMount(async () => {
		try {
			// Загружаем уникальные отделы из all_employees-dataset (один запрос).
			const empDs = await getExportDataset('all_employees');
			const deptIdx = empDs.headers.indexOf('Отдел');
			if (deptIdx >= 0) {
				const set = new Set<string>();
				for (const r of empDs.rows) {
					const v = String(r[deptIdx] ?? '').trim();
					if (v) set.add(v);
				}
				allDepartments = [...set].sort();
			}
			// Пресеты.
			const r = await listPresets();
			presets = r.presets ?? [];
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		} finally {
			loading = false;
		}
		await resetSource();
	});

	// При смене источника — сбрасываем выбор и подтягиваем все колонки.
	async function resetSource() {
		error = null;
		success = null;
		preview = null;
		const meta = currentSource;
		if (meta.usePeriod && meta.defaultFrom && meta.defaultTo) {
			fromDate = toYMD(meta.defaultFrom());
			toDate = toYMD(meta.defaultTo());
		} else {
			fromDate = '';
			toDate = '';
		}
		pickedDepartments = new Set();
		try {
			const ds = await getExportDataset(meta.kind);
			allColumns = ds.headers;
			pickedColumns = new Set(allColumns);
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
			allColumns = [];
			pickedColumns = new Set();
		}
	}

	function onKindChange() {
		activePresetId = null;
		void resetSource();
	}

	function toggleColumn(name: string) {
		const s = new Set(pickedColumns);
		if (s.has(name)) s.delete(name);
		else s.add(name);
		pickedColumns = s;
	}
	function toggleDept(name: string) {
		const s = new Set(pickedDepartments);
		if (s.has(name)) s.delete(name);
		else s.add(name);
		pickedDepartments = s;
	}
	function selectAllColumns() {
		pickedColumns = new Set(allColumns);
	}
	function clearColumns() {
		pickedColumns = new Set();
	}

	// --- сборка query для бэка ---
	function buildQuery() {
		const orderedColumns = allColumns.filter((c) => pickedColumns.has(c));
		const q: { from?: string; to?: string; departments?: string[]; columns?: string[] } = {};
		if (currentSource.usePeriod) {
			if (fromDate) q.from = fromDate;
			if (toDate) q.to = toDate;
		}
		if (pickedDepartments.size > 0) q.departments = [...pickedDepartments];
		if (orderedColumns.length < allColumns.length) q.columns = orderedColumns;
		return q;
	}

	// --- превью (топ N строк) ---
	async function refreshPreview() {
		busy = 'preview';
		error = null;
		success = null;
		try {
			const ds = await getExportDatasetFiltered(selectedKind, buildQuery());
			preview = ds;
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		} finally {
			busy = null;
		}
	}

	// --- скачивание ---
	async function downloadExcel() {
		busy = 'xlsx';
		error = null;
		success = null;
		try {
			const tok = getAccessToken() ?? '';
			const url = backendURL() + buildExportURL(selectedKind, 'xlsx', buildQuery());
			const resp = await fetch(url, {
				headers: tok ? { Authorization: `Bearer ${tok}` } : {}
			});
			if (!resp.ok) throw new Error(`status ${resp.status}`);
			const blob = await resp.blob();
			const cd = resp.headers.get('Content-Disposition') ?? '';
			const m = cd.match(/filename="?([^"]+)"?/);
			const filename = m ? m[1] : `worktime-${selectedKind}.xlsx`;
			triggerDownload(blob, filename);
			success = `Excel сохранён (${formatBytes(blob.size)}).`;
		} catch (e) {
			error = `Не удалось выгрузить Excel: ${e instanceof Error ? e.message : String(e)}`;
		} finally {
			busy = null;
		}
	}

	async function downloadPDF() {
		busy = 'pdf';
		error = null;
		success = null;
		try {
			const ds = await getExportDatasetFiltered(selectedKind, buildQuery());
			await renderPDF(ds);
			success = 'PDF сохранён.';
		} catch (e) {
			error = `Не удалось собрать PDF: ${e instanceof Error ? e.message : String(e)}`;
		} finally {
			busy = null;
		}
	}

	function triggerDownload(blob: Blob, filename: string) {
		const url = URL.createObjectURL(blob);
		const a = document.createElement('a');
		a.href = url;
		a.download = filename;
		document.body.appendChild(a);
		a.click();
		a.remove();
		URL.revokeObjectURL(url);
	}

	async function renderPDF(ds: ExportDataset) {
		if (!browser) return;
		const mod = await import('html2pdf.js');
		const html2pdf = mod.default;

		const container = document.createElement('div');
		container.style.cssText = `
			font-family: Arial, Helvetica, sans-serif;
			color: #0f172a;
			padding: 24px;
			width: 1024px;
			word-spacing: 0.1em;
		`;
		container.innerHTML = buildPDFHtml(ds);
		document.body.appendChild(container);

		const dateStr = new Date().toISOString().slice(0, 10);
		const filename = `worktime-${selectedKind}-${dateStr}.pdf`;

		try {
			await html2pdf()
				.from(container)
				.set({
					margin: [10, 12, 12, 12],
					filename,
					image: { type: 'jpeg', quality: 0.96 },
					html2canvas: { scale: 2, useCORS: true },
					jsPDF: { unit: 'mm', format: 'a4', orientation: 'landscape' }
				})
				.save();
		} finally {
			container.remove();
		}
	}

	function escapeHtml(s: string): string {
		return s
			.replaceAll('&', '&amp;')
			.replaceAll('<', '&lt;')
			.replaceAll('>', '&gt;')
			.replaceAll('"', '&quot;');
	}

	function buildPDFHtml(ds: ExportDataset): string {
		const now = new Date();
		const dateStr = now.toLocaleDateString('ru', {
			day: 'numeric',
			month: 'long',
			year: 'numeric'
		});
		const timeStr = now.toLocaleTimeString('ru', { hour: '2-digit', minute: '2-digit' });
		const headerCells = ds.headers
			.map(
				(h) =>
					`<th style="text-align:left;padding:8px 10px;background:#f1f5f9;border-bottom:1px solid #cbd5e1;font-size:11px;text-transform:uppercase;letter-spacing:.5px;color:#475569;">${escapeHtml(h)}</th>`
			)
			.join('');
		const rowsHtml = ds.rows
			.map((row, idx) => {
				const cells = row
					.map(
						(v) =>
							`<td style="padding:6px 10px;border-bottom:1px solid #e2e8f0;font-size:11px;color:#0f172a;vertical-align:top;">${escapeHtml(String(v ?? ''))}</td>`
					)
					.join('');
				const bg = idx % 2 === 0 ? '#ffffff' : '#fafafa';
				return `<tr style="background:${bg};">${cells}</tr>`;
			})
			.join('');
		const empty =
			ds.rows.length === 0
				? `<div style="padding:24px;text-align:center;color:#64748b;font-size:13px;">Нет данных</div>`
				: '';
		const filterChips: string[] = [];
		if (fromDate) filterChips.push(`с ${fromDate}`);
		if (toDate) filterChips.push(`по ${toDate}`);
		if (pickedDepartments.size > 0) filterChips.push(`Отделы: ${[...pickedDepartments].join(', ')}`);
		const filtersLine =
			filterChips.length > 0
				? `<div style="font-size:11px;color:#64748b;margin-top:4px;">${escapeHtml(filterChips.join(' · '))}</div>`
				: '';
		return `
			<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:18px;border-bottom:2px solid #3b82f6;padding-bottom:10px;">
				<div>
					<div style="font-size:11px;color:#64748b;letter-spacing:.5px;text-transform:uppercase;">Workie · отчёт</div>
					<div style="font-size:22px;font-weight:700;color:#0f172a;margin-top:2px;word-spacing:0.25em;">${escapeHtml(ds.title).replace(/ /g, '&nbsp;')}</div>
					${filtersLine}
				</div>
				<div style="text-align:right;font-size:11px;color:#64748b;">
					Сформировано<br/>
					<span style="color:#0f172a;font-weight:600;">${dateStr}, ${timeStr}</span><br/>
					Записей: <b>${ds.rows.length}</b>
				</div>
			</div>
			${empty}
			<table style="width:100%;border-collapse:collapse;">
				<thead><tr>${headerCells}</tr></thead>
				<tbody>${rowsHtml}</tbody>
			</table>
		`;
	}

	// --- пресеты ---
	function applyPreset(p: ReportPreset) {
		activePresetId = p.id;
		selectedKind = p.kind;
		presetName = p.name;
		(async () => {
			// Сначала подтянем все колонки источника.
			await resetSource();
			if (p.columns && p.columns.length > 0) {
				pickedColumns = new Set(p.columns);
			}
			if (p.filters?.from) {
				fromDate = p.filters.from.slice(0, 10);
			}
			if (p.filters?.to) {
				toDate = p.filters.to.slice(0, 10);
			}
			if (p.filters?.departments) {
				pickedDepartments = new Set(p.filters.departments);
			}
		})();
	}

	function buildPresetBody() {
		const orderedColumns = allColumns.filter((c) => pickedColumns.has(c));
		return {
			name: presetName.trim() || `${currentSource.label} ${toYMD(new Date())}`,
			kind: selectedKind,
			columns: orderedColumns,
			filters: {
				from: currentSource.usePeriod && fromDate ? new Date(fromDate).toISOString() : undefined,
				to: currentSource.usePeriod && toDate ? new Date(toDate).toISOString() : undefined,
				departments: pickedDepartments.size > 0 ? [...pickedDepartments] : undefined
			}
		};
	}

	async function onSavePreset() {
		busy = 'save';
		error = null;
		success = null;
		try {
			const body = buildPresetBody();
			let saved: ReportPreset;
			if (activePresetId) {
				saved = await updatePreset(activePresetId, body);
				success = `Пресет «${saved.name}» обновлён.`;
			} else {
				saved = await createPreset(body);
				success = `Пресет «${saved.name}» сохранён.`;
				activePresetId = saved.id;
			}
			const list = await listPresets();
			presets = list.presets ?? [];
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		} finally {
			busy = null;
		}
	}

	async function onDeletePreset(p: ReportPreset) {
		if (!confirm(`Удалить пресет «${p.name}»?`)) return;
		busy = 'delete';
		error = null;
		success = null;
		try {
			await deletePreset(p.id);
			success = `Пресет «${p.name}» удалён.`;
			if (activePresetId === p.id) {
				activePresetId = null;
				presetName = '';
			}
			const list = await listPresets();
			presets = list.presets ?? [];
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		} finally {
			busy = null;
		}
	}
</script>

<div class="page-header">
	<div>
		<h1>Конструктор отчётов</h1>
		<div class="page-header__subtitle">
			Выбери источник, нужные колонки и фильтры. Сохрани как пресет, чтобы возвращаться к нему быстро.
		</div>
	</div>
	<div>
		<Button variant="ghost" icon="ti-arrow-left" onclick={() => (location.href = '/reports')}>
			Готовые пресеты
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
	<!-- Сохранённые пресеты -->
	{#if presets.length > 0}
		<div class="section">
			<Card title="Мои сохранённые пресеты" subtitle="Кликни, чтобы применить">
				<div class="presets">
					{#each presets as p (p.id)}
						<div class="preset" class:preset--active={activePresetId === p.id}>
							<button type="button" class="preset__main" onclick={() => applyPreset(p)}>
								<div class="preset__name">{p.name}</div>
								<div class="preset__meta">
									<span>{SOURCES.find((s) => s.kind === p.kind)?.label ?? p.kind}</span>
									{#if p.filters?.departments && p.filters.departments.length > 0}
										<span>· {p.filters.departments.length} отд.</span>
									{/if}
									{#if p.columns?.length}
										<span>· {p.columns.length} колонок</span>
									{/if}
								</div>
							</button>
							<button
								type="button"
								class="preset__del"
								onclick={() => onDeletePreset(p)}
								disabled={busy !== null}
								title="Удалить пресет"
							>
								<i class="ti ti-trash"></i>
							</button>
						</div>
					{/each}
				</div>
			</Card>
		</div>
	{/if}

	<div class="section grid-2" style="gap: 16px;">
		<Card title="Источник и период">
			<div class="field">
				<label class="field__label" for="b-kind">Источник данных</label>
				<select id="b-kind" bind:value={selectedKind} onchange={onKindChange}>
					{#each SOURCES as s (s.kind)}
						<option value={s.kind}>{s.label}</option>
					{/each}
				</select>
			</div>

			{#if currentSource.usePeriod}
				<div class="flex flex-wrap gap-2" style="margin-top: 12px;">
					<div class="field" style="flex: 1; min-width: 120px;">
						<label class="field__label" for="b-from">С</label>
						<input id="b-from" type="date" bind:value={fromDate} />
					</div>
					<div class="field" style="flex: 1; min-width: 120px;">
						<label class="field__label" for="b-to">По</label>
						<input id="b-to" type="date" bind:value={toDate} />
					</div>
				</div>
			{/if}

			{#if allDepartments.length > 0}
				<div class="field" style="margin-top: 12px;">
					<div class="field__label">Отделы</div>
					<div class="chips">
						{#each allDepartments as d (d)}
							<button
								type="button"
								class="chip"
								class:chip--on={pickedDepartments.has(d)}
								onclick={() => toggleDept(d)}
							>
								{d}
							</button>
						{/each}
					</div>
					{#if pickedDepartments.size === 0}
						<div class="text-text-3 text-xs" style="margin-top: 4px;">
							Не выбрано — попадут все отделы.
						</div>
					{/if}
				</div>
			{/if}
		</Card>

		<Card title="Колонки">
			<div class="cols-toolbar">
				<button type="button" class="ghost-btn" onclick={selectAllColumns}>Все</button>
				<button type="button" class="ghost-btn" onclick={clearColumns}>Очистить</button>
				<span class="text-text-3 text-xs">
					Выбрано {pickedColumns.size} из {allColumns.length}
				</span>
			</div>
			<div class="cols">
				{#each allColumns as c (c)}
					<label class="col-row">
						<input
							type="checkbox"
							checked={pickedColumns.has(c)}
							onchange={() => toggleColumn(c)}
						/>
						<span>{c}</span>
					</label>
				{/each}
			</div>
		</Card>
	</div>

	<div class="section">
		<Card title="Сохранить как пресет" subtitle="Имя для быстрого возврата">
			<div class="flex gap-2" style="align-items: flex-end;">
				<div class="field" style="flex: 1; margin-bottom: 0;">
					<label class="field__label" for="b-name">Имя пресета</label>
					<input
						id="b-name"
						type="text"
						bind:value={presetName}
						placeholder="Например: Отпуска платформы на месяц"
					/>
				</div>
				<Button
					variant="ghost"
					icon="ti-device-floppy"
					onclick={onSavePreset}
					disabled={busy !== null || pickedColumns.size === 0}
				>
					{busy === 'save'
						? 'Сохраняем…'
						: activePresetId
							? 'Перезаписать'
							: 'Сохранить'}
				</Button>
				{#if activePresetId}
					<Button
						variant="ghost"
						icon="ti-x"
						onclick={() => {
							activePresetId = null;
							presetName = '';
						}}
					>
						Новый
					</Button>
				{/if}
			</div>
		</Card>
	</div>

	<div class="section">
		<Card>
			<div class="actions">
				<Button
					variant="ghost"
					icon="ti-eye"
					onclick={refreshPreview}
					disabled={busy !== null || pickedColumns.size === 0}
				>
					{busy === 'preview' ? 'Считаем…' : 'Просмотр'}
				</Button>
				<Button
					variant="ghost"
					icon="ti-file-spreadsheet"
					onclick={downloadExcel}
					disabled={busy !== null || pickedColumns.size === 0}
				>
					{busy === 'xlsx' ? 'Excel…' : 'Excel'}
				</Button>
				<Button
					variant="primary"
					icon="ti-file-type-pdf"
					onclick={downloadPDF}
					disabled={busy !== null || pickedColumns.size === 0}
				>
					{busy === 'pdf' ? 'PDF…' : 'PDF'}
				</Button>
			</div>
		</Card>
	</div>

	{#if preview}
		<div class="section">
			<Card
				title={preview.title}
				subtitle="Первые 20 строк ({preview.rows.length} всего)"
			>
				{#if preview.rows.length === 0}
					<div class="text-text-3 text-sm" style="padding: 16px; text-align: center;">
						Нет данных по выбранным фильтрам.
					</div>
				{:else}
					<div class="preview">
						<table>
							<thead>
								<tr>
									{#each preview.headers as h (h)}
										<th>{h}</th>
									{/each}
								</tr>
							</thead>
							<tbody>
								{#each preview.rows.slice(0, 20) as row, i (i)}
									<tr>
										{#each row as cell, j (j)}
											<td>{cell ?? ''}</td>
										{/each}
									</tr>
								{/each}
							</tbody>
						</table>
					</div>
				{/if}
			</Card>
		</div>
	{/if}
{/if}

<style>
	.presets {
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	.preset {
		display: flex;
		align-items: stretch;
		border: 1px solid var(--border);
		border-radius: 10px;
		background: var(--surface);
		overflow: hidden;
	}
	.preset--active {
		border-color: var(--info-strong);
		background: var(--info-bg);
	}
	.preset__main {
		flex: 1;
		text-align: left;
		padding: 8px 12px;
		background: transparent;
		border: 0;
		cursor: pointer;
		color: var(--text);
	}
	.preset__name {
		font-weight: 600;
		font-size: 13px;
	}
	.preset__meta {
		font-size: 11px;
		color: var(--text-2);
		margin-top: 2px;
		display: flex;
		gap: 4px;
	}
	.preset__del {
		background: transparent;
		border: 0;
		border-left: 1px solid var(--border);
		padding: 0 12px;
		color: var(--text-3);
		cursor: pointer;
		font-size: 14px;
	}
	.preset__del:hover {
		color: var(--danger-strong);
		background: var(--danger-bg);
	}

	.chips {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
	}
	.chip {
		padding: 4px 10px;
		border-radius: 999px;
		border: 1px solid var(--border);
		background: var(--surface);
		color: var(--text-2);
		font-size: 12px;
		cursor: pointer;
		transition: all 0.12s;
	}
	.chip:hover {
		border-color: var(--info-strong);
		color: var(--text);
	}
	.chip--on {
		background: var(--info-bg);
		border-color: var(--info-strong);
		color: var(--info-strong);
		font-weight: 600;
	}

	.cols-toolbar {
		display: flex;
		gap: 8px;
		align-items: center;
		margin-bottom: 8px;
	}
	.ghost-btn {
		background: transparent;
		border: 1px solid var(--border);
		border-radius: 8px;
		padding: 4px 10px;
		font-size: 12px;
		cursor: pointer;
		color: var(--text-2);
	}
	.ghost-btn:hover {
		color: var(--text);
		border-color: var(--info-strong);
	}
	.cols {
		display: grid;
		grid-template-columns: repeat(2, 1fr);
		gap: 4px 12px;
	}
	@media (max-width: 720px) {
		.cols {
			grid-template-columns: 1fr;
		}
	}
	.col-row {
		display: flex;
		align-items: center;
		gap: 8px;
		font-size: 13px;
		color: var(--text);
		cursor: pointer;
		padding: 4px 0;
	}

	.actions {
		display: flex;
		gap: 10px;
		justify-content: flex-end;
		flex-wrap: wrap;
	}

	.preview {
		overflow-x: auto;
		max-height: 480px;
	}
	.preview table {
		border-collapse: collapse;
		width: 100%;
		font-size: 12px;
	}
	.preview th {
		text-align: left;
		padding: 8px 10px;
		background: var(--surface-2);
		border-bottom: 1px solid var(--border);
		font-size: 11px;
		text-transform: uppercase;
		letter-spacing: 0.4px;
		color: var(--text-2);
		position: sticky;
		top: 0;
	}
	.preview td {
		padding: 6px 10px;
		border-bottom: 1px solid var(--border);
		vertical-align: top;
		color: var(--text);
	}
	.preview tr:nth-child(even) td {
		background: var(--surface-2);
	}
</style>
