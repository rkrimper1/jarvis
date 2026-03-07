FROM golang:1.22-alpine AS builder

WORKDIR /app

# Copy pre-built proto stubs from the shared proto-gen image (built once, reused by all services)
COPY --from=jarvis-proto-gen /workspace/gen ./gen

COPY go.mod go.sum ./
RUN go mod download

COPY services/intelligence-service/ services/intelligence-service/

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /bin/intelligence-service \
    ./services/intelligence-service/cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/intelligence-service /intelligence-service
EXPOSE 50056
USER nonroot:nonroot
ENTRYPOINT ["/intelligence-service"]
