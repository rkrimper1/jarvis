# J.A.R.V.I.S.
### Just A Rather Very Intelligent System

A cloud-native AI assistant platform built with **Go**, **gRPC**, **Protobuf**, and **Docker** — inspired by Tony Stark's legendary AI from the Marvel Cinematic Universe.

---

## Architecture

```
  ┌─────────────────────────────────────────────────────────────┐
  │                      Client Layer                           │
  │                                                             │
  │   Web HUD :5173          iOS / Android       Voice / STT    │
  │   (SvelteKit)            (Swift / Kotlin)    (gRPC stream)  │
  └───────────────┬──────────────────┬───────────────┬──────────┘
                  │  REST :8080      │               │ gRPC :50051
                  ▼                  ▼               ▼
  ┌─────────────────────────────────────────────────────────────┐
  │                    J.A.R.V.I.S.                             │
  │                  Single Go Binary                           │
  │                                                             │
  │   grpc-gateway (in-process REST → gRPC transcoder)          │
  │                                                             │
  │  ┌──────────────────────────────────────────────────────┐   │
  │  │  command      │ business-ops │ facility              │   │
  │  │  intelligence │ learning     │ security  │ task      │   │
  │  │  user         │ nlp ◄──────► voice (in-process)      │   │
  │  └──────────────────────────────────────────────────────┘   │
  │                                                             │
  │   Claude API (NLP · knowledge search · face sentiment)      │
  │   Alexa GraphQL API (facility smart home control)           │
  │   Redis (session state)   SMTP (invites)                    │
  │   pigo (face detection)   ~/.jarvis/faces/ (annotated imgs) │
  │   /tmp/profiles          → ./profiles/    (heap profiles)   │
  │   ~/.jarvis/jarvis.db    → host ~/.jarvis/jarvis.db (all DBs) │
  │   ~/.jarvis/faces/       → host ~/.jarvis/faces/ (annotated) │
  │   ~/credentials/jarvis/  → host ~/credentials/jarvis/      │
  └─────────────────────────────────────────────────────────────┘
```

- NLP and Voice are wired in-process — no network hop between them
- `ListAlexaDevices` and `SendAlexaCommand` call `alexa.amazon.com/nexus/v1/graphql` using session cookies exported from a logged-in browser; a background goroutine pings the session endpoint every `ALEXA_KEEPALIVE_INTERVAL` to prevent expiry
- The Alexa cookie file at `~/credentials/jarvis/alexa-cookies.json` is host-mounted read/write so the container can hot-reload updated cookies without a restart
- Dialogue turns are powered by the Claude API, with session history stored in Redis
- `SearchKnowledge` uses the Claude API (and optionally web search) as a fallback when no local SQLite result is found
- `ScheduleEvent` sends iCalendar invite emails to all attendees via SMTP
- `AnalyzeFaces` detects faces with the pigo cascade detector, annotates them with a HUD overlay, and uses the Claude API to generate per-face sentiment commentary
- Annotated face images are written to `~/.jarvis/faces/` (host-mounted) and served at `/faces/<filename>` via the HTTP server
- All SQLite data (users, tasks, analytics, audit, knowledge) is stored in a single unified `~/.jarvis/jarvis.db` — host-mounted so data persists across container restarts
- The Web HUD proxies `/v1/*` to the REST gateway at `:8080`
- Heap profiles written to `/tmp/profiles` inside Docker are mounted to `./profiles` on the host
- OpenCensus distributed tracing exports to Stackdriver when `TRACING_ENABLED=true`; every gRPC span carries `original_request_id`, `x_request_id`, `request_id`, and `user_id` attributes

## Services

