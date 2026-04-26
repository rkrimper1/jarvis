# JARVIS REST API Reference

Base URL: `http://localhost:8080`
gRPC direct: `localhost:50051`

All 10 services are served by a single binary via grpc-gateway (in-process, no proxy hop).

---

## Authentication

All endpoints except `/v1/security/authenticate` require a Bearer token.

```bash
# 1. Get a token
TOKEN=$(curl -s -X POST http://localhost:8080/v1/security/authenticate \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "auth-001"},
    "subject_id": "tony-stark",
    "method": "AUTH_METHOD_TOKEN",
    "credential_payload": "'"$(echo -n 'tony-stark' | base64)"'"
  }' | jq -r '.accessToken')

# 2. Use the token
curl http://localhost:8080/v1/business/schedule/tony-stark \
  -H "Authorization: Bearer $TOKEN"
```

---

## Observability

JARVIS uses OpenCensus for distributed tracing, exporting spans to Stackdriver when enabled.

**Enabling tracing:**
Set `TRACING_ENABLED=true` in the environment. When disabled (the default), all spans use `NeverSample()` and no exporter is registered. When enabled, the Stackdriver exporter is initialised at startup and flushed on shutdown; `GCP_PROJECT` and `GOOGLE_APPLICATION_CREDENTIALS` must be set.

**Span naming:**
- gRPC RPCs: `<package>.<Service>/<Method>` (the full gRPC method path, e.g. `jarvis.nlp.NLPService/ProcessDialogueTurn`)
- Internal calls: `jarvis/<function>`

**Span attributes added by the tracing interceptors:**

| Attribute | Source |
|---|---|
| `original_request_id` | UUID generated per RPC by `UnaryTracing` / `StreamTracing` |
| `x_request_id` | `x-request-id` header forwarded by the client in gRPC metadata |
| `request_id` | From `RequestMeta.request_id` — added by handlers via `middleware.AddRequestAttributes` |
| `user_id` | From `RequestMeta.user_id` — added by handlers via `middleware.AddRequestAttributes` |

The `original_request_id` is also available to all downstream code via `middleware.OriginalRequestIDFromCtx(ctx)`.

---

## Command Service

### Request Memory Profile

Triggers an immediate heap profile snapshot. Writes a `.prof` file and (if graphviz is available) a `.gif` call-graph image to `PPROF_DIR` (default `/tmp/profiles`). In Docker, both files are automatically available on the host at `./profiles/` via the volume mount.

```bash
curl -X POST http://localhost:8080/v1/command/memory-profile \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"meta": {"request_id": "cmd-001"}}'
```

**Response:**
```json
{
  "meta": {"requestId": "cmd-001", "success": true},
  "profPath": "/tmp/profiles/heap-20260322-153045.prof",
  "gifPath":  "/tmp/profiles/heap-20260322-153045.gif"
}
```

> `gifPath` is empty if graphviz is not available. The `.prof` file is always written and can be inspected manually:
> ```bash
> go tool pprof /tmp/profiles/heap-20260322-153045.prof
> ```

> A background snapshot is also taken automatically every `PPROF_INTERVAL` (default `5m`).

**gRPC:**
```bash
grpcurl -plaintext -d '{"meta": {"request_id": "cmd-001"}}' \
  localhost:50051 jarvis.command.CommandService/RequestMemoryProfile
```

---

## NLP Service

### Parse Intent
```bash
curl -X POST http://localhost:8080/v1/nlp/parse \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "nlp-001"},
    "raw_text": "JARVIS, run diagnostics on the Mark VII suit",
    "language_code": "en-US",
    "session_id": "session-tony-001"
  }'
```

### Dialogue Turn

`ProcessDialogueTurn` is powered by **Claude (Anthropic)**. The intent classifier routes each utterance to a persona-specific system prompt; conversation history is stored in Redis per session.

| Intent routed to Claude | Persona |
|---|---|
| `INTENT_ANALYSIS_REQUEST` | Terse, precise, Jarvis-like |
| `INTENT_QUERY` | Factual, conceitedly witty, wiseass but respectful |
| `INTENT_SMALL_TALK` | Warm, witty, professional |
| `INTENT_EMERGENCY` / `INTENT_COMMAND` | Deterministic — no Claude call |

**Small talk:**
```bash
curl -X POST http://localhost:8080/v1/nlp/dialogue \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "dlg-001"},
    "session_id": "session-tony-001",
    "utterance": "Good morning JARVIS"
  }'
```

**Query (witty/factual):**
```bash
curl -X POST http://localhost:8080/v1/nlp/dialogue \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "dlg-002"},
    "session_id": "session-tony-001",
    "utterance": "What is the current threat level in Monaco?"
  }'
```

**Analysis request:**
```bash
curl -X POST http://localhost:8080/v1/nlp/dialogue \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "dlg-003"},
    "session_id": "session-tony-001",
    "utterance": "Run a full diagnostic on the Mark VII repulsor array"
  }'
```

Session history persists across turns for the duration of `DIALOGUE_SESSION_TTL` (default 30 min). Use a consistent `session_id` across requests to maintain context.

---

## Security Service

### Authenticate

