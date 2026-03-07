FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY --from=jarvis-proto-gen /workspace/gen ./gen

COPY go.mod go.sum ./
RUN go mod download

COPY gateway/ gateway/

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /bin/gateway \
    ./gateway/cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/gateway /gateway
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/gateway"]
