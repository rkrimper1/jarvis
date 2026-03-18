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
  │  │  business-ops │ facility │ intelligence │ learning   │  │
  │  │  security     │ nlp ◄──► voice (in-process)          │  │
  │  └──────────────────────────────────────────────────────┘  │
  │                                                             │
  │   Claude API (NLP dialogue)    SMTP (calendar invites)     │
  │   Redis (session state)                                     │
  └─────────────────────────────────────────────────────────────┘
```

- NLP and Voice are wired in-process — no network hop between them
- Dialogue turns are powered by the Claude API, with session history stored in Redis
- `ScheduleEvent` sends iCalendar invite emails to all attendees via SMTP
- The Web HUD proxies `/v1/*` to the REST gateway at `:8080`

## Services

| Service | Responsibility |
|---|---|
| `business-ops` | Scheduling, tasks, messaging, reports. `ScheduleEvent` emails iCalendar invites via SMTP. |
| `facility` | Building systems, environment monitoring |
| `intelligence` | Research, artifact analysis, cross-referencing |
| `learning` | Feedback loops, behavior profiling, model metrics |
| `nlp` | Intent parsing, Claude-powered dialogue, voice transcription |
| `security` | Auth, threat assessment, emergency protocols |
| `voice` | Wake word, STT, bidi voice streaming, TTS |

All 7 services are exposed as both gRPC (`:50051`) and REST (`:8080`).

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
```

## Quick Start

> **First time?** Run the bootstrap script — it installs `buf`, the Go protoc plugins,
> generates all proto stubs into `api/pb/`, and runs `go mod tidy` in one step.

```bash
# 1. Bootstrap (run once after cloning)
bash setup.sh

# 2. Start with Docker
make docker-up

# 3. Verify it's running (gRPC health)
grpcurl -plaintext localhost:50051 list

# 4. Call a REST endpoint
curl -X POST http://localhost:8080/v1/nlp/dialogue \
  -H "Content-Type: application/json" \
  -d '{"meta":{"request_id":"r1"},"session_id":"s1","utterance":"Hello JARVIS"}'
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
├── proto/                  # Protobuf definitions (source of truth)
│   ├── common/             # Shared types (RequestMeta, ResponseMeta, etc.)
│   ├── business/
│   ├── facility/
│   ├── intelligence/
│   ├── learning/
│   ├── nlp/
│   ├── security/
│   └── voice/
├── api/                    # Single Go module
│   ├── cmd/grpc-server/    # Entry point (main.go, server.go, nlp_adapter.go)
│   ├── internal/           # Service implementations (Go internal — not importable outside api/)
│   │   ├── business-ops/server/
│   │   ├── facility/server/
│   │   ├── intelligence/server/
│   │   ├── learning/server/
│   │   ├── nlp/server/
│   │   ├── security/server/
│   │   └── voice/server/
│   ├── middleware/         # Shared gRPC interceptors (logging, recovery)
│   ├── pb/                 # Generated Go stubs — gitignored, do not edit
│   └── rest/               # grpc-gateway error handler
├── gen/
│   └── swift/              # Generated Swift stubs for the iOS client
├── clients/
│   ├── ios/JarvisClient/   # SwiftUI iOS HUD client
│   └── android/            # Android client
├── docker/
│   ├── jarvis/Dockerfile   # Single multi-stage build
│   └── docker-compose.yml  # jarvis + redis
├── gateway/
│   └── docs/api-reference.md
├── buf.yaml                # Buf lint/breaking config
├── buf.gen.yaml            # Code generation config (Go → api/pb/, Swift → gen/swift/)
├── Makefile
└── setup.sh                # First-time bootstrap script
```

## Proto Conventions

- All RPCs accept a `RequestMeta` and return a `ResponseMeta` for tracing
- Streaming RPCs are defined where real-time data flow is needed
- Bidirectional streaming used for voice conversation
- All services share `common.proto` types to avoid duplication

## gRPC Stream Patterns Used

| Pattern | Example |
|---|---|
| Unary | `Authenticate`, `QueryIntel`, `ScheduleEvent` |
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
