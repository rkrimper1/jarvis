FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY api/ api/
COPY proto/ proto/

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /bin/jarvis \
    ./api/cmd/grpc-server

# Build standalone pprof binary — avoids needing the full Go toolchain at runtime
RUN CGO_ENABLED=0 GOOS=linux go install github.com/google/pprof@latest

FROM debian:12-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates graphviz \
 && rm -rf /var/lib/apt/lists/*

COPY --from=builder /bin/jarvis /jarvis
COPY --from=builder /go/bin/pprof /usr/local/bin/pprof

EXPOSE 50051 8080
ENTRYPOINT ["/jarvis"]
