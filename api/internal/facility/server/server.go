package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"go.opencensus.io/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1   "github.com/rkrimper1/jarvis/api/pb/common"
	facilityv1 "github.com/rkrimper1/jarvis/api/pb/facility"
	"github.com/rkrimper1/jarvis/api/internal/facility/alexa"
	"github.com/rkrimper1/jarvis/api/internal/facility/environment"
	"github.com/rkrimper1/jarvis/api/internal/facility/zone"
	"github.com/rkrimper1/jarvis/api/middleware"
)

type FacilityServer struct {
	facilityv1.UnimplementedFacilityServiceServer
	zones       *zone.Store
	mu          sync.RWMutex
	alexa       *alexa.Client // nil if ALEXA_COOKIES_PATH not set
	alexaDebug  bool          // controlled by ALEXA_DEBUG env var
	cookiesPath string
	log         *slog.Logger
}

func New(log *slog.Logger) *FacilityServer {
	return &FacilityServer{zones: zone.New(), log: log}
}

func NewWithAlexa(log *slog.Logger, client *alexa.Client, debug bool, cookiesPath string) *FacilityServer {
	return &FacilityServer{zones: zone.New(), alexa: client, alexaDebug: debug, cookiesPath: cookiesPath, log: log}
}

func (s *FacilityServer) getAlexaClient() *alexa.Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.alexa
}

func (s *FacilityServer) alexaRequired() error {
	if s.getAlexaClient() == nil {
		return status.Error(codes.FailedPrecondition, "alexa not configured (set ALEXA_COOKIES_PATH)")
	}
	return nil
}

// TextCommandHandler returns an HTTP handler for POST /alexa/text-command.
// Body: {"text":"<natural language command>"}
// The handler parses the text for a power action (on/off) and a device name,
// fuzzy-matches the name against valid GraphQL endpoints, then sends the
// command via SendCommand (same GraphQL path as the device control buttons).
func (s *FacilityServer) TextCommandHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ac := s.getAlexaClient()
		if ac == nil {
			http.Error(w, "alexa not configured", http.StatusPreconditionFailed)
			return
		}
		var req struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.Text == "" {
			http.Error(w, "text is required", http.StatusBadRequest)
			return
		}

		action, deviceHint := parseTextCommand(req.Text)
		if action == "" {
			http.Error(w, `could not determine action — try "turn on <device>" or "turn off <device>"`, http.StatusBadRequest)
			return
		}

		// Fetch smart home devices and validate via GraphQL (same path as ListAlexaDevices).
		smDevices, err := ac.ListSmartHomeDevices(r.Context())
		if err != nil {
			http.Error(w, "list devices: "+err.Error(), http.StatusInternalServerError)
			return
		}
		entityIDs := make([]string, 0, len(smDevices))
		smByID := make(map[string]alexa.SmartHomeDevice, len(smDevices))
		for _, d := range smDevices {
			entityIDs = append(entityIDs, d.ID)
			smByID[d.ID] = d
		}
		endpointStates, err := ac.BatchGetEndpointStates(r.Context(), entityIDs)
		if err != nil {
			http.Error(w, "get endpoint states: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Find best matching device by name (case-insensitive substring).
		matchID, matchName := matchDevice(deviceHint, smByID, endpointStates)
		if matchID == "" {
			http.Error(w, `no device matched "`+deviceHint+`" — use the exact device name from the Smart Home list`, http.StatusNotFound)
			return
		}

		s.log.Info("text command via GraphQL",
			slog.String("text", req.Text),
			slog.String("action", action),
			slog.String("device", matchName),
			slog.String("appliance_id", matchID),
		)
		if err := ac.SendCommand(r.Context(), matchID, alexa.CommandParams{Action: action}); err != nil {
			s.log.Warn("text command failed", slog.Any("err", err))
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "device": matchName, "action": action})
	}
}

// parseTextCommand extracts a power action and device name from a natural language
// command string. Handles patterns like "turn on office lights", "office lights on",
// "office lights turnOn", etc.
func parseTextCommand(text string) (action, deviceName string) {
	s := strings.TrimSpace(text)
	lower := strings.ToLower(s)

	// Check for action prefix ("turn on <device>", "switch off <device>").
	prefixes := []struct{ phrase, action string }{
		{"turn on the ", "turnOn"},
		{"turn on ", "turnOn"},
		{"switch on the ", "turnOn"},
		{"switch on ", "turnOn"},
		{"turn off the ", "turnOff"},
		{"turn off ", "turnOff"},
		{"switch off the ", "turnOff"},
		{"switch off ", "turnOff"},
	}
	for _, p := range prefixes {
		if strings.HasPrefix(lower, p.phrase) {
			return p.action, strings.TrimSpace(s[len(p.phrase):])
		}
	}

	// Check for action suffix ("office lights on", "office lights turnoff").
	suffixes := []struct{ phrase, action string }{
		{" turn on", "turnOn"},
		{" turn off", "turnOff"},
		{" turnon", "turnOn"},
		{" turnoff", "turnOff"},
		{" on", "turnOn"},
		{" off", "turnOff"},
	}
	for _, sf := range suffixes {
		if strings.HasSuffix(lower, sf.phrase) {
			return sf.action, strings.TrimSpace(s[:len(s)-len(sf.phrase)])
		}
	}

	return "", s
}