`credential_payload` is the user's password, base64-encoded. The server checks it against the bcrypt hash in the users DB (`AUTH_METHOD_PASSCODE`). On success the response includes a signed JWT in `accessToken`.

```bash
curl -X POST http://localhost:8080/v1/security/authenticate \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "sec-auth-001"},
    "subject_id": "tony-stark",
    "method": "AUTH_METHOD_PASSCODE",
    "credential_payload": "'"$(echo -n 'tony-stark' | base64)"'"
  }'
```

```bash
# grpcurl
grpcurl -plaintext \
  -d '{
    "meta": {"request_id": "sec-auth-001"},
    "subject_id": "tony-stark",
    "method": "AUTH_METHOD_PASSCODE",
    "credential_payload": "'"$(echo -n 'tony-stark' | base64)"'"
  }' \
  localhost:50051 jarvis.security.SecurityService/Authenticate
```

### Assess Threat
```bash
curl -X POST http://localhost:8080/v1/security/threat \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "threat-001"},
    "subject_id": "ivan-vanko",
    "location": "monaco-circuit",
    "observed_signals": ["energy_signature", "weapons_detected", "criminal_record"]
  }'
```

### Execute Protocol
```bash
curl -X POST http://localhost:8080/v1/security/protocol \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "proto-001"},
    "protocol": "PROTOCOL_TYPE_LOCKDOWN",
    "reason": "intruder detected",
    "requires_confirmation": false
  }'
```

### Audit Log

Returns recent audit entries and a computed surroundings status derived from THREAT and FACES analytics events in the last 30 minutes (70% THREAT weight, 30% FACES weight).

```bash
curl "http://localhost:8080/v1/security/audit?meta.request_id=audit-001&page_size=20" \
  -H "Authorization: Bearer $TOKEN"
```

**Response:**
```json
{
  "meta": {"requestId": "audit-001", "success": true},
  "entries": [
    {"eventId": "evt-000003", "subjectId": "face-001", "action": "face_analysis:faces=2", "resource": "security/faces", "success": true, "timestamp": "2026-03-27T14:05:00Z"},
    {"eventId": "evt-000002", "subjectId": "threat-001", "action": "threat_assessed:MODERATE", "resource": "security/threat", "success": true, "timestamp": "2026-03-27T14:02:00Z"}
  ],
  "surroundingsStatus": {
    "score": 35.0,
    "color": "YELLOW",
    "status": "NOMINAL"
  }
}
```

> `surroundingsStatus.color` is `GREEN` (0–20), `YELLOW` (21–70), or `RED` (71–100).
> `surroundingsStatus.status` is `NOMINAL` (score < 40) or `COMPROMISED` (score ≥ 40).
> Scores are computed from up to 30 minutes of recent events, falling back to the last 20 events if the window is empty.

**gRPC:**
```bash
grpcurl -plaintext -d '{"meta": {"request_id": "audit-001"}, "page_size": 20}' \
  localhost:50051 jarvis.security.SecurityService/GetAuditLog
```

### Analyze Faces

Detects faces in an uploaded image using the pigo cascade detector, annotates each with a HUD overlay and Claude-generated sentiment commentary, and returns the annotated image URL.

```bash
curl -X POST http://localhost:8080/v1/security/faces \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "face-001"},
    "image_data": "'"$(base64 -w0 /path/to/photo.jpg)"'",
    "filename": "photo.jpg"
  }'
```

**Response:**
```json
{
  "meta": {"requestId": "face-001", "success": true},
  "imageUrl": "/faces/annotated_photo.png",
  "faceCount": 2,
  "faces": [
    {"faceIndex": 1, "sentiment": "HAPPY",   "commentary": "Cheeks at maximum capacity, sir.",    "boundingBox": {"x": 120, "y": 80,  "width": 210, "height": 210}},
    {"faceIndex": 2, "sentiment": "NEUTRAL", "commentary": "Contemplating life's mysteries.",     "boundingBox": {"x": 380, "y": 95,  "width": 195, "height": 195}}
  ]
}
```

> Detection is tunable without rebuilding via env vars: `FACE_MIN_SIZE` (px, default `65`), `FACE_QUALITY_THRESHOLD` (default `6.0`), `FACE_CLUSTER_OVERLAP` (default `0.25`).
> The annotated image is served at the returned `imageUrl` path from the same HTTP server (`:8080`).

**gRPC:**
```bash
grpcurl -plaintext -d "{
  \"meta\": {\"request_id\": \"face-001\"},
  \"image_data\": \"$(base64 -w0 /path/to/photo.jpg)\",
  \"filename\": \"photo.jpg\"
}" localhost:50051 jarvis.security.SecurityService/AnalyzeFaces
```

### Analyze Threat Scene

Sends a camera frame to **Claude Vision** and returns a structured threat assessment. Requires `THREAT_VISION_ENABLED=true` and `ANTHROPIC_API_KEY`. When disabled, returns `THREAT_LEVEL_UNSPECIFIED` (not an error). The response includes `log_mode` so the client knows the server-side logging policy without a separate call.

`detected_objects` is an optional list of client-side COCO-SSD labels (e.g. `["person(0.91)", "knife(0.78)"]`) that are prepended to the Claude prompt as context.

