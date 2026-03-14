FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY api/ api/
COPY proto/ proto/

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /bin/jarvis \
    ./api/cmd/grpc-server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/jarvis /jarvis
EXPOSE 50051 8080
USER nonroot:nonroot
ENTRYPOINT ["/jarvis"]
