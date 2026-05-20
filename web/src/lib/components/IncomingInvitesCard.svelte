<script lang="ts">
	// IncomingInvitesCard — самостоятельный компонент-карточка для блока
	// «Входящие приглашения». Сам грузит /meetings/incoming, обрабатывает
	// accept/decline. Используется в /dashboard и опционально может быть
	// заменён ручной разметкой на /scheduler.
	import { onMount } from 'svelte';
	import Card from './Card.svelte';
	import Badge from './Badge.svelte';
	import Button from './Button.svelte';
	import Modal from './Modal.svelte';
	import {
		listIncomingInvites,
		respondToMeeting,
		type IncomingMeeting
	} from '$lib/api/meetings';
	import { ApiError } from '$lib/api/client';

	interface Props {
		// Если true — карточка не показывается совсем когда нет приглашений.
		hideEmpty?: boolean;
		// Заголовок карточки.
		title?: string;
		subtitle?: string;
	}

	let { hideEmpty = true, title = 'Входящие приглашения', subtitle = '' }: Props = $props();

	let invites = $state<IncomingMeeting[]>([]);
	let loading = $state(true);
	let respondingId = $state<string | null>(null);
	let error = $state<string | null>(null);
	let success = $state<string | null>(null);

	// Состояние модалок.
	type ModalKind = 'accept' | 'decline' | 'revert';
	let modalOpen = $state(false);
	let modalKind = $state<ModalKind>('accept');
	let modalInvite = $state<IncomingMeeting | null>(null);

	function openModal(kind: ModalKind, inv: IncomingMeeting) {
		modalKind = kind;
		modalInvite = inv;
		modalOpen = true;
	}
	function closeModal() {
		modalOpen = false;
		modalInvite = null;
	}

	onMount(async () => {
		await load();
	});

	async function load() {
		try {
			const r = await listIncomingInvites();
			invites = r.invites ?? [];
		} catch {
			invites = [];
		} finally {
			loading = false;
		}
	}

	function fmt(iso: string): string {
		try {
			return new Date(iso).toLocaleString('ru', {
				weekday: 'short',
				day: 'numeric',
				month: 'short',
				hour: '2-digit',
				minute: '2-digit'
			});
		} catch {
			return iso;
		}
	}

	// Универсальный отправитель respond. Закрывает модалку, обновляет список.
	async function sendRespond(
		inv: IncomingMeeting,
		status: 'accepted' | 'declined',
		pushYandex: boolean,
		successMsg: string
	) {
		respondingId = inv.meeting_id;
		error = null;
		success = null;
		try {
			await respondToMeeting(inv.meeting_id, { status, push_yandex: pushYandex });
			success = successMsg;
			await load();
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		} finally {
			respondingId = null;
			closeModal();
		}
	}

	function onAccept(inv: IncomingMeeting) {
		if (!inv.has_yandex) {
			// Нет интеграции — спрашивать про календарь нечего, сразу accept без модалки.
			void sendRespond(inv, 'accepted', false, 'Подтверждено.');
			return;
		}
		openModal('accept', inv);
	}

	function onDecline(inv: IncomingMeeting) {
		openModal('decline', inv);
	}

	function onRevert(inv: IncomingMeeting) {
		openModal('revert', inv);
	}

	// Действия из модалки accept.
	function confirmWithYandex() {
		if (modalInvite) {
			void sendRespond(modalInvite, 'accepted', true, 'Подтверждено. Событие в календаре.');
		}
	}
	function confirmWithoutYandex() {
		if (modalInvite) {
			void sendRespond(modalInvite, 'accepted', false, 'Подтверждено.');
		}
	}
	function confirmDecline() {
		if (modalInvite) {
			void sendRespond(modalInvite, 'declined', false, 'Отклонено.');
		}
	}
	function confirmRevert() {
		if (!modalInvite) return;
		const newStatus = modalInvite.status === 'accepted' ? 'declined' : 'accepted';
		void sendRespond(
			modalInvite,
			newStatus,
			newStatus === 'accepted' && modalInvite.has_yandex,
			newStatus === 'accepted' ? 'Подтверждено.' : 'Отклонено.'
		);
	}

	const pendingCount = $derived(invites.filter((i) => i.status === 'pending').length);
