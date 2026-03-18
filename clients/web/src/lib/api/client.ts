// Typed REST client for all 7 JARVIS services via grpc-gateway (:8080)

let _token: string | null = null;

export function setToken(t: string | null) { _token = t; }

function reqId() {
	return `web-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`;
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
	const headers: Record<string, string> = { 'Content-Type': 'application/json' };
	if (_token) headers['Authorization'] = `Bearer ${_token}`;

	const res = await fetch(`/v1${path}`, {
		method,
		headers,
		body: body ? JSON.stringify(body) : undefined
	});

	if (!res.ok) {
		const err = await res.json().catch(() => ({ message: res.statusText }));
		throw new Error(err.message || res.statusText);
	}
	return res.json() as Promise<T>;
}

// ── Security ────────────────────────────────────────────────────────

export interface AuthResponse {
	accessToken: string;
	expiresAt: string;
	meta: ResponseMeta;
}

export interface ResponseMeta {
	requestId: string;
	success: boolean;
	errorCode?: string;
	errorMessage?: string;
}

export const security = {
	authenticate(subjectId: string, credential?: string): Promise<AuthResponse> {
		return call('POST', '/security/authenticate', {
			meta: { request_id: reqId() },
			subject_id: subjectId,
			method: 'AUTH_METHOD_TOKEN',
			credential_payload: credential ?? ''
		});
	},
	assessThreat(subjectId: string, location: string, signals: string[]) {
		return call('POST', '/security/threat', {
			meta: { request_id: reqId() },
			subject_id: subjectId,
			location,
			observed_signals: signals
		});
	},
	auditLog(pageSize = 20) {
		return call('GET', `/security/audit?page_size=${pageSize}`);
	}
};

// ── NLP ─────────────────────────────────────────────────────────────

export interface DialogueTurnResponse {
	replyText: string;
	resolvedIntent: string;
	requiresConfirmation: boolean;
	sessionId: string;
	meta: ResponseMeta;
}

export const nlp = {
	dialogue(sessionId: string, utterance: string, userId = 'web-user'): Promise<DialogueTurnResponse> {
		return call('POST', '/nlp/dialogue', {
			meta: { request_id: reqId(), user_id: userId, session_id: sessionId },
			session_id: sessionId,
			utterance
		});
	},
	parseIntent(rawText: string, sessionId?: string) {
		return call('POST', '/nlp/parse', {
			meta: { request_id: reqId(), session_id: sessionId },
			raw_text: rawText,
			language_code: 'en-US',
			session_id: sessionId
		});
	}
};

// ── Business Ops ─────────────────────────────────────────────────────

export interface ScheduleEventResponse {
	eventId: string;
	conflicts: string[];
	autoResolved: boolean;
	meta: ResponseMeta;
}

export interface ScheduledEvent {
	eventId: string;
	title: string;
	location: string;
	attendees: string[];
	start: string;
	end: string;
	status: string;
}

export const business = {
	scheduleEvent(params: {
		title: string;
		description?: string;
		location?: string;
		attendees?: string[];
		start: string;
		end: string;
		highPriority?: boolean;
	}): Promise<ScheduleEventResponse> {
		return call('POST', '/business/schedule', {
			meta: { request_id: reqId() },
			title: params.title,
			description: params.description ?? '',
			location: params.location ?? '',
			attendees: params.attendees ?? [],
			start: params.start,
			end: params.end,
			high_priority: params.highPriority ?? false
		});
	},
	getSchedule(subjectId: string) {
		return call('GET', `/business/schedule/${subjectId}`);
	},
	createTask(title: string, assigneeId: string, description?: string) {
		return call('POST', '/business/tasks', {
			meta: { request_id: reqId() },
			title,
			description: description ?? '',
			assignee_id: assigneeId,
			priority: 3
		});
	},
	getTasks() {
		return call('GET', '/business/tasks');
	}
};

// ── Intelligence ─────────────────────────────────────────────────────

export const intel = {
	query(query: string, subjectType = 'SUBJECT_TYPE_UNKNOWN', depth = 'ANALYSIS_DEPTH_STANDARD') {
		return call('POST', '/intel/query', {
			meta: { request_id: reqId() },
			query,
			subject_type: subjectType,
			depth,
			data_sources: []
		});
	},
	analyzeArtifact(artifactId: string, description: string) {
		return call('POST', '/intel/artifact', {
			meta: { request_id: reqId() },
			artifact_id: artifactId,
			artifact_description: description
		});
	},
	crossReference(subjectIds: string[], hint?: string) {
		return call('POST', '/intel/crossref', {
			meta: { request_id: reqId() },
			subject_ids: subjectIds,
			relationship_hint: hint ?? ''
		});
	}
};

// ── Facility ─────────────────────────────────────────────────────────

export const facility = {
	getEnvironment(zoneId: string) {
		return call('GET', `/facility/zones/${zoneId}/environment`);
	},
	controlSystem(zoneId: string, system: string, command: string, settings: Record<string, string> = {}) {
		return call('POST', `/facility/zones/${zoneId}/system`, {
			meta: { request_id: reqId() },
			zone_id: zoneId,
			system,
			command,
			settings
		});
	}
};

// ── Learning ─────────────────────────────────────────────────────────

export const learning = {
	getProfile(userId: string) {
		return call('GET', `/learning/profile/${userId}`);
	},
	submitFeedback(interactionId: string, correction: string, rating: number) {
		return call('POST', '/learning/feedback', {
			meta: { request_id: reqId() },
			interaction_id: interactionId,
			feedback_type: 'FEEDBACK_TYPE_CORRECTION',
			correction,
			rating
		});
	}
};
