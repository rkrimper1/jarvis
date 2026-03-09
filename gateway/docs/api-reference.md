# JARVIS REST API Reference

Base URL: `http://localhost:8080` (via Envoy)
Direct gateway: `http://localhost:8081` (bypass Envoy — dev only)

---

## Authentication

All endpoints except `/healthz` and `/v1/security/authenticate` require a Bearer token.

```bash
# 1. Get a token
TOKEN=$(curl -s -X POST http://localhost:8080/v1/security/authenticate \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "auth-001"},
    "subject_id": "tony-stark",
    "method": "AUTH_METHOD_TOKEN",
    "credential_payload": ""
  }' | jq -r '.accessToken')

# 2. Use the token
curl http://localhost:8080/v1/agents/status \
  -H "Authorization: Bearer $TOKEN"
```

---

## Health Check

```bash
curl http://localhost:8080/healthz
# {"status":"ok","service":"jarvis-gateway"}
```

---

## NLP Service

### Parse Intent
```bash
curl -X POST http://localhost:8080/v1/nlp/parse \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "nlp-001"},
    "raw_text": "JARVIS, run diagnostics on the Mark VII suit",
    "language_code": "en-US",
    "session_id": "session-tony-001"
  }'
```

### Dialogue Turn
```bash
curl -X POST http://localhost:8080/v1/nlp/dialogue \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "dlg-001"},
    "session_id": "session-tony-001",
    "utterance": "What is the current threat level?"
  }'
```

---

## Security Service

### Authenticate
```bash
curl -X POST http://localhost:8080/v1/security/authenticate \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "sec-auth-001"},
    "subject_id": "tony-stark",
    "method": "AUTH_METHOD_TOKEN"
  }'
```

### Assess Threat
```bash
curl -X POST http://localhost:8080/v1/security/threat \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "threat-001"},
    "subject_id": "ivan-vanko",
    "location": "monaco-circuit",
    "observed_signals": ["energy_signature", "weapons_detected", "criminal_record"]
  }'
```

### Execute Protocol
```bash
curl -X POST http://localhost:8080/v1/security/protocol \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "proto-001"},
    "protocol": "PROTOCOL_TYPE_LOCKDOWN",
    "reason": "intruder detected",
    "requires_confirmation": false
  }'
```

### Audit Log
```bash
curl "http://localhost:8080/v1/security/audit?page_size=20" \
  -H "Authorization: Bearer $TOKEN"
```

---

## Agent Coordinator

### Get Agent Status
```bash
curl http://localhost:8080/v1/agents/status \
  -H "Authorization: Bearer $TOKEN"
```

### Dispatch Task
```bash
curl -X POST http://localhost:8080/v1/agents/dispatch \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "dispatch-001"},
    "task_description": "Perimeter patrol — sector 7",
    "priority": "TASK_PRIORITY_HIGH",
    "target_agent_ids": ["drone-01", "drone-02"]
  }'
```

### Broadcast
```bash
curl -X POST http://localhost:8080/v1/agents/broadcast \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "broadcast-001"},
    "message": "Return to base immediately",
    "priority": "TASK_PRIORITY_CRITICAL"
  }'
```

---

## Hardware Service

### Send Command
```bash
curl -X POST http://localhost:8080/v1/hardware/mark-vii-suit/command \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "hw-001"},
    "device_id": "mark-vii-suit",
    "command": "POWER_ON"
  }'
```

### Run Diagnostics
```bash
curl "http://localhost:8080/v1/hardware/arc-reactor-primary/diagnostics?deep_scan=true" \
  -H "Authorization: Bearer $TOKEN"
```

### Scan Energy Sources
```bash
curl "http://localhost:8080/v1/hardware/energy/scan?location=malibu&scan_radius_km=50" \
  -H "Authorization: Bearer $TOKEN"
```

---

## Facility Service

### Control System
```bash
curl -X POST http://localhost:8080/v1/facility/zones/workshop/system \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "fac-001"},
    "zone_id": "workshop",
    "system": "SYSTEM_TYPE_LIGHTING",
    "command": "SET",
    "settings": {"brightness": "80", "color_temp": "warm"}
  }'
```

### Manage Access
```bash
curl -X POST http://localhost:8080/v1/facility/zones/server-room/access \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "access-001"},
    "zone_id": "server-room",
    "subject_id": "happy-hogan",
    "action": "GRANT"
  }'
```

### Get Environment Reading
```bash
curl http://localhost:8080/v1/facility/zones/lab-01/environment \
  -H "Authorization: Bearer $TOKEN"
```