```bash
curl -X POST http://localhost:8080/v1/security/threat-scene \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "scene-001"},
    "image_data": "'"$(base64 -w0 /path/to/frame.jpg)"'",
    "detected_objects": ["person(0.91)", "knife(0.78)"]
  }'
```

**Response:**
```json
{
  "meta": {"requestId": "scene-001", "success": true},
  "level": "THREAT_LEVEL_HIGH",
  "confidence": 0.91,
  "threatSummary": "Armed individual detected at entrance, sir.",
  "recommendedActions": ["Lock down the area.", "Alert security personnel."],
  "logMode": "manual"
}
```

**gRPC:**
```bash
grpcurl -plaintext -d "{
  \"meta\": {\"request_id\": \"scene-001\"},
  \"image_data\": \"$(base64 -w0 /path/to/frame.jpg)\",
  \"detected_objects\": [\"person(0.91)\", \"knife(0.78)\"]
}" localhost:50051 jarvis.security.SecurityService/AnalyzeThreatScene
```

### Log Threat Event

Persists a threat event to the `threat_events` table in the shared SQLite DB. Whether the event is actually stored depends on `THREAT_LOG_MODE` and the `force` field:

| `THREAT_LOG_MODE` | `force` | Stored? |
|---|---|---|
| `manual` | `false` | No |
| `manual` | `true` | Yes |
| `auto` | any | Yes |
| `all` | any | Yes |

When `THREAT_LOG_IMAGES=true` and `THREAT_EVENT_DIR` is configured, the raw `image_data` (JPEG) is saved to disk and the URL is included in the returned event. The event is also appended to the audit log as `threat-event:<LEVEL>`.

```bash
curl -X POST http://localhost:8080/v1/security/threat-events \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "log-001"},
    "camera_label": "Front Door",
    "detected_objects": ["person(0.91)", "knife(0.78)"],
    "level": "THREAT_LEVEL_HIGH",
    "confidence": 0.91,
    "threat_summary": "Armed individual detected at entrance.",
    "recommended_actions": ["Lock down the area."],
    "force": true
  }'
```

**Response:**
```json
{
  "meta": {"requestId": "log-001", "success": true},
  "event": {
    "eventId": "3f4a1b2c-...",
    "timestamp": "2026-04-26T14:05:00Z",
    "cameraLabel": "Front Door",
    "detectedObjects": ["person(0.91)", "knife(0.78)"],
    "level": "THREAT_LEVEL_HIGH",
    "confidence": 0.91,
    "threatSummary": "Armed individual detected at entrance.",
    "recommendedActions": ["Lock down the area."],
    "imageUrl": ""
  },
  "logged": true
}
```

**gRPC:**
```bash
grpcurl -plaintext -d '{
  "meta": {"request_id": "log-001"},
  "camera_label": "Front Door",
  "level": "THREAT_LEVEL_HIGH",
  "confidence": 0.91,
  "threat_summary": "Armed individual detected.",
  "force": true
}' localhost:50051 jarvis.security.SecurityService/LogThreatEvent
```

### List Threat Events

Returns stored threat events newest-first. `page_size` is clamped: `0` or `>100` both default to `20`. Returns an empty list (no error) when `JARVIS_DB_PATH` is unset.

```bash
curl "http://localhost:8080/v1/security/threat-events?meta.request_id=list-001&pageSize=20" \
  -H "Authorization: Bearer $TOKEN"
```

**Response:**
```json
{
  "meta": {"requestId": "list-001", "success": true},
  "events": [
    {
      "eventId": "3f4a1b2c-...",
      "timestamp": "2026-04-26T14:05:00Z",
      "cameraLabel": "Front Door",
      "level": "THREAT_LEVEL_HIGH",
      "confidence": 0.91,
      "threatSummary": "Armed individual detected at entrance.",
      "imageUrl": ""
    }
  ]
}
```

**gRPC:**
```bash
grpcurl -plaintext -d '{"meta": {"request_id": "list-001"}, "page_size": 20}' \
  localhost:50051 jarvis.security.SecurityService/ListThreatEvents
```

---

## Facility Service

### Control System
```bash
curl -X POST http://localhost:8080/v1/facility/zones/workshop/system \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "fac-001"},
    "zone_id": "workshop",
    "system": "SYSTEM_TYPE_LIGHTING",
    "command": "SET",
    "settings": {"brightness": "80", "color_temp": "warm"}
  }'
```

### Manage Access
```bash
curl -X POST http://localhost:8080/v1/facility/zones/server-room/access \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "access-001"},
    "zone_id": "server-room",
    "subject_id": "happy-hogan",
    "action": "GRANT"
  }'
```

### Get Environment Reading
```bash
curl http://localhost:8080/v1/facility/zones/lab-01/environment \
  -H "Authorization: Bearer $TOKEN"
```

### List Alexa Devices

Returns all Echo and smart home appliances visible to the authenticated Amazon account. Smart home devices include their current power state (`ON`/`OFF`), capability set, and `applianceId` required for `SendAlexaCommand`. Requires `ALEXA_COOKIES_PATH` to be configured.

```bash
curl http://localhost:8080/v1/facility/alexa/devices \
  -H "Authorization: Bearer $TOKEN"
```