| Service | Responsibility |
|---|---|
| `command` | On-demand diagnostics — `RequestMemoryProfile` captures a heap profile and renders a GIF |
| `business-ops` | Scheduling, tasks, messaging, reports. `ScheduleEvent` emails iCalendar invites via SMTP. |
| `facility` | Building systems, environment monitoring, Alexa smart home device listing and control (`ListAlexaDevices`, `SendAlexaCommand`) |
| `intelligence` | Research, artifact analysis, cross-referencing. **Intel Hunt**: ingest signals (manual, RSS, file), AI-powered fusion via Claude, review queue with confirm/dismiss workflow. |
| `learning` | Feedback loops, behavior profiling, model metrics. `SearchKnowledge` queries a SQLite knowledge base with FTS5, falling back to Claude API or web search. |
| `nlp` | Intent parsing, Claude-powered dialogue, voice transcription |
| `security` | Auth, threat assessment, emergency protocols, face detection + sentiment analysis, audit log, surroundings analytics |
| `task` | Task and sprint management — full Scrum backlog (CRUD, priorities, story points, parent/child hierarchy, Epics, Stories) and sprint lifecycle (`CreateSprint`, `CloseSprint`, `GetSprintVelocity`) |
| `user` | User CRUD, profile management, password change, role-based access (SQLite + bcrypt) |
| `voice` | Wake word, STT, bidi voice streaming, TTS |

All 10 services are exposed as both gRPC (`:50051`) and REST (`:8080`).

### Facility — Alexa Smart Home

`ListAlexaDevices` returns all Echo and smart home appliances visible to the authenticated Amazon account. Smart home devices include their current power state (`ON`/`OFF`) and capability set (`LIGHT`, `THERMOSTAT`, `LOCK`, `SWITCH`, etc.), resolved via a batch GraphQL call to `alexa.amazon.com/nexus/v1/graphql` — only devices Amazon's GraphQL layer recognises as valid endpoints are returned.

`SendAlexaCommand` sends a device control mutation (`turnOn`, `turnOff`, `lock`, `unlock`, `setTargetTemperature`, `setBrightness`) via the same GraphQL API.

Session authentication is cookie-based. Export cookies from a logged-in browser using the **Cookie-Editor** extension and point `ALEXA_COOKIES_PATH` at the JSON file. A background goroutine pings `/api/bootstrap` every `ALEXA_KEEPALIVE_INTERVAL` to keep the session alive. If `ALEXA_COOKIES_PATH` is unset the service starts normally and all other Facility RPCs remain available; only the Alexa RPCs return an error.

Three REST handlers are registered outside grpc-gateway when `ALEXA_COOKIES_PATH` is set:
- `GET  /alexa/cookie-status` — returns days until cookie expiry
- `POST /alexa/cookies`       — accepts a new Cookie-Editor JSON export and hot-reloads the client
- `POST /alexa/text-command`  — forwards a free-text command string to the Alexa voice API

Required env vars (store outside the repo, e.g. `$HOME/credentials/jarvis/.env`):
```
ALEXA_COOKIES_PATH=/path/to/alexa-cookies.json
```

### NLP Dialogue — Claude AI

`ProcessDialogueTurn` routes to **Claude (Anthropic)** for three intents, each with its own system prompt and tone:

| Intent | Persona |
|---|---|
| `INTENT_ANALYSIS_REQUEST` | Terse, precise, Jarvis-like — no filler, bullet findings |
| `INTENT_QUERY` | Factual, conceitedly witty, wiseass but respectful |
| `INTENT_SMALL_TALK` | Warm, witty, professional |

`INTENT_EMERGENCY` and `INTENT_COMMAND` remain deterministic (no Claude call).

Multi-turn conversation history is stored per session in **Redis** and sent to Claude on every turn. Sessions expire after `DIALOGUE_SESSION_TTL` (default 30 min).

### Business Ops — Calendar Invites

When `SMTP_*` env vars are configured, `ScheduleEvent` automatically emails an iCalendar (`.ics`) invite to every address in `attendees`. Recipients can accept directly from their email client.

### Command — Heap Profiler

`RequestMemoryProfile` triggers an immediate heap snapshot. The `.prof` file and rendered `.gif` call-graph are written to `PPROF_DIR` and automatically appear in `./profiles/` on the host via the Docker volume mount.

