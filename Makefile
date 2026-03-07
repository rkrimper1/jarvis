# ═══════════════════════════════════════════════════════════════════
#  J.A.R.V.I.S. — Makefile
#  Just A Rather Very Intelligent System
# ═══════════════════════════════════════════════════════════════════

.PHONY: all proto proto-lint proto-breaking build test \
        docker-build docker-up docker-down docker-logs docker-ps \
        gateway tidy clean ios-open ios-clean help

SERVICES := nlp-service security-service agent-coordinator \
            hardware-service facility-service intelligence-service \
            business-ops-service learning-service voice-service gateway

# ── Docker Compose command ────────────────────────────────────────────
# Auto-detect Compose V2 plugin (docker compose) vs V1 standalone (docker-compose).
# Override by running: make docker-up DC="docker compose"
COMPOSE_V2 := $(shell docker compose version > /dev/null 2>&1 && echo yes || echo no)
ifeq ($(COMPOSE_V2),yes)
  DC ?= docker compose
else
  DC ?= docker-compose
endif

COMPOSE_FILE := docker/docker-compose.yml

# ── Proto ───────────────────────────────────────────────────────────

proto:          ## Generate Go code from all .proto files (requires buf)
	buf generate

proto-lint:     ## Lint all proto files
	buf lint

proto-breaking: ## Check for breaking proto changes vs main branch
	buf breaking --against '.git#branch=main'

# ── Build ────────────────────────────────────────────────────────────

build:          ## Build all service binaries locally
	@mkdir -p bin
	@for svc in nlp-service security-service agent-coordinator \
	            hardware-service facility-service intelligence-service \
	            business-ops-service learning-service voice-service; do \
		echo "▶ Building $$svc..."; \
		go build -o bin/$$svc ./services/$$svc/cmd/server; \
	done
	@echo "▶ Building gateway..."
	@go build -o bin/gateway ./gateway/cmd/server

# ── Test ─────────────────────────────────────────────────────────────

test:           ## Run all tests with race detector
	go test ./... -v -race -cover

test-short:     ## Run tests without -v (faster CI output)
	go test ./... -race -cover

# ── Docker ───────────────────────────────────────────────────────────

docker-build:   ## Build all Docker images (without starting containers)
	docker build -t jarvis-proto-gen -f docker/proto-gen/Dockerfile .
	$(DC) -f $(COMPOSE_FILE) build

docker-up:      ## Build images then start all services in background
	docker build -t jarvis-proto-gen -f docker/proto-gen/Dockerfile .
	$(DC) -f $(COMPOSE_FILE) build
	$(DC) -f $(COMPOSE_FILE) up -d

docker-up-fg:   ## Build images then start all services in foreground (shows logs)
	docker build -t jarvis-proto-gen -f docker/proto-gen/Dockerfile .
	$(DC) -f $(COMPOSE_FILE) build
	$(DC) -f $(COMPOSE_FILE) up

docker-down:    ## Stop and remove all containers
	$(DC) -f $(COMPOSE_FILE) down

docker-down-v:  ## Stop containers AND remove volumes
	$(DC) -f $(COMPOSE_FILE) down -v

docker-logs:    ## Tail logs from all containers
	$(DC) -f $(COMPOSE_FILE) logs -f

docker-ps:      ## Show running container status
	$(DC) -f $(COMPOSE_FILE) ps

docker-restart: ## Restart all containers without rebuilding
	$(DC) -f $(COMPOSE_FILE) restart

# ── Individual service targets ────────────────────────────────────────

logs-%:         ## Tail logs for a specific service, e.g. make logs-gateway
	$(DC) -f $(COMPOSE_FILE) logs -f $*

restart-%:      ## Restart a specific service, e.g. make restart-nlp-service
	$(DC) -f $(COMPOSE_FILE) restart $*

# ── Utilities ────────────────────────────────────────────────────────

setup:          ## First-time setup: install buf, generate protos, tidy modules
	bash setup.sh

tidy: proto     ## Generate protos then tidy Go modules (gen/ must exist first)
	go mod tidy
	go mod verify

clean:          ## Remove compiled binaries and generated code
	rm -rf bin/ gen/ docs/openapi/

# ── iOS Client ────────────────────────────────────────────────────────

IOS_PROJECT := clients/ios/JarvisClient/JarvisClient.xcodeproj

ios-open:       ## Generate protos then open the Xcode project
	@$(MAKE) proto
	open $(IOS_PROJECT)

ios-clean:      ## Remove Swift generated stubs
	rm -rf gen/swift/


test-voice:     ## Run voice-service unit + integration tests only
	go test ./services/voice-service/... -v -race -count=1 -timeout=60s

compose-version: ## Show which Docker Compose version is being used
	@echo "Using: $(DC)"
	@$(DC) version

help:           ## Show this help message
	@echo ""
	@echo "  J.A.R.V.I.S. — Available make targets"
	@echo ""
	@grep -E '^[a-zA-Z_%\-]+:.*?##' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "  Compose command: $(DC)"
	@echo ""
