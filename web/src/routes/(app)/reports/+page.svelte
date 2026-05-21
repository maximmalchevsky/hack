<script lang="ts">
	import { browser } from '$app/environment';
	import { env } from '$env/dynamic/public';
	import Card from '$lib/components/Card.svelte';
	import Button from '$lib/components/Button.svelte';
	import Badge from '$lib/components/Badge.svelte';
	import { getAccessToken } from '$lib/api/client';
	import { getExportDataset, type ExportDataset } from '$lib/api/exports';

	type Kind = 'upcoming_vacations' | 'stale_profiles' | 'conflicts' | 'all_employees';

	interface Preset {
		kind: Kind;
		title: string;
		subtitle: string;
		icon: string;
		variant: 'info' | 'warning' | 'danger' | 'success';
	}

	const presets: Preset[] = [
		{
			kind: 'upcoming_vacations',
			title: 'Ближайшие отпуска',
			subtitle: 'Отпуска, больничные и командировки на 30 дней вперёд',
			icon: 'ti-beach',
			variant: 'info'
		},
		{
			kind: 'stale_profiles',
			title: 'Устаревшие профили',
			subtitle: 'Сотрудники с устаревшим или неподтверждённым графиком',
			icon: 'ti-clock-exclamation',
			variant: 'warning'
		},
		{
			kind: 'conflicts',
			title: 'Конфликты в календаре',
			subtitle: 'События вне рабочего графика за −7/+30 дней',
			icon: 'ti-alert-triangle',
			variant: 'danger'
		},
		{
			kind: 'all_employees',
			title: 'Все сотрудники',
			subtitle: 'Справочник: имя, роль, отдел, TZ, формат, обновление',
			icon: 'ti-users',
			variant: 'success'
		}
	];

	let busy = $state<Record<string, 'pdf' | 'xlsx' | null>>({});
	let error = $state<string | null>(null);
	let success = $state<string | null>(null);

	function backendURL(): string {
		return env.PUBLIC_API_URL || (browser ? '' : 'http://localhost:8080');
	}

	function formatBytes(n: number): string {
		if (n < 1024) return `${n} Б`;
		if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} КБ`;
		return `${(n / 1024 / 1024).toFixed(2)} МБ`;
	}

	async function downloadExcel(p: Preset) {
		busy[p.kind] = 'xlsx';
		error = null;
		success = null;
		try {
			const tok = getAccessToken() ?? '';
			const resp = await fetch(`${backendURL()}/api/v1/exports/${p.kind}`, {
				headers: tok ? { Authorization: `Bearer ${tok}` } : {}
			});
			if (!resp.ok) throw new Error(`status ${resp.status}`);
			const blob = await resp.blob();
			const cd = resp.headers.get('Content-Disposition') ?? '';
			const m = cd.match(/filename="?([^"]+)"?/);
			const filename = m ? m[1] : `worktime-${p.kind}.xlsx`;
			triggerDownload(blob, filename);
			success = `${p.title} — Excel (${formatBytes(blob.size)}) сохранён.`;
		} catch (e) {
			error = `Не удалось выгрузить Excel: ${e instanceof Error ? e.message : String(e)}`;
		} finally {
			busy[p.kind] = null;
		}
	}

	async function downloadPDF(p: Preset) {
		busy[p.kind] = 'pdf';
		error = null;
		success = null;
		try {
			const ds = await getExportDataset(p.kind);
			await renderPDF(ds, p);
			success = `${p.title} — PDF сохранён.`;
		} catch (e) {
			error = `Не удалось собрать PDF: ${e instanceof Error ? e.message : String(e)}`;
		} finally {
			busy[p.kind] = null;
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

	async function renderPDF(ds: ExportDataset, p: Preset) {
		if (!browser) return;
		// Динамический импорт — html2pdf.js не SSR-friendly.
		const mod = await import('html2pdf.js');
		const html2pdf = mod.default;

		const container = document.createElement('div');
		// font-family: Arial — без Inter. Canvas-рендер html2canvas не всегда
		// успевает подгрузить веб-шрифт и тогда «съедает» пробелы между
		// жирными кириллическими словами. Системный sans-serif не глючит.
		container.style.cssText = `
			font-family: Arial, Helvetica, sans-serif;
			color: #0f172a;
			padding: 24px;
			width: 1024px;
			word-spacing: 0.1em;
		`;
		container.innerHTML = buildPDFHtml(ds, p);
		document.body.appendChild(container);

		const dateStr = new Date().toISOString().slice(0, 10);
		const filename = `worktime-${p.kind}-${dateStr}.pdf`;

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

	function buildPDFHtml(ds: ExportDataset, p: Preset): string {
		const now = new Date();
		const dateStr = now.toLocaleDateString('ru', {
			day: 'numeric',
			month: 'long',
			year: 'numeric'
		});
		const timeStr = now.toLocaleTimeString('ru', {
			hour: '2-digit',
			minute: '2-digit'
		});
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
		return `
			<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:18px;border-bottom:2px solid #3b82f6;padding-bottom:10px;">
				<div>
					<div style="font-size:11px;color:#64748b;letter-spacing:.5px;text-transform:uppercase;">Workie · отчёт</div>
					<div style="font-size:22px;font-weight:700;color:#0f172a;margin-top:2px;word-spacing:0.25em;">${escapeHtml(p.title).replace(/ /g, '&nbsp;')}</div>
					<div style="font-size:12px;color:#475569;margin-top:4px;">${escapeHtml(ds.title).replace(/ /g, '&nbsp;')}</div>
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

	function escapeHtml(s: string): string {
		return s
			.replaceAll('&', '&amp;')
			.replaceAll('<', '&lt;')
			.replaceAll('>', '&gt;')
			.replaceAll('"', '&quot;');
	}
