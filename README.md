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
  │  │  intelligence │ learning     │ security               │  │
  │  │  nlp ◄──────► voice (in-process)                     │  │
  │  └──────────────────────────────────────────────────────┘  │
  │                                                             │
  │   Claude API (NLP dialogue)    SMTP (calendar invites)     │
  │   Redis (session state)                                     │
  │   /tmp/profiles → ./profiles  (heap profile volume mount)  │
  └─────────────────────────────────────────────────────────────┘
```

- NLP and Voice are wired in-process — no network hop between them
- Dialogue turns are powered by the Claude API, with session history stored in Redis
- `ScheduleEvent` sends iCalendar invite emails to all attendees via SMTP
- The Web HUD proxies `/v1/*` to the REST gateway at `:8080`
- Heap profiles written to `/tmp/profiles` inside Docker are mounted to `./profiles` on the host

## Services

| Service | Responsibility |
|---|---|
| `command` | On-demand diagnostics — `RequestMemoryProfile` captures a heap profile and renders a GIF |
| `business-ops` | Scheduling, tasks, messaging, reports. `ScheduleEvent` emails iCalendar invites via SMTP. |
| `facility` | Building systems, environment monitoring |
| `intelligence` | Research, artifact analysis, cross-referencing |
| `learning` | Feedback loops, behavior profiling, model metrics |
| `nlp` | Intent parsing, Claude-powered dialogue, voice transcription |
| `security` | Auth, threat assessment, emergency protocols |
| `voice` | Wake word, STT, bidi voice streaming, TTS |

All 8 services are exposed as both gRPC (`:50051`) and REST (`:8080`).

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

# 6. Open a browser (login: tony-stark - no password setup yet)
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
│       └── voice/
├── api/                          # Single Go module
│   ├── cmd/grpc-server/          # Entry point
│   │   ├── main.go               # Listeners, env vars, heap profiler, graceful shutdown
│   │   ├── server.go             # Wires all 8 services onto gRPC + grpc-gateway
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
│   │   ├── learning/server/
│   │   ├── nlp/
│   │   │   ├── config/
│   │   │   ├── dialogue/         # Manager, Redis session store, prompts
│   │   │   ├── entity/
│   │   │   ├── intent/
│   │   │   └── server/
│   │   ├── security/server/
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
│       │   │   ├── api/          # Typed fetch wrappers for all 8 REST services
│       │   │   └── stores/       # Auth store (localStorage + derived state)
│       │   └── routes/           # Pages: login, dashboard, dialogue, schedule, tasks, intel, security
│       └── static/               # Static assets (hud-bg.png, etc.)
├── profiles/                     # Heap profile output — .prof + .gif (volume-mounted from Docker)
├── docker/
│   ├── jarvis/Dockerfile         # Multi-stage build: builder (Go/Alpine) → runtime (debian:slim + graphviz)
│   └── docker-compose.yml        # jarvis + redis; mounts ./profiles → /tmp/profiles
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
make build          # compile the jarvis binary (bin/jarvis)
make test           # run all tests with race detector
make proto          # regenerate Go stubs from proto files (→ api/pb/)
make proto-lint     # lint proto files
make proto-android  # generate Kotlin/gRPC stubs via Gradle
make docker-up      # build image and start all services
make docker-down    # stop all services
make docker-logs    # tail all container logs
make logs-<svc>     # tail a specific service, e.g. make logs-jarvis
make ios-open       # generate protos and open the Xcode project
make android-open   # generate Android stubs and open Android Studio
make help           # list all available targets
```
