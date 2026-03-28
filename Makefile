# ═══════════════════════════════════════════════════════════════════
#  J.A.R.V.I.S. — Makefile
#  Just A Rather Very Intelligent System
# ═══════════════════════════════════════════════════════════════════

.PHONY: all proto proto-lint proto-breaking proto-android build run test \
        docker-build docker-up docker-down docker-logs docker-ps \
        tidy clean ios-open ios-clean android-open \
        web-dev web-build web-preview help

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
ENV_FILE      := /home/vagrant/credentials/jarvis/.env
ENV_FLAG      := $(shell [ -f $(ENV_FILE) ] && echo "--env-file $(ENV_FILE)")

# ── Proto ───────────────────────────────────────────────────────────

proto:          ## Generate Go code from all .proto files (requires buf)
	buf generate

proto-lint:     ## Lint all proto files
	buf lint

proto-breaking: ## Check for breaking proto changes vs main branch
	buf breaking --against '.git#branch=main'

# ── Build ────────────────────────────────────────────────────────────

build:          ## Build the single Jarvis binary
	@mkdir -p bin
	@echo "▶ Building jarvis..."
	@go build -o bin/jarvis ./api/cmd/grpc-server

ENV_PATH ?= /home/vagrant/credentials/jarvis/.env

run:            ## Build then run the binary directly (loads $$ENV_PATH if present)
	@$(MAKE) build
	@if [ -f "$(ENV_PATH)" ]; then \
		export $$(grep -v '^#' "$(ENV_PATH)" | grep -v '^$$' | xargs) && ./bin/jarvis; \
	else \
		./bin/jarvis; \
	fi

# ── Test ─────────────────────────────────────────────────────────────

test:           ## Run all tests with race detector
	go test ./... -v -race -cover

test-short:     ## Run tests without -v (faster CI output)
	go test ./... -race -cover

test-voice:     ## Run voice tests only
	go test ./api/internal/voice/... -v -race -count=1 -timeout=60s

# ── Docker ───────────────────────────────────────────────────────────

ENV_FILE := $(wildcard $(ENV_PATH))
ENV_FLAG  := $(if $(ENV_FILE),--env-file $(ENV_PATH))

docker-build:   ## Build the Jarvis Docker image
	$(DC) $(ENV_FLAG) -f $(COMPOSE_FILE) build

docker-up:      ## Build image then start jarvis + redis in background
	$(DC) $(ENV_FLAG) -f $(COMPOSE_FILE) build
	$(DC) $(ENV_FLAG) -f $(COMPOSE_FILE) up -d

docker-up-fg:   ## Build image then start in foreground (shows logs)
	$(DC) $(ENV_FLAG) -f $(COMPOSE_FILE) build
	$(DC) $(ENV_FLAG) -f $(COMPOSE_FILE) up

docker-down:    ## Stop and remove all containers
	$(DC) -f $(COMPOSE_FILE) down

docker-down-v:  ## Stop containers AND remove volumes
	$(DC) -f $(COMPOSE_FILE) down -v

docker-logs:    ## Tail logs
	$(DC) -f $(COMPOSE_FILE) logs -f

docker-ps:      ## Show running container status
	$(DC) -f $(COMPOSE_FILE) ps

docker-restart: ## Restart containers without rebuilding
	$(DC) -f $(COMPOSE_FILE) restart

logs-%:         ## Tail logs for a specific service, e.g. make logs-jarvis
	$(DC) -f $(COMPOSE_FILE) logs -f $*

restart-%:      ## Restart a specific service, e.g. make restart-jarvis
	$(DC) -f $(COMPOSE_FILE) restart $*

# ── Utilities ────────────────────────────────────────────────────────

setup:          ## First-time setup: install buf, generate protos, tidy modules
	bash setup.sh

tidy: proto     ## Generate protos then tidy Go modules (api/pb/ must exist first)
	go mod tidy
	go mod verify

clean:          ## Remove compiled binaries and generated code
	rm -rf bin/ api/pb/ docs/openapi/

compose-version: ## Show which Docker Compose version is being used
	@echo "Using: $(DC)"
	@$(DC) version

# ── Open helper (macOS: open, Linux: xdg-open / studio) ──────────────

UNAME := $(shell uname)
ifeq ($(UNAME),Darwin)
  OPEN := open
else
  # Prefer Android Studio CLI on Linux; fall back to xdg-open
  OPEN := $(shell command -v android-studio 2>/dev/null || command -v studio 2>/dev/null || command -v xdg-open 2>/dev/null || echo open)
endif

# ── iOS Client ────────────────────────────────────────────────────────

IOS_PROJECT := clients/ios/JarvisClient/JarvisClient.xcodeproj

ios-open:       ## Generate protos then open the Xcode project
	@$(MAKE) proto
	$(OPEN) $(IOS_PROJECT)

ios-clean:      ## Remove Swift generated stubs
	rm -rf gen/swift/

# ── Android Client ────────────────────────────────────────────────────

ANDROID_PROJECT := clients/android

proto-android:  ## Generate Kotlin/gRPC stubs via Gradle
	cd $(ANDROID_PROJECT) && ./gradlew generateDebugProto

android-open:   ## Generate Android proto stubs then open in Android Studio
	@$(MAKE) proto-android
	@if [ -z "$$DISPLAY" ] && [ "$(UNAME)" != "Darwin" ]; then \
		echo ""; \
		echo "  Proto stubs generated successfully."; \
		echo ""; \
		echo "  No display detected (headless VM). To open Android Studio:"; \
		echo "    • SSH with X11 forwarding:  ssh -X vagrant@<host>  then  make android-open"; \
		echo "    • Or open the project directly on your host machine:  $(ANDROID_PROJECT)"; \
		echo ""; \
	elif command -v android-studio >/dev/null 2>&1; then \
		android-studio $(ANDROID_PROJECT); \
	elif command -v studio >/dev/null 2>&1; then \
		studio $(ANDROID_PROJECT); \
	elif [ "$(UNAME)" = "Darwin" ]; then \
		open $(ANDROID_PROJECT); \
	else \
		echo "Android Studio not found. Install it with: sudo snap install android-studio --classic"; \
	fi

# ── Web Client ────────────────────────────────────────────────────────

WEB_DIR := clients/web

web-dev:        ## Start the SvelteKit HUD client in dev mode (hot reload, proxies :8080)
	cd $(WEB_DIR) && npm run dev

web-build:      ## Build the SvelteKit HUD client for production
	cd $(WEB_DIR) && npm run build

web-preview:    ## Preview the production build locally
	cd $(WEB_DIR) && npm run preview

# ── Help ──────────────────────────────────────────────────────────────

help:           ## Show this help message
	@echo ""
	@echo "  J.A.R.V.I.S. — Available make targets"
	@echo ""
	@grep -E '^[a-zA-Z_%\-]+:.*?##' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "  Compose command: $(DC)"
	@echo ""
