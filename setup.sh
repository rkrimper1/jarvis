#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────
#  JARVIS — Project Bootstrap
#  Run once after cloning/extracting. Installs buf + Go protoc plugins,
#  generates proto stubs into gen/, then runs go mod tidy.
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

command -v go >/dev/null 2>&1 || die "Go is not installed. Install from https://go.dev/dl/"
info "Go version: $(go version | awk '{print $3}' | sed 's/go//')"

export PATH="$(go env GOPATH)/bin:$PATH"
export PATH="$HOME/.local/bin:$PATH"
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

install_plugin "google.golang.org/protobuf/cmd/protoc-gen-go@v1.34.2"                              protoc-gen-go
install_plugin "google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.4.0"                              protoc-gen-go-grpc
install_plugin "github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@v2.20.0"         protoc-gen-grpc-gateway
install_plugin "github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2@v2.20.0"            protoc-gen-openapiv2

# ── 3. Generate proto stubs ───────────────────────────────────────────
info "Generating proto stubs..."
rm -rf gen docs/openapi
mkdir -p gen docs/openapi

buf dep update 2>&1 | grep -v "^WARN" || true
buf generate 2>&1 | grep -v "^WARN" || true

# Remove google/api stubs emitted by buf — these live in google.golang.org/genproto
if [ -d "gen/google" ]; then
  warn "Removing gen/google/ (provided by google.golang.org/genproto)"
  rm -rf gen/google
fi
for dir in gen/grpc gen/protoc_gen_openapiv2; do
  [ -d "$dir" ] && rm -rf "$dir" && warn "Removed $dir/"
done

success "Proto stubs generated in gen/"

# ── 4. Pin deps to Go 1.22-compatible versions, then tidy ────────────
info "Pinning Go 1.22-compatible dependency versions..."
# Pin before tidy so go mod tidy doesn't resolve to newer incompatible versions
go get google.golang.org/grpc@v1.65.0                                                  2>&1 | grep -v "^warning:" || true
go get google.golang.org/protobuf@v1.34.2                                              2>&1 | grep -v "^warning:" || true
go get github.com/grpc-ecosystem/grpc-gateway/v2@v2.20.0                               2>&1 | grep -v "^warning:" || true
go get google.golang.org/genproto/googleapis/api@v0.0.0-20240617180043-68d350f18fd4   2>&1 | grep -v "^warning:" || true
go get google.golang.org/genproto/googleapis/rpc@v0.0.0-20240617180043-68d350f18fd4   2>&1 | grep -v "^warning:" || true

info "Running go mod tidy..."
go mod tidy 2>&1 | grep -v "^warning:" || true
success "go.sum created"

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

# ── 6. Done ───────────────────────────────────────────────────────────
echo ""
echo -e "${GREEN}════════════════════════════════════════════════${NC}"
echo -e "${GREEN}  JARVIS project setup complete!                ${NC}"
echo -e "${GREEN}════════════════════════════════════════════════${NC}"
echo ""
echo "  Run tests:    go test ./..."
echo "  Docker:       cd docker && sudo docker-compose build && sudo docker-compose up -d"
echo "  With make:    sudo make docker-up"
echo "  Android:      make proto-android  (requires Android SDK)"
echo ""
