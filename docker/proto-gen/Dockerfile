# This image is built ONCE and shared by all service images.
# It runs buf dep update + buf generate a single time, avoiding BSR rate limits.
FROM bufbuild/buf:1.66.0

WORKDIR /workspace
COPY buf.yaml buf.gen.yaml ./
COPY proto/ proto/
RUN mkdir -p gen docs/openapi
RUN buf dep update && buf generate
# Remove google/api stubs — provided by google.golang.org/genproto, not our module
RUN rm -rf gen/google gen/grpc gen/protoc_gen_openapiv2