**Response:**
```json
{
  "meta": {"requestId": "fac-alexa-001", "success": true},
  "devices": [
    {"name": "Office Lights", "isSmartHome": true, "applianceId": "amzn1.alexa.endpoint.<uuid>", "capabilities": ["LIGHT"], "powerState": "ON", "online": true},
    {"name": "Living Room Echo", "deviceFamily": "ECHO", "online": true, "isSmartHome": false}
  ]
}
```

**gRPC:**
```bash
grpcurl -plaintext -d '{"meta": {"request_id": "fac-alexa-001"}}' \
  localhost:50051 jarvis.facility.FacilityService/ListAlexaDevices
```

### Send Alexa Command

Sends a device control command to a smart home appliance via the Alexa GraphQL API. `action` must be one of: `turnOn`, `turnOff`, `lock`, `unlock`, `setTargetTemperature`, `setBrightness`. Use the `applianceId` returned by `ListAlexaDevices`.

```bash
curl -X POST http://localhost:8080/v1/facility/alexa/command \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "fac-cmd-001"},
    "appliance_id": "amzn1.alexa.endpoint.<uuid>",
    "action": "turnOn"
  }'
```

**With parameters (e.g. brightness):**
```bash
curl -X POST http://localhost:8080/v1/facility/alexa/command \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "fac-cmd-002"},
    "appliance_id": "amzn1.alexa.endpoint.<uuid>",
    "action": "setTargetTemperature",
    "parameters": {"targetTemperature": "72"}
  }'
```

**gRPC:**
```bash
grpcurl -plaintext -d '{
  "meta": {"request_id": "fac-cmd-001"},
  "appliance_id": "amzn1.alexa.endpoint.<uuid>",
  "action": "turnOff"
}' localhost:50051 jarvis.facility.FacilityService/SendAlexaCommand
```

---

## Intelligence Service

### Query Intel
```bash
curl -X POST http://localhost:8080/v1/intel/query \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "intel-001"},
    "query": "ivan-vanko",
    "subject_type": "SUBJECT_TYPE_PERSON",
    "depth": "ANALYSIS_DEPTH_DEEP",
    "data_sources": ["SHIELD", "STARK_DB"]
  }'
```

### Analyze Artifact
```bash
curl -X POST http://localhost:8080/v1/intel/artifact \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "artifact-001"},
    "artifact_id": "unknown-device-x7",
    "artifact_description": "unknown origin weapon device recovered from Monaco"
  }'
```

### Cross Reference
```bash
curl -X POST http://localhost:8080/v1/intel/crossref \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "crossref-001"},
    "subject_ids": ["ivan-vanko", "hammer-industries"],
    "relationship_hint": "allied"
  }'
```

---

### Intel Hunt

> Requires `ANTHROPIC_API_KEY` and `JARVIS_DB_PATH`. RPCs return `FailedPrecondition` when either is absent.

#### Ingest Signal (manual)
```bash
curl -X POST http://localhost:8080/v1/intel/signals \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "ingest-001"},
    "source_type": "SOURCE_TYPE_MANUAL",
    "raw_content": "Acme Corp cut enterprise SaaS pricing by 15%, effective immediately. Renewal pipeline at risk."
  }'
```

**Response**
```json
{
  "meta": {"request_id": "ingest-001", "success": true},
  "signal": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "source_type": "SOURCE_TYPE_MANUAL",
    "raw_content": "Acme Corp cut enterprise SaaS pricing...",
    "ingested_at": "2026-04-14T10:00:00Z"
  },
  "card": {
    "id": "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
    "title": "Acme drops enterprise price",
    "summary": "Acme Corp cut enterprise SaaS pricing by 15%. Directly undercuts Stark Industries mid-market positioning.",
    "opportunity_type": "OPPORTUNITY_TYPE_TACTICAL",
    "confidence_score": 0.88,
    "suggested_action": "Brief the sales team on competitive pricing response before end of day.",
    "status": "INTEL_CARD_STATUS_PENDING_REVIEW",
    "raw_signal_ids": ["550e8400-e29b-41d4-a716-446655440000"],
    "created_at": "2026-04-14T10:00:01Z",
    "updated_at": "2026-04-14T10:00:01Z"
  }
}
```

#### Ingest Signal via File Upload
Accepts `.txt`, `.csv`, `.tsv`, and `.pdf` (text extracted with Apache pdfcpu). The `source_uri` field is optional and stored for provenance.

```bash
# Plain text
curl -X POST http://localhost:8080/v1/intel/ingest/file \
  -F "file=@/path/to/competitive-brief.txt" \
  -F "source_uri=https://example.com/brief.txt"

# PDF
curl -X POST http://localhost:8080/v1/intel/ingest/file \
  -F "file=@/path/to/market-report.pdf"

# CSV (first row treated as headers)
curl -X POST http://localhost:8080/v1/intel/ingest/file \
  -F "file=@/path/to/signals.csv"
```

Returns the same `{signal, card}` JSON as the gRPC `IngestSignal` response.

#### List Intel Cards
`ListIntelCards` uses a `GET` binding — all fields are passed as query parameters.