### Intelligence — Intel Hunt

Intel Hunt is a competitive intelligence pipeline built into the `intelligence` service. Raw signals (manual text, RSS/Atom feeds, uploaded files) are fused by Claude into structured **IntelCards** with a title, summary, opportunity type, confidence score, and suggested action. Cards sit in a review queue (`PENDING_REVIEW`) until an operator confirms or dismisses them.

**Signal ingestion paths:**

| Path | How |
|---|---|
| Manual | `POST /v1/intel/signals` gRPC/REST |
| RSS / Atom | Background poller — runs at `RSS_POLL_INTERVAL`, deduplicates by GUID/link |
| File upload | `POST /v1/intel/ingest/file` multipart — `.txt`, `.csv`, `.tsv`, `.pdf` (Apache pdfcpu) |

**Opportunity types:** `TACTICAL` (24 h action) · `STRATEGIC` · `RESOURCE` · `THREAT_MITIGATION`

**Confidence bands** (thresholds configurable via env vars):
- `≥ FUSION_CONF_IMMEDIATE` — multiple corroborating sources; action recommended immediately
- `≥ FUSION_CONF_VERIFY` — single credible source; verify before acting
- `≥ FUSION_CONF_REVIEW` — fragmentary signal; flag for human review
- `< FUSION_CONF_REVIEW` — noise; log but deprioritise

Requires `JARVIS_DB_PATH` (store) and `ANTHROPIC_API_KEY` (fusion). The service starts and all other Intelligence RPCs remain available when either is absent; only Intel Hunt RPCs return `FailedPrecondition`.

### Task — Sprint Management

`TaskService` provides a full Scrum-style task tracker backed by the shared `jarvis.db` SQLite database. Tasks carry type (`TASK`, `EPIC`, `STORY`, `BUG`, `SUBTASK`), priority, story points, due date, assignee, and optional `parent_id` for epic/story hierarchies. Sequential `display_id` values (e.g. `JARVIS-0001`) are auto-assigned.

Sprints have a name, goal, and date range. `CloseSprint` marks the sprint closed and rolls any incomplete tasks back to the backlog. `GetSprintVelocity` returns per-user story-point totals for a completed sprint. `MoveTaskStatus` advances a task through `UNASSIGNED → ASSIGNED → IN_PROGRESS → TESTING → REVIEW → COMPLETED`.

The service starts with a no-op in-memory stub when `JARVIS_DB_PATH` is unset; all other services are unaffected.

Required env vars (optional — service degrades gracefully without them):
```
JARVIS_DB_PATH=$HOME/.jarvis/jarvis.db  # optional — shared SQLite DB path (created by setup.sh)
TOKEN_SECRET=<random-string>            # optional — JWT signing secret (default: stark-industries-dev-secret-change-in-prod — change in production)
```

### Security — Face Analysis

`AnalyzeFaces` accepts a JPEG or PNG image, detects faces using **pigo** (loaded once at startup from `FACE_CASCADE_PATH`), crops each face and sends it to **Claude** for sentiment classification, then writes a HUD-annotated PNG to `FACE_OUTPUT_DIR`. The result includes per-face sentiment, commentary, and bounding-box coordinates. Detection runs only when both `FACE_CASCADE_PATH` and `FACE_OUTPUT_DIR` are set; the service starts and handles all other RPCs normally if they are absent.

Audit events are recorded per call, and face sentiment scores feed into the `SurroundingsStatus` composite score returned by `GetAuditLog` (70 % threat weight, 30 % face weight over the last 30 minutes).

Required env vars (store outside the repo, e.g. `$HOME/credentials/jarvis/.env`):

