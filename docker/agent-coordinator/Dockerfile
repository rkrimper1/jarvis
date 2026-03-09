FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy pre-built proto stubs from the shared proto-gen image (built once, reused by all services)
COPY --from=jarvis-proto-gen /workspace/gen ./gen

COPY go.mod go.sum ./
RUN go mod download

COPY services/agent-coordinator/ services/agent-coordinator/

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /bin/agent-coordinator \
    ./services/agent-coordinator/cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/agent-coordinator /agent-coordinator
EXPOSE 50053
USER nonroot:nonroot
ENTRYPOINT ["/agent-coordinator"]
