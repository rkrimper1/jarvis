# J.A.R.V.I.S.
### Just A Rather Very Intelligent System

A cloud-native AI assistant platform built with **Go**, **gRPC**, **Protobuf**, and **Docker** — inspired by Tony Stark's legendary AI from the Marvel Cinematic Universe.

---

## Architecture

```
                         ┌─────────────────────┐
                         │   Client (Voice/HUD) │
                         └──────────┬──────────┘
                                    │
                         ┌──────────▼──────────┐
                         │   Envoy / Gateway    │  ← REST + gRPC
                         └──────────┬──────────┘
                                    │
          ┌──────────┬──────────────┼──────────────┬──────────┐
          │          │              │               │          │
    ┌─────▼──┐  ┌────▼───┐  ┌──────▼─────┐  ┌─────▼──┐ ┌────▼──────┐
    │  NLP   │  │Security│  │Intelligence│  │Hardware│ │ Facility  │
    └────────┘  └────────┘  └────────────┘  └────────┘ └───────────┘
          │          │              │               │          │
    ┌─────▼──┐  ┌────▼───────────────────────────────────────▼──────┐
    │Business│  │              AgentCoordinator                      │
    └────────┘  └─────────────────────────────────────────┬─────────┘
                                                          │
                                               ┌──────────▼──────┐
                                               │    Learning      │
                                               └─────────────────┘
                         ┌─────────────────────┐
                         │    Voice Service     │  ← bidi gRPC stream
                         └─────────────────────┘
```

## Services

| Service | Port | Responsibility |
|---|---|---|
| `nlp-service` | 50051 | Intent parsing, dialogue, voice transcription |
| `security-service` | 50052 | Auth, threat assessment, emergency protocols |
| `agent-coordinator` | 50053 | Multi-agent orchestration, task dispatch |
| `hardware-service` | 50054 | Suit/device control, telemetry, energy scanning |
| `facility-service` | 50055 | Building systems, environment monitoring |
| `intelligence-service` | 50056 | Research, artifact analysis, cross-referencing |
| `business-ops-service` | 50057 | Scheduling, tasks, messaging, reports |
| `learning-service` | 50058 | Feedback loops, behavior profiling, model metrics |
| `voice-service` | 50059 | Wake word, STT, bidi voice streaming, TTS |

## Quick Start

> **First time?** Run the bootstrap script — it installs `buf`, the Go protoc plugins,
> generates all proto stubs into `gen/`, and runs `go mod tidy` in one step.

```bash
# 1. Bootstrap (run once after extracting the project)
bash setup.sh

# 2. Start everything with Docker
make docker-up

# 3. Check it's running
curl http://localhost:8080/healthz
# {"status":"ok","service":"jarvis-gateway"}

# 4. Call an endpoint
curl -X POST http://localhost:8080/v1/nlp/dialogue \
  -H "Content-Type: application/json" \
  -d '{"meta":{"request_id":"r1"},"session_id":"s1","utterance":"Hello JARVIS"}'
```

### Manual setup (if you prefer step-by-step)

```bash
# Install buf CLI — https://buf.build/docs/installation
# On Linux/Mac:
curl -fsSL https://github.com/bufbuild/buf/releases/download/v1.30.0/buf-$(uname -s)-$(uname -m) \
  -o /usr/local/bin/buf && chmod +x /usr/local/bin/buf

# Install Go protoc plugins
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.34.1
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.3.0
go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@v2.19.1
go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2@v2.19.1

# Generate proto stubs (creates gen/ directory)
buf dep update
buf generate

# Tidy modules (now that gen/ exists, go.sum can be resolved)
go mod tidy

# Build and run
make docker-up
```

### Docker-only (no local Go/buf required)

The Dockerfiles handle everything internally — proto generation, `go mod download`,
and compilation all happen inside multi-stage Docker builds. You only need Docker.

```bash
make docker-up      # build images and start all services
make docker-logs    # tail logs from all containers
make docker-down    # stop everything
```

## Project Structure

```
jarvis/
├── proto/                  # Protobuf definitions (source of truth)
│   ├── common/             # Shared types (RequestMeta, ResponseMeta, etc.)
│   ├── nlp/
│   ├── security/
│   ├── agent/
│   ├── hardware/
│   ├── facility/
│   ├── intelligence/
│   ├── business/
│   ├── learning/
│   └── voice/
├── gen/                    # Generated Go + Swift stubs (gitignored)
│   └── swift/              # Swift stubs for the iOS client
├── services/               # Service implementations
│   ├── nlp-service/
│   ├── security-service/
│   ├── agent-coordinator/
│   ├── hardware-service/
│   ├── facility-service/
│   ├── intelligence-service/
│   ├── business-ops-service/
│   ├── learning-service/
│   └── voice-service/
├── gateway/                # API Gateway (gRPC-Gateway + REST transcoding)
│   └── docs/api-reference.md
├── clients/
│   └── ios/JarvisClient/   # SwiftUI iOS HUD client
├── docker/                 # Dockerfiles + docker-compose.yml
├── buf.yaml                # Buf lint/breaking config
├── buf.gen.yaml            # Code generation config (Go + Swift)
├── Makefile
└── setup.sh                # First-time bootstrap script
```

## Proto Conventions

- All RPCs accept a `RequestMeta` and return a `ResponseMeta` for tracing
- Streaming RPCs are defined where real-time data flow is needed
- Bidirectional streaming used for suit control, agent heartbeat, and voice conversation
- All services share `common.proto` types to avoid duplication

## gRPC Stream Patterns Used

| Pattern | Example |
|---|---|
| Unary | `Authenticate`, `QueryIntel`, `ScheduleEvent` |
| Server streaming | `StreamTelemetry`, `StreamSecurityAlerts`, `StreamCoordinationEvents`, `StreamAdaptationEvents`, `StreamIntelUpdates`, `StreamEnvironment` |
| Client streaming | `StreamVoiceInput` |
| Bidirectional | `Converse` (voice), `SuitControlStream`, `AgentHeartbeat` |

## Make Targets

```bash
make build          # compile all service binaries
make test           # run all tests with race detector
make proto          # regenerate Go stubs from proto files
make proto-lint     # lint proto files
make docker-up      # build images and start all services
make docker-down    # stop all services
make docker-logs    # tail all container logs
make logs-<svc>     # tail a single service, e.g. make logs-gateway
make ios-open       # generate protos and open the Xcode project
make help           # list all available targets
```
