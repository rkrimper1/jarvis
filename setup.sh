#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────
#  JARVIS — Project Bootstrap
#  Run once after cloning/extracting. Installs buf + Go protoc plugins,
#  generates proto stubs into api/pb/, then runs go mod tidy.
#  Prerequisites: Go 1.22+, internet access
# ─────────────────────────────────────────────────────────────────────
set -euo pipefail

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
info()    { echo -e "${CYAN}▶ $*${NC}"; }
success() { echo -e "${GREEN}✔ $*${NC}"; }
warn()    { echo -e "${YELLOW}⚠ $*${NC}"; }
die()     { echo -e "${RED}✘ $*${NC}"; exit 1; }

# ── 0. System packages ────────────────────────────────────────────────
if ! command -v xdg-open >/dev/null 2>&1; then
  info "Installing xdg-utils (provides xdg-open)..."
  sudo apt-get install -y xdg-utils 2>&1 | grep -E "^(Setting up|E:)" || true
  success "xdg-utils installed"
else
  success "xdg-open already installed"
fi

if ! command -v dot >/dev/null 2>&1; then
  info "Installing graphviz (required for pprof GIF rendering)..."
  sudo apt-get install -y graphviz 2>&1 | grep -E "^(Setting up|E:)" || true
  success "graphviz installed"
else
  success "graphviz already installed"
fi

if ! command -v sqlite3 >/dev/null 2>&1; then
  info "Installing sqlite3..."
  sudo apt-get install -y sqlite3 2>&1 | grep -E "^(Setting up|E:)" || true
  success "sqlite3 installed"
else
  success "sqlite3 already installed"
fi

if ! command -v unzip >/dev/null 2>&1; then
  info "Installing unzip..."
  sudo apt-get install -y unzip 2>&1 | grep -E "^(Setting up|E:)" || true
  success "unzip installed"
else
  success "unzip already installed"
fi

if ! command -v git >/dev/null 2>&1; then
  info "Installing git..."
  sudo apt-get install -y git 2>&1 | grep -E "^(Setting up|E:)" || true
  success "git installed"
else
  success "git already installed"
fi

if ! command -v make >/dev/null 2>&1; then
  info "Installing make..."
  sudo apt-get install -y make 2>&1 | grep -E "^(Setting up|E:)" || true
  success "make installed"
else
  success "make already installed"
fi

# ── 0a. Go ────────────────────────────────────────────────────────────
if command -v go >/dev/null 2>&1; then
  success "Go already installed: $(go version | awk '{print $3}')"
else
  info "Installing Go 1.26..."
  GO_VERSION="1.26.0"
  GO_ARCH="$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')"
  GO_TAR="go${GO_VERSION}.linux-${GO_ARCH}.tar.gz"
  curl -fsSL "https://go.dev/dl/${GO_TAR}" -o "/tmp/${GO_TAR}"
  sudo rm -rf /usr/local/go
  sudo tar -C /usr/local -xzf "/tmp/${GO_TAR}"
  rm "/tmp/${GO_TAR}"
  if ! grep -q '/usr/local/go/bin' "$HOME/.bashrc"; then
    echo 'export PATH="/usr/local/go/bin:$PATH"' >> "$HOME/.bashrc"
  fi
  export PATH="/usr/local/go/bin:$PATH"
  if ! grep -q 'GOROOT' "$HOME/.bashrc"; then
    echo 'export GOROOT="/usr/local/go"' >> "$HOME/.bashrc"
  fi
  export GOROOT="/usr/local/go"
  success "Go installed: $(go version | awk '{print $3}')"
fi

# ── 0b. Docker + Docker Compose ───────────────────────────────────────
if command -v docker >/dev/null 2>&1; then
  success "Docker already installed: $(docker --version | awk '{print $3}' | tr -d ',')"
else
  info "Installing Docker..."
  curl -fsSL https://get.docker.com | sudo sh
  sudo usermod -aG docker "$USER"
  success "Docker installed — re-login or run 'newgrp docker' to use without sudo"
fi

if docker compose version >/dev/null 2>&1; then
  success "Docker Compose already installed: $(docker compose version --short)"
