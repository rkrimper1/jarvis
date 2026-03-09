FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy pre-built proto stubs from the shared proto-gen image (built once, reused by all services)
COPY --from=jarvis-proto-gen /workspace/gen ./gen

COPY go.mod go.sum ./
RUN go mod download

COPY services/learning-service/ services/learning-service/

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /bin/learning-service \
    ./services/learning-service/cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/learning-service /learning-service
EXPOSE 50058
USER nonroot:nonroot
ENTRYPOINT ["/learning-service"]
