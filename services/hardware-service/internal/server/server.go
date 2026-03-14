package server

import (
	"context"
	"io"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/rkrimper1/jarvis/api/pb/common"
	hardwarev1 "github.com/rkrimper1/jarvis/api/pb/hardware"
	"github.com/rkrimper1/jarvis/services/hardware-service/internal/config"
	"github.com/rkrimper1/jarvis/services/hardware-service/internal/device"
	"github.com/rkrimper1/jarvis/services/hardware-service/internal/telemetry"
)

type HardwareServer struct {
	hardwarev1.UnimplementedHardwareServiceServer
	cfg      *config.Config
	registry *device.Registry
	log      *slog.Logger
}

func New(cfg *config.Config, log *slog.Logger) *HardwareServer {
	return &HardwareServer{cfg: cfg, registry: device.New(), log: log}
}

func (s *HardwareServer) SendCommand(ctx context.Context, req *hardwarev1.SendCommandRequest) (*hardwarev1.SendCommandResponse, error) {
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	if req.DeviceId == "" || req.Command == "" {
		return nil, status.Error(codes.InvalidArgument, "device_id and command are required")
	}
	s.log.InfoContext(ctx, "SendCommand", slog.String("device_id", req.DeviceId), slog.String("command", req.Command))

	detail, err := s.registry.ExecuteCommand(req.DeviceId, req.Command, req.Params)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%v", err)
	}
	return &hardwarev1.SendCommandResponse{
		Meta:            metaOK(req.Meta.RequestId),
		DeviceId:        req.DeviceId,
		CommandExecuted: req.Command,
		ResultDetail:    detail,
	}, nil
}

func (s *HardwareServer) RunDiagnostics(ctx context.Context, req *hardwarev1.RunDiagnosticsRequest) (*hardwarev1.RunDiagnosticsResponse, error) {
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	s.log.InfoContext(ctx, "RunDiagnostics", slog.String("device_id", req.DeviceId), slog.Bool("deep", req.DeepScan))

	warnings, errors, score, stats := s.registry.RunDiagnostics(req.DeviceId, req.DeepScan)
	return &hardwarev1.RunDiagnosticsResponse{
		Meta:               metaOK(req.Meta.RequestId),
		DeviceId:           req.DeviceId,
		Warnings:           warnings,
		Errors:             errors,
		OverallHealthScore: score,
		SystemStats:        stats,
	}, nil
}

func (s *HardwareServer) ScanEnergySources(ctx context.Context, req *hardwarev1.ScanEnergySourcesRequest) (*hardwarev1.ScanEnergySourcesResponse, error) {
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	s.log.InfoContext(ctx, "ScanEnergySources", slog.String("location", req.Location), slog.Float64("radius_km", float64(req.ScanRadiusKm)))
	sources := telemetry.EnergySourceScan(req.Location, req.ScanRadiusKm)
	return &hardwarev1.ScanEnergySourcesResponse{Meta: metaOK(req.Meta.RequestId), Sources: sources}, nil
}

func (s *HardwareServer) StreamTelemetry(req *hardwarev1.StreamTelemetryRequest, stream hardwarev1.HardwareService_StreamTelemetryServer) error {
	if _, ok := s.registry.Get(req.DeviceId); !ok {
		return status.Errorf(codes.NotFound, "device %q not found", req.DeviceId)
	}
	ch := make(chan *hardwarev1.TelemetryReading, s.cfg.Telemetry.BufferSize)
	telemetry.Stream(req.DeviceId, s.cfg.Telemetry.StreamInterval, ch)

	for {
		select {
		case <-stream.Context().Done():
			return nil
		case reading := <-ch:
			if err := stream.Send(&hardwarev1.StreamTelemetryResponse{Reading: reading}); err != nil {
				return status.Errorf(codes.Internal, "stream send: %v", err)
			}
		}
	}
}

// SuitControlStream is a bidirectional stream: commands flow in, telemetry flows back.
func (s *HardwareServer) SuitControlStream(stream hardwarev1.HardwareService_SuitControlStreamServer) error {
	ctx := stream.Context()
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return status.Errorf(codes.Internal, "recv: %v", err)
		}
		s.log.InfoContext(ctx, "SuitControlStream: command received",
			slog.String("device_id", req.DeviceId), slog.String("command", req.Command))

		// Execute command and stream back a telemetry snapshot
		_, _ = s.registry.ExecuteCommand(req.DeviceId, req.Command, req.Params)
		reading := telemetry.Reading(req.DeviceId, "POWER")
		if err := stream.Send(&hardwarev1.SuitControlStreamResponse{Reading: reading}); err != nil {
			return status.Errorf(codes.Internal, "send: %v", err)
		}
	}
}

func validateMeta(meta *commonv1.RequestMeta) error {
	if meta == nil {
		return status.Error(codes.InvalidArgument, "meta is required")
	}
	if meta.RequestId == "" {
		return status.Error(codes.InvalidArgument, "meta.request_id is required")
	}
	return nil
}

func metaOK(requestID string) *commonv1.ResponseMeta {
	return &commonv1.ResponseMeta{RequestId: requestID, Success: true, Timestamp: timestamppb.Now()}
}
