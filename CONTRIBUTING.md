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

# Bootstrap: installs buf plugins, generates api/pb/, runs go mod tidy
bash setup.sh

# Verify everything builds
make build
```

> **Do not** run `go mod tidy` before `api/pb/` exists — the generated code is a required dependency. `setup.sh` handles ordering correctly.

---

## Project Structure

```
jarvis/
├── proto/                        # Source of truth — edit these, never api/pb/
│   ├── common/common.proto       # Shared types (RequestMeta, ResponseMeta, Severity, …)
│   ├── business/business.proto
│   ├── facility/facility.proto
│   ├── intelligence/intelligence.proto
│   ├── learning/learning.proto
│   ├── nlp/nlp.proto
│   ├── security/security.proto
│   └── voice/voice.proto
├── api/                          # Single Go module — one binary
│   ├── cmd/grpc-server/          # Entry point
│   │   ├── main.go               # Listeners, env vars, graceful shutdown
│   │   ├── server.go             # Wires all 7 services onto gRPC + grpc-gateway
│   │   └── nlp_adapter.go        # In-process NLP→Voice adapter (no dial)
│   ├── internal/                 # Service implementations (Go internal package)
│   │   ├── business-ops/server/
│   │   ├── facility/server/
│   │   ├── intelligence/server/
│   │   ├── learning/server/
│   │   ├── nlp/
│   │   │   ├── config/
│   │   │   └── server/
│   │   ├── security/
│   │   │   ├── config/
│   │   │   └── server/
│   │   └── voice/
│   │       ├── config/
│   │       └── server/
│   ├── middleware/               # Shared gRPC interceptors (logging, panic recovery)
│   │   └── interceptors.go
│   ├── pb/                       # Generated Go stubs — gitignored, do not edit
│   │   ├── business/
│   │   ├── facility/
│   │   ├── intelligence/
│   │   ├── learning/
│   │   ├── nlp/
│   │   ├── security/
│   │   └── voice/
│   └── rest/                     # grpc-gateway custom error handler
├── gen/
│   └── swift/                    # Generated Swift stubs for the iOS client
├── clients/
│   ├── ios/JarvisClient/         # SwiftUI iOS client
│   └── android/                  # Android client
├── docker/
│   ├── jarvis/Dockerfile         # Multi-stage build → single static binary
│   └── docker-compose.yml        # jarvis + redis only
├── gateway/
│   └── docs/api-reference.md     # REST API reference
├── docs/openapi/                 # Generated OpenAPI spec — gitignored
├── buf.yaml                      # Buf lint / breaking-change config
├── buf.gen.yaml                  # Code generation config (Go → api/pb/, Swift → gen/swift/)
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
make proto        # runs buf generate → api/pb/
make proto-lint   # must pass before opening a PR
```

### 2. Check for breaking changes

```bash
make proto-breaking   # compares against main branch
```

Breaking changes to existing RPCs or message fields require a discussion in the PR. Additive changes (new fields, new RPCs) are generally fine.

### 3. Build and test locally

```bash
make build        # compiles bin/jarvis
make test         # go test ./... -race -cover
```

### 4. Run the full stack

```bash
make docker-up        # builds image and starts jarvis + redis
make docker-logs      # tail logs
make logs-jarvis      # tail jarvis service only
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
| Bidirectional | Ongoing dialogue between client and service |

---

## Adding a New Service

1. **Define the proto** under `proto/<domain>/<domain>.proto`
2. **Regenerate**: `make proto`
3. **Implement** the service under `api/internal/<service>/server/`
   - Follow the layout of an existing service (e.g., `api/internal/facility/server/`)
   - Config (if needed): `api/internal/<service>/config/`
4. **Register** in `api/cmd/grpc-server/server.go`:
   - Instantiate the server and call `RegisterXxxServiceServer(grpcSrv, srv)`
   - Call `RegisterXxxServiceHandlerServer(ctx, gwMux, srv)` for REST
   - Add the fully-qualified service name to `serviceNames` for health tracking
5. **No new Dockerfile or compose entry needed** — the service runs inside the single `jarvis` binary

If the new service needs to call another service in-process, follow the `nlpServerAdapter` pattern in `api/cmd/grpc-server/nlp_adapter.go` — wrap the `ServiceServer` to satisfy the `ServiceClient` interface rather than dialing over gRPC.

---

## Testing

- Unit tests live alongside the code they test (`foo_test.go` next to `foo.go`)
- Use the race detector: `make test` runs with `-race` by default
- Aim for coverage on business logic; generated code in `api/pb/` does not need tests
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
git checkout -b fix/voice-wake-word-threshold
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
fix(voice): correct wake word threshold default
```

### Pull requests

- One logical change per PR
- Proto changes and their implementation may be in the same PR
- All of the following must pass before merge:
  - `make proto-lint`
  - `make proto-breaking`
  - `make test`
- Attach the results of above to the PR as a text file
- Describe *why* the change is needed, not just what it does

---

## Make Targets Reference

Run `make help` to see all available targets with descriptions.

Key targets:

| Target | Description |
|---|---|
| `make proto` | Generate Go stubs from proto files (→ `api/pb/`) |
| `make proto-lint` | Lint proto files |
| `make proto-breaking` | Check for breaking changes vs main |
| `make proto-android` | Generate Kotlin/gRPC stubs via Gradle |
| `make build` | Build `bin/jarvis` |
| `make test` | Run all tests with race detector |
| `make docker-up` | Build image and start jarvis + redis |
| `make docker-down` | Stop all services |
| `make tidy` | Generate protos then run go mod tidy |
| `make clean` | Remove `bin/`, `api/pb/`, `docs/openapi/` |
