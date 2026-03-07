// Package middleware provides gRPC interceptors for the NLP service.
package middleware

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// UnaryLogger returns a gRPC unary interceptor that logs every RPC call.
func UnaryLogger(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		elapsed := time.Since(start)

		code := codes.OK
		if err != nil {
			code = status.Code(err)
		}

		log.InfoContext(ctx, "unary rpc",
			slog.String("method", info.FullMethod),
			slog.String("code", code.String()),
			slog.Duration("duration", elapsed),
			slog.String("request_id", requestIDFromCtx(ctx)),
		)
		return resp, err
	}
}

// UnaryRecovery returns a gRPC unary interceptor that recovers from panics.
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

// StreamLogger returns a gRPC stream interceptor that logs stream lifecycle.
func StreamLogger(log *slog.Logger) grpc.StreamServerInterceptor {
	return func(
		srv any,
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		start := time.Now()
		log.Info("stream rpc started", slog.String("method", info.FullMethod))

		err := handler(srv, ss)
		elapsed := time.Since(start)

		code := codes.OK
		if err != nil {
			code = status.Code(err)
		}

		log.Info("stream rpc finished",
			slog.String("method", info.FullMethod),
			slog.String("code", code.String()),
			slog.Duration("duration", elapsed),
		)
		return err
	}
}

// StreamRecovery returns a gRPC stream interceptor that recovers from panics.
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

// ── helpers ───────────────────────────────────────────────────────────

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
