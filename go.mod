module github.com/rkrimper1/jarvis

go 1.23

// toolchain allows developers running Go 1.25+ locally to build without
// rewriting this file. Docker builds use golang:1.23-alpine which satisfies
// the minimum declared above.
toolchain go1.23.0

require (
	github.com/google/uuid v1.6.0
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.20.0
	google.golang.org/grpc v1.78.0
	google.golang.org/protobuf v1.36.11
)

require google.golang.org/genproto/googleapis/api v0.0.0-20260203192932-546029d2fa20

require (
	cloud.google.com/go/speech v1.30.0 // indirect
	cloud.google.com/go/texttospeech v1.16.0 // indirect
	golang.org/x/net v0.49.0 // indirect
	golang.org/x/sys v0.40.0 // indirect
	golang.org/x/text v0.33.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260203192932-546029d2fa20 // indirect
)