```bash
# All cards (paginated, newest first)
curl "http://localhost:8080/v1/intel/cards?meta.request_id=list-001&page_size=20"

# Filter by status: INTEL_CARD_STATUS_PENDING_REVIEW | INTEL_CARD_STATUS_CONFIRMED | INTEL_CARD_STATUS_DISMISSED
curl "http://localhost:8080/v1/intel/cards?meta.request_id=list-002&status_filter=INTEL_CARD_STATUS_PENDING_REVIEW&page_size=10"

# Next page — pass next_page_token value from prior response as page_token
curl "http://localhost:8080/v1/intel/cards?meta.request_id=list-003&page_size=10&page_token=10"
```

**Response**
```json
{
  "meta": {"requestId": "list-001", "success": true},
  "cards": [ /* array of IntelCard */ ],
  "totalCount": 42,
  "nextPageToken": "20"
}
```

**gRPC:**
```bash
grpcurl -plaintext -d '{"meta": {"request_id": "list-001"}, "page_size": 20}' \
  localhost:50051 jarvis.intelligence.IntelligenceService/ListIntelCards
```

#### Search Intel Cards
Full-text keyword search across `title`, `summary`, and `suggested_action` of all stored Intel Cards. Results are ordered by `confidence_score DESC`, then newest first. This is a direct HTTP handler — not a grpc-gateway route.

```bash
# Search for cards mentioning "pricing"
curl "http://localhost:8080/v1/intel/cards/search?q=pricing&page_size=20"

# Return up to 50 results for a broader query
curl "http://localhost:8080/v1/intel/cards/search?q=supply+chain&page_size=50"
```

**Query parameters:**

| Parameter | Type | Default | Description |
|---|---|---|---|
| `q` | string | `""` | Search term (case-insensitive LIKE match) |
| `page_size` | int | `20` | Maximum results to return (max 100) |

**Response**
```json
{
  "cards": [ /* array of IntelCard, ordered by confidence desc */ ],
  "total": 7
}
```

#### Confirm or Dismiss a Card
```bash
# Confirm — operator will act on this signal
curl -X POST http://localhost:8080/v1/intel/cards/6ba7b810-9dad-11d1-80b4-00c04fd430c8/confirm \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "confirm-001"},
    "card_id": "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
    "new_status": "INTEL_CARD_STATUS_CONFIRMED"
  }'

# Dismiss — low value / false positive
curl -X POST http://localhost:8080/v1/intel/cards/6ba7b810-9dad-11d1-80b4-00c04fd430c8/confirm \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "confirm-002"},
    "card_id": "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
    "new_status": "INTEL_CARD_STATUS_DISMISSED"
  }'
```

**Opportunity types**

| Value | Meaning |
|---|---|
| `OPPORTUNITY_TYPE_TACTICAL` | Near-term action required within 24 h (price change, key hire, customer loss) |
| `OPPORTUNITY_TYPE_STRATEGIC` | Long-term positioning signal (market shift, platform change, new entrant) |
| `OPPORTUNITY_TYPE_RESOURCE` | Supply chain, funding, capacity, or talent availability |
| `OPPORTUNITY_TYPE_THREAT_MITIGATION` | Regulatory, legal, reputational, or supply disruption risk |

---

## Business Ops Service

### Schedule Event
```bash
curl -X POST http://localhost:8080/v1/business/schedule \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "sched-001"},
    "title": "Stark Expo Press Conference",
    "description": "Q4 press briefing and live demo of the new arc reactor.",
    "attendees": ["pepper@starkindustries.com", "happy@starkindustries.com"],
    "location": "stark-expo-pavilion",
    "start": "2026-04-01T14:00:00Z",
    "end":   "2026-04-01T15:00:00Z",
    "high_priority": true
  }'
```

**Calendar invite:** If `SMTP_*` env vars are configured, JARVIS will automatically
email an iCalendar (`.ics`) invite to every address in `attendees`, plus the organizer
(`SMTP_TO`). Each recipient receives the invite as an email attachment — open it to
add the event to their calendar.

> **Setup** — requires a Gmail App Password:
> 1. Enable 2-Step Verification on your Google Account
> 2. Go to **myaccount.google.com/apppasswords** → create an app password named `jarvis`
> 3. Set env vars (store outside the repo, e.g. `$HOME/credentials/jarvis/.env`):
>    ```
>    SMTP_HOST=smtp.gmail.com
>    SMTP_PORT=587
>    SMTP_USER=you@gmail.com
>    SMTP_PASS=<16-char app password>
>    SMTP_TO=you@gmail.com
>    ```
> 4. Run with `make run` — env file is loaded automatically from `ENV_PATH`.

### Get Schedule
```bash
curl http://localhost:8080/v1/business/schedule/tony-stark \
  -H "Authorization: Bearer $TOKEN"
```

### Create Task
```bash
curl -X POST http://localhost:8080/v1/business/tasks \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "task-001"},
    "title": "Review arc reactor upgrade specs",
    "assignee_id": "tony-stark",
    "priority": 5
  }'
```

### Send Message
```bash
curl -X POST http://localhost:8080/v1/business/messages \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "msg-001"},
    "recipients": ["pepper-potts"],
    "channel": "MESSAGE_CHANNEL_SECURE",
    "subject": "Urgent: Board meeting rescheduled",
    "body": "Please move the Q4 review to 1400 tomorrow.",
    "encrypt": true
  }'
```

