FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy pre-built proto stubs from the shared proto-gen image (built once, reused by all services)
COPY --from=jarvis-proto-gen /workspace/gen ./gen

COPY go.mod go.sum ./
RUN go mod download

COPY services/facility-service/ services/facility-service/

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /bin/facility-service \
    ./services/facility-service/cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/facility-service /facility-service
EXPOSE 50055
USER nonroot:nonroot
ENTRYPOINT ["/facility-service"]
