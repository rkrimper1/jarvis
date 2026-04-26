// Typed REST client for all 10 JARVIS services via grpc-gateway (:8080)

import { get } from 'svelte/store';
import { userId } from '$lib/stores/auth';

let _token: string | null = null;
let _onUnauthorized: (() => void) | null = null;

export function setToken(t: string | null) { _token = t; }
export function onUnauthorized(fn: () => void) { _onUnauthorized = fn; }

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

	if (res.status === 401) {
		_onUnauthorized?.();
		throw new Error('Session expired. Please log in again.');
	}

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
	},
	analyzeThreatScene(imageData: string, detectedObjects: string[] = []): Promise<AnalyzeThreatSceneResponse> {
		return call('POST', '/security/threat-scene', {
			meta: { request_id: reqId() },
			image_data: imageData,
			detected_objects: detectedObjects
		});
	},
	logThreatEvent(params: {
		cameraLabel: string;
		detectedObjects: string[];
		level: ThreatLevel;
		confidence: number;
		threatSummary: string;
		recommendedActions: string[];
		imageData?: string; // base64 JPEG; only stored when THREAT_LOG_IMAGES=true
		force?: boolean;    // true = manual button press
	}): Promise<LogThreatEventResponse> {
		return call('POST', '/security/threat-events', {
			meta:                { request_id: reqId() },
			camera_label:        params.cameraLabel,
			detected_objects:    params.detectedObjects,
			level:               params.level,
			confidence:          params.confidence,
			threat_summary:      params.threatSummary,
			recommended_actions: params.recommendedActions,
			image_data:          params.imageData ?? '',
			force:               params.force ?? false
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

export type ThreatLevel =
	| 'THREAT_LEVEL_UNSPECIFIED'
	| 'THREAT_LEVEL_LOW'
	| 'THREAT_LEVEL_MODERATE'
	| 'THREAT_LEVEL_HIGH'
	| 'THREAT_LEVEL_CRITICAL';

export interface AnalyzeThreatSceneResponse {
	meta: ResponseMeta;
	level: ThreatLevel;
	confidence: number;
	threatSummary: string;
	recommendedActions: string[];
	logMode: string; // "auto" | "manual" | "all"
}

export interface ThreatEvent {
	eventId: string;
	timestamp: string;
	cameraLabel: string;
	detectedObjects: string[];
	level: ThreatLevel;
	confidence: number;
	threatSummary: string;
	recommendedActions: string[];
	imageUrl: string;
}

export interface LogThreatEventResponse {
	meta: ResponseMeta;
	event: ThreatEvent;
	logged: boolean;
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

export interface RawSignal {
	id: string;
	rawContent: string;
	sourceType: string;
	sourceUri: string;
	ingestedAt: string;
}

export type OpportunityType =
	| 'OPPORTUNITY_TYPE_TACTICAL'
	| 'OPPORTUNITY_TYPE_STRATEGIC'
	| 'OPPORTUNITY_TYPE_RESOURCE'
	| 'OPPORTUNITY_TYPE_THREAT_MITIGATION';

export type IntelCardStatus =
	| 'INTEL_CARD_STATUS_PENDING_REVIEW'
	| 'INTEL_CARD_STATUS_CONFIRMED'
	| 'INTEL_CARD_STATUS_DISMISSED';

export interface IntelCard {
	id: string;
	title: string;
	summary: string;
	opportunityType: OpportunityType;
	confidenceScore: number;
	suggestedAction: string;
	status: IntelCardStatus;
	rawSignalIds: string[];
	createdAt: string;
	updatedAt: string;
}

export interface IngestSignalResponse {
	meta: ResponseMeta;
	signal: RawSignal;
	card: IntelCard;
}

export interface ListIntelCardsResponse {
	meta: ResponseMeta;
	cards: IntelCard[];
	totalCount: number;
	nextPageToken: string;
}

export interface ConfirmActionResponse {
	meta: ResponseMeta;
	card: IntelCard;
}

export const intel = {
	// ── Legacy RPCs ───────────────────────────────────────────────────
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
	},

	// ── Intel Hunt RPCs ───────────────────────────────────────────────
	ingestSignal(rawContent: string, sourceType = 'SOURCE_TYPE_MANUAL', sourceUri = ''): Promise<IngestSignalResponse> {
		return call('POST', '/intel/signals', {
			meta: { request_id: reqId() },
			source_type: sourceType,
			raw_content: rawContent,
			source_uri: sourceUri
		});
	},
	listCards(statusFilter = '', pageSize = 20, pageToken = ''): Promise<ListIntelCardsResponse> {
		const params = new URLSearchParams({
			'meta.request_id': reqId(),
			page_size: String(pageSize)
		});
		if (statusFilter && statusFilter !== 'INTEL_CARD_STATUS_UNSPECIFIED') {
			params.set('status_filter', statusFilter);
		}
		if (pageToken) params.set('page_token', pageToken);
		return call('GET', `/intel/cards?${params}`);
	},
	confirmAction(cardId: string, newStatus: IntelCardStatus): Promise<ConfirmActionResponse> {
		return call('POST', `/intel/cards/${cardId}/confirm`, {
			meta: { request_id: reqId() },
			card_id: cardId,
			new_status: newStatus
		});
	},
	searchCards(query: string, pageSize = 20): Promise<{ cards: IntelCard[]; total: number }> {
		const params = new URLSearchParams({ q: query, page_size: String(pageSize) });
		return call('GET', `/intel/cards/search?${params}`);
	},
	async ingestFile(file: File, sourceUri = ''): Promise<IngestSignalResponse> {
		const fd = new FormData();
		fd.append('file', file);
		if (sourceUri) fd.append('source_uri', sourceUri);

		const headers: Record<string, string> = {};
		if (_token) headers['Authorization'] = `Bearer ${_token}`;

		const res = await fetch('/v1/intel/ingest/file', { method: 'POST', headers, body: fd });
		if (res.status === 401) { _onUnauthorized?.(); throw new Error('Session expired. Please log in again.'); }
		if (!res.ok) {
			const err = await res.json().catch(() => ({ error: res.statusText }));
			throw new Error(err.error || res.statusText);
		}
		return res.json();
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
	},
	async alexaCookieStatus(): Promise<{ configured: boolean; expires_at?: string; days_until_expiry?: number; expired?: boolean }> {
		const res = await fetch('/alexa/cookie-status');
		if (!res.ok) throw new Error(`cookie status: HTTP ${res.status}`);
		return res.json();
	},
	async sendAlexaTextCommand(text: string): Promise<{ device?: string; action?: string }> {
		const res = await fetch('/alexa/text-command', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ text })
		});
		if (!res.ok) {
			const t = await res.text();
			throw new Error(t || `HTTP ${res.status}`);
		}
		return res.json();
	},
	async refreshAlexaCookies(cookieJson: string): Promise<void> {
		// Validate it parses before sending.
		JSON.parse(cookieJson);
		const res = await fetch('/alexa/cookies', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: cookieJson
		});
		if (!res.ok) {
			const text = await res.text();
			throw new Error(text || `HTTP ${res.status}`);
		}
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

// ── Tasks ─────────────────────────────────────────────────────────────

export type TaskType =
	| 'TASK_TYPE_UNSPECIFIED'
	| 'TASK_TYPE_TASK'
	| 'TASK_TYPE_EPIC'
	| 'TASK_TYPE_STORY'
	| 'TASK_TYPE_BUG'
	| 'TASK_TYPE_SUBTASK';

export type TaskPriority =
	| 'TASK_PRIORITY_UNSPECIFIED'
	| 'TASK_PRIORITY_CRITICAL'
	| 'TASK_PRIORITY_HIGH'
	| 'TASK_PRIORITY_MEDIUM'
	| 'TASK_PRIORITY_LOW';

export type TaskStatus =
	| 'TASK_STATUS_UNSPECIFIED'
	| 'TASK_STATUS_UNASSIGNED'
	| 'TASK_STATUS_ASSIGNED'
	| 'TASK_STATUS_IN_PROGRESS'
	| 'TASK_STATUS_TESTING'
	| 'TASK_STATUS_REVIEW'
	| 'TASK_STATUS_COMPLETED';

export type SprintStatus =
	| 'SPRINT_STATUS_UNSPECIFIED'
	| 'SPRINT_STATUS_ACTIVE'
	| 'SPRINT_STATUS_CLOSED';

export interface Task {
	taskId: string;
	displayId: number;
	title: string;
	description: string;
	assigneeId: string;
	reporterId: string;
	priority: TaskPriority;
	taskType: TaskType;
	parentId: string;
	storyPoints: number;
	dueDate: string;
	sprintId: string;
	status: TaskStatus;
	completedById: string;
	completedAt: string;
	createdAt: string;
	updatedAt: string;
}

export interface Sprint {
	sprintId: string;
	name: string;
	goal: string;
	startDate: string;
	endDate: string;
	status: SprintStatus;
	createdAt: string;
	updatedAt: string;
}

export interface UserVelocity {
	userId: string;
	storyPoints: number;
}

export const tasks = {
	createTask(params: {
		title: string;
		description?: string;
		assigneeId: string;
		reporterId: string;
		priority?: TaskPriority;
		taskType?: TaskType;
		parentId?: string;
		storyPoints?: number;
		dueDate?: string;
		sprintId?: string;
	}): Promise<{ meta: ResponseMeta; task: Task }> {
		return call('POST', '/tasks', {
			meta: { request_id: reqId(), user_id: get(userId) },
			title: params.title,
			description: params.description ?? '',
			assignee_id: params.assigneeId,
			reporter_id: params.reporterId,
			priority: params.priority ?? 'TASK_PRIORITY_MEDIUM',
			task_type: params.taskType ?? 'TASK_TYPE_TASK',
			parent_id: params.parentId ?? '',
			story_points: params.storyPoints ?? 0,
			due_date: params.dueDate ?? '',
			sprint_id: params.sprintId ?? ''
		});
	},
	getTask(taskId: string): Promise<{ meta: ResponseMeta; task: Task }> {
		return call('GET', `/tasks/${taskId}?meta.request_id=${reqId()}&meta.user_id=${get(userId)}`);
	},
	updateTask(taskId: string, params: {
		title?: string;
		description?: string;
		assigneeId?: string;
		priority?: TaskPriority;
		taskType?: TaskType;
		parentId?: string;
		storyPoints?: number;
		dueDate?: string;
	}): Promise<{ meta: ResponseMeta; task: Task }> {
		return call('PATCH', `/tasks/${taskId}`, {
			meta: { request_id: reqId(), user_id: get(userId) },
			task_id: taskId,
			title: params.title ?? '',
			description: params.description ?? '',
			assignee_id: params.assigneeId ?? '',
			priority: params.priority ?? 'TASK_PRIORITY_UNSPECIFIED',
			task_type: params.taskType ?? 'TASK_TYPE_UNSPECIFIED',
			parent_id: params.parentId ?? '',
			story_points: params.storyPoints ?? 0,
			due_date: params.dueDate ?? ''
		});
	},
	deleteTask(taskId: string): Promise<{ meta: ResponseMeta; taskId: string }> {
		return call('DELETE', `/tasks/${taskId}?meta.request_id=${reqId()}&meta.user_id=${get(userId)}`);
	},
	listBacklog(): Promise<{ meta: ResponseMeta; tasks: Task[] }> {
		return call('GET', `/tasks/backlog?meta.request_id=${reqId()}&meta.user_id=${get(userId)}`);
	},
	listAllTasks(): Promise<{ meta: ResponseMeta; tasks: Task[] }> {
		return call('GET', `/tasks?meta.request_id=${reqId()}&meta.user_id=${get(userId)}`);
	},
	listSprintTasks(sprintId: string): Promise<{ meta: ResponseMeta; tasks: Task[] }> {
		return call('GET', `/sprints/${sprintId}/tasks?meta.request_id=${reqId()}&meta.user_id=${get(userId)}`);
	},
	assignToSprint(taskId: string, sprintId: string): Promise<{ meta: ResponseMeta; task: Task }> {
		return call('POST', `/tasks/${taskId}/sprint`, {
			meta: { request_id: reqId(), user_id: get(userId) },
			task_id: taskId,
			sprint_id: sprintId
		});
	},
	moveStatus(taskId: string, newStatus: TaskStatus, opts?: { userId?: string }): Promise<{ meta: ResponseMeta; task: Task }> {
		return call('POST', `/tasks/${taskId}/status`, {
			meta: { request_id: reqId(), user_id: get(userId) },
			task_id: taskId,
			new_status: newStatus,
			user_id: opts?.userId ?? get(userId) ?? ''
		});
	},
	createSprint(params: { name: string; goal?: string; startDate?: string; endDate?: string }): Promise<{ meta: ResponseMeta; sprint: Sprint }> {
		return call('POST', '/sprints', {
			meta: { request_id: reqId(), user_id: get(userId) },
			name: params.name,
			goal: params.goal ?? '',
			start_date: params.startDate ?? '',
			end_date: params.endDate ?? ''
		});
	},
	updateSprint(sprintId: string, params: { name?: string; goal?: string; startDate?: string; endDate?: string }): Promise<{ meta: ResponseMeta; sprint: Sprint }> {
		return call('PATCH', `/sprints/${sprintId}`, {
			meta: { request_id: reqId(), user_id: get(userId) },
			sprint_id: sprintId,
			name: params.name ?? '',
			goal: params.goal ?? '',
			start_date: params.startDate ?? '',
			end_date: params.endDate ?? ''
		});
	},
	deleteSprint(sprintId: string): Promise<{ meta: ResponseMeta; sprintId: string }> {
		return call('DELETE', `/sprints/${sprintId}?meta.request_id=${reqId()}&meta.user_id=${get(userId)}`);
	},
	closeSprint(sprintId: string): Promise<{ meta: ResponseMeta; sprint: Sprint }> {
		return call('POST', `/sprints/${sprintId}/close`, {
			meta: { request_id: reqId(), user_id: get(userId) },
			sprint_id: sprintId
		});
	},
	listSprints(): Promise<{ meta: ResponseMeta; sprints: Sprint[] }> {
		return call('GET', `/sprints?meta.request_id=${reqId()}&meta.user_id=${get(userId)}`);
	},
	getSprintVelocity(sprintId: string): Promise<{ meta: ResponseMeta; velocities: UserVelocity[] }> {
		return call('GET', `/sprints/${sprintId}/velocity?meta.request_id=${reqId()}&meta.user_id=${get(userId)}`);
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
