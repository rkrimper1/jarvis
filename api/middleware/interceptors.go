// Package middleware provides shared gRPC interceptors for the Jarvis gRPC server.
package middleware

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/google/uuid"
	"go.opencensus.io/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type originalRequestIDKey struct{}

// OriginalRequestIDFromCtx returns the interceptor-generated original request ID stored in ctx.
func OriginalRequestIDFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(originalRequestIDKey{}).(string)
	return v
}

// AddRequestAttributes annotates the active span with request_id and user_id from RequestMeta.
// Call this at the top of each gRPC handler after extracting req.Meta.
func AddRequestAttributes(ctx context.Context, requestID, userID string) {
	span := trace.FromContext(ctx)
	if span == nil {
		return
	}
	span.AddAttributes(
		trace.StringAttribute("request_id", requestID),
		trace.StringAttribute("user_id", userID),
	)
}

// UnaryTracing starts a root OpenCensus span for every unary RPC.
// It generates a stable original_request_id UUID and extracts x-request-id from
// incoming gRPC metadata, storing both as span attributes and on the context.
func UnaryTracing() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		origID := uuid.NewString()
		ctx = context.WithValue(ctx, originalRequestIDKey{}, origID)
		xReqID := requestIDFromCtx(ctx) // existing helper reads x-request-id from metadata

		ctx, span := trace.StartSpan(ctx, info.FullMethod)
		defer span.End()
		span.AddAttributes(
			trace.StringAttribute("original_request_id", origID),
			trace.StringAttribute("x_request_id", xReqID),
		)

		return handler(ctx, req)
	}
}

// StreamTracing starts a root OpenCensus span for every streaming RPC connection.
func StreamTracing() grpc.StreamServerInterceptor {
	return func(
		srv any,
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		ctx := ss.Context()
		origID := uuid.NewString()
		ctx = context.WithValue(ctx, originalRequestIDKey{}, origID)
		xReqID := requestIDFromCtx(ctx)

		ctx, span := trace.StartSpan(ctx, info.FullMethod)
		defer span.End()
		span.AddAttributes(
			trace.StringAttribute("original_request_id", origID),
			trace.StringAttribute("x_request_id", xReqID),
		)

		// Wrap the stream so handlers see the enriched context.
		return handler(srv, &tracedStream{ServerStream: ss, ctx: ctx})
	}
}

// tracedStream wraps grpc.ServerStream to inject the enriched context.
type tracedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *tracedStream) Context() context.Context { return s.ctx }

// UnaryLogger logs every unary RPC.
func UnaryLogger(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		code := codes.OK
		if err != nil {
			code = status.Code(err)
		}
		log.InfoContext(ctx, "unary rpc",
			slog.String("method", info.FullMethod),
			slog.String("code", code.String()),
			slog.Duration("duration", time.Since(start)),
			slog.String("request_id", requestIDFromCtx(ctx)),
		)
		return resp, err
	}
}

// UnaryRecovery catches panics in unary handlers.
func UnaryRecovery(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				log.ErrorContext(ctx, "panic recovered",
					slog.String("method", info.FullMethod),
					slog.Any("panic", r),
					slog.String("stack", string(debug.Stack())),
				)
				err = status.Errorf(codes.Internal, "internal error")
			}
		}()
		return handler(ctx, req)
	}
}

// StreamLogger logs stream RPC lifecycle events.
func StreamLogger(log *slog.Logger) grpc.StreamServerInterceptor {
	return func(
		srv any,
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		start := time.Now()
		log.Info("stream rpc started",
			slog.String("method", info.FullMethod),
			slog.Bool("client_stream", info.IsClientStream),
			slog.Bool("server_stream", info.IsServerStream),
		)
		err := handler(srv, ss)
		code := codes.OK
		if err != nil {
			code = status.Code(err)
		}
		log.Info("stream rpc finished",
			slog.String("method", info.FullMethod),
			slog.String("code", code.String()),
			slog.Duration("duration", time.Since(start)),
		)
		return err
	}
}

// StreamRecovery catches panics in streaming handlers.
func StreamRecovery(log *slog.Logger) grpc.StreamServerInterceptor {
	return func(
		srv any,
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) (err error) {
		defer func() {
			if r := recover(); r != nil {
				log.Error("stream panic recovered",
					slog.String("method", info.FullMethod),
					slog.Any("panic", r),
					slog.String("stack", string(debug.Stack())),
				)
				err = status.Errorf(codes.Internal, "internal error")
			}
		}()
		return handler(srv, ss)
	}
}

func requestIDFromCtx(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	vals := md.Get("x-request-id")
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}