// matchDevice finds the best smart home device whose DisplayName contains
// deviceHint (case-insensitive), restricted to IDs present in endpointStates
// (i.e. valid GraphQL endpoints). Returns empty strings if no match.
func matchDevice(deviceHint string, smByID map[string]alexa.SmartHomeDevice, endpointStates map[string]alexa.EndpointState) (id, name string) {
	hint := strings.ToLower(strings.TrimSpace(deviceHint))
	if hint == "" {
		return "", ""
	}
	// Prefer exact match over substring match.
	for eid := range endpointStates {
		d := smByID[eid]
		if strings.ToLower(d.DisplayName) == hint {
			return eid, d.DisplayName
		}
	}
	for eid := range endpointStates {
		d := smByID[eid]
		nameLower := strings.ToLower(d.DisplayName)
		if strings.Contains(nameLower, hint) || strings.Contains(hint, nameLower) {
			return eid, d.DisplayName
		}
	}
	return "", ""
}

// CookieStatusHandler returns an HTTP handler for GET /alexa/cookie-status.
func (s *FacilityServer) CookieStatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if s.cookiesPath == "" {
			json.NewEncoder(w).Encode(map[string]any{"configured": false})
			return
		}
		expiry, err := alexa.CookiesExpiry(s.cookiesPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		daysLeft := 0
		expired := false
		if !expiry.IsZero() {
			d := expiry.Sub(time.Now())
			daysLeft = int(d.Hours() / 24)
			expired = d <= 0
		}
		json.NewEncoder(w).Encode(map[string]any{
			"configured":        true,
			"expires_at":        expiry.UTC().Format(time.RFC3339),
			"days_until_expiry": daysLeft,
			"expired":           expired,
		})
	}
}

// CookieUploadHandler returns an HTTP handler for POST /alexa/cookies.
// It accepts a Cookie-Editor JSON export, writes it to cookiesPath, and
// hot-reloads the Alexa client without a server restart.
func (s *FacilityServer) CookieUploadHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if s.cookiesPath == "" {
			http.Error(w, "alexa not configured", http.StatusPreconditionFailed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MiB max
		if err != nil {
			http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
			return
		}
		// Validate it parses as a cookie array before overwriting the file.
		var cookies []map[string]any
		if err := json.Unmarshal(body, &cookies); err != nil {
			http.Error(w, "invalid cookie JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := os.WriteFile(s.cookiesPath, body, 0600); err != nil {
			http.Error(w, "save cookies: "+err.Error(), http.StatusInternalServerError)
			return
		}
		newClient, err := alexa.New(r.Context(), s.cookiesPath, s.alexaDebug)
		if err != nil {
			http.Error(w, "reload client: "+err.Error(), http.StatusInternalServerError)
			return
		}
		s.mu.Lock()
		s.alexa = newClient
		s.mu.Unlock()
		s.log.Info("alexa cookies refreshed", slog.String("path", s.cookiesPath))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}
}

func (s *FacilityServer) ControlSystem(ctx context.Context, req *facilityv1.ControlSystemRequest) (*facilityv1.ControlSystemResponse, error) {
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "facility/ControlSystem")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

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
	ctx, span := trace.StartSpan(ctx, "facility/ManageAccess")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	s.log.InfoContext(ctx, "ManageAccess",
		slog.String("subject_id", req.SubjectId),
		slog.String("zone_id", req.ZoneId),
		slog.String("action", req.Action),
	)

	_, zoneExists := s.zones.Get(req.ZoneId)
	if !zoneExists {
		return nil, status.Errorf(codes.NotFound, "zone %q not found", req.ZoneId)
	}

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
	ctx, span := trace.StartSpan(ctx, "facility/GetEnvironmentReading")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

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

// ── Alexa ─────────────────────────────────────────────────────────────────────

func (s *FacilityServer) ListAlexaDevices(ctx context.Context, req *facilityv1.ListAlexaDevicesRequest) (*facilityv1.ListAlexaDevicesResponse, error) {
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	if err := s.alexaRequired(); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "facility/ListAlexaDevices")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	ac := s.getAlexaClient()

	var out []*facilityv1.AlexaDevice

	// Echo devices
	echoDevices, err := ac.ListDevices(ctx)
	if err != nil {
		s.log.WarnContext(ctx, "ListAlexaDevices: echo devices failed", slog.Any("err", err))
	}
	for _, d := range echoDevices {
		out = append(out, &facilityv1.AlexaDevice{
			SerialNumber: d.SerialNumber,
			Name:         d.AccountName,
			DeviceFamily: d.DeviceFamily,
			DeviceType:   d.DeviceType,
			Online:       d.Online,
			IsSmartHome:  false,
		})
	}

	// Smart home appliances — use behaviors/entities as the source of entity IDs,
	// then batch-validate via GraphQL (which filters out non-endpoint entities like
	// Cync virtual groups) and get real-time power state in the same request.
	smDevices, err := ac.ListSmartHomeDevices(ctx)
	if err != nil {
		s.log.WarnContext(ctx, "ListAlexaDevices: smart home devices failed", slog.Any("err", err))
	}

	// Collect entity IDs for batch GraphQL validation.
	entityIDs := make([]string, 0, len(smDevices))
	smByID := make(map[string]alexa.SmartHomeDevice, len(smDevices))
	for _, d := range smDevices {
		entityIDs = append(entityIDs, d.ID)
		smByID[d.ID] = d
	}

	// Batch-query GraphQL for power state; only valid endpoints are returned.
	endpointStates, err := ac.BatchGetEndpointStates(ctx, entityIDs)
	if err != nil {
		s.log.WarnContext(ctx, "ListAlexaDevices: endpoint states unavailable", slog.Any("err", err))
		endpointStates = map[string]alexa.EndpointState{}
	}

	for id, state := range endpointStates {
		d := smByID[id]
		ps := ""
		if state.PowerOn {
			ps = "ON"
		} else {
			ps = "OFF"
		}
		out = append(out, &facilityv1.AlexaDevice{
			Name:         d.DisplayName,
			ApplianceId:  d.ID,
			DeviceType:   d.Description,
			IsSmartHome:  true,
			Online:       state.Online,
			Capabilities: capabilityTypes(d.SupportedProperties),
			PowerState:   ps,
		})
	}

	span.AddAttributes(trace.Int64Attribute("device_count", int64(len(out))))
	return &facilityv1.ListAlexaDevicesResponse{
		Meta:    metaOK(req.Meta.RequestId),
		Devices: out,
	}, nil
}

