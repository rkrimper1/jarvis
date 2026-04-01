// Typed REST client for all 9 JARVIS services via grpc-gateway (:8080)

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
	grantedScopes: string[];
	meta: ResponseMeta;
}

export interface ResponseMeta {
	requestId: string;
	success: boolean;
	errorCode?: string;
	errorMessage?: string;
}

export const security = {
	authenticate(subjectId: string, credential = ''): Promise<AuthResponse> {
		return call('POST', '/security/authenticate', {
			meta: { request_id: reqId() },
			subject_id: subjectId,
			method: 'AUTH_METHOD_TOKEN',
			// bytes field — must be base64-encoded so grpc-gateway decodes back to raw password bytes
			credential_payload: btoa(credential)
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
	auditLog(pageSize = 20): Promise<AuditLogResponse> {
		return call('GET', `/security/audit?meta.request_id=${reqId()}&page_size=${pageSize}`);
	},
	analyzeFaces(imageData: string, filename: string): Promise<AnalyzeFacesResponse> {
		return call('POST', '/security/faces', {
			meta: { request_id: reqId() },
			image_data: imageData,
			filename
		});
	}
};

export interface AuditEntry {
	eventId: string;
	subjectId: string;
	action: string;
	resource: string;
	success: boolean;
	timestamp: string;
}

export interface SurroundingsStatus {
	score: number;
	color: string;  // GREEN | YELLOW | RED
	status: string; // NOMINAL | COMPROMISED
}

export interface AuditLogResponse {
	meta: ResponseMeta;
	entries: AuditEntry[];
	nextPageToken: string;
	surroundingsStatus?: SurroundingsStatus;
}

export interface BoundingBox {
	x: number;
	y: number;
	width: number;
	height: number;
}

export interface FaceAnalysis {
	faceIndex: number;
	sentiment: string;
	commentary: string;
	boundingBox: BoundingBox;
}

export interface AnalyzeFacesResponse {
	meta: ResponseMeta;
	imageUrl: string;
	faceCount: number;
	faces: FaceAnalysis[];
}

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

export interface AlexaDevice {
	serialNumber?: string;
	name: string;
	deviceFamily?: string;
	deviceType?: string;
	online: boolean;
	isSmartHome: boolean;
	applianceId?: string;
	capabilities?: string[];
	powerState?: string; // "ON", "OFF", or empty if unknown
}

export interface ListAlexaDevicesResponse {
	meta: ResponseMeta;
	devices: AlexaDevice[];
}

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
	},
	listAlexaDevices(): Promise<ListAlexaDevicesResponse> {
		return call('GET', `/facility/alexa/devices?meta.request_id=${reqId()}`);
	},
	sendAlexaCommand(applianceId: string, action: string, parameters: Record<string, string> = {}): Promise<{ meta: ResponseMeta }> {
		return call('POST', '/facility/alexa/command', {
			meta: { request_id: reqId() },
			appliance_id: applianceId,
			action,
			parameters
		});
	}
};

// ── Learning ─────────────────────────────────────────────────────────

export type KnowledgeSource =
	| 'KNOWLEDGE_SOURCE_UNSPECIFIED'
	| 'KNOWLEDGE_SOURCE_WEB_SEARCH'
	| 'KNOWLEDGE_SOURCE_CLAUDE_API'
	| 'KNOWLEDGE_SOURCE_MANUAL';

export interface KnowledgeEntry {
	id: string;
	query: string;
	summary: string;
	source: KnowledgeSource;
	confidence: number;
	tags: string;
	createdAt: string;
	updatedAt: string;
}

export interface AddKnowledgeResponse {
	meta: ResponseMeta;
	entry: KnowledgeEntry;
}

export interface ListKnowledgeResponse {
	meta: ResponseMeta;
	entries: KnowledgeEntry[];
}

export interface SearchKnowledgeResponse {
	meta: ResponseMeta;
	results: KnowledgeEntry[];
	needsConfirmation: boolean;
	suggestedSource: KnowledgeSource;
	searchesRemaining: number;
}

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
	},
	addKnowledge(query: string, summary: string, tags: string, confidence: number): Promise<AddKnowledgeResponse> {
		return call('POST', '/learning/knowledge', {
			meta: { request_id: reqId() },
			query, summary, tags, confidence
		});
	},
	listKnowledge(limit = 5): Promise<ListKnowledgeResponse> {
		return call('POST', '/learning/knowledge/list', {
			meta: { request_id: reqId() },
			limit
		});
	},
	searchKnowledge(
		query: string,
		preferredSource: KnowledgeSource = 'KNOWLEDGE_SOURCE_UNSPECIFIED',
		confirmed = false
	): Promise<SearchKnowledgeResponse> {
		return call('POST', '/learning/knowledge/search', {
			meta: { request_id: reqId() },
			query,
			preferred_source: preferredSource,
			confirmed
		});
	}
};

// ── Users ─────────────────────────────────────────────────────────────

export type UserRole = 'ROLE_UNSPECIFIED' | 'ROLE_ADMIN' | 'ROLE_EDITOR' | 'ROLE_VIEWER';

export interface User {
	id: string;
	username: string;
	email: string;
	displayName: string;
	role: UserRole;
	isActive: boolean;
	createdAt: string;
	updatedAt: string;
}

export const users = {
	getMe(username: string): Promise<{ user: User }> {
		return call('GET', `/users/me?username=${encodeURIComponent(username)}`);
	},
	updateProfile(id: string, email: string, displayName: string): Promise<{ user: User }> {
		return call('POST', '/users/me/profile', { id, email, display_name: displayName });
	},
	changePassword(id: string, currentPassword: string, newPassword: string): Promise<Record<string, never>> {
		return call('POST', '/users/me/password', {
			id,
			current_password: currentPassword,
			new_password: newPassword
		});
	},
	list(): Promise<{ users: User[]; totalCount: number }> {
		return call('GET', '/users');
	},
	create(username: string, email: string, displayName: string, password: string, role: UserRole): Promise<{ user: User }> {
		return call('POST', '/users', { username, email, display_name: displayName, password, role });
	},
	delete(id: string): Promise<{ id: string }> {
		return call('DELETE', `/users/${id}`);
	}
};
