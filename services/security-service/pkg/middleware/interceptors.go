// Package middleware provides gRPC interceptors for the Security service.
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

func UnaryLogger(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
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

func UnaryRecovery(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
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

func StreamLogger(log *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()
		log.Info("stream rpc started", slog.String("method", info.FullMethod))
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

func StreamRecovery(log *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
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
	if vals := md.Get("x-request-id"); len(vals) > 0 {
		return vals[0]
	}
	return ""
}