---

## Intelligence Service

### Query Intel
```bash
curl -X POST http://localhost:8080/v1/intel/query \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "intel-001"},
    "query": "ivan-vanko",
    "subject_type": "SUBJECT_TYPE_PERSON",
    "depth": "ANALYSIS_DEPTH_DEEP",
    "data_sources": ["SHIELD", "STARK_DB"]
  }'
```

### Analyze Artifact
```bash
curl -X POST http://localhost:8080/v1/intel/artifact \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "artifact-001"},
    "artifact_id": "unknown-device-x7",
    "artifact_description": "unknown origin weapon device recovered from Monaco"
  }'
```

### Cross Reference
```bash
curl -X POST http://localhost:8080/v1/intel/crossref \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "crossref-001"},
    "subject_ids": ["ivan-vanko", "hammer-industries"],
    "relationship_hint": "allied"
  }'
```

---

## Business Ops Service

### Schedule Event
```bash
curl -X POST http://localhost:8080/v1/business/schedule \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "sched-001"},
    "title": "Stark Expo Press Conference",
    "attendees": ["tony-stark", "pepper-potts", "happy-hogan"],
    "location": "stark-expo-pavilion",
    "high_priority": true
  }'
```

### Get Schedule
```bash
curl http://localhost:8080/v1/business/schedule/tony-stark \
  -H "Authorization: Bearer $TOKEN"
```

### Create Task
```bash
curl -X POST http://localhost:8080/v1/business/tasks \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "task-001"},
    "title": "Review arc reactor upgrade specs",
    "assignee_id": "tony-stark",
    "priority": 5
  }'
```

### Send Message
```bash
curl -X POST http://localhost:8080/v1/business/messages \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "msg-001"},
    "recipients": ["pepper-potts"],
    "channel": "MESSAGE_CHANNEL_SECURE",
    "subject": "Urgent: Board meeting rescheduled",
    "body": "Please move the Q4 review to 1400 tomorrow.",
    "encrypt": true
  }'
```

### Generate Report
```bash
curl -X POST http://localhost:8080/v1/business/reports \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "report-001"},
    "report_type": "THREAT_SUMMARY"
  }'
```

---

## Learning Service

### Submit Feedback
```bash
curl -X POST http://localhost:8080/v1/learning/feedback \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "meta": {"request_id": "fb-001"},
    "interaction_id": "dlg-001",
    "feedback_type": "FEEDBACK_TYPE_CORRECTION",
    "correction": "The threat level was HIGH not MODERATE",
    "rating": 0.3
  }'
```

### Get Behavior Profile
```bash
curl http://localhost:8080/v1/learning/profile/tony-stark \
  -H "Authorization: Bearer $TOKEN"
```

### Get Model Performance
```bash
curl "http://localhost:8080/v1/learning/performance?domain=MODEL_DOMAIN_NLP" \
  -H "Authorization: Bearer $TOKEN"
```

---

## gRPC Direct Access (via Envoy :9090)

```bash
# List all services
grpcurl -plaintext localhost:9090 list

# Call NLP directly
grpcurl -plaintext -d '{
  "meta": {"request_id": "grpc-001"},
  "raw_text": "Power up the Mark VII",
  "session_id": "session-001"
}' localhost:9090 jarvis.nlp.NLPService/ParseIntent

# Stream coordination events
grpcurl -plaintext -d '{
  "meta": {"request_id": "stream-001"}
}' localhost:9090 jarvis.agent.AgentCoordinatorService/StreamCoordinationEvents
```

---

## Architecture Diagram

```
                        ┌─────────────────────────────────────────┐
                        │           Client Applications           │
                        │   (HUD / Mobile App / Voice Interface)  │
                        └──────────────┬──────────────────────────┘
                                       │
                          ┌────────────▼────────────┐
                          │         Envoy           │
                          │   :8080 REST  :9090 gRPC│
                          └─────┬────────────┬──────┘
                                │            │
                     ┌──────────▼──┐    Direct gRPC
                     │ Go Gateway  │    passthrough
                     │ grpc-gateway│         │
                     │ :8080       │         │
                     └──────┬──────┘         │
                            │                │
          ┌─────────────────▼────────────────▼──────────────────┐
          │                    gRPC Services                     │
          │                                                      │
          │  nlp:50051   security:50052  agent:50053             │
          │  hardware:50054  facility:50055  intel:50056         │
          │  business:50057  learning:50058  voice:50059         │
          └──────────────────────────────────────────────────────┘
```
