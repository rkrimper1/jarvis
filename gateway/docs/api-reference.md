# JARVIS REST API Reference

Base URL: `http://localhost:8080`
gRPC direct: `localhost:50051`

All 9 services are served by a single binary via grpc-gateway (in-process, no proxy hop).

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
```bash
curl -X POST http://localhost:8080/v1/security/authenticate \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "sec-auth-001"},
    "subject_id": "tony-stark",
    "method": "AUTH_METHOD_TOKEN"
  }'
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
```bash
curl "http://localhost:8080/v1/security/audit?page_size=20" \
  -H "Authorization: Bearer $TOKEN"
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

Users are stored in a SQLite DB (`USERS_DB_PATH`). Passwords are bcrypt-hashed. Two users are seeded on first start: `tony-stark` (ROLE_VIEWER) and `rob-krimper` (ROLE_ADMIN). The role is encoded in the JWT `granted_scopes` on every `Authenticate` call.

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
                  │  user         nlp ◄────────► voice        │
                  │               (nlp↔voice in-process)      │
                  │                                           │
                  │  nlp → Claude API (dialogue, streaming)  │
                  │  learning → Claude API (knowledge search) │
                  │  Redis (dialogue history + sessions)      │
                  │                                           │
                  │  /tmp/profiles ──────────────────────┐   │
                  │  ~/.jarvis (knowledge + users DBs) ───┤   │
                  └──────────────┬───────────────────────┼───┘
                                 │                        │ volume mounts
                  ┌──────────────▼──────────────┐   ┌────▼──────────────────┐
                  │  External Services           │   │  Host                 │
                  │  Anthropic API (Claude)      │   │  ./profiles/          │
                  │  · NLP dialogue              │   │  *.prof  *.gif        │
                  │  · knowledge search          │   │  ~/.jarvis/           │
                  │  SMTP → iCal email invites   │   │  knowledge.db         │
                  └──────────────────────────────┘   │  users.db             │
                                                      └───────────────────────┘
```
