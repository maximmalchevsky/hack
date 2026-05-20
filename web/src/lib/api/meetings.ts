import { api } from './client';

export interface MyMeeting {
	id: string;
	title: string;
	start_at: string; // ISO
	end_at: string;
	team_id?: string;
	team_name?: string;
	created_at: string;
	cancelled_at?: string;
	yandex_pushed: number;
	is_owner: boolean;
	can_cancel: boolean;
	accepted: number;
	declined: number;
	pending: number;
	total_invited: number;
}

export interface IncomingMeeting {
	meeting_id: string;
	title: string;
	start_at: string;
	end_at: string;
	team_id?: string;
	team_name?: string;
	initiator_name?: string;
	status: 'pending' | 'accepted' | 'declined';
	yandex_pushed: boolean;
	has_yandex: boolean; // у меня подключён Яндекс — можно предложить «в мой календарь»
	responded_at?: string;
}

export interface MeetingResponseRow {
	employee_id: string;
	full_name: string;
	status: 'pending' | 'accepted' | 'declined';
	yandex_pushed: boolean;
	responded_at?: string;
}

export const listMyMeetings = () =>
	api.get<{ meetings: MyMeeting[] }>('/api/v1/meetings/my');

export const cancelMeeting = (id: string) =>
	api.delete<{ ok: boolean }>(`/api/v1/meetings/${id}`);

export interface UpdateMeetingBody {
	title?: string;
	start_at?: string; // ISO с TZ (отправляем UTC через Date.toISOString())
	end_at?: string;
}

export const updateMeeting = (id: string, body: UpdateMeetingBody) =>
	api.put<{ ok: boolean }>(`/api/v1/meetings/${id}`, body);

export const listIncomingInvites = () =>
	api.get<{ invites: IncomingMeeting[] }>('/api/v1/meetings/incoming');

export const respondToMeeting = (id: string, body: { status: 'accepted' | 'declined'; push_yandex?: boolean }) =>
	api.post<{ ok: boolean }>(`/api/v1/meetings/${id}/respond`, body);

export const listMeetingResponses = (id: string) =>
	api.get<{ responses: MeetingResponseRow[] }>(`/api/v1/meetings/${id}/responses`);
