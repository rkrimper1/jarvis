module github.com/rkrimper1/jarvis

go 1.22

// Pin all deps to versions that are compatible with Go 1.22.
// Do NOT run go mod tidy without go.sum present — it will resolve to
// latest versions which may require Go 1.25+. Use setup.sh which pins
// exact versions via go get before tidying.
require (
	github.com/google/uuid v1.6.0
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.20.0
	google.golang.org/grpc v1.65.0
	google.golang.org/protobuf v1.34.2
)

require google.golang.org/genproto/googleapis/api v0.0.0-20240617180043-68d350f18fd4

require (
	golang.org/x/net v0.26.0 // indirect
	golang.org/x/sys v0.21.0 // indirect
	golang.org/x/text v0.16.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240617180043-68d350f18fd4 // indirect
)