else
  info "Installing Docker Compose plugin..."
  DOCKER_CONFIG="${DOCKER_CONFIG:-$HOME/.docker}"
  mkdir -p "$DOCKER_CONFIG/cli-plugins"
  COMPOSE_VERSION="v2.36.0"
  COMPOSE_ARCH="$(uname -m | sed 's/x86_64/x86_64/;s/aarch64/aarch64/')"
  curl -fsSL "https://github.com/docker/compose/releases/download/${COMPOSE_VERSION}/docker-compose-linux-${COMPOSE_ARCH}" \
    -o "$DOCKER_CONFIG/cli-plugins/docker-compose"
  chmod +x "$DOCKER_CONFIG/cli-plugins/docker-compose"
  success "Docker Compose installed"
fi

if command -v android-studio >/dev/null 2>&1 || command -v studio >/dev/null 2>&1; then
  success "Android Studio already installed"
elif command -v snap >/dev/null 2>&1; then
  info "Installing Android Studio via snap..."
  sudo snap install android-studio --classic 2>&1 | grep -E "^(android-studio|error)" || true
  success "Android Studio installed"
else
  warn "snap not available — skipping Android Studio install (install manually from https://developer.android.com/studio)"
fi

ANDROID_HOME="${ANDROID_HOME:-$HOME/Android/Sdk}"
if [ -d "$ANDROID_HOME/platforms" ] && [ -d "$ANDROID_HOME/build-tools" ]; then
  success "Android SDK already installed at $ANDROID_HOME"
else
  info "Installing Android SDK (API 35, build-tools 35.0.0)..."
  CMDLINE_TOOLS_ZIP="/tmp/cmdline-tools.zip"
  CMDLINE_TOOLS_VERSION="14742923"
  mkdir -p "$ANDROID_HOME/cmdline-tools"
  curl -fsSL "https://dl.google.com/android/repository/commandlinetools-linux-${CMDLINE_TOOLS_VERSION}_latest.zip" \
    -o "$CMDLINE_TOOLS_ZIP"
  unzip -q "$CMDLINE_TOOLS_ZIP" -d /tmp/cmdline-tools-extract
  mv /tmp/cmdline-tools-extract/cmdline-tools "$ANDROID_HOME/cmdline-tools/latest"
  rm -rf "$CMDLINE_TOOLS_ZIP" /tmp/cmdline-tools-extract

  export PATH="$ANDROID_HOME/cmdline-tools/latest/bin:$PATH"
  yes | sdkmanager --licenses > /dev/null 2>&1 || true
  sdkmanager "platform-tools" "platforms;android-35" "build-tools;35.0.0"

  # Write local.properties for Gradle
  echo "sdk.dir=$ANDROID_HOME" > clients/android/local.properties

  # Persist ANDROID_HOME in ~/.bashrc if not already set
  if ! grep -q "ANDROID_HOME" "$HOME/.bashrc"; then
    cat >> "$HOME/.bashrc" << BASHRC

# Android SDK
export ANDROID_HOME="$ANDROID_HOME"
export PATH="\$ANDROID_HOME/cmdline-tools/latest/bin:\$ANDROID_HOME/platform-tools:\$PATH"
BASHRC
  fi

  success "Android SDK installed at $ANDROID_HOME"
fi

info "Go version: $(go version | awk '{print $3}' | sed 's/go//')"

export PATH="$(go env GOPATH)/bin:$PATH"
export PATH="$HOME/.local/bin:$PATH"
if ! grep -q 'GOTOOLCHAIN' "$HOME/.bashrc"; then
  echo 'export GOTOOLCHAIN=local' >> "$HOME/.bashrc"
fi
export GOTOOLCHAIN=local

# ── 1. buf ────────────────────────────────────────────────────────────
if command -v buf >/dev/null 2>&1; then
  success "buf already installed: $(buf --version)"
