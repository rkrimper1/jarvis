FROM golang:1.22-alpine AS builder

WORKDIR /app

# Copy pre-built proto stubs from the shared proto-gen image (built once, reused by all services)
COPY --from=jarvis-proto-gen /workspace/gen ./gen

COPY go.mod go.sum ./
RUN go mod download

COPY services/business-ops-service/ services/business-ops-service/

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /bin/business-ops-service \
    ./services/business-ops-service/cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/business-ops-service /business-ops-service
EXPOSE 50057
USER nonroot:nonroot
ENTRYPOINT ["/business-ops-service"]