### Generate Report
```bash
curl -X POST http://localhost:8080/v1/business/reports \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "report-001"},
    "report_type": "THREAT_SUMMARY"
  }'
```

---

## Learning Service

### Submit Feedback
```bash
curl -X POST http://localhost:8080/v1/learning/feedback \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "fb-001"},
    "interaction_id": "dlg-001",
    "feedback_type": "FEEDBACK_TYPE_CORRECTION",
    "correction": "The threat level was HIGH not MODERATE",
    "rating": 0.3
  }'
```

### Get Behavior Profile
```bash
curl http://localhost:8080/v1/learning/profile/tony-stark \
  -H "Authorization: Bearer $TOKEN"
```

### Get Model Performance
```bash
curl "http://localhost:8080/v1/learning/performance?domain=MODEL_DOMAIN_NLP" \
  -H "Authorization: Bearer $TOKEN"
```

### Search Knowledge

Searches the local SQLite knowledge base (FTS5 + fuzzy). Returns `needs_confirmation: true` when no fresh result exists and an external search is available. Re-send with `confirmed: true` and your chosen `preferred_source` to execute the search, save the result, and return it.

**First call (DB lookup):**
```bash
curl -X POST http://localhost:8080/v1/learning/knowledge/search \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "know-001"},
    "query": "arc reactor palladium toxicity",
    "preferred_source": "KNOWLEDGE_SOURCE_CLAUDE_API"
  }'
```

**Confirmed external search:**
```bash
curl -X POST http://localhost:8080/v1/learning/knowledge/search \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "know-002"},
    "query": "arc reactor palladium toxicity",
    "preferred_source": "KNOWLEDGE_SOURCE_CLAUDE_API",
    "confirmed": true
  }'
```

**Response:**
```json
{
  "meta": {"requestId": "know-002", "success": true},
  "results": [{
    "id": "1",
    "query": "arc reactor palladium toxicity",
    "summary": "Palladium poisoning from arc reactor use causes...",
    "source": "KNOWLEDGE_SOURCE_CLAUDE_API",
    "confidence": 0.85,
    "updatedAt": "2026-03-25T00:00:00Z"
  }],
  "searchesRemaining": 9
}
```

> `preferred_source` accepts `KNOWLEDGE_SOURCE_CLAUDE_API` or `KNOWLEDGE_SOURCE_WEB_SEARCH`.
> `KNOWLEDGE_WEB_SEARCH_MAX_USES` (default `10`) controls how many external searches are allowed per session.
> Entries older than `KNOWLEDGE_STALE_DAYS` (default `30`) are excluded from DB results.

**gRPC:**
```bash
grpcurl -plaintext -d '{
  "meta": {"request_id": "know-001"},
  "query": "arc reactor palladium toxicity",
  "preferred_source": "KNOWLEDGE_SOURCE_CLAUDE_API",
  "confirmed": true
}' localhost:50051 jarvis.learning.LearningService/SearchKnowledge
```

---

## User Service

Users are stored in the shared `jarvis.db` SQLite database (`JARVIS_DB_PATH`). Passwords are bcrypt-hashed. Two users are seeded on first start: `tony-stark` (ROLE_VIEWER) and `rob-krimper` (ROLE_ADMIN). The role is encoded in the JWT `granted_scopes` on every `Authenticate` call.

### Get Current User
```bash
curl "http://localhost:8080/v1/users/me?username=tony-stark" \
  -H "Authorization: Bearer $TOKEN"
```

### Update Profile
```bash
curl -X POST http://localhost:8080/v1/users/me/profile \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "id": "<user-uuid>",
    "email": "tony@stark.industries",
    "display_name": "Tony Stark"
  }'
```

### Change Password
```bash
curl -X POST http://localhost:8080/v1/users/me/password \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "id": "<user-uuid>",
    "current_password": "tony-stark",
    "new_password": "new-secret"
  }'
```

### List Users (admin only)
```bash
curl http://localhost:8080/v1/users \
  -H "Authorization: Bearer $TOKEN"
```

### Create User (admin only)
```bash
curl -X POST http://localhost:8080/v1/users \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "username": "pepper-potts",
    "email": "pepper@stark.industries",
    "display_name": "Pepper Potts",
    "password": "secure-pass",
    "role": "ROLE_EDITOR"
  }'
```

### Look Up User (admin only)
```bash
curl -X POST http://localhost:8080/v1/users/lookup \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"username": "pepper-potts"}'
```

### Update User (admin only)
```bash
curl -X PATCH http://localhost:8080/v1/users/<user-uuid> \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "id": "<user-uuid>",
    "role": "ROLE_EDITOR",
    "is_active": true
  }'
```

### Delete User (admin only)
```bash
curl -X DELETE http://localhost:8080/v1/users/<user-uuid> \
  -H "Authorization: Bearer $TOKEN"
```

### Grant Entitlement (admin only)
```bash
curl -X POST http://localhost:8080/v1/users/<user-uuid>/entitlements \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "<user-uuid>",
    "entitlement": {"application": "jarvis-hud", "access_level": "ACCESS_LEVEL_WRITE"}
  }'
```