else
  info "Installing buf CLI v1.66.0..."
  BUF_VERSION="1.66.0"
  BUF_URL="https://github.com/bufbuild/buf/releases/download/v${BUF_VERSION}/buf-$(uname -s)-$(uname -m)"
  mkdir -p "$HOME/.local/bin"
  curl -fsSL "$BUF_URL" -o "$HOME/.local/bin/buf" && chmod +x "$HOME/.local/bin/buf"
  success "buf installed"
fi

# ── 2. Go protoc plugins ──────────────────────────────────────────────
install_plugin() {
  local pkg=$1 bin=$2
  if command -v "$bin" >/dev/null 2>&1; then
    success "$bin already installed"
  else
    info "Installing $bin..."
    go install "$pkg" 2>&1 | grep -v "^warning:" || true
    success "$bin installed"
  fi
}

install_plugin "google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11"                             protoc-gen-go
install_plugin "google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1"                             protoc-gen-go-grpc
install_plugin "github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@v2.20.0"        protoc-gen-grpc-gateway
install_plugin "github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2@v2.20.0"           protoc-gen-openapiv2
install_plugin "github.com/fullstorydev/grpcurl/cmd/grpcurl@latest"                               grpcurl

# ── 3. Generate proto stubs ───────────────────────────────────────────
info "Generating proto stubs..."
rm -rf api/pb docs/openapi
mkdir -p api/pb docs/openapi

buf dep update 2>&1 | grep -v "^WARN" || true
buf generate 2>&1 | grep -v "^WARN" || true

success "Proto stubs generated in api/pb/"

# ── 4. Tidy modules ───────────────────────────────────────────────────
info "Running go mod tidy..."
go mod tidy 2>&1 | grep -v "^warning:" || true
success "go.sum updated"

# ── 5. Android Gradle wrapper ─────────────────────────────────────────
ANDROID_DIR="clients/android"
WRAPPER_JAR="$ANDROID_DIR/gradle/wrapper/gradle-wrapper.jar"
WRAPPER_PROPS="$ANDROID_DIR/gradle/wrapper/gradle-wrapper.properties"
GRADLEW="$ANDROID_DIR/gradlew"

if [ -f "$WRAPPER_JAR" ] && [ -x "$GRADLEW" ]; then
  success "Android Gradle wrapper already present"
else
  info "Installing Android Gradle wrapper (Gradle 8.9)..."
  mkdir -p "$ANDROID_DIR/gradle/wrapper"

  # wrapper shell script
  curl -fsSL "https://raw.githubusercontent.com/gradle/gradle/v8.9.0/gradlew" \
    -o "$GRADLEW" && chmod +x "$GRADLEW"

  # wrapper bat (Windows)
  curl -fsSL "https://raw.githubusercontent.com/gradle/gradle/v8.9.0/gradlew.bat" \
    -o "$ANDROID_DIR/gradlew.bat"

  # wrapper jar
  curl -fsSL "https://github.com/gradle/gradle/raw/v8.9.0/gradle/wrapper/gradle-wrapper.jar" \
    -o "$WRAPPER_JAR"

  # wrapper properties
  cat > "$WRAPPER_PROPS" <<'PROPS'
distributionBase=GRADLE_USER_HOME
distributionPath=wrapper/dists
distributionUrl=https\://services.gradle.org/distributions/gradle-8.9-bin.zip
networkTimeout=10000
validateDistributionUrl=true
zipStoreBase=GRADLE_USER_HOME
zipStorePath=wrapper/dists
PROPS

  success "Android Gradle wrapper installed"
fi

# ── 6. Unified JARVIS database (jarvis.db) ───────────────────────────
JARVIS_DIR="$HOME/.jarvis"
JARVIS_DB="${JARVIS_DB_PATH:-$JARVIS_DIR/jarvis.db}"
mkdir -p "$JARVIS_DIR"

if [ -f "$JARVIS_DB" ]; then
  success "Jarvis DB already exists at $JARVIS_DB"
else
  info "Creating unified jarvis.db at $JARVIS_DB..."
  # Only enable WAL here. The Go server applies the full schema on startup
  # via CREATE TABLE IF NOT EXISTS — including FTS5 tables that require
  # the modernc.org/sqlite driver (the system sqlite3 CLI may lack FTS5).
  sqlite3 "$JARVIS_DB" "PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;"
  success "Jarvis DB created at $JARVIS_DB (schema applied by server on first start)"
