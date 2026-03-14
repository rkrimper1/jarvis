package server

import (
	"context"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	businessv1 "github.com/rkrimper1/jarvis/api/pb/business"
	commonv1 "github.com/rkrimper1/jarvis/api/pb/common"
	"github.com/rkrimper1/jarvis/api/internal/business-ops/calendar"
	"github.com/rkrimper1/jarvis/api/internal/business-ops/messaging"
	"github.com/rkrimper1/jarvis/api/internal/business-ops/reports"
	"github.com/rkrimper1/jarvis/api/internal/business-ops/tasks"
)

// BusinessOpsServer implements businessv1.BusinessOpsServiceServer.
type BusinessOpsServer struct {
	businessv1.UnimplementedBusinessOpsServiceServer
	cal      *calendar.Store
	tasks    *tasks.Store
	router   *messaging.Router
	log      *slog.Logger
}

// New wires all dependencies.
func New(log *slog.Logger) *BusinessOpsServer {
	return &BusinessOpsServer{
		cal:    calendar.New(),
		tasks:  tasks.New(),
		router: messaging.New(log),
		log:    log,
	}
}

func (s *BusinessOpsServer) ScheduleEvent(ctx context.Context, req *businessv1.ScheduleEventRequest) (*businessv1.ScheduleEventResponse, error) {
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	if req.Title == "" {
		return nil, status.Error(codes.InvalidArgument, "title is required")
	}

	var start, end interface{ AsTime() interface{ IsZero() bool } }
	_ = start
	_ = end

	startTime := req.GetStart().AsTime()
	endTime := req.GetEnd().AsTime()

	s.log.InfoContext(ctx, "ScheduleEvent", slog.String("title", req.Title), slog.Bool("high_priority", req.HighPriority))

	id, conflicts, autoResolved := s.cal.Schedule(
		req.Title, req.Description, req.Location,
		req.Attendees, startTime, endTime, req.HighPriority,
	)

	return &businessv1.ScheduleEventResponse{
		Meta:         metaOK(req.Meta.RequestId),
		EventId:      id,
		Conflicts:    conflicts,
		AutoResolved: autoResolved,
	}, nil
}

func (s *BusinessOpsServer) GetSchedule(ctx context.Context, req *businessv1.GetScheduleRequest) (*businessv1.GetScheduleResponse, error) {
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	s.log.InfoContext(ctx, "GetSchedule", slog.String("subject_id", req.SubjectId))

	events := s.cal.List(req.SubjectId, req.GetFrom().AsTime(), req.GetTo().AsTime())
	return &businessv1.GetScheduleResponse{Meta: metaOK(req.Meta.RequestId), Events: events}, nil
}

func (s *BusinessOpsServer) CreateTask(ctx context.Context, req *businessv1.CreateTaskRequest) (*businessv1.CreateTaskResponse, error) {
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	if req.Title == "" {
		return nil, status.Error(codes.InvalidArgument, "title is required")
	}
	s.log.InfoContext(ctx, "CreateTask", slog.String("title", req.Title), slog.String("assignee", req.AssigneeId))

	id := s.tasks.Create(req.Title, req.Description, req.AssigneeId, req.GetDue().AsTime(), req.Priority)
	return &businessv1.CreateTaskResponse{
		Meta:   metaOK(req.Meta.RequestId),
		TaskId: id,
		Status: businessv1.TaskStatus_TASK_STATUS_PENDING,
	}, nil
}

func (s *BusinessOpsServer) SendMessage(ctx context.Context, req *businessv1.SendMessageRequest) (*businessv1.SendMessageResponse, error) {
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	if len(req.Recipients) == 0 {
		return nil, status.Error(codes.InvalidArgument, "at least one recipient required")
	}
	if req.Body == "" {
		return nil, status.Error(codes.InvalidArgument, "body is required")
	}
	s.log.InfoContext(ctx, "SendMessage",
		slog.String("channel", req.Channel.String()),
		slog.Int("recipients", len(req.Recipients)),
		slog.Bool("encrypt", req.Encrypt),
	)

	msgID, delivered, failed := s.router.Send(req.Recipients, req.Channel, req.Subject, req.Body, req.Encrypt)
	return &businessv1.SendMessageResponse{
		Meta:        metaOK(req.Meta.RequestId),
		MessageId:   msgID,
		DeliveredTo: delivered,
		FailedTo:    failed,
	}, nil
}

func (s *BusinessOpsServer) GenerateReport(ctx context.Context, req *businessv1.GenerateReportRequest) (*businessv1.GenerateReportResponse, error) {
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	if req.ReportType == "" {
		return nil, status.Error(codes.InvalidArgument, "report_type is required")
	}
	s.log.InfoContext(ctx, "GenerateReport", slog.String("type", req.ReportType))

	result := reports.Generate(req.ReportType, req.GetFrom().AsTime(), req.GetTo().AsTime(), req.Filters)
	return &businessv1.GenerateReportResponse{
		Meta:       metaOK(req.Meta.RequestId),
		ReportId:   result.ID,
		Summary:    result.Summary,
		ReportData: result.Data,
		Format:     result.Format,
	}, nil
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