```
# ── SMTP / Calendar invites ───────────────────────────────────────
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_USER=you@gmail.com
SMTP_PASS=<Gmail App Password>
SMTP_TO=you@gmail.com

# ── Claude AI (NLP dialogue) ──────────────────────────────────────
ANTHROPIC_API_KEY=sk-ant-...
CLAUDE_MODEL=claude-sonnet-4-6        # optional — default: claude-sonnet-4-6
CLAUDE_MAX_TOKENS=1024                # optional — default: 1024

# ── Dialogue tuning ───────────────────────────────────────────────
DIALOGUE_MAX_HISTORY=20               # optional — turns of history sent to Claude
DIALOGUE_SESSION_TTL=30m              # optional — Redis session expiry
DIALOGUE_CONFIDENCE_THRESH=0.6        # optional — below this → requires_confirmation

# ── STT (Speech-to-Text) ──────────────────────────────────────────
STT_PROVIDER=stub                     # stub | google (default: stub)
STT_MODEL=latest_long
STT_AUTO_PUNCTUATION=true
STT_WORD_TIME_OFFSETS=false
STT_MAX_SYNC_DURATION_SEC=55

# ── TTS (Text-to-Speech) ──────────────────────────────────────────
TTS_PROVIDER=stub                     # stub | google (default: stub)
TTS_VOICE_ID=en-US-Journey-D
TTS_LANGUAGE_CODE=en-US
TTS_SPEAKING_RATE=1.0
TTS_PITCH=0.0
TTS_AUDIO_ENCODING=pcm
TTS_CHUNK_SIZE_BYTES=8192

# ── GCP (required when STT_PROVIDER or TTS_PROVIDER = google) ─────
GCP_PROJECT=your-project-id
GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account.json

# ── Distributed tracing ───────────────────────────────────────────
TRACING_ENABLED=false                 # optional — set true to enable OpenCensus → Stackdriver export (default: false)

# ── Heap profiler ─────────────────────────────────────────────────
PPROF_DIR=/tmp/profiles               # optional — output dir (default: /tmp/profiles)
PPROF_INTERVAL=5m                     # optional — background capture interval (default: 5m)

# ── Shared SQLite database (all stores: users, tasks, analytics, audit, knowledge) ───
JARVIS_DB_PATH=$HOME/.jarvis/jarvis.db  # optional — created by setup.sh (schema applied by server on first start)

# ── Knowledge base (Learning service) ─────────────────────────────
KNOWLEDGE_STALE_DAYS=30               # optional — exclude entries older than N days (default: 30)
KNOWLEDGE_WEB_SEARCH_MAX_USES=10      # optional — max external searches per session (default: 10)

# ── User store ────────────────────────────────────────────────────
SEED_TONY_USER=tony-stark             # optional — seeded Tony admin username (default: tony-stark)
SEED_TONY_PASSWORD=tony-stark         # optional — seeded Tony admin password (default: tony-stark)

# ── Task store ─────────────────────────────────────────────────────────────────
TOKEN_SECRET=stark-industries-dev-secret-change-in-prod  # optional — JWT signing secret (change in production)

# ── Face analysis (Security service) ──────────────────────────────
FACE_CASCADE_PATH=$HOME/.jarvis/facefinder  # cascade file downloaded by setup.sh
FACE_OUTPUT_DIR=$HOME/.jarvis/faces         # annotated image output dir
FACE_MIN_SIZE=65                            # optional — minimum face pixel size (default: 65)
FACE_QUALITY_THRESHOLD=6.0                  # optional — pigo quality score cutoff (default: 6.0)
FACE_CLUSTER_OVERLAP=0.25                   # optional — duplicate detection merge factor (default: 0.25)
FACE_OUTPUT_TRIANGLE_SIZE=0.22             # optional — triangle padding multiplier (default: 0.22, 0 = use default)
FACE_OUTPUT_OPACITY=1.0                    # optional — HUD overlay opacity 0.0–1.0 (default: 1.0, 0 = use default)
FACE_OUTPUT_FONT_SIZE=0                    # optional — annotation font size in points (default: 0 = auto from image width)
FACE_MAX_IMAGE_BYTES=5242880               # optional — max uploaded image size in bytes (default: 5 MiB)

# ── Alexa smart home (Facility service) ───────────────────────
ALEXA_COOKIES_PATH=/path/to/alexa-cookies.json  # optional — Cookie-Editor JSON export from alexa.amazon.com
ALEXA_DEBUG=false                                # optional — log Alexa HTTP requests/responses (default: false)
ALEXA_KEEPALIVE_INTERVAL=12h                    # optional — session keep-alive ping interval (default: 12h)

# ── Intel Hunt (Intelligence service) ─────────────────────────
# Requires ANTHROPIC_API_KEY (above) and JARVIS_DB_PATH (above).
# All vars are optional — defaults shown. Intel Hunt is disabled when
# ANTHROPIC_API_KEY or JARVIS_DB_PATH are unset.
FUSION_MODEL=claude-haiku-4-5-20251001  # optional — model for signal fusion (default: claude-haiku-4-5-20251001)
FUSION_MAX_TOKENS=512                   # optional — max response tokens (default: 512)
FUSION_CONF_IMMEDIATE=0.90              # optional — confidence threshold for immediate action (default: 0.90)
FUSION_CONF_VERIFY=0.70                 # optional — threshold for verify-before-acting (default: 0.70)
FUSION_CONF_REVIEW=0.50                 # optional — threshold for human review (default: 0.50)

# ── RSS poller (Intel Hunt) ────────────────────────────────────
# Comma-separated feed URLs. Leave empty to disable the poller.
RSS_FEED_URLS=                          # optional — e.g. https://feeds.reuters.com/reuters/businessNews
RSS_POLL_INTERVAL=15m                   # optional — polling cadence (default: 15m)
```