### Revoke Entitlement (admin only)
```bash
curl -X DELETE "http://localhost:8080/v1/users/<user-uuid>/entitlements/jarvis-hud" \
  -H "Authorization: Bearer $TOKEN"
```

### List Entitlements
```bash
curl "http://localhost:8080/v1/users/<user-uuid>/entitlements" \
  -H "Authorization: Bearer $TOKEN"
```

**gRPC:**
```bash
grpcurl -plaintext -d '{"username": "tony-stark"}' \
  localhost:50051 jarvis.user.UserService/GetMe

grpcurl -plaintext -d '{}' \
  localhost:50051 jarvis.user.UserService/ListUsers

grpcurl -plaintext -d '{"username": "pepper-potts"}' \
  localhost:50051 jarvis.user.UserService/GetUser

grpcurl -plaintext -d '{"id": "<user-uuid>", "role": "ROLE_EDITOR"}' \
  localhost:50051 jarvis.user.UserService/UpdateUser

grpcurl -plaintext -d '{"user_id": "<user-uuid>"}' \
  localhost:50051 jarvis.user.UserService/ListEntitlements
```

---

---

## Task Service

Tasks support full Scrum hierarchy: `TASK_TYPE_EPIC` → `TASK_TYPE_STORY` → `TASK_TYPE_TASK` / `TASK_TYPE_SUBTASK` / `TASK_TYPE_BUG` via `parent_id`. Display IDs (`JARVIS-0001`) are auto-assigned.

### Create Task
```bash
curl -X POST http://localhost:8080/v1/tasks \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "task-001"},
    "title": "Upgrade Mark VII repulsor array",
    "description": "Replace palladium core with vibranium.",
    "assignee_id": "<user-uuid>",
    "reporter_id": "<user-uuid>",
    "priority": "TASK_PRIORITY_HIGH",
    "task_type": "TASK_TYPE_TASK",
    "story_points": 5
  }'
```

**gRPC:**
```bash
grpcurl -plaintext -d '{
  "meta": {"request_id": "task-001"},
  "title": "Upgrade Mark VII repulsor array",
  "priority": "TASK_PRIORITY_HIGH",
  "task_type": "TASK_TYPE_TASK",
  "story_points": 5
}' localhost:50051 jarvis.task.TaskService/CreateTask
```

### List Backlog
```bash
curl http://localhost:8080/v1/tasks/backlog \
  -H "Authorization: Bearer $TOKEN"
```

### List All Tasks
```bash
curl http://localhost:8080/v1/tasks \
  -H "Authorization: Bearer $TOKEN"
```

### Get Task
```bash
curl http://localhost:8080/v1/tasks/<task-uuid> \
  -H "Authorization: Bearer $TOKEN"
```

### Update Task
```bash
curl -X PATCH http://localhost:8080/v1/tasks/<task-uuid> \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "task_id": "<task-uuid>",
    "title": "Upgrade Mark VII repulsor array — Phase 2",
    "priority": "TASK_PRIORITY_CRITICAL",
    "story_points": 8
  }'
```

### Move Task Status
```bash
curl -X POST http://localhost:8080/v1/tasks/<task-uuid>/status \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "task_id": "<task-uuid>",
    "new_status": "TASK_STATUS_IN_PROGRESS",
    "user_id": "<user-uuid>"
  }'
```

> Valid status values: `TASK_STATUS_UNASSIGNED`, `TASK_STATUS_ASSIGNED`, `TASK_STATUS_IN_PROGRESS`, `TASK_STATUS_TESTING`, `TASK_STATUS_REVIEW`, `TASK_STATUS_COMPLETED`.

### Delete Task
```bash
curl -X DELETE http://localhost:8080/v1/tasks/<task-uuid> \
  -H "Authorization: Bearer $TOKEN"
```

### Create Sprint
```bash
curl -X POST http://localhost:8080/v1/sprints \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "sprint-001"},
    "name": "Sprint 1 — Arc Reactor",
    "goal": "Complete core repulsor upgrade",
    "start_date": "2026-04-14",
    "end_date": "2026-04-28"
  }'
```

**gRPC:**
```bash
grpcurl -plaintext -d '{
  "meta": {"request_id": "sprint-001"},
  "name": "Sprint 1 — Arc Reactor",
  "goal": "Complete core repulsor upgrade",
  "start_date": "2026-04-14",
  "end_date": "2026-04-28"
}' localhost:50051 jarvis.task.TaskService/CreateSprint
```

### List Sprints
```bash
curl http://localhost:8080/v1/sprints \
  -H "Authorization: Bearer $TOKEN"
```

### Assign Task to Sprint
```bash
curl -X POST http://localhost:8080/v1/tasks/<task-uuid>/sprint \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"task_id": "<task-uuid>", "sprint_id": "<sprint-uuid>"}'
```

### List Sprint Tasks
```bash
curl http://localhost:8080/v1/sprints/<sprint-uuid>/tasks \
  -H "Authorization: Bearer $TOKEN"
```

### Get Sprint Velocity
```bash
curl http://localhost:8080/v1/sprints/<sprint-uuid>/velocity \
  -H "Authorization: Bearer $TOKEN"
```

