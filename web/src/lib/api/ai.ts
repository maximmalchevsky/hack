import { api, getAccessToken } from './client';
import { browser } from '$app/environment';
import { env } from '$env/dynamic/public';

export interface ChatRequest {
	conversation_id?: string;
	message: string;
}

export interface ChatResponse {
	conversation_id: string;
	answer: string;
	available: boolean;
}

export interface AIHealth {
	ok: boolean;
	model?: string;
	latency_ms?: number;
	reason?: string;
}

export const askAI = (body: ChatRequest) => api.post<ChatResponse>('/api/v1/ai/chat', body);

export const aiStatus = () => api.get<{ available: boolean }>('/api/v1/ai/status');

export const aiHealth = () => api.get<AIHealth>('/api/v1/ai/health');

export interface StoredChatMessage {
	role: 'user' | 'assistant' | 'system';
	content: string;
	created_at: string;
}

export const getLatestConversation = () =>
	api.get<{ conversation_id: string | null }>('/api/v1/ai/conversations/latest');

export const getConversationMessages = (id: string) =>
	api.get<{ messages: StoredChatMessage[] }>(`/api/v1/ai/conversations/${id}/messages`);

export const deleteConversation = (id: string) =>
	api.delete<{ ok: boolean }>(`/api/v1/ai/conversations/${id}`);

function baseURL(): string {
	return env.PUBLIC_API_URL || (browser ? '' : 'http://localhost:8080');
}

export type StreamEvent =
	| { type: 'meta'; conversation_id: string }
	| { type: 'delta'; text: string }
	| { type: 'error'; message: string }
	| { type: 'done' };

/**
 * streamChat — POST на /api/v1/ai/chat/stream, парсит SSE.
 *
 * Используем fetch + ReadableStream вместо EventSource (тот не умеет POST).
 * Поток событий: meta → delta* → done|error (см. handler/ai.go).
 */
export async function streamChat(
	body: ChatRequest,
	onEvent: (e: StreamEvent) => void,
	signal?: AbortSignal
): Promise<void> {
	const token = getAccessToken();
	const res = await fetch(`${baseURL()}/api/v1/ai/chat/stream`, {
		method: 'POST',
		headers: {
			'Content-Type': 'application/json',
			Accept: 'text/event-stream',
			...(token ? { Authorization: `Bearer ${token}` } : {})
		},
		body: JSON.stringify(body),
		signal
	});

	if (!res.ok) {
		let msg = `request failed: ${res.status}`;
		try {
			const j = await res.json();
			if (j && typeof j.error === 'string') msg = j.error;
		} catch {
			// ignore
		}
		throw new Error(msg);
	}

	if (!res.body) {
		throw new Error('empty body');
	}

	const reader = res.body.getReader();
	const decoder = new TextDecoder();
	let buf = '';

	while (true) {
		const { value, done } = await reader.read();
		if (done) break;
		buf += decoder.decode(value, { stream: true });

		let idx: number;
		while ((idx = buf.indexOf('\n\n')) >= 0) {
			const frame = buf.slice(0, idx);
			buf = buf.slice(idx + 2);
			const parsed = parseFrame(frame);
			if (parsed) onEvent(parsed);
		}
	}
}

function parseFrame(frame: string): StreamEvent | null {
	let event = 'message';
	let data = '';
	for (const line of frame.split('\n')) {
		if (line.startsWith('event:')) event = line.slice(6).trim();
		else if (line.startsWith('data:')) data += line.slice(5).trim();
	}
	if (!data) return null;
	let payload: Record<string, unknown> = {};
	try {
		payload = JSON.parse(data);
	} catch {
		return null;
	}
	switch (event) {
		case 'meta':
			return { type: 'meta', conversation_id: String(payload.conversation_id ?? '') };
		case 'delta':
			return { type: 'delta', text: String(payload.text ?? '') };
		case 'error':
			return { type: 'error', message: String(payload.message ?? 'unknown') };
		case 'done':
			return { type: 'done' };
		default:
			return null;
	}
}