> STT and TTS default to `stub` — mock responses, no cloud API required.
> Set `STT_PROVIDER=google` / `TTS_PROVIDER=google` and supply GCP credentials to enable real speech recognition and synthesis.

> Heap profiles are also captured on demand via `POST /v1/command/memory-profile` regardless of `PPROF_INTERVAL`.
> In Docker, output files appear in `./profiles/` on the host automatically via the volume mount.
> GIF rendering requires `graphviz` — installed automatically by `setup.sh` and baked into the Docker image.

## Quick Start

> **First time?** Run the bootstrap script — it installs `buf`, the Go protoc plugins,
> generates all proto stubs into `api/pb/`, and runs `go mod tidy` in one step.

```bash
# 1. Bootstrap (run once after cloning)
bash setup.sh

# 2. Start with Docker for the api microservices & redis
make docker-up

# 3. Verify it's running (gRPC health)
grpcurl -plaintext localhost:50051 list

# 4. Call a REST endpoint
curl -X POST http://localhost:8080/v1/nlp/dialogue \
  -H "Content-Type: application/json" \
  -d '{"meta":{"request_id":"r1"},"session_id":"s1","utterance":"Hello JARVIS"}'

# 5. Web UI Start Up (suggest running in a separate terminal)
make web-dev

# 6. Open a browser (login: tony-stark / tony-stark  or  rob-krimper / rob-krimper)
http://localhost:5173/

# 7. Shut Down Web UI
Ctrl-C

# 8. Shut Down the api microservices
make docker-down
```

### Manual setup (step-by-step)

```bash
# Install buf CLI — https://buf.build/docs/installation
curl -fsSL https://github.com/bufbuild/buf/releases/download/v1.30.0/buf-$(uname -s)-$(uname -m) \
  -o /usr/local/bin/buf && chmod +x /usr/local/bin/buf

# Install Go protoc plugins
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.34.1
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.3.0
go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@v2.19.1
go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2@v2.19.1

# Generate proto stubs (creates api/pb/)
buf dep update
buf generate

# Tidy modules
go mod tidy

# Build and run
make docker-up
```

### Docker-only (no local Go/buf required)

