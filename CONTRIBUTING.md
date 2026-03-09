# Contributing to J.A.R.V.I.S.

Thank you for contributing to **Just A Rather Very Intelligent System**. This guide covers everything you need to get up and running and the conventions the project follows.

---

## Table of Contents

- [Getting Started](#getting-started)
- [Project Structure](#project-structure)
- [Development Workflow](#development-workflow)
- [Proto Conventions](#proto-conventions)
- [Adding a New Service](#adding-a-new-service)
- [Testing](#testing)
- [Commit & PR Guidelines](#commit--pr-guidelines)
- [Make Targets Reference](#make-targets-reference)

---

## Getting Started

### Prerequisites

| Tool | Version | Purpose |
|---|---|---|
| Go | 1.22+ | Service implementation |
| buf | v1.30+ | Proto linting & code generation |
| Docker | 24+ | Running services locally |
| Docker Compose | V2 | Orchestration (`docker compose`) |

### First-time setup

```bash
# Clone the repo
git clone https://github.com/rkrimper1/jarvis.git
cd jarvis

# Bootstrap: installs buf plugins, generates gen/, runs go mod tidy
bash setup.sh

# Verify everything builds
make build
```

> **Do not** run `go mod tidy` before `gen/` exists — the generated code is a required dependency. `setup.sh` handles ordering correctly.

---

## Project Structure

```
jarvis/
├── proto/                        # Source of truth — edit these, never gen/
│   ├── common/common.proto       # Shared types (RequestMeta, ResponseMeta, Severity, …)
│   ├── nlp/nlp.proto
│   ├── security/security.proto
│   ├── agent/agent.proto
│   ├── hardware/hardware.proto
│   ├── facility/facility.proto
│   ├── intelligence/intelligence.proto
│   ├── business/business.proto
│   ├── learning/learning.proto
│   └── voice/voice.proto
├── gen/                          # Generated code — gitignored, do not edit
│   ├── <domain>/                 # Go stubs (protobuf + gRPC)
│   └── swift/                    # Swift stubs for the iOS client
├── services/                     # gRPC service implementations (Go)
│   ├── nlp-service/
│   ├── security-service/
│   ├── agent-coordinator/
│   ├── hardware-service/
│   ├── facility-service/
│   ├── intelligence-service/
│   ├── business-ops-service/
│   ├── learning-service/
│   └── voice-service/
│       └── cmd/server/           # Each service follows this layout
│           internal/             # (business logic, unexported)
│           └── pkg/middleware/   # (shared gRPC interceptors)
├── gateway/                      # API Gateway — REST ↔ gRPC transcoding
│   ├── cmd/server/main.go
│   ├── internal/
│   │   ├── config/
│   │   ├── middleware/
│   │   └── proxy/
│   └── docs/api-reference.md
├── clients/
│   └── ios/JarvisClient/         # SwiftUI iOS client
│       ├── JarvisClient/
│       │   ├── Services/         # GRPCVoiceService, AudioCaptureEngine, WakeWordDetector
│       │   ├── ViewModels/       # VoiceViewModel
│       │   └── Proto/            # Symlink / copy of gen/swift stubs
│       └── Views/                # HUDView, TranscriptView, WaveformView, DesignSystem
├── docker/                       # Dockerfiles + orchestration
│   ├── docker-compose.yml
│   ├── envoy.yaml
│   ├── proto-gen/Dockerfile      # Buf codegen container
│   └── <service>/Dockerfile      # One per service + gateway
├── docs/openapi/                 # Generated OpenAPI spec — gitignored
├── buf.yaml                      # Buf lint / breaking-change config
├── buf.gen.yaml                  # Code generation config (Go + Swift plugins)
├── buf.lock                      # Pinned buf dependency versions
├── go.mod / go.sum
├── Makefile
└── setup.sh                      # First-time bootstrap script
```

---

## Development Workflow

### 1. Generate code after proto changes

Always regenerate after editing any `.proto` file:

```bash
make proto        # runs buf generate
make proto-lint   # must pass before opening a PR
```

### 2. Check for breaking changes

```bash
make proto-breaking   # compares against main branch
```

Breaking changes to existing RPCs or message fields require a discussion in the PR. Additive changes (new fields, new RPCs) are generally fine.

### 3. Build and test locally

```bash
make build        # compiles all service binaries into bin/
make test         # go test ./... -race -cover
```

### 4. Run the full stack

```bash
make docker-up        # builds images and starts all services
make docker-logs      # tail all logs
make logs-nlp-service # tail a single service
make docker-down      # stop everything
```

---

## Proto Conventions

All `.proto` files live under `proto/<domain>/`. Follow these rules:

- **Package name**: `jarvis.<domain>` (e.g., `jarvis.security`)
- **Go package**: managed automatically by `buf.gen.yaml` — do not set `option go_package` manually
- **Every RPC** must accept a `jarvis.common.RequestMeta` as the first field and return a `jarvis.common.ResponseMeta` as the first field
- **Field names**: `snake_case`, matching Go idiom after generation
- **Enums**: prefix values with the enum name (e.g., `AUTH_VOICE_PRINT` in `AuthMethod`)
- **No version suffixes** in package names — the lint rule `PACKAGE_VERSION_SUFFIX` is suppressed intentionally

### Streaming patterns

| Pattern | When to use |
|---|---|
| Unary | Request/response with a defined end |
| Server streaming | Continuous data pushed to client (telemetry, alerts) |
| Client streaming | Continuous data pushed to server (voice input) |
| Bidirectional | Ongoing dialogue between client and service (suit control, agent heartbeat) |

---

## Adding a New Service

1. **Define the proto** under `proto/<domain>/<domain>.proto`
2. **Regenerate**: `make proto`
3. **Implement** the service under `services/<service-name>/`
   - Follow the layout of an existing service (e.g., `services/nlp-service/`)
   - Entry point: `services/<service-name>/cmd/server/main.go`
4. **Add a Dockerfile** at `docker/<service-name>/Dockerfile`
5. **Register** the service in `docker/docker-compose.yml` and `Makefile` (`SERVICES` list)
6. **Wire routing** in the gateway if REST access is needed

---

## Testing

- Unit tests live alongside the code they test (`foo_test.go` next to `foo.go`)
- Use the race detector: `make test` runs with `-race` by default
- Aim for coverage on business logic; generated code in `gen/` does not need tests
- Integration tests that require a running service should be in a `_integration_test.go` file with build tag `//go:build integration`

---

## Commit & PR Guidelines

### Branching

Branch off `main` using this naming convention:

```
<type>/<short-description>
```

| Type | Use for |
|---|---|
| `feat` | New feature or RPC |
| `fix` | Bug fix |
| `proto` | Proto-only change |
| `refactor` | Restructuring without behavior change |
| `docs` | Documentation only |
| `chore` | Build scripts, CI, deps |

**Examples:**

```bash
git checkout -b feat/stream-security-alerts
git checkout -b proto/add-confidence-field
git checkout -b fix/gateway-hardware-route
```

Keep branch names lowercase and hyphen-separated. Delete the branch after the PR is merged.

### Commit messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <short description>

[optional body]
```

| Type | Use for |
|---|---|
| `feat` | New feature or RPC |
| `fix` | Bug fix |
| `proto` | Proto-only change (no logic) |
| `refactor` | Code restructuring, no behavior change |
| `test` | Adding or updating tests |
| `docs` | Documentation only |
| `chore` | Build scripts, CI, deps |

**Examples:**

```
feat(security): add StreamSecurityAlerts server-streaming RPC
proto(nlp): add confidence field to IntentResponse
fix(gateway): correct route prefix for hardware endpoints
```

### Pull requests

- One logical change per PR
- Proto changes and their implementation may be in the same PR
- All of the following must pass before merge:
  - `make proto-lint`
  - `make proto-breaking`
  - `make test`
- attach the results of above to the PR request as a text file
- Describe *why* the change is needed, not just what it does 
---

## Make Targets Reference

Run `make help` to see all available targets with descriptions.

Key targets:

| Target | Description |
|---|---|
| `make proto` | Generate Go stubs from proto files |
| `make proto-lint` | Lint proto files |
| `make proto-breaking` | Check for breaking changes vs main |
| `make build` | Build all service binaries |
| `make test` | Run all tests with race detector |
| `make docker-up` | Build images and start all services |
| `make docker-down` | Stop all services |
| `make tidy` | Generate protos then run go mod tidy |
| `make clean` | Remove bin/, gen/, docs/openapi/ |
