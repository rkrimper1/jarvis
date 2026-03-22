package server

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	commandv1 "github.com/rkrimper1/jarvis/api/pb/command"
	commonv1 "github.com/rkrimper1/jarvis/api/pb/common"
	"github.com/rkrimper1/jarvis/api/internal/profiler"
)

type CommandServer struct {
	commandv1.UnimplementedCommandServiceServer
	hp  *profiler.HeapProfiler
	log *slog.Logger
}

func New(hp *profiler.HeapProfiler, log *slog.Logger) *CommandServer {
	return &CommandServer{hp: hp, log: log}
}

func (s *CommandServer) RequestMemoryProfile(
	ctx context.Context,
	req *commandv1.RequestMemoryProfileRequest,
) (*commandv1.RequestMemoryProfileResponse, error) {
	profPath, gifPath, err := s.hp.Capture()
	if err != nil {
		s.log.Error("command: memory profile failed", slog.Any("err", err))
		return nil, status.Errorf(codes.Internal, "capture failed: %v", err)
	}

	return &commandv1.RequestMemoryProfileResponse{
		Meta: &commonv1.ResponseMeta{
			RequestId: req.GetMeta().GetRequestId(),
			Success:   true,
			Timestamp: timestamppb.New(time.Now()),
		},
		ProfPath: profPath,
		GifPath:  gifPath,
	}, nil
}