func (s *FacilityServer) SendAlexaCommand(ctx context.Context, req *facilityv1.SendAlexaCommandRequest) (*facilityv1.SendAlexaCommandResponse, error) {
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	if err := s.alexaRequired(); err != nil {
		return nil, err
	}
	if req.ApplianceId == "" {
		return nil, status.Error(codes.InvalidArgument, "appliance_id is required")
	}
	if req.Action == "" {
		return nil, status.Error(codes.InvalidArgument, "action is required")
	}

	ctx, span := trace.StartSpan(ctx, "facility/SendAlexaCommand")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())
	span.AddAttributes(
		trace.StringAttribute("appliance_id", req.ApplianceId),
		trace.StringAttribute("action", req.Action),
	)

	s.log.InfoContext(ctx, "SendAlexaCommand",
		slog.String("appliance_id", req.ApplianceId),
		slog.String("action", req.Action),
	)

	if err := s.getAlexaClient().SendCommand(ctx, req.ApplianceId, alexa.CommandParams{
		Action:     req.Action,
		Parameters: req.Parameters,
	}); err != nil {
		return nil, status.Errorf(codes.Internal, "alexa command: %v", err)
	}

	return &facilityv1.SendAlexaCommandResponse{Meta: metaOK(req.Meta.RequestId)}, nil
}

// capabilityTypes derives a human-readable capability list from the raw
// supportedProperties strings returned by the Alexa behaviors/entities API.
// Properties arrive both as short tokens ("setBrightness") and long-form
// controller strings ("Alexa.Operation.ConnectedDevice#Alexa.LockController#3#Lock"),
// so we check both exact match and substring.
func capabilityTypes(props []string) []string {
	var light, thermostat, lock, color bool
	for _, p := range props {
		switch {
		case p == "setBrightness" || p == "rampBrightness" || p == "adjustBrightness" ||
			strings.Contains(p, "BrightnessController"):
			light = true
		case p == "setTargetTemperature" || p == "setThermostatMode" ||
			strings.Contains(p, "ThermostatController"):
			thermostat = true
		case p == "lock" || p == "unlock" || p == "lockAction" || p == "unlockAction" ||
			strings.Contains(p, "LockController"):
			lock = true
		case p == "setColor" || p == "setColorTemperature" ||
			strings.Contains(p, "ColorController"):
			color = true
		}
	}
	switch {
	case thermostat:
		return []string{"THERMOSTAT"}
	case lock:
		return []string{"LOCK"}
	case color:
		return []string{"LIGHT", "COLOR"}
	case light:
		return []string{"LIGHT"}
	default:
		return []string{"SWITCH"}
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

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