</script>

{#if !loading && (invites.length > 0 || !hideEmpty)}
	<Card {title} subtitle={subtitle || (pendingCount > 0 ? `${pendingCount} ждёт ответа` : '')}>
		{#if error}
			<Badge variant="danger"><i class="ti ti-alert-circle"></i>{error}</Badge>
		{/if}
		{#if success}
			<Badge variant="success"><i class="ti ti-check"></i>{success}</Badge>
		{/if}

		{#if invites.length === 0}
			<div class="text-text-3 text-sm" style="padding: 12px; text-align: center;">
				Нет приглашений.
			</div>
		{:else}
			<div class="space-y-2">
				{#each invites as inv (inv.meeting_id)}
					<div
						class="inv-card inv-card--{inv.status}"
						class:inv-card--working={respondingId === inv.meeting_id}
					>
						<div class="inv-card__main">
							<div class="inv-card__title">
								{inv.title}
								{#if inv.status === 'accepted'}
									<Badge variant="success">подтверждено</Badge>
								{:else if inv.status === 'declined'}
									<Badge variant="danger">отклонено</Badge>
								{:else}
									<Badge variant="warning">ждёт ответа</Badge>
								{/if}
							</div>
							<div class="inv-card__meta">
								<span><i class="ti ti-clock"></i> {fmt(inv.start_at)} — {fmt(inv.end_at)}</span>
								{#if inv.team_name}
									<span><i class="ti ti-users"></i> {inv.team_name}</span>
								{/if}
								{#if inv.initiator_name}
									<span><i class="ti ti-user"></i> {inv.initiator_name}</span>
								{/if}
							</div>
						</div>

						{#if inv.status === 'pending'}
							<div class="inv-card__actions">
								<Button
									size="sm"
									variant="primary"
									icon="ti-check"
									onclick={() => onAccept(inv)}
									disabled={respondingId !== null}
								>
									Подтвердить
								</Button>
								<Button
									size="sm"
									variant="ghost"
									icon="ti-x"
									onclick={() => onDecline(inv)}
									disabled={respondingId !== null}
								>
									Отклонить
								</Button>
							</div>
						{:else}
							<div class="inv-card__actions">
								<Button
									size="sm"
									variant="ghost"
									icon="ti-pencil"
									onclick={() => onRevert(inv)}
									disabled={respondingId !== null}
								>
									Изменить
								</Button>
							</div>
						{/if}
					</div>
				{/each}
			</div>
		{/if}
	</Card>
{/if}

<!-- ============= Модалки accept / decline / revert ============= -->

{#if modalInvite}
	{@const inv = modalInvite}

	<!-- ACCEPT с вопросом про календарь -->
	<Modal
		open={modalOpen && modalKind === 'accept'}
		title="Подтвердить участие"
		onClose={closeModal}
	>
		<div class="m-summary">
			<div class="m-summary__title">{inv.title}</div>
			<div class="m-summary__meta">
				<span><i class="ti ti-clock"></i> {fmt(inv.start_at)} — {fmt(inv.end_at)}</span>
				{#if inv.team_name}<span><i class="ti ti-users"></i> {inv.team_name}</span>{/if}
				{#if inv.initiator_name}<span><i class="ti ti-user"></i> {inv.initiator_name}</span>{/if}
			</div>
		</div>
		<p class="m-question">Добавить событие в твой календарь?</p>

		{#snippet footer()}
			<Button variant="ghost" onclick={closeModal} disabled={respondingId !== null}>
				Отмена
			</Button>
			<Button
				variant="ghost"
				icon="ti-check"
				onclick={confirmWithoutYandex}
				disabled={respondingId !== null}
			>
				Без календаря
			</Button>
			<Button
				variant="primary"
				icon="ti-calendar-plus"
				onclick={confirmWithYandex}
				disabled={respondingId !== null}
			>
				{respondingId ? 'Добавляю…' : 'Да, в календарь'}
			</Button>
		{/snippet}
	</Modal>

	<!-- DECLINE -->
	<Modal
		open={modalOpen && modalKind === 'decline'}
		title="Отклонить участие"
		onClose={closeModal}
	>
		<div class="m-summary">
			<div class="m-summary__title">{inv.title}</div>
			<div class="m-summary__meta">
				<span><i class="ti ti-clock"></i> {fmt(inv.start_at)} — {fmt(inv.end_at)}</span>
			</div>
		</div>
		<p class="m-question">Отклонить? Ответ можно изменить позже.</p>

		{#snippet footer()}
			<Button variant="ghost" onclick={closeModal} disabled={respondingId !== null}>
				Отмена
			</Button>
			<Button
				variant="danger"
				icon="ti-x"
				onclick={confirmDecline}
				disabled={respondingId !== null}
			>
				{respondingId ? 'Отклоняю…' : 'Отклонить'}
			</Button>
		{/snippet}
	</Modal>

	<!-- REVERT (изменить ответ) -->
	<Modal
		open={modalOpen && modalKind === 'revert'}
		title="Изменить ответ"
		onClose={closeModal}
	>
		<div class="m-summary">
			<div class="m-summary__title">{inv.title}</div>
			<div class="m-summary__meta">
				<span><i class="ti ti-clock"></i> {fmt(inv.start_at)} — {fmt(inv.end_at)}</span>
			</div>
		</div>
		<p class="m-question">
			{#if inv.status === 'accepted'}
				Сейчас: <strong>подтверждено</strong>. Отклонить?
				{#if inv.yandex_pushed}<br/><span class="m-note">Событие удалится из твоего календаря.</span>{/if}
			{:else}
				Сейчас: <strong>отклонено</strong>. Подтвердить?
			{/if}
		</p>

		{#snippet footer()}
			<Button variant="ghost" onclick={closeModal} disabled={respondingId !== null}>
				Отмена
			</Button>
			<Button
				variant={inv.status === 'accepted' ? 'danger' : 'primary'}
				icon={inv.status === 'accepted' ? 'ti-x' : 'ti-check'}
				onclick={confirmRevert}
				disabled={respondingId !== null}
			>
				{respondingId
					? 'Обновляю…'
					: inv.status === 'accepted'
						? 'Отклонить'
						: 'Подтвердить'}
			</Button>
		{/snippet}
	</Modal>
{/if}

<style>
	.inv-card {
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 10px 12px;
		border: 1px solid var(--border);
		border-radius: 10px;
		background: var(--surface);
		transition: opacity 0.15s;
	}
	.inv-card--working {
		opacity: 0.6;
	}
	.inv-card--pending {
		border-left: 3px solid var(--warning-strong);
	}
	.inv-card--accepted {
		border-left: 3px solid var(--success-strong);
	}
	.inv-card--declined {
		border-left: 3px solid var(--danger-strong);
		opacity: 0.75;
	}
	.inv-card__main {
		flex: 1;
		min-width: 0;
	}
	.inv-card__title {
		font-weight: 600;
		font-size: 14px;
		color: var(--text);
		margin-bottom: 4px;
		display: flex;
		align-items: center;
		gap: 8px;
		flex-wrap: wrap;
	}
	.inv-card__meta {
		display: flex;
		flex-wrap: wrap;
		gap: 10px;
		font-size: 12px;
		color: var(--text-2);
	}
	.inv-card__meta i {
		font-size: 13px;
		opacity: 0.7;
		margin-right: 3px;
	}
	.inv-card__actions {
		display: flex;
		gap: 6px;
		flex-shrink: 0;
	}

	/* Модалки */
	.m-summary {
		padding: 10px 12px;
		border: 1px solid var(--border);
		border-radius: 10px;
		background: var(--surface-2);
		margin-bottom: 12px;
	}
	.m-summary__title {
		font-weight: 600;
		font-size: 14px;
		color: var(--text);
	}
	.m-summary__meta {
		display: flex;
		flex-wrap: wrap;
		gap: 10px;
		margin-top: 4px;
		font-size: 12px;
		color: var(--text-2);
	}
	.m-summary__meta i {
		font-size: 13px;
		opacity: 0.7;
		margin-right: 3px;
	}
	.m-question {
		margin: 0;
		font-size: 13px;
		line-height: 1.5;
		color: var(--text-2);
	}
	.m-question strong {
		color: var(--text);
		font-weight: 600;
	}
	.m-note {
		font-size: 12px;
		color: var(--warning-strong);
	}
</style>
