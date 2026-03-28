# J.A.R.V.I.S.
### Just A Rather Very Intelligent System

A cloud-native AI assistant platform built with **Go**, **gRPC**, **Protobuf**, and **Docker** — inspired by Tony Stark's legendary AI from the Marvel Cinematic Universe.

---

## Architecture

```
  ┌─────────────────────────────────────────────────────────────┐
  │                      Client Layer                           │
  │                                                             │
  │   Web HUD :5173          iOS / Android       Voice / STT   │
  │   (SvelteKit)            (Swift / Kotlin)    (gRPC stream) │
  └───────────────┬──────────────────┬───────────────┬─────────┘
                  │  REST :8080      │               │ gRPC :50051
                  ▼                  ▼               ▼
  ┌─────────────────────────────────────────────────────────────┐
  │                    J.A.R.V.I.S.                             │
  │                  Single Go Binary                           │
  │                                                             │
  │   grpc-gateway (in-process REST → gRPC transcoder)         │
  │                                                             │
  │  ┌──────────────────────────────────────────────────────┐  │
  │  │  command      │ business-ops │ facility               │  │
  │  │  intelligence │ learning     │ security  │ user       │  │
  │  │  nlp ◄──────► voice (in-process)                     │  │
  │  └──────────────────────────────────────────────────────┘  │
  │                                                             │
  │   Claude API (NLP · knowledge search · face sentiment)      │
  │   Redis (session state)   SMTP (invites)                    │
  │   pigo (face detection)   ~/.jarvis/faces/ (annotated imgs) │
  │   /tmp/profiles  → ./profiles/        (heap profile mount)  │
  │   ~/.jarvis/     → host ~/.jarvis/    (knowledge + users)   │
  └─────────────────────────────────────────────────────────────┘
```

- NLP and Voice are wired in-process — no network hop between them
- Dialogue turns are powered by the Claude API, with session history stored in Redis
- `SearchKnowledge` uses the Claude API (and optionally web search) as a fallback when no local SQLite result is found
- `ScheduleEvent` sends iCalendar invite emails to all attendees via SMTP
- `AnalyzeFaces` detects faces with the pigo cascade detector, annotates them with a HUD overlay, and uses the Claude API to generate per-face sentiment commentary
- Annotated face images are written to `~/.jarvis/faces/` (host-mounted) and served at `/faces/<filename>` via the HTTP server
- THREAT and FACES analytics events are stored in `~/.jarvis/analytics.db` (SQLite), including pre-computed scores and the audit log — data persists across container restarts
- The Web HUD proxies `/v1/*` to the REST gateway at `:8080`
- Heap profiles written to `/tmp/profiles` inside Docker are mounted to `./profiles` on the host
- The SQLite knowledge DB at `~/.jarvis/knowledge.db` and users DB at `~/.jarvis/users.db` are mounted read/write so the container persists data to the host

## Services

| Service | Responsibility |
|---|---|
| `command` | On-demand diagnostics — `RequestMemoryProfile` captures a heap profile and renders a GIF |
| `business-ops` | Scheduling, tasks, messaging, reports. `ScheduleEvent` emails iCalendar invites via SMTP. |
| `facility` | Building systems, environment monitoring |
| `intelligence` | Research, artifact analysis, cross-referencing |
| `learning` | Feedback loops, behavior profiling, model metrics. `SearchKnowledge` queries a SQLite knowledge base with FTS5, falling back to Claude API or web search. |
| `nlp` | Intent parsing, Claude-powered dialogue, voice transcription |
| `security` | Auth, threat assessment, emergency protocols |
| `user` | User CRUD, profile management, password change, role-based access (SQLite + bcrypt) |
| `voice` | Wake word, STT, bidi voice streaming, TTS |

All 9 services are exposed as both gRPC (`:50051`) and REST (`:8080`).

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

# ── Heap profiler ─────────────────────────────────────────────────
PPROF_DIR=/tmp/profiles               # optional — output dir (default: /tmp/profiles)
PPROF_INTERVAL=5m                     # optional — background capture interval (default: 5m)

# ── Knowledge base (Learning service) ─────────────────────────────
KNOWLEDGE_DB_PATH=$HOME/.jarvis/knowledge.db  # optional — SQLite DB path (created by setup.sh)
KNOWLEDGE_STALE_DAYS=30               # optional — exclude entries older than N days (default: 30)
KNOWLEDGE_WEB_SEARCH_MAX_USES=10      # optional — max external searches per session (default: 10)

# ── User store ────────────────────────────────────────────────────
USERS_DB_PATH=$HOME/.jarvis/users.db  # optional — SQLite DB path (created by setup.sh)
SEED_TONY_USER=tony-stark             # optional — seeded admin username (default: tony-stark)
SEED_TONY_PASSWORD=tony-stark         # optional — seeded admin password (default: tony-stark)

# ── Face analysis (Security service) ──────────────────────────────
FACE_CASCADE_PATH=$HOME/.jarvis/facefinder  # cascade file downloaded by setup.sh
FACE_OUTPUT_DIR=$HOME/.jarvis/faces         # annotated image output dir
FACE_MIN_SIZE=65                            # optional — minimum face pixel size (default: 65)
FACE_QUALITY_THRESHOLD=6.0                  # optional — pigo quality score cutoff (default: 6.0)
FACE_CLUSTER_OVERLAP=0.25                   # optional — duplicate detection merge factor (default: 0.25)
SECURITY_ANALYTICS_DB_PATH=$HOME/.jarvis/analytics.db  # analytics event store (THREAT + FACES metadata)
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
│       ├── user/
│       └── voice/
├── api/                          # Single Go module
│   ├── cmd/grpc-server/          # Entry point
│   │   ├── main.go               # Listeners, env vars, heap profiler, graceful shutdown
│   │   ├── server.go             # Wires all 9 services onto gRPC + grpc-gateway
│   │   └── nlp_adapter.go        # In-process NLP→Voice adapter (no dial)
│   ├── internal/                 # Service implementations (Go internal package)
│   │   ├── command/server/       # CommandService — on-demand heap profiling
│   │   ├── profiler/             # HeapProfiler — runtime/pprof + pprof/graphviz GIF
│   │   ├── business-ops/server/
│   │   ├── facility/server/
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
│   │   ├── user/
│   │   │   ├── server/           # UserService — CRUD, profile, password, entitlements
│   │   │   └── store/            # SQLite store (bcrypt, seed users, UUID ids)
│   │   └── voice/server/
│   ├── middleware/               # Shared gRPC interceptors (logging, recovery)
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
│       │   └── routes/           # Pages: login, dashboard, dialogue, schedule, tasks, intel, security, profile, admin/users
│       └── static/               # Static assets (hud-bg.png, etc.)
├── profiles/                     # Heap profile output — .prof + .gif (volume-mounted from Docker)
├── docker/
│   ├── jarvis/Dockerfile         # Multi-stage build: builder (Go/Alpine) → runtime (debian:slim + graphviz)
│   └── docker-compose.yml        # jarvis + redis; mounts ./profiles → /tmp/profiles, ~/.jarvis → /home/vagrant/.jarvis (knowledge + users DBs)
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
