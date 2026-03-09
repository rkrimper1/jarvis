FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy pre-built proto stubs from the shared proto-gen image (built once, reused by all services)
COPY --from=jarvis-proto-gen /workspace/gen ./gen

COPY go.mod go.sum ./
RUN go mod download

COPY services/voice-service/ services/voice-service/

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /bin/voice-service \
    ./services/voice-service/cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/voice-service /voice-service
EXPOSE 50059
USER nonroot:nonroot
ENTRYPOINT ["/voice-service"]