</script>

<div class="page-header">
	<div>
		<h1>Отчёты</h1>
		<div class="page-header__subtitle">
			Готовые пресеты выгрузки в Excel (для дальнейшей обработки) или PDF (для печати/отправки).
		</div>
	</div>
	<div>
		<Button variant="ghost" icon="ti-adjustments" onclick={() => (location.href = '/reports/builder')}>
			Конструктор
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

<div class="section grid-2" style="gap: 16px;">
	{#each presets as p (p.kind)}
		<Card>
			<div class="report">
				<div class="report__icon report__icon--{p.variant}">
					<i class="ti {p.icon}"></i>
				</div>
				<div class="report__main">
					<div class="report__title">{p.title}</div>
					<div class="report__subtitle">{p.subtitle}</div>
				</div>
				<div class="report__actions">
					<Button
						size="sm"
						variant="ghost"
						icon="ti-file-spreadsheet"
						onclick={() => downloadExcel(p)}
						disabled={busy[p.kind] !== null && busy[p.kind] !== undefined}
					>
						{busy[p.kind] === 'xlsx' ? 'Готовим…' : 'Excel'}
					</Button>
					<Button
						size="sm"
						variant="primary"
						icon="ti-file-type-pdf"
						onclick={() => downloadPDF(p)}
						disabled={busy[p.kind] !== null && busy[p.kind] !== undefined}
					>
						{busy[p.kind] === 'pdf' ? 'Готовим…' : 'PDF'}
					</Button>
				</div>
			</div>
		</Card>
	{/each}
</div>

<style>
	.report {
		display: flex;
		align-items: center;
		gap: 14px;
	}
	.report__icon {
		width: 44px;
		height: 44px;
		border-radius: 12px;
		display: flex;
		align-items: center;
		justify-content: center;
		font-size: 22px;
		flex-shrink: 0;
	}
	.report__icon--info {
		background: var(--info-bg);
		color: var(--info-strong);
	}
	.report__icon--warning {
		background: var(--warning-bg);
		color: var(--warning-strong);
	}
	.report__icon--danger {
		background: var(--danger-bg);
		color: var(--danger-strong);
	}
	.report__icon--success {
		background: var(--success-bg);
		color: var(--success-strong);
	}
	.report__main {
		flex: 1;
		min-width: 0;
	}
	.report__title {
		font-weight: 600;
		font-size: 15px;
		color: var(--text);
	}
	.report__subtitle {
		font-size: 12px;
		color: var(--text-2);
		margin-top: 2px;
	}
	.report__actions {
		display: flex;
		gap: 8px;
		flex-shrink: 0;
	}
</style>
