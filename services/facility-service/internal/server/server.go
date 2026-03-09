package server

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/rkrimper1/jarvis/gen/common"
	facilityv1 "github.com/rkrimper1/jarvis/gen/facility"
	"github.com/rkrimper1/jarvis/services/facility-service/internal/environment"
	"github.com/rkrimper1/jarvis/services/facility-service/internal/zone"
)

type FacilityServer struct {
	facilityv1.UnimplementedFacilityServiceServer
	zones *zone.Store
	log   *slog.Logger
}

func New(log *slog.Logger) *FacilityServer {
	return &FacilityServer{zones: zone.New(), log: log}
}

func (s *FacilityServer) ControlSystem(ctx context.Context, req *facilityv1.ControlSystemRequest) (*facilityv1.ControlSystemResponse, error) {
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	s.log.InfoContext(ctx, "ControlSystem",
		slog.String("zone_id", req.ZoneId),
		slog.String("system", req.System.String()),
		slog.String("command", req.Command),
	)
	newState, effects, err := s.zones.ControlSystem(req.ZoneId, req.System, req.Command, req.Settings)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%v", err)
	}
	return &facilityv1.ControlSystemResponse{
		Meta:        metaOK(req.Meta.RequestId),
		ZoneId:      req.ZoneId,
		System:      req.System,
		NewState:    newState,
		SideEffects: effects,
	}, nil
}

func (s *FacilityServer) ManageAccess(ctx context.Context, req *facilityv1.ManageAccessRequest) (*facilityv1.ManageAccessResponse, error) {
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	s.log.InfoContext(ctx, "ManageAccess",
		slog.String("subject_id", req.SubjectId),
		slog.String("zone_id", req.ZoneId),
		slog.String("action", req.Action),
	)

	_, zoneExists := s.zones.Get(req.ZoneId)
	if !zoneExists {
		return nil, status.Errorf(codes.NotFound, "zone %q not found", req.ZoneId)
	}

	// Simplified access policy: only tony-stark has admin access everywhere
	granted := req.SubjectId == "tony-stark" || req.Action == "QUERY"
	reason := "access policy check passed"
	if !granted {
		reason = "subject does not have clearance for zone " + req.ZoneId
	}

	return &facilityv1.ManageAccessResponse{
		Meta:          metaOK(req.Meta.RequestId),
		AccessGranted: granted,
		Reason:        reason,
		ValidUntil:    timestamppb.New(time.Now().Add(8 * time.Hour)),
	}, nil
}

func (s *FacilityServer) GetEnvironmentReading(ctx context.Context, req *facilityv1.GetEnvironmentReadingRequest) (*facilityv1.GetEnvironmentReadingResponse, error) {
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	return &facilityv1.GetEnvironmentReadingResponse{
		Meta:    metaOK(req.Meta.RequestId),
		Reading: environment.Reading(req.ZoneId),
	}, nil
}

func (s *FacilityServer) StreamEnvironment(req *facilityv1.StreamEnvironmentRequest, stream facilityv1.FacilityService_StreamEnvironmentServer) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stream.Context().Done():
			return nil
		case <-ticker.C:
			reading := environment.Reading(req.ZoneId)
			if err := stream.Send(&facilityv1.StreamEnvironmentResponse{Reading: reading}); err != nil {
				return status.Errorf(codes.Internal, "stream send: %v", err)
			}
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