```bash
make docker-up      # build image and start jarvis + redis
make docker-logs    # tail logs
make docker-down    # stop everything
```

## Project Structure

```
jarvis/
├── proto/                        # Protobuf definitions (source of truth)
│   └── pb/
│       ├── common/               # Shared types (RequestMeta, ResponseMeta, etc.)
│       ├── command/              # CommandService (RequestMemoryProfile)
│       ├── business/
│       ├── facility/
│       ├── intelligence/
│       ├── learning/
│       ├── nlp/
│       ├── security/
│       ├── task/
│       ├── user/
│       └── voice/
├── api/                          # Single Go module
│   ├── cmd/grpc-server/          # Entry point
│   │   ├── main.go               # Listeners, env vars, heap profiler, graceful shutdown
│   │   ├── server.go             # Wires all 10 services onto gRPC + grpc-gateway
│   │   └── nlp_adapter.go        # In-process NLP→Voice adapter (no dial)
│   ├── internal/                 # Service implementations (Go internal package)
│   │   ├── command/server/       # CommandService — on-demand heap profiling
│   │   ├── profiler/             # HeapProfiler — runtime/pprof + pprof/graphviz GIF
│   │   ├── business-ops/server/
│   │   ├── facility/
│   │   │   ├── alexa/            # Amazon Alexa HTTP client (cookies, GraphQL device control, keep-alive)
│   │   │   │   └── testharness/  # CLI test tool for Alexa API exploration
│   │   │   ├── config/
│   │   │   ├── environment/
│   │   │   ├── server/           # FacilityService gRPC implementation + Alexa REST handlers
│   │   │   └── zone/
│   │   ├── intelligence/server/
│   │   ├── integrations/
│   │   │   ├── claude/           # Anthropic Claude API client (NLP dialogue)
│   │   │   └── email/            # SMTP + iCalendar invite sender
│   │   ├── learning/
│   │   │   ├── knowledge/            # SQLite + FTS5 knowledge store + Claude/web-search fallback
│   │   │   └── server/
│   │   ├── nlp/
│   │   │   ├── config/
│   │   │   ├── dialogue/         # Manager, Redis session store, prompts
│   │   │   ├── entity/
│   │   │   ├── intent/
│   │   │   └── server/
│   │   ├── security/
│   │   │   ├── analyticsstore/   # SQLite analytics + audit event store (THREAT/FACES scores, audit log)
│   │   │   ├── audit/            # Append-only audit log (in-memory or SQLite backend)
│   │   │   ├── auth/             # JWT issuance and verification
│   │   │   ├── config/           # FaceConfig and security env var helpers
│   │   │   ├── faceanalysis/     # pigo face detector + HUD annotation renderer
│   │   │   ├── protocol/         # Protocol execution stubs
│   │   │   ├── server/           # SecurityService gRPC implementation
│   │   │   ├── threat/           # Threat assessment logic
│   │   │   └── token/            # Token store
│   │   ├── task/
│   │   │   ├── server/           # TaskService — CRUD, sprint lifecycle, velocity
│   │   │   └── store/            # SQLite store (tasks, sprints, display_id sequence)
│   │   ├── user/
│   │   │   ├── server/           # UserService — CRUD, profile, password, entitlements
│   │   │   └── store/            # SQLite store (bcrypt, seed users, UUID ids)
│   │   └── voice/server/
│   ├── middleware/               # Shared gRPC interceptors (tracing, logging, recovery)
│   ├── pb/                       # Generated Go stubs — gitignored, do not edit
│   └── rest/                     # grpc-gateway custom error handler
├── gen/
│   └── swift/                    # Generated Swift stubs for the iOS client
├── clients/
│   ├── ios/JarvisClient/         # SwiftUI iOS HUD client
│   ├── android/                  # Android client (Kotlin + gRPC)
│   └── web/                      # SvelteKit HUD web client
│       ├── src/
│       │   ├── lib/
│       │   │   ├── api/          # Typed fetch wrappers for all 9 REST services
│       │   │   └── stores/       # Auth store (localStorage + derived state, role, isAdmin)
│       │   └── routes/           # Pages: login, dashboard, dialogue, schedule, tasks (backlog/board/scrum), intel, security, profile, admin/users
│       └── static/               # Static assets (hud-bg.png, etc.)
├── profiles/                     # Heap profile output — .prof + .gif (volume-mounted from Docker)
├── docker/
│   ├── jarvis/Dockerfile         # Multi-stage build: builder (Go/Alpine) → runtime (debian:slim + graphviz)
│   └── docker-compose.yml        # jarvis + redis; mounts ./profiles → /tmp/profiles, ~/.jarvis/jarvis.db (unified DB), ~/.jarvis/faces/
├── gateway/
│   └── docs/api-reference.md     # REST + gRPC API reference
├── docs/openapi/                 # Generated OpenAPI spec — gitignored
├── buf.yaml                      # Buf lint/breaking config
├── buf.gen.yaml                  # Code generation (Go → api/pb/, Swift → gen/swift/)
├── Makefile
└── setup.sh                      # First-time bootstrap script (installs buf, graphviz, node, etc.)
```