fi

# ── 7. (Renumbered) ──────────────────────────────────────────────────
# Sections 7-8 were the old per-service databases (users.db, tasks.db, analytics.db).
# They are superseded by the unified jarvis.db above.
# Run scripts/migrate-to-jarvis-db.sh if you have existing data to migrate.

# ── 9. Face analysis — cascade file + output dir ─────────────────────
FACE_CASCADE="${FACE_CASCADE_PATH:-$JARVIS_DIR/facefinder}"
FACE_DIR="${FACE_OUTPUT_DIR:-$JARVIS_DIR/faces}"

mkdir -p "$FACE_DIR"
success "Face output dir ready at $FACE_DIR"

if [ -f "$FACE_CASCADE" ]; then
  success "pigo face cascade already exists at $FACE_CASCADE"
else
  info "Downloading pigo facefinder cascade to $FACE_CASCADE..."
  curl -fsSL \
    "https://raw.githubusercontent.com/esimov/pigo/master/cascade/facefinder" \
    -o "$FACE_CASCADE" \
    && success "pigo cascade downloaded" \
    || warn "Failed to download pigo cascade — face analysis will be unavailable until $FACE_CASCADE exists"
fi

# ── 10. Node.js + Web client deps ────────────────────────────────────
if command -v node >/dev/null 2>&1 && node --version | grep -qE '^v(18|20|22)'; then
  success "Node.js already installed: $(node --version)"
else
  info "Installing Node.js 22 LTS..."
  curl -fsSL https://deb.nodesource.com/setup_22.x | sudo -E bash - 2>&1 | grep -E "^(##|E:)" || true
  sudo apt-get install -y nodejs 2>&1 | grep -E "^(Setting up|E:)" || true
  success "Node.js installed: $(node --version)"
fi

WEB_DIR="clients/web"
if [ -f "$WEB_DIR/package.json" ]; then
  info "Installing web client npm dependencies..."
  cd "$WEB_DIR" && npm install 2>&1 | tail -3
  cd - > /dev/null
  success "Web client dependencies installed"

  # TF.js + COCO-SSD — required for the Security Threat AR overlay (client-side
  # object detection). Listed in package.json so npm install above covers them,
  # but we verify here to surface any missing-peer issues early.
  info "Verifying TF.js + COCO-SSD packages..."
  cd "$WEB_DIR"
  if node -e "require('@tensorflow/tfjs'); require('@tensorflow-models/coco-ssd')" 2>/dev/null; then
    success "TF.js + COCO-SSD ready"
  else
    info "Installing TF.js + COCO-SSD explicitly..."
    npm install @tensorflow/tfjs @tensorflow-models/coco-ssd 2>&1 | tail -3
    success "TF.js + COCO-SSD installed"
  fi
  cd - > /dev/null
fi

# ── 11. VS Code extensions ───────────────────────────────────────────
if command -v code >/dev/null 2>&1; then
  install_ext() {
    code --install-extension "$1" --force 2>&1 | grep -E "(installed|already installed|error)" || true
  }
  info "Installing VS Code extensions..."
  install_ext "golang.go"
  install_ext "svelte.svelte-vscode"
  install_ext "drblury.protobuf-vsc"
  install_ext "esbenp.prettier-vscode"
  install_ext "ms-vscode.remote-explorer"
  success "VS Code extensions installed"
else
  warn "VS Code CLI (code) not on PATH — skipping extension install"
fi

# ── 12. Done ──────────────────────────────────────────────────────────
echo ""
echo -e "${GREEN}════════════════════════════════════════════════${NC}"
echo -e "${GREEN}  JARVIS project setup complete!                ${NC}"
echo -e "${GREEN}════════════════════════════════════════════════${NC}"
echo ""
echo "  Run tests:    go test ./..."
echo "  Docker:       make docker-up"
echo "  Run binary:   make run"
echo "  Web UI:       make web-dev  → http://localhost:5173"
echo "  Android:      make proto-android  (requires Android SDK)"
echo ""