**Response:**
```json
{
  "meta": {"requestId": "sprint-vel-001", "success": true},
  "velocities": [
    {"userId": "<user-uuid>", "storyPoints": 13},
    {"userId": "<user-uuid>", "storyPoints": 8}
  ]
}
```

### Close Sprint
```bash
curl -X POST http://localhost:8080/v1/sprints/<sprint-uuid>/close \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"sprint_id": "<sprint-uuid>"}'
```

> Closing a sprint marks its status `SPRINT_STATUS_CLOSED`. Any tasks not yet `COMPLETED` remain in the sprint record for historical reference.

### Update Sprint
```bash
curl -X PATCH http://localhost:8080/v1/sprints/<sprint-uuid> \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "sprint_id": "<sprint-uuid>",
    "name": "Sprint 1 — Arc Reactor (revised)",
    "end_date": "2026-05-05"
  }'
```

### Delete Sprint
```bash
curl -X DELETE http://localhost:8080/v1/sprints/<sprint-uuid> \
  -H "Authorization: Bearer $TOKEN"
```

## gRPC Direct Access

```bash
# List all services
grpcurl -plaintext localhost:50051 list

# Request a heap memory profile
grpcurl -plaintext -d '{"meta": {"request_id": "grpc-cmd-001"}}' \
  localhost:50051 jarvis.command.CommandService/RequestMemoryProfile

# Parse intent
grpcurl -plaintext -d '{
  "meta": {"request_id": "grpc-001"},
  "raw_text": "Power up the Mark VII",
  "session_id": "session-001"
}' localhost:50051 jarvis.nlp.NLPService/ParseIntent

# Dialogue turn (routed to Claude based on intent)
grpcurl -plaintext -d '{
  "meta": {"request_id": "grpc-dlg-001"},
  "session_id": "session-tony-001",
  "utterance": "What do you know about palladium toxicity?"
}' localhost:50051 jarvis.nlp.NLPService/ProcessDialogueTurn

# Schedule an event (triggers calendar invite email if SMTP is configured)
grpcurl -plaintext -d '{
  "meta": {"request_id": "grpc-sched-001"},
  "title": "Stark Expo Press Conference",
  "description": "Q4 press briefing and live demo.",
  "location": "stark-expo-pavilion",
  "attendees": ["pepper@starkindustries.com", "happy@starkindustries.com"],
  "start": "2026-04-01T14:00:00Z",
  "end":   "2026-04-01T15:00:00Z",
  "high_priority": true
}' localhost:50051 jarvis.business.BusinessOpsService/ScheduleEvent

# Search knowledge base (confirmed external search)
grpcurl -plaintext -d '{
  "meta": {"request_id": "grpc-know-001"},
  "query": "vibranium tensile strength",
  "preferred_source": "KNOWLEDGE_SOURCE_CLAUDE_API",
  "confirmed": true
}' localhost:50051 jarvis.learning.LearningService/SearchKnowledge

# Health check
grpcurl -plaintext localhost:50051 grpc.health.v1.Health/Check
```

---

## Architecture Diagram

```
                  ┌─────────────────────────────────────────┐
                  │          Client Applications             │
                  │  (HUD / Mobile App / Voice Interface)    │
                  └──────────────┬──────────────────────────┘
                                 │
                    REST :8080   │   gRPC :50051
                  ┌──────────────▼──────────────────────────┐
                  │            J.A.R.V.I.S.                  │
                  │    Single Go Binary · debian:slim         │
                  │                                           │
                  │  grpc-gateway (in-process REST → gRPC)   │
                  │                                           │
                  │  command      business-ops  facility      │
                  │  intelligence learning      security      │
                  │  task         user                        │
                  │  nlp ◄────────► voice (in-process)    │
                  │                                           │
                  │  nlp → Claude API (dialogue, streaming)  │
                  │  learning → Claude API (knowledge search) │
                  │  security → Claude API (face sentiment)   │
                  │  security → pigo (face detection)         │
                  │  Redis (dialogue history + sessions)      │
                  │                                           │
                  │  /tmp/profiles ──────────────────────┐   │
                  │  ~/.jarvis/jarvis.db (unified DB)  ───┤   │
                  │  ~/.jarvis/faces/ (annotated imgs) ───┤   │
                  │  ~/credentials/jarvis/ (cookies)  ────┤   │
                  └──────────────┬───────────────────────┼───┘
                                 │                        │ volume mounts
                  ┌──────────────▼──────────────┐   ┌────▼──────────────────┐
                  │  External Services           │   │  Host                 │
                  │  Anthropic API (Claude)      │   │  ./profiles/          │
                  │  · NLP dialogue              │   │  *.prof  *.gif        │
                  │  · knowledge search          │   │  ~/.jarvis/           │
                  │  · face sentiment analysis   │   │  jarvis.db            │
                  │  Alexa GraphQL API           │   │  faces/ (annotated)   │
                  │  · smart home device control │   │                       │
                  │  SMTP → iCal email invites   │   │  ~/credentials/jarvis/│
                  └──────────────────────────────┘   │  alexa-cookies.json   │
                                                      └───────────────────────┘
```