## Proto Conventions

- All RPCs accept a `RequestMeta` and return a `ResponseMeta` for tracing
- Streaming RPCs are defined where real-time data flow is needed
- Bidirectional streaming used for voice conversation
- All services share `common.proto` types to avoid duplication

## gRPC Stream Patterns Used

| Pattern | Example |
|---|---|
| Unary | `Authenticate`, `QueryIntel`, `ScheduleEvent`, `RequestMemoryProfile` |
| Server streaming | `StreamTelemetry`, `StreamSecurityAlerts`, `StreamAdaptationEvents`, `StreamIntelUpdates`, `StreamEnvironment` |
| Client streaming | `StreamVoiceInput` |
| Bidirectional | `Converse` (voice) |

## Make Targets

```bash
# ── Proto ────────────────────────────────────────────────────
make proto              # regenerate Go stubs from proto files (→ api/pb/)
make proto-lint         # lint proto files
make proto-breaking     # check for breaking proto changes vs main branch
make proto-android      # generate Kotlin/gRPC stubs via Gradle

# ── Build & Run ──────────────────────────────────────────────
make build              # compile the jarvis binary (bin/jarvis)
make run                # build then run locally (loads ENV_PATH if present)

# ── Test ─────────────────────────────────────────────────────
make test               # run all tests with race detector
make test-short         # run tests without -v (faster CI output)
make test-voice         # run voice tests only

# ── Docker ───────────────────────────────────────────────────
make docker-build       # build the Jarvis Docker image (no start)
make docker-up          # build image and start jarvis + redis in background
make docker-up-fg       # build image and start in foreground (shows logs)
make docker-down        # stop and remove all containers
make docker-down-v      # stop containers and remove volumes
make docker-logs        # tail all container logs
make docker-ps          # show running container status
make docker-restart     # restart containers without rebuilding
make logs-<svc>         # tail logs for a specific service, e.g. make logs-jarvis
make restart-<svc>      # restart a specific service, e.g. make restart-jarvis

# ── Utilities ────────────────────────────────────────────────
make setup              # first-time bootstrap: buf, graphviz, node, proto stubs
make tidy               # generate protos then tidy Go modules
make clean              # remove compiled binaries and generated code (bin/, api/pb/)
make compose-version    # show which Docker Compose version is being used

# ── Clients ──────────────────────────────────────────────────
make ios-open           # generate protos and open the Xcode project
make ios-clean          # remove Swift generated stubs (gen/swift/)
make android-open       # generate Android stubs and open Android Studio
make web-dev            # start the SvelteKit HUD client in dev mode (hot reload, proxies :8080)
make web-build          # build the SvelteKit HUD client for production
make web-preview        # preview the production build locally

make help               # list all available targets
```
